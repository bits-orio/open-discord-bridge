package main

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// TestRelayQueuePreservesFIFOOrder covers fix #6's ordering requirement: items come out via
// run in exactly the order they were pushed, as long as the queue never fills.
func TestRelayQueuePreservesFIFOOrder(t *testing.T) {
	q := newRelayQueue(16)
	const n = 10
	for i := 0; i < n; i++ {
		q.push("chan", fmt.Sprintf("msg-%02d", i))
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var mu sync.Mutex
	var got []string
	go q.run(ctx, func(channel, content string) {
		mu.Lock()
		got = append(got, content)
		mu.Unlock()
	})

	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		l := len(got)
		mu.Unlock()
		if l == n {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out, got %d/%d", l, n)
		case <-time.After(5 * time.Millisecond):
		}
	}

	for i, m := range got {
		if want := fmt.Sprintf("msg-%02d", i); m != want {
			t.Fatalf("out of order at %d: got %q want %q (all: %v)", i, m, want, got)
		}
	}
}

// TestRelayQueueDropsOldestWhenFull covers fix #6's bounding + drop policy: once the queue
// is full, pushing a new item drops the oldest queued item (not the new one) so the queue
// stays FIFO and bounded — better to lose stale notifications than let lag/memory grow
// without bound during a chat burst.
func TestRelayQueueDropsOldestWhenFull(t *testing.T) {
	const capacity = 4
	q := newRelayQueue(capacity)

	// Fill the queue, then push capacity more — each push should evict the oldest.
	for i := 0; i < capacity+3; i++ {
		q.push("chan", fmt.Sprintf("msg-%02d", i))
	}

	var got []string
	for i := 0; i < capacity; i++ {
		select {
		case item := <-q.ch:
			got = append(got, item.content)
		default:
			t.Fatalf("queue underfull: only got %d items, want %d", len(got), capacity)
		}
	}

	// The oldest (msg-00, msg-01, msg-02) should have been dropped; the newest `capacity`
	// items (msg-03..msg-06) should remain, still in FIFO order.
	want := []string{"msg-03", "msg-04", "msg-05", "msg-06"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// TestRelayQueueNeverExceedsCapacity is a property-style check: pushing far more items
// than capacity must never make the underlying channel buffer more than capacity items.
func TestRelayQueueNeverExceedsCapacity(t *testing.T) {
	const capacity = 8
	q := newRelayQueue(capacity)
	for i := 0; i < capacity*10; i++ {
		q.push("chan", fmt.Sprintf("msg-%03d", i))
		if len(q.ch) > capacity {
			t.Fatalf("queue exceeded capacity: len=%d cap=%d", len(q.ch), capacity)
		}
	}
	if len(q.ch) != capacity {
		t.Fatalf("expected a full queue of %d items, got %d", capacity, len(q.ch))
	}
}
