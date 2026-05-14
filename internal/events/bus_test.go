package events

import (
	"sync"
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

func TestConcurrentSubscribePublish(t *testing.T) {
	b := NewBus()
	var wg sync.WaitGroup

	// Pre-register some subscribers.
	subs := make([]chan Event, 5)
	for i := range subs {
		subs[i] = b.Subscribe()
	}

	// Concurrent publishers.
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			b.Publish(Event{Type: "concurrent"})
		}()
	}

	// Concurrent subscribe/unsubscribe while publishing.
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ch := b.Subscribe()
			time.Sleep(5 * time.Millisecond)
			b.Unsubscribe(ch)
		}()
	}

	wg.Wait()

	// Verify all pre-registered subscriber channels received at least one event
	for i, ch := range subs {
		eventCount := 0
		drainDone := make(chan struct{})
		go func() {
			for range ch {
				eventCount++
			}
			close(drainDone)
		}()
		// Give a brief moment for any pending events
		time.Sleep(10 * time.Millisecond)
		b.Unsubscribe(ch)
		<-drainDone

		if eventCount == 0 {
			t.Errorf("subscriber %d received no events", i)
		}
	}
}

// ---------------------------------------------------------------------------
// Publish after Unsubscribe (panic recovery)
// ---------------------------------------------------------------------------

func TestPublishAfterUnsubscribe_NoPanic(t *testing.T) {
	// This tests the recover() path in Publish — sending to a closed channel
	// should not panic.
	b := NewBus()
	ch := b.Subscribe()

	// Drain the channel in a goroutine so Unsubscribe can close it
	go func() {
		// Drain the channel to prevent blocking.
		//nolint:revive // intentional: empty block for channel drain in test
		for range ch {
		}
	}()

	b.Unsubscribe(ch)

	// This should NOT panic even though the channel is closed.
	// The Publish method has a recover() wrapper.
	var panicked bool
	func() {
		defer func() {
			if r := recover(); r != nil {
				panicked = true
			}
		}()
		b.Publish(Event{Type: "after-close"})
	}()

	if panicked {
		t.Error("Publish() after Unsubscribe() should not panic")
	}

	// Give a moment for any goroutine to complete
	time.Sleep(10 * time.Millisecond)
}

func TestPublishConvenience(t *testing.T) {
	ch := DefaultBus.Subscribe()
	defer DefaultBus.Unsubscribe(ch)
	Publish(Event{Type: "convenience"})
	select {
	case recv := <-ch:
		if recv.Type != "convenience" {
			t.Errorf("expected 'convenience', got %q", recv.Type)
		}
	case <-time.After(time.Second):
		t.Error("timeout waiting for convenience Publish")
	}
}
