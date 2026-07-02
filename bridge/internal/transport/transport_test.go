package transport

import (
	"bytes"
	"errors"
	"io/fs"
	"log"
	"reflect"
	"testing"
)

var errConnectionLost = errors.New("connection lost")

func collect(t *testing.T, chunks ...string) [][]byte {
	t.Helper()
	var got [][]byte
	var buf []byte
	for _, c := range chunks {
		buf = emitLines(append(buf, c...), func(line []byte) {
			got = append(got, append([]byte(nil), line...))
		})
	}
	return got
}

func TestEmitLinesSplitsAndBuffers(t *testing.T) {
	// A line split across two reads should emit once, fully assembled.
	got := collect(t, `{"a":1}`+"\n"+`{"b":`, `2}`+"\n")
	want := [][]byte{[]byte(`{"a":1}`), []byte(`{"b":2}`)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEmitLinesSkipsBlank(t *testing.T) {
	got := collect(t, "\n\n"+`{"x":1}`+"\n")
	if len(got) != 1 || string(got[0]) != `{"x":1}` {
		t.Fatalf("got %q", got)
	}
}

// fakeReader is a fully in-memory reader implementation for driving poll() directly in
// tests, without a real filesystem or SFTP server.
type fakeReader struct {
	content  []byte
	notExist bool  // Stat/Read/Head report a "file does not exist" error
	statErr  error // arbitrary non-notExist error for Stat, to test the "genuine error" path
	closed   bool
}

func (f *fakeReader) Stat() (int64, error) {
	if f.notExist {
		return 0, &fs.PathError{Op: "stat", Path: "fake", Err: fs.ErrNotExist}
	}
	if f.statErr != nil {
		return 0, f.statErr
	}
	return int64(len(f.content)), nil
}

func (f *fakeReader) Read(offset int64) ([]byte, error) {
	if offset < 0 || offset > int64(len(f.content)) {
		offset = int64(len(f.content))
	}
	return append([]byte(nil), f.content[offset:]...), nil
}

func (f *fakeReader) Head(n int64) ([]byte, error) {
	if n > int64(len(f.content)) {
		n = int64(len(f.content))
	}
	return append([]byte(nil), f.content[:n]...), nil
}

func (f *fakeReader) Close() error { f.closed = true; return nil }

func runPoll(r reader, st *tailState) [][]byte {
	var got [][]byte
	poll(r, st, func(line []byte) {
		got = append(got, append([]byte(nil), line...))
	})
	return got
}

// A file that already has history when first observed (e.g. an SFTP reconnect to a file
// that was written to while disconnected) must not be replayed from byte 0 — only lines
// written after the tailer starts watching should ever be emitted.
func TestPollFirstObservationSeeksToEOF(t *testing.T) {
	r := &fakeReader{content: []byte(`{"a":1}` + "\n" + `{"b":2}` + "\n")}
	var st tailState

	if got := runPoll(r, &st); got != nil {
		t.Fatalf("first observation replayed history: %q", got)
	}
	if !st.haveOffset || st.offset != int64(len(r.content)) {
		t.Fatalf("offset not seeked to EOF: haveOffset=%v offset=%d want=%d", st.haveOffset, st.offset, len(r.content))
	}

	// Only newly appended data should be emitted from here on.
	r.content = append(r.content, []byte(`{"c":3}`+"\n")...)
	got := runPoll(r, &st)
	want := [][]byte{[]byte(`{"c":3}`)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

// A file that doesn't exist yet (server not started, mod hasn't written its first event)
// must not be treated as an established (empty) file, and must not produce a hard error —
// it's a normal, expected, retryable condition.
func TestPollNotExistDoesNotEstablishOffset(t *testing.T) {
	r := &fakeReader{notExist: true}
	var st tailState

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	defer log.SetOutput(nil)

	for i := 0; i < 3; i++ {
		if got := runPoll(r, &st); got != nil {
			t.Fatalf("poll %d emitted lines for a nonexistent file: %q", i, got)
		}
	}
	if st.haveOffset {
		t.Fatal("haveOffset should remain false while the file doesn't exist")
	}
	if logBuf.Len() != 0 {
		t.Fatalf("not-exist should not log a stat-error line, got: %q", logBuf.String())
	}

	// Once the file appears, we must seek to its current EOF (its existing content),
	// not replay from 0.
	r.notExist = false
	r.content = []byte(`{"late":1}` + "\n" + `{"later":2}` + "\n")
	if got := runPoll(r, &st); got != nil {
		t.Fatalf("first successful stat after not-exist replayed history: %q", got)
	}
	if !st.haveOffset || st.offset != int64(len(r.content)) {
		t.Fatalf("offset not seeked to EOF after recovery: haveOffset=%v offset=%d want=%d", st.haveOffset, st.offset, len(r.content))
	}
}

// A genuine connection-style error (as opposed to "file not found") should still be
// logged, since it's actionable/unexpected — this is what a caller like the SFTP reader
// relies on to know something needs attention, vs. the file simply not being there yet.
func TestPollGenuineStatErrorIsLogged(t *testing.T) {
	r := &fakeReader{statErr: errConnectionLost}
	var st tailState

	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	defer log.SetOutput(nil)

	runPoll(r, &st)
	if st.haveOffset {
		t.Fatal("haveOffset should remain false after a stat error")
	}
	if logBuf.Len() == 0 {
		t.Fatal("genuine stat error should be logged")
	}
}

// Truncation via a plain size shrink (the mod starting a new game session, the common
// case) resets the offset and re-reads from 0.
func TestPollTruncationSizeShrink(t *testing.T) {
	r := &fakeReader{content: []byte(`{"old-session-data":1}` + "\n")}
	var st tailState
	runPoll(r, &st) // establish at EOF

	r.content = []byte(`{"new":1}` + "\n") // shorter than the old content: a plain shrink
	got := runPoll(r, &st)
	want := [][]byte{[]byte(`{"new":1}`)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if st.offset != int64(len(r.content)) {
		t.Fatalf("offset after truncation = %d, want %d", st.offset, len(r.content))
	}
}

// Truncation where the replacement file already grew past the old offset before the next
// poll (e.g. old offset 5000, file rotates and is rewritten past 5000 bytes within one
// poll interval) is invisible to a size-only check (size only ever looks like it grew).
// The content fingerprint must catch it.
func TestPollTruncationDetectedDespiteSizeGrowth(t *testing.T) {
	oldContent := bytes.Repeat([]byte("a"), 20) // ends mid-line on purpose (no trailing \n)
	r := &fakeReader{content: oldContent}
	var st tailState
	runPoll(r, &st) // establish offset = 20, fingerprint = 20 "a"s

	// Rotated file: different content, but longer than the old offset, so `sz < offset`
	// never fires.
	newContent := append([]byte(`{"fresh":1}`+"\n"), bytes.Repeat([]byte("b"), 20)...)
	if len(newContent) <= len(oldContent) {
		t.Fatalf("test setup: new content must be longer than old offset")
	}
	r.content = newContent

	got := runPoll(r, &st)
	want := [][]byte{[]byte(`{"fresh":1}`)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("fingerprint-based truncation not detected: got %q, want %q", got, want)
	}
}

// When truncation is detected, any buffered partial (not-yet-newline-terminated) line
// from the old file incarnation must be dropped — otherwise it gets incorrectly prepended
// to the first line read from the new file, corrupting it.
func TestPollTruncationClearsPartialLineBuffer(t *testing.T) {
	r := &fakeReader{content: []byte(`{"complete":1}` + "\n")}
	var st tailState
	runPoll(r, &st) // establish at EOF (nothing emitted, nothing buffered yet)

	// Write (in two steps, like a real in-progress append) an incomplete trailing line —
	// this must accumulate in the partial-line buffer since it has no newline yet.
	r.content = append(r.content, []byte(`{"partial":`)...)
	if got := runPoll(r, &st); got != nil {
		t.Fatalf("unexpected emit before newline: %q", got)
	}
	r.content = append(r.content, []byte(`2}`)...) // still no trailing \n: {"partial":2}
	if got := runPoll(r, &st); got != nil {
		t.Fatalf("unexpected emit before newline: %q", got)
	}
	if string(st.buf) != `{"partial":2}` {
		t.Fatalf("expected a buffered partial line, got %q", st.buf)
	}

	// Now the file is truncated/rotated with unrelated new content.
	r.content = []byte(`{"unrelated":9}` + "\n")
	got := runPoll(r, &st)
	want := [][]byte{[]byte(`{"unrelated":9}`)}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stale partial buffer corrupted the new line: got %q, want %q", got, want)
	}
}
