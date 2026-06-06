package proxy

import (
	"testing"

	"github.com/hugalafutro/model-hotel/internal/events"
)

func TestPublishRequestStartedEvent(t *testing.T) {
	sub := events.DefaultBus.Subscribe()
	defer events.DefaultBus.Unsubscribe(sub)

	logEntry := &requestLogData{
		id:        "req-123",
		modelID:   "gpt-4",
		streaming: true,
		state:     "started",
	}
	publishRequestStartedEvent(logEntry)

	select {
	case evt := <-sub:
		if evt.Type != "request.started" {
			t.Errorf("Event type = %q, want %q", evt.Type, "request.started")
		}
		if evt.Severity != "info" {
			t.Errorf("Event severity = %q, want %q", evt.Severity, "info")
		}
		if evt.Source != "proxy" {
			t.Errorf("Event source = %q, want %q", evt.Source, "proxy")
		}
		meta := evt.Metadata
		if meta["request_id"] != "req-123" {
			t.Errorf("metadata request_id = %v, want req-123", meta["request_id"])
		}
		if meta["model_id"] != "gpt-4" {
			t.Errorf("metadata model_id = %v, want gpt-4", meta["model_id"])
		}
		if meta["streaming"] != true {
			t.Errorf("metadata streaming = %v, want true", meta["streaming"])
		}
	default:
		t.Error("Expected event on bus, got none")
	}
}
