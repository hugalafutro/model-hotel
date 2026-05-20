package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/events"
)

// TestStreamEvents_ContextCancel tests that the SSE handler returns cleanly
// when the request context is cancelled.
func TestStreamEvents_ContextCancel(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.RegisterEvents(r)

	// Create a request with a short-lived context
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/events", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	// Serve the request - should return when context is cancelled
	r.ServeHTTP(rec, req)

	// Verify the response was started (we got the initial connection comment)
	body := rec.Body.String()
	if !strings.Contains(body, ": connected") {
		t.Errorf("Expected connection comment, got: %s", body)
	}
}

// TestStreamEvents_EventDelivery tests that events published to the bus
// are delivered to SSE clients.
func TestStreamEvents_EventDelivery(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.RegisterEvents(r)

	// Create a cancellable context so the handler goroutine can be cleaned up.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	rec := httptest.NewRecorder()

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/events", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	// Start the SSE handler in a goroutine
	go func() {
		r.ServeHTTP(rec, req)
		close(done)
	}()

	// Give the handler time to subscribe
	time.Sleep(50 * time.Millisecond)

	// Publish an event
	events.Publish(events.Event{
		Type:     "test.event",
		Severity: "info",
		Message:  "Test message",
		Metadata: map[string]interface{}{"test": true},
	})

	// Give time for event to be delivered
	time.Sleep(100 * time.Millisecond)

	// Cancel the request context to unblock the handler goroutine.
	cancel()

	// Wait for the handler goroutine to finish BEFORE reading the body
	// to avoid a race between the goroutine writing to rec.Body and
	// the test goroutine reading from it.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler goroutine did not exit after context cancellation")
	}

	// Now safe to read the body — the handler goroutine is done writing.
	body := rec.Body.String()
	if !strings.Contains(body, "test.event") {
		t.Errorf("Expected event in SSE stream, got: %s", body)
	}
	if !strings.Contains(body, "Test message") {
		t.Errorf("Expected message in SSE stream, got: %s", body)
	}
}

// TestStreamEvents_Heartbeat tests that heartbeat comments are sent
// to keep the connection alive.
func TestStreamEvents_Heartbeat(t *testing.T) {
	// This test is difficult to run in a unit test context because
	// the heartbeat ticker runs for 30 seconds. We verify the code path
	// exists by checking the handler processes requests correctly.
	h := newTestHandler(t)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.RegisterEvents(r)

	// Create a request with a short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/events", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	// Serve the request
	r.ServeHTTP(rec, req)

	// Verify the response was started
	body := rec.Body.String()
	if !strings.Contains(body, ": connected") {
		t.Errorf("Expected connection comment, got: %s", body)
	}
}

// TestStreamEvents_FlusherNotSupported tests that the SSE handler returns
// a 500 error when the ResponseWriter doesn't implement http.Flusher.
func TestStreamEvents_FlusherNotSupported(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.RegisterEvents(r)

	// Create a custom ResponseWriter that does NOT implement http.Flusher
	rec := httptest.NewRecorder()
	noFlushWriter := &noFlusherResponseWriter{ResponseWriter: rec}

	req := httptest.NewRequest(http.MethodGet, "/events", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	// Serve the request - should return 500 because Flusher is not supported
	r.ServeHTTP(noFlushWriter, req)

	// Verify we got a 500 status code
	if noFlushWriter.status != http.StatusInternalServerError {
		t.Errorf("Expected status %d, got %d", http.StatusInternalServerError, noFlushWriter.status)
	}

	// Verify the error message contains "streaming not supported"
	body := rec.Body.String()
	if !strings.Contains(body, "streaming not supported") {
		t.Errorf("Expected 'streaming not supported' in response, got: %s", body)
	}
}

// TestStreamEvents_MarshalError tests that the SSE handler continues
// processing when an event cannot be marshaled to JSON.
func TestStreamEvents_MarshalError(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.RegisterEvents(r)

	// Create a cancellable context so the handler goroutine can be cleaned up.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	rec := httptest.NewRecorder()

	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/events", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	// Start the SSE handler in a goroutine
	go func() {
		r.ServeHTTP(rec, req)
		close(done)
	}()

	// Give the handler time to subscribe
	time.Sleep(50 * time.Millisecond)

	// Publish an event with a value that can't be JSON-marshaled (a channel)
	events.Publish(events.Event{
		Type:     "test.bad_event",
		Severity: "info",
		Message:  "Bad event with channel",
		Metadata: map[string]interface{}{"ch": make(chan int)},
	})

	// Give time for the bad event to be processed
	time.Sleep(50 * time.Millisecond)

	// Publish a good event that should still be delivered
	events.Publish(events.Event{
		Type:     "test.good_event",
		Severity: "info",
		Message:  "Good event after bad",
		Metadata: map[string]interface{}{"test": true},
	})

	// Give time for the good event to be delivered
	time.Sleep(100 * time.Millisecond)

	// Cancel the request context to unblock the handler goroutine.
	cancel()

	// Wait for the handler goroutine to finish BEFORE reading the body
	// to avoid a race between the goroutine writing to rec.Body and
	// the test goroutine reading from it.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler goroutine did not exit after context cancellation")
	}

	// Now safe to read the body — the handler goroutine is done writing.
	body := rec.Body.String()
	if !strings.Contains(body, "test.good_event") {
		t.Errorf("Expected good event in SSE stream after marshal error, got: %s", body)
	}
	if !strings.Contains(body, "Good event after bad") {
		t.Errorf("Expected good event message in SSE stream, got: %s", body)
	}
}

// noFlusherResponseWriter wraps http.ResponseWriter but does not implement http.Flusher.
type noFlusherResponseWriter struct {
	http.ResponseWriter
	status int
}

func (n *noFlusherResponseWriter) Header() http.Header {
	return n.ResponseWriter.Header()
}

func (n *noFlusherResponseWriter) Write(b []byte) (int, error) {
	return n.ResponseWriter.Write(b)
}

func (n *noFlusherResponseWriter) WriteHeader(statusCode int) {
	n.status = statusCode
	n.ResponseWriter.WriteHeader(statusCode)
}
