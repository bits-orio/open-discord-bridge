package transport

import (
	"bytes"
	"context"
	"io"
	"log"
	"os"
	"time"
)

// Local tails the JSONL events file on the same host by polling for new bytes. It
// starts at the current end of file (so a bridge restart does not replay history) and
// detects truncation (the mod overwrites the file once per game session) by a shrink
// in file size, resetting its read offset.
type Local struct {
	path     string
	interval time.Duration
	offset   int64
	buf      []byte
}

func NewLocal(path string, interval time.Duration) *Local {
	return &Local{path: path, interval: interval}
}

// Run polls until ctx is cancelled, invoking onLine for each complete JSONL line.
func (l *Local) Run(ctx context.Context, onLine func([]byte)) {
	if fi, err := os.Stat(l.path); err == nil {
		l.offset = fi.Size()
	}

	ticker := time.NewTicker(l.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.poll(onLine)
		}
	}
}

func (l *Local) poll(onLine func([]byte)) {
	fi, err := os.Stat(l.path)
	if err != nil {
		return // file not present yet; the mod creates it on first event
	}
	size := fi.Size()
	if size < l.offset {
		// Truncation: new game session started, file was overwritten.
		l.offset = 0
		l.buf = l.buf[:0]
	}
	if size == l.offset {
		return
	}

	f, err := os.Open(l.path)
	if err != nil {
		return
	}
	defer f.Close()

	if _, err := f.Seek(l.offset, io.SeekStart); err != nil {
		return
	}
	data, err := io.ReadAll(f)
	if err != nil {
		log.Printf("transport: read error: %v", err)
		return
	}
	l.offset += int64(len(data))
	l.buf = append(l.buf, data...)

	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			break
		}
		line := l.buf[:i]
		rest := l.buf[i+1:]
		if t := bytes.TrimSpace(line); len(t) > 0 {
			cp := make([]byte, len(t))
			copy(cp, t)
			onLine(cp)
		}
		l.buf = append(l.buf[:0], rest...)
	}
}
