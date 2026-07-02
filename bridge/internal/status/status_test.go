package status

import (
	"testing"
	"time"
)

// fakeSetter records every channel-topic push so tests can assert whether/what got pushed.
type fakeSetter struct {
	topics []string
}

func (f *fakeSetter) SetChannelTopic(_, topic string) error {
	f.topics = append(f.topics, topic)
	return nil
}

// TestFirstObservationSchedulesAPushEvenWhenDisconnected covers the Manager-side half of
// fix #3: main.go's monitorConnection now always calls OnConnected/OnDisconnected on the
// very first poll after startup, even if Factorio is already down — but that fix only
// actually clears a stale "online" topic if Manager pushes on that first call. schedule()
// arms a push whenever lastUpdate is still zero, regardless of online/offline state; this
// asserts that contract holds for OnDisconnected specifically (previously, before the
// main.go fix, OnDisconnected was never even called in this scenario).
func TestFirstObservationSchedulesAPushEvenWhenDisconnected(t *testing.T) {
	set := &fakeSetter{}
	m := New("chan1", set)

	m.OnDisconnected(time.Now())

	m.mu.Lock()
	armed := m.pendingTimer != nil
	m.mu.Unlock()
	if !armed {
		t.Fatal("OnDisconnected as the first-ever observation should arm a pending push")
	}

	// Bypass the real initialDelay timer for the test — push() is exactly what the timer
	// would have called.
	m.push()
	if len(set.topics) != 1 {
		t.Fatalf("want exactly one topic push, got %d: %v", len(set.topics), set.topics)
	}
	if got := set.topics[0]; got != "⚫ Connecting..." {
		t.Errorf("first-ever-and-down topic = %q, want the not-yet-connected placeholder", got)
	}
}

// TestFirstObservationConnectedPushesOnlineTopic is the mirror case: startup with Factorio
// already up should also push (this already worked before fix #3, kept as a regression
// guard alongside the disconnected case above).
func TestFirstObservationConnectedPushesOnlineTopic(t *testing.T) {
	set := &fakeSetter{}
	m := New("chan1", set)

	m.OnConnected(time.Now(), []string{"Alice"}, nil)
	m.push()

	if len(set.topics) != 1 {
		t.Fatalf("want exactly one topic push, got %d: %v", len(set.topics), set.topics)
	}
	if got := set.topics[0]; got == "⚫ Connecting..." || got == "" {
		t.Errorf("first-ever-and-up topic should reflect online state, got %q", got)
	}
}
