// Package transport reads the companion mod's JSONL events file and delivers complete
// lines to the bridge. The read mechanism is pluggable (Local file, SFTP); the polling,
// offset tracking, truncation detection, and line buffering are shared.
package transport

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"log"
	"time"
)

// fingerprintLen is how many bytes from the start of the file we remember to detect
// truncation/rotation (see truncated). Small enough to be a cheap extra read (SFTP is a
// network round trip per read), large enough that two unrelated files landing on the same
// prefix is not a practical concern for a JSONL events file.
const fingerprintLen = 64

// reader abstracts "how to read the events file". Implementations handle their own
// connection lifecycle (e.g. SFTP reconnect) and should return an error — never panic —
// when the source is temporarily unavailable. A "file does not exist" condition must be
// reported as an error satisfying errors.Is(err, fs.ErrNotExist); this is a normal,
// expected state (e.g. the server hasn't started yet) and must not be treated the same as
// a connection failure.
type reader interface {
	// Stat returns the current file size, used to detect growth and truncation.
	Stat() (int64, error)
	// Read returns the bytes from offset to the current end of file.
	Read(offset int64) ([]byte, error)
	// Head returns up to n bytes from the start of the file, fewer if the file is
	// shorter. Used to fingerprint the file's identity so truncation/rotation can be
	// detected even when the new file's size races past the old read offset before the
	// next poll (a plain size comparison would miss that).
	Head(n int64) ([]byte, error)
	// Close releases any underlying connection.
	Close() error
}

// Tailer polls a reader and emits each complete JSONL line via onLine. It starts at the
// current end of file the first time it observes the file existing (so neither a fresh
// start nor a reconnect to a file that already has history replays it), and re-reads from
// byte 0 when the file is truncated or replaced (the mod does this once per game session,
// and it's also what a reconnecting SFTP transport may see if the remote file rotated
// while disconnected).
type Tailer struct {
	reader   reader
	interval time.Duration
}

// tailState is the mutable state polled and updated on each tick. It's a separate type
// (rather than fields directly on Tailer) so tests can drive poll() in isolation without
// spinning up the ticker/context machinery in Run.
type tailState struct {
	offset      int64
	haveOffset  bool   // true once we've established a starting offset for the file
	fingerprint []byte // first fingerprintLen bytes of the file at offset 0, for truncation detection
	buf         []byte // partial (not yet newline-terminated) trailing line
}

func (t *Tailer) Run(ctx context.Context, onLine func([]byte)) {
	defer t.reader.Close()

	var st tailState
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()

	// Poll once immediately so a short-lived process (or a test) doesn't wait a full
	// interval to establish its starting offset.
	poll(t.reader, &st, onLine)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			poll(t.reader, &st, onLine)
		}
	}
}

// poll runs one tailing cycle: stat the file, detect truncation/rotation, read and emit
// any new lines. It never tears down the reader's connection itself — that's the reader
// implementation's job, and only for genuine connection failures (see the sftp reader,
// which distinguishes "file not found" from connection errors and only reconnects on the
// latter).
func poll(r reader, st *tailState, onLine func([]byte)) {
	sz, err := r.Stat()
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			// Normal, expected: the file hasn't been created yet (server not started,
			// mod hasn't written its first event, or — for SFTP — the file briefly
			// absent across a session boundary). Keep waiting; do NOT touch offset
			// tracking, and do not treat this as a connection problem.
			return
		}
		log.Printf("transport: stat error (will retry): %v", err)
		return
	}

	if !st.haveOffset {
		// First time we see the file (process start, or first successful stat after a
		// prior "not found"). Seek to EOF: only relay events written from here on.
		// Critically, this must NOT be 0 — starting at 0 on a file that already has a
		// full session's history (e.g. after an SFTP reconnect) would replay it all,
		// flooding Discord and re-processing stale odb.link_confirmed/removed events.
		st.offset = sz
		st.haveOffset = true
		st.fingerprint = headFingerprint(r, sz)
		return
	}

	if truncated(r, st, sz) {
		st.offset = 0
		st.buf = st.buf[:0]
		st.fingerprint = headFingerprint(r, sz)
	}

	if sz == st.offset {
		return
	}
	data, err := r.Read(st.offset)
	if err != nil {
		log.Printf("transport: read error: %v", err)
		return
	}
	st.offset += int64(len(data))
	st.buf = emitLines(append(st.buf, data...), onLine)
}

// truncated reports whether the file appears to have been truncated or replaced since the
// last poll. A shrunk size is the obvious signal, but relying on size alone misses the
// race where a rotated file already grew past the old offset again before the next poll
// (e.g. old offset 5000, file rotates and is rewritten past 5000 bytes within one poll
// interval) — so once there's any new data to read, we also compare a fingerprint of the
// file's first bytes against what we saw when the current offset was established. A
// changed fingerprint means this isn't the same file content anymore, regardless of size.
func truncated(r reader, st *tailState, sz int64) bool {
	if sz < st.offset {
		return true
	}
	if sz == st.offset || len(st.fingerprint) == 0 {
		return false
	}
	fp, err := r.Head(int64(len(st.fingerprint)))
	if err != nil {
		// Transient read error; don't force a truncation off the back of it, the next
		// poll will try again.
		return false
	}
	return !bytes.Equal(fp, st.fingerprint)
}

func headFingerprint(r reader, sz int64) []byte {
	if sz == 0 {
		return nil
	}
	fp, err := r.Head(fingerprintLen)
	if err != nil {
		return nil
	}
	return fp
}

// emitLines emits each complete (newline-terminated) line in buf and returns the
// remaining partial line.
func emitLines(buf []byte, onLine func([]byte)) []byte {
	for {
		i := bytes.IndexByte(buf, '\n')
		if i < 0 {
			break
		}
		line := bytes.TrimSpace(buf[:i])
		rest := buf[i+1:]
		if len(line) > 0 {
			cp := make([]byte, len(line))
			copy(cp, line)
			onLine(cp)
		}
		buf = append(buf[:0], rest...)
	}
	return buf
}
