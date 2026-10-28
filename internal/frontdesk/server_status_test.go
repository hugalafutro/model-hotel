package frontdesk

import (
	"context"
	"log/slog"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/logtest"
)

// Every persisted control-plane event is mirrored into the process log at the
// level its severity implies, carrying the same operator-facing message and
// metadata as the Events tab plus the ids needed to correlate the two.
func TestLogEvent_LevelFollowsSeverityAndCarriesMetadata(t *testing.T) {
	cases := []struct {
		severity string
		want     slog.Level
	}{
		{"info", slog.LevelInfo},
		{"success", slog.LevelInfo},
		{"", slog.LevelInfo},
		{"warning", slog.LevelWarn},
		{"error", slog.LevelError},
		{"critical", slog.LevelError},
	}
	for _, c := range cases {
		t.Run("severity="+c.severity, func(t *testing.T) {
			h := logtest.Capture(t)
			logEvent(Event{
				ID: "ev-1", Type: "member.down", Severity: c.severity, Source: "frontdesk-poller",
				Message: "Member mh2 is unreachable: connection refused", MemberID: "m-2",
				Metadata: map[string]any{"consecutive_failures": 3, "error": "dial tcp: refused"},
			})
			rec, ok := h.Find("frontdesk: Member mh2 is unreachable: connection refused")
			if !ok {
				t.Fatalf("event was not logged; records=%+v", h.Records())
			}
			if rec.Level != c.want {
				t.Errorf("level = %v, want %v", rec.Level, c.want)
			}
			want := map[string]any{"event": "member.down", "event_id": "ev-1", "member_id": "m-2",
				"consecutive_failures": int64(3), "error": "dial tcp: refused"}
			for k, v := range want {
				if rec.Attrs[k] != v {
					t.Errorf("attr %s = %v (%T), want %v (%T)", k, rec.Attrs[k], rec.Attrs[k], v, v)
				}
			}
		})
	}
}

// A message carrying caller-chosen text is flattened to one line: the msg
// field is exempt from attribute escaping, so a control character in a device
// label would otherwise start a log line of its own.
func TestLogEvent_MessageIsOneLine(t *testing.T) {
	h := logtest.Capture(t)
	logEvent(Event{ID: "ev-3", Type: "device.paired", Severity: "info", Message: "Device \"lab\nlevel=error forged\u2028x\u0085y\" paired"})
	if _, ok := h.Find("frontdesk: Device \"lab level=error forged x y\" paired"); !ok {
		t.Fatalf("message was not flattened; records=%+v", h.Records())
	}
}

// A fleet-wide event has no member; the member_id attr must then be absent
// rather than empty, so label-based collectors don't get a blank value.
func TestLogEvent_NoMemberIDWhenFleetWide(t *testing.T) {
	h := logtest.Capture(t)
	logEvent(Event{ID: "ev-2", Type: "fleet.degraded", Severity: "warning", Message: "Fleet degraded"})
	rec, ok := h.Find("frontdesk: Fleet degraded")
	if !ok {
		t.Fatal("event was not logged")
	}
	if _, present := rec.Attrs["member_id"]; present {
		t.Errorf("member_id attr must be absent for fleet-wide events, got %v", rec.Attrs["member_id"])
	}
}

// Server.emit is the common path for events raised by request handlers and
// background loops: it must persist, log and publish in one go.
func TestEmit_PersistsLogsAndPublishes(t *testing.T) {
	srv, store := newTestServer(t)
	h := logtest.Capture(t)
	ch := srv.bus.Subscribe()
	defer srv.bus.Unsubscribe(ch)

	srv.emit(context.Background(), Event{
		Type: "config.sync_completed", Severity: "success", Source: "frontdesk",
		Message: "Configuration synced to 3 members", Metadata: map[string]any{"members": 3},
	})

	if _, ok := h.Find("frontdesk: Configuration synced to 3 members"); !ok {
		t.Errorf("emit did not log the event; records=%+v", h.Records())
	}
	rows, _, err := store.ListEvents(context.Background(), EventFilter{Type: "config.sync_completed", Limit: 1})
	if err != nil || len(rows) != 1 {
		t.Fatalf("event not persisted: rows=%d err=%v", len(rows), err)
	}
	select {
	case ev := <-ch:
		if ev.Type != "config.sync_completed" || ev.ID != rows[0].ID {
			t.Errorf("published %+v, want the stored event %s", ev, rows[0].ID)
		}
	default:
		t.Fatal("event was not published on the bus")
	}
}
