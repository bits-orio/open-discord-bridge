package main

import (
	"context"
	"log"
)

// relayQueueCapacity bounds the game→Discord event relay queue. This is a high-volume
// firehose (chat, joins/leaves, integrator events) rather than deliberate individual user
// actions, so — unlike the Discord→game inbound queue, which blocks/backpressures — this
// one drops the oldest queued item (with a logged warning) when full: better to skip a
// handful of stale notifications than let memory and relay lag grow without bound during a
// chat burst or a Discord rate-limit stall.
const relayQueueCapacity = 256

// relaySend is one queued Discord post.
type relaySend struct {
	channel string
	content string
}

// relayQueue decouples file-tailing (the producer, via push) from Discord-sending (the
// single consumer, via run) so a slow/rate-limited Discord API call can never stall event
// ingestion. Single-producer only: push is not safe to call concurrently from multiple
// goroutines (the drop-oldest sequence below assumes exclusive access to the channel).
type relayQueue struct {
	ch chan relaySend
}

func newRelayQueue(capacity int) *relayQueue {
	return &relayQueue{ch: make(chan relaySend, capacity)}
}

// push enqueues a send. If the queue is full, it drops the oldest queued item (logging a
// warning) to make room, preserving FIFO order for everything that remains queued.
func (q *relayQueue) push(channel, content string) {
	item := relaySend{channel: channel, content: content}
	select {
	case q.ch <- item:
		return
	default:
	}

	select {
	case dropped := <-q.ch:
		log.Printf("relay: outbound queue full (cap %d) — dropping oldest queued message to channel %s", cap(q.ch), dropped.channel)
	default:
		// The consumer drained a slot between the two selects; nothing to drop.
	}
	select {
	case q.ch <- item:
	default:
		// Extremely unlikely (another push raced in): drop the new item rather than block
		// the single producer goroutine.
		log.Printf("relay: outbound queue still full after dropping oldest — dropping message to channel %s", channel)
	}
}

// run drains the queue and sends each item via send, in order, until ctx is done.
func (q *relayQueue) run(ctx context.Context, send func(channel, content string)) {
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-q.ch:
			send(item.channel, item.content)
		}
	}
}
