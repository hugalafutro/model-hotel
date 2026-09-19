// Package events provides a simple event bus for SSE subscriptions.
package events

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// Event represents a publishable event for SSE distribution.
type Event struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	Severity  string         `json:"severity"` // "success", "info", "warning", "error"
	Source    string         `json:"source"`
	Message   string         `json:"message"`
	Metadata  map[string]any `json:"metadata,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// Bus is an event bus for distributing events to SSE subscribers.
//
// Each subscriber carries its own drop counter rather than a plain presence
// marker: Publish runs under the read lock, so the counter has to be an atomic
// the send path can bump without upgrading to a write lock, and hanging it off
// the map value means Unsubscribe and Close retire it with the channel instead
// of leaking a per-subscriber entry in a side map.
type Bus struct {
	mu          sync.RWMutex
	subscribers map[chan Event]*atomic.Uint64
	closed      bool
}

// dropLogEvery is how often a still-stalled subscriber gets another dropped
// event logged, after the first one.
const dropLogEvery = 100

// DefaultBus is the global default event bus.
var DefaultBus = NewBus()

// NewBus creates a new event bus instance.
func NewBus() *Bus {
	return &Bus{
		subscribers: make(map[chan Event]*atomic.Uint64),
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

	// No recover around the send: every close happens under the write lock or
	// after the channel has left the map, and Publish holds the read lock, so
	// the channel it is sending to cannot be closed underneath it.
	b.mu.RLock()
	defer b.mu.RUnlock()
	for ch, drops := range b.subscribers {
		select {
		case ch <- event:
		default:
			// One line per drop would amplify a single stalled consumer into a
			// log record (and an app_logs row) per event for as long as it stays
			// stalled. The first drop is the one that tells an operator
			// something is wrong; after that the running total every dropLogEvery
			// says the same thing at a bounded rate. The count is the
			// subscriber's lifetime total, not reset by a delivery in between:
			// a consumer flapping between full and drained would otherwise log
			// on every first drop, the amplification this exists to stop.
			if n := drops.Add(1); n == 1 || n%dropLogEvery == 0 {
				debuglog.Warn("events: event dropped, subscriber too slow", "type", event.Type, "dropped", n)
			}
		}
	}
}

// Subscribe registers a buffered channel and returns it. The caller must
// call Unsubscribe with the same channel when done. After Close the returned
// channel is already closed, so a subscriber that arrives during shutdown
// sees end-of-stream immediately instead of parking on a bus nobody
// publishes to.
func (b *Bus) Subscribe() chan Event {
	ch := make(chan Event, 64)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		close(ch)
		return ch
	}
	b.subscribers[ch] = &atomic.Uint64{}
	return ch
}

// Unsubscribe removes and closes a previously subscribed channel.
// The channel is closed first, then drained in a goroutine so that
// any Publish call currently sending to the channel completes safely
// before the buffer is discarded. A channel the bus no longer tracks
// (already unsubscribed, or closed by Close) is left alone, so the usual
// `defer bus.Unsubscribe(ch)` in a stream handler stays safe across shutdown.
func (b *Bus) Unsubscribe(ch chan Event) {
	b.mu.Lock()
	_, tracked := b.subscribers[ch]
	delete(b.subscribers, ch)
	b.mu.Unlock()
	if !tracked {
		return
	}
	closeAndDrain(ch)
}

// Close ends every subscription: each subscriber channel is closed (and
// drained) so stream handlers blocked on it return, and later Subscribe calls
// get an already-closed channel. Publish becomes a no-op. Call it at the start
// of graceful shutdown, before http.Server.Shutdown: an open SSE stream is an
// in-flight request that Shutdown otherwise waits on until its deadline, so
// without this every restart with a dashboard tab open burns the full timeout.
// Idempotent.
func (b *Bus) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for ch := range b.subscribers {
		delete(b.subscribers, ch)
		closeAndDrain(ch)
	}
}

// closeAndDrain closes ch, then drains its buffer in the background so any
// events still queued for the departed subscriber are discarded.
func closeAndDrain(ch chan Event) {
	close(ch)
	go func() {
		//nolint:revive // intentional: empty block for channel drain
		for range ch {
		}
	}()
}

// Publish is a convenience function that publishes to the DefaultBus.
func Publish(event Event) {
	DefaultBus.Publish(event)
}

// Subscribe is a convenience function that subscribes to the DefaultBus.
func Subscribe() chan Event {
	return DefaultBus.Subscribe()
}

// Unsubscribe is a convenience function that unsubscribes from the DefaultBus.
func Unsubscribe(ch chan Event) {
	DefaultBus.Unsubscribe(ch)
}
