package events

import (
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
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
		func() {
			defer func() {
				if r := recover(); r != nil {
					debuglog.Warn("events: failed to send event", "type", event.Type)
				}
			}()
			select {
			case ch <- event:
			default:
				debuglog.Warn("events: event dropped, subscriber too slow", "type", event.Type)
			}
		}()
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
// The channel is closed first, then drained in a goroutine so that
// any Publish call currently sending to the channel completes safely
// before the buffer is discarded.
func (b *Bus) Unsubscribe(ch chan Event) {
	b.mu.Lock()
	delete(b.subscribers, ch)
	b.mu.Unlock()
	close(ch)
	go func() {
		for range ch {
		}
	}()
}

// Publish is a convenience function that publishes to the DefaultBus.
func Publish(event Event) {
	DefaultBus.Publish(event)
}
