// Package events provides bounded in-process event fan-out.
package events

import (
	"context"
	"sync"
	"time"
)

const subscriberBuffer = 64

// Event is a named JSON-ready event. Data should be an immutable value.
type Event struct {
	Type string `json:"type"`
	Data any    `json:"data"`
}

type subscription struct {
	id   uint64
	ch   chan Event
	done chan struct{}
}

// Hub fans events out without allowing a slow consumer to block publishers.
type Hub struct {
	mu   sync.Mutex
	next uint64
	subs map[uint64]subscription
}

func NewHub() *Hub { return &Hub{subs: make(map[uint64]subscription)} }

// Subscribe returns a channel that closes on context cancellation or overflow.
func (h *Hub) Subscribe(ctx context.Context) <-chan Event {
	ch := make(chan Event, subscriberBuffer)
	if ctx == nil {
		close(ch)
		return ch
	}
	h.mu.Lock()
	if h.subs == nil {
		h.subs = make(map[uint64]subscription)
	}
	h.next++
	id := h.next
	finished := make(chan struct{})
	h.subs[id] = subscription{id: id, ch: ch, done: finished}
	h.mu.Unlock()
	go func() {
		select {
		case <-ctx.Done():
		case <-finished:
			return
		}
		h.mu.Lock()
		if sub, ok := h.subs[id]; ok {
			delete(h.subs, id)
			close(sub.done)
			close(sub.ch)
		}
		h.mu.Unlock()
	}()
	return ch
}

// Publish delivers to each subscriber, removing any whose buffer is full.
func (h *Hub) Publish(event Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, sub := range h.subs {
		select {
		case sub.ch <- event:
		default:
			delete(h.subs, id)
			close(sub.done)
			close(sub.ch)
		}
	}
}

func (h *Hub) Hello() Event {
	return Event{Type: "hello", Data: map[string]string{"server_time": time.Now().UTC().Format(time.RFC3339)}}
}
