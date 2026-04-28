package events

import (
	"log"
	"sync"
	"time"

	"github.com/google/uuid"
)

type Event struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`
	Severity  string                 `json:"severity"` // "success", "info", "warning", "error"
	Message   string                 `json:"message"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

type Bus struct {
	mu          sync.RWMutex
	subscribers map[chan Event]struct{}
}

var DefaultBus = NewBus()

func NewBus() *Bus {
	return &Bus{
		subscribers: make(map[chan Event]struct{}),
	}
}

// Publish sends an event to all current subscribers. Non-blocking: if a
// subscriber's channel is full the event is dropped for that subscriber.
func (b *Bus) Publish(event Event) {
	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch := range b.subscribers {
		select {
		case ch <- event:
		default:
			log.Printf("[events] warning: event %q dropped, subscriber too slow", event.Type)
		}
	}
}

// Subscribe registers a buffered channel and returns it. The caller must
// call Unsubscribe with the same channel when done.
func (b *Bus) Subscribe() chan Event {
	ch := make(chan Event, 64)
	b.mu.Lock()
	b.subscribers[ch] = struct{}{}
	b.mu.Unlock()
	return ch
}

// Unsubscribe removes and closes a previously subscribed channel.
func (b *Bus) Unsubscribe(ch chan Event) {
	b.mu.Lock()
	delete(b.subscribers, ch)
	b.mu.Unlock()
	// Drain and close in a goroutine to avoid deadlock if Publish is
	// blocked on this channel.
	go func() {
		for range ch {
		}
		close(ch)
	}()
}

// Publish is a convenience function that publishes to the DefaultBus.
func Publish(event Event) {
	DefaultBus.Publish(event)
}
