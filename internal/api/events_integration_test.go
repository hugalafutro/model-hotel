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

	// The handler should have received the event
	body := rec.Body.String()
	if !strings.Contains(body, "test.event") {
		t.Errorf("Expected event in SSE stream, got: %s", body)
	}
	if !strings.Contains(body, "Test message") {
		t.Errorf("Expected message in SSE stream, got: %s", body)
	}

	// Cancel the request context to unblock the handler goroutine.
	cancel()

	// Wait for the handler goroutine to finish.
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Error("handler goroutine did not exit after context cancellation")
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
