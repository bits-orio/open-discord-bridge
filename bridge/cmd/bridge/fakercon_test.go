package main

import "sync"

// fakeRCON is a minimal in-memory rconExecutor for tests: it records every command it's
// asked to run and, optionally, returns a scripted response/error per exact command.
type fakeRCON struct {
	mu       sync.Mutex
	calls    []string
	handlers map[string]func() (string, error)
}

func newFakeRCON() *fakeRCON {
	return &fakeRCON{handlers: map[string]func() (string, error){}}
}

// on scripts the response for an exact command string.
func (f *fakeRCON) on(cmd string, resp string, err error) {
	f.handlers[cmd] = func() (string, error) { return resp, err }
}

func (f *fakeRCON) Execute(cmd string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, cmd)
	h := f.handlers[cmd]
	f.mu.Unlock()
	if h != nil {
		return h()
	}
	return "", nil
}
