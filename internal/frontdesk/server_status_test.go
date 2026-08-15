package frontdesk

import (
	"context"
	"log/slog"
	"sync"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// recordingHandler captures slog records so a test can assert what Front Desk
// logged; safe for concurrent use because background pollers may log too.
type recordingHandler struct {
	mu      sync.Mutex
	records []capturedRecord
}

type capturedRecord struct {
	level slog.Level
	msg   string
	attrs map[string]any
}

func (h *recordingHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *recordingHandler) WithAttrs([]slog.Attr) slog.Handler       { return h }
func (h *recordingHandler) WithGroup(string) slog.Handler            { return h }
func (h *recordingHandler) Handle(_ context.Context, r slog.Record) error {
	rec := capturedRecord{level: r.Level, msg: r.Message, attrs: map[string]any{}}
	r.Attrs(func(a slog.Attr) bool {
		rec.attrs[a.Key] = a.Value.Any()
		return true
	})
	h.mu.Lock()
	h.records = append(h.records, rec)
	h.mu.Unlock()
	return nil
}

// snapshot copies the captured records for a failure message.
func (h *recordingHandler) snapshot() []capturedRecord {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]capturedRecord(nil), h.records...)
}

// find returns the first captured record with the given message.
func (h *recordingHandler) find(msg string) (capturedRecord, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, r := range h.records {
		if r.msg == msg {
			return r, true
		}
	}
	return capturedRecord{}, false
}

func captureLogs(t *testing.T) *recordingHandler {
	t.Helper()
	h := &recordingHandler{}
	debuglog.SetHandler(h)
	t.Cleanup(func() { debuglog.Init(false) })
	return h
}

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
			h := captureLogs(t)
			logEvent(Event{
				ID: "ev-1", Type: "member.down", Severity: c.severity, Source: "frontdesk-poller",
				Message: "Member mh2 is unreachable: connection refused", MemberID: "m-2",
				Metadata: map[string]any{"consecutive_failures": 3, "error": "dial tcp: refused"},
			})
			rec, ok := h.find("frontdesk: Member mh2 is unreachable: connection refused")
			if !ok {
				t.Fatalf("event was not logged; records=%+v", h.snapshot())
			}
			if rec.level != c.want {
				t.Errorf("level = %v, want %v", rec.level, c.want)
			}
			want := map[string]any{"event": "member.down", "event_id": "ev-1", "member_id": "m-2",
				"consecutive_failures": int64(3), "error": "dial tcp: refused"}
			for k, v := range want {
				if rec.attrs[k] != v {
					t.Errorf("attr %s = %v (%T), want %v (%T)", k, rec.attrs[k], rec.attrs[k], v, v)
				}
			}
		})
	}
}

// A fleet-wide event has no member; the member_id attr must then be absent
// rather than empty, so label-based collectors don't get a blank value.
func TestLogEvent_NoMemberIDWhenFleetWide(t *testing.T) {
	h := captureLogs(t)
	logEvent(Event{ID: "ev-2", Type: "fleet.degraded", Severity: "warning", Message: "Fleet degraded"})
	rec, ok := h.find("frontdesk: Fleet degraded")
	if !ok {
		t.Fatal("event was not logged")
	}
	if _, present := rec.attrs["member_id"]; present {
		t.Errorf("member_id attr must be absent for fleet-wide events, got %v", rec.attrs["member_id"])
	}
}

// Server.emit is the common path for events raised by request handlers and
// background loops: it must persist, log and publish in one go.
func TestEmit_PersistsLogsAndPublishes(t *testing.T) {
	srv, store := newTestServer(t)
	h := captureLogs(t)
	ch := srv.bus.Subscribe()
	defer srv.bus.Unsubscribe(ch)

	srv.emit(context.Background(), Event{
		Type: "config.sync_completed", Severity: "success", Source: "frontdesk",
		Message: "Configuration synced to 3 members", Metadata: map[string]any{"members": 3},
	})

	if _, ok := h.find("frontdesk: Configuration synced to 3 members"); !ok {
		t.Errorf("emit did not log the event; records=%+v", h.snapshot())
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
