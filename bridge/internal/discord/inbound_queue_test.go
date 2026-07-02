package discord

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/bwmarrin/discordgo"
)

// TestInboundWorkerPreservesQueueOrder covers fix #5's core guarantee: whatever order
// messages land in inboundQueue, runInboundWorker dispatches them to onMsg in that same
// order, one at a time — even though each was enqueued sequentially rather than through a
// slow onMsg call directly. This is what actually fixes game-chat relay ordering: the
// previous code called the (slow, RCON-round-tripping) onMsg straight from
// discordgo's per-event goroutine, so a burst of messages could finish — and relay to the
// game — in whatever order those goroutines happened to be scheduled.
func TestInboundWorkerPreservesQueueOrder(t *testing.T) {
	var mu sync.Mutex
	var got []string

	c := &Client{
		onMsg: func(m InboundMessage) {
			mu.Lock()
			got = append(got, m.Message)
			mu.Unlock()
		},
		inbound:      map[string]bool{},
		inboundQueue: make(chan InboundMessage, inboundQueueCapacity),
	}
	stop := make(chan struct{})
	go c.runInboundWorker(stop)
	defer close(stop)

	const n = 50
	for i := 0; i < n; i++ {
		c.inboundQueue <- InboundMessage{Message: fmt.Sprintf("msg-%03d", i)}
	}

	waitForLen(t, &mu, &got, n)

	for i, m := range got {
		want := fmt.Sprintf("msg-%03d", i)
		if m != want {
			t.Fatalf("out of order at index %d: got %q want %q (all: %v)", i, m, want, got)
		}
	}
}

// TestHandleMessageQueuesRatherThanCallingOnMsgDirectly covers the handleMessage side of
// fix #5: it must enqueue (cheap, fast) instead of invoking onMsg (which does a
// synchronous RCON round-trip) inline — the queue + single worker is what gives relay order
// a guarantee despite discordgo dispatching each MessageCreate on its own goroutine.
func TestHandleMessageQueuesRatherThanCallingOnMsgDirectly(t *testing.T) {
	var mu sync.Mutex
	var got []string

	c := &Client{
		onMsg: func(m InboundMessage) {
			mu.Lock()
			got = append(got, m.Message)
			mu.Unlock()
		},
		inbound:      map[string]bool{"chan1": true},
		inboundQueue: make(chan InboundMessage, inboundQueueCapacity),
	}
	stop := make(chan struct{})
	go c.runInboundWorker(stop)
	defer close(stop)

	const n = 20
	for i := 0; i < n; i++ {
		// GuildID left empty so authorIsAdmin (which touches c.session) short-circuits —
		// handleMessage is testable here without a live discordgo session.
		c.handleMessage(nil, &discordgo.MessageCreate{Message: &discordgo.Message{
			Author:    &discordgo.User{ID: "u1"},
			ChannelID: "chan1",
			Content:   fmt.Sprintf("msg-%02d", i),
		}})
	}

	waitForLen(t, &mu, &got, n)
	for i, m := range got {
		want := fmt.Sprintf("msg-%02d", i)
		if m != want {
			t.Fatalf("out of order at index %d: got %q want %q (all: %v)", i, m, want, got)
		}
	}
}

// TestHandleMessageIgnoresBotsAndNonInboundChannels covers that filtering still happens
// before enqueueing (bots and non-bridged channels never reach onMsg).
func TestHandleMessageIgnoresBotsAndNonInboundChannels(t *testing.T) {
	var mu sync.Mutex
	var got []string
	c := &Client{
		onMsg:        func(m InboundMessage) { mu.Lock(); got = append(got, m.Message); mu.Unlock() },
		inbound:      map[string]bool{"chan1": true},
		inboundQueue: make(chan InboundMessage, inboundQueueCapacity),
	}
	stop := make(chan struct{})
	go c.runInboundWorker(stop)
	defer close(stop)

	c.handleMessage(nil, &discordgo.MessageCreate{Message: &discordgo.Message{
		Author: &discordgo.User{ID: "bot1", Bot: true}, ChannelID: "chan1", Content: "should be ignored",
	}})
	c.handleMessage(nil, &discordgo.MessageCreate{Message: &discordgo.Message{
		Author: &discordgo.User{ID: "u1"}, ChannelID: "not-bridged", Content: "should be ignored too",
	}})
	c.handleMessage(nil, &discordgo.MessageCreate{Message: &discordgo.Message{
		Author: &discordgo.User{ID: "u1"}, ChannelID: "chan1", Content: "real message",
	}})

	waitForLen(t, &mu, &got, 1)
	if got[0] != "real message" {
		t.Fatalf("got %v, want only [\"real message\"]", got)
	}
}

func waitForLen(t *testing.T, mu *sync.Mutex, got *[]string, want int) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		n := len(*got)
		mu.Unlock()
		if n >= want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for %d messages, got %d", want, n)
		case <-time.After(5 * time.Millisecond):
		}
	}
}
