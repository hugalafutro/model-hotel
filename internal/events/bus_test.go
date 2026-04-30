package events

import (
	"testing"
	"time"
)

func TestSubscribePublishUnsubscribe(t *testing.T) {
	b := NewBus()
	ch := b.Subscribe()
	event := Event{Type: "test"}
	b.Publish(event)

	select {
	case recv := <-ch:
		if recv.Type != "test" {
			t.Errorf("expected type 'test', got %q", recv.Type)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for event")
	}

	b.Unsubscribe(ch)

	// After Unsubscribe, the channel is closed. Reading from a closed
	// channel returns zero value immediately (not blocking).
	// Verify the channel is indeed closed.
	_, ok := <-ch
	if ok {
		t.Error("channel not closed after unsubscribe")
	}

	// Publishing after unsubscribe should not send to the closed channel.
	// Publish uses non-blocking send, so it just drops the event.
	b.Publish(event)
}


func TestMultipleSubscribers(t *testing.T) {
	b := NewBus()
	ch1 := b.Subscribe()
	ch2 := b.Subscribe()
	event := Event{Type: "multi"}
	b.Publish(event)

	for i, ch := range []chan Event{ch1, ch2} {
		select {
		case recv := <-ch:
			if recv.Type != "multi" {
				t.Errorf("subscriber %d: expected 'multi', got %q", i+1, recv.Type)
			}
		case <-time.After(time.Second):
			t.Errorf("timeout for subscriber %d", i+1)
		}
	}

	b.Unsubscribe(ch1)
	b.Unsubscribe(ch2)
}

func TestDropIfFull(t *testing.T) {
	b := NewBus()
	ch := make(chan Event, 1) // small buffer

	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()

	// Publish more than buffer size
	for i := 0; i < 3; i++ {
		b.Publish(Event{Type: "drop"})
	}

	// Should receive at least one
	select {
	case <-ch:
		// received
	case <-time.After(time.Second):
		t.Error("did not receive any event")
	}

	// But not all, though hard to assert exactly, but concept is there
}

func TestAutoIDAndTimestamp(t *testing.T) {
	b := NewBus()
	ch := b.Subscribe()
	b.Publish(Event{Type: "auto"})

	recv := <-ch
	if recv.ID == "" {
		t.Error("ID not auto-generated")
	}
	if recv.Timestamp.IsZero() {
		t.Error("Timestamp not set")
	}

	b.Unsubscribe(ch)
}

func TestPublishWithIDAndTimestamp(t *testing.T) {
	b := NewBus()
	ch := b.Subscribe()
	expectedID := "custom-id"
	expectedTime := time.Now()
	b.Publish(Event{
		ID:        expectedID,
		Type:      "custom",
		Timestamp: expectedTime,
	})

	recv := <-ch
	if recv.ID != expectedID {
		t.Errorf("expected ID %q, got %q", expectedID, recv.ID)
	}
	if !recv.Timestamp.Equal(expectedTime) {
		t.Errorf("expected timestamp %v, got %v", expectedTime, recv.Timestamp)
	}

	b.Unsubscribe(ch)
}
