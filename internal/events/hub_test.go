package events

import (
	"context"
	"testing"
	"time"
)

func TestSlowSubscriberIsDroppedWithoutBlocking(t *testing.T) {
	h := NewHub()
	ch := h.Subscribe(context.Background())
	for i := 0; i < subscriberBuffer+1; i++ {
		h.Publish(Event{Type: "activity.queued", Data: i})
	}
	deadline := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-deadline:
			t.Fatal("subscriber was not dropped")
		}
	}
}

func TestSubscribeClosesOnCancellation(t *testing.T) {
	h := NewHub()
	ctx, cancel := context.WithCancel(context.Background())
	ch := h.Subscribe(ctx)
	cancel()
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("unexpected event")
		}
	case <-time.After(time.Second):
		t.Fatal("subscription did not close")
	}
}
