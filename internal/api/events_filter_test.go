package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

func TestEventVisible(t *testing.T) {
	ownerID := uuid.New()
	otherID := uuid.New()
	logsUser := &user.Identity{Role: user.RoleUser, Grants: []string{"logs"}, UserID: &ownerID}
	logsNoUser := &user.Identity{Role: user.RoleUser, Grants: []string{"logs"}}
	chatUser := &user.Identity{Role: user.RoleUser, Grants: []string{"chat"}, UserID: &ownerID}

	// own scopes an event to ownerID; other scopes it to a different user; unowned
	// carries no owner (admin-only, like an admin/unowned virtual key).
	own := map[string]interface{}{"owner_user_id": ownerID.String()}
	other := map[string]interface{}{"owner_user_id": otherID.String()}
	unowned := map[string]interface{}{"owner_user_id": ""}

	cases := []struct {
		name string
		id   *user.Identity
		ev   events.Event
		want bool
	}{
		{"admin sees own-scoped request", user.AdminIdentity(), events.Event{Type: "request.completed", Metadata: own}, true},
		{"admin sees others' requests", user.AdminIdentity(), events.Event{Type: "request.completed", Metadata: other}, true},
		{"admin sees operational", user.AdminIdentity(), events.Event{Type: "backup.created"}, true},
		{"logs grant sees own request", logsUser, events.Event{Type: "request.completed", Metadata: own}, true},
		{"logs grant sees own streaming", logsUser, events.Event{Type: "request.streaming", Metadata: own}, true},
		{"logs grant denied other user's request", logsUser, events.Event{Type: "request.completed", Metadata: other}, false},
		{"logs grant denied unowned request", logsUser, events.Event{Type: "request.completed", Metadata: unowned}, false},
		{"logs grant denied request with no owner metadata", logsUser, events.Event{Type: "request.completed"}, false},
		{"logs grant without user id denied own-looking request", logsNoUser, events.Event{Type: "request.completed", Metadata: own}, false},
		{"logs grant denied discovery progress", logsUser, events.Event{Type: "request.discovery.provider_starting", Metadata: own}, false},
		{"logs grant denied operational", logsUser, events.Event{Type: "backup.created"}, false},
		{"logs grant denied fleet", logsUser, events.Event{Type: "member.state_changed"}, false},
		{"chat grant denied own request", chatUser, events.Event{Type: "request.started", Metadata: own}, false},
		{"future type default-denied", logsUser, events.Event{Type: "shiny.new_event", Metadata: own}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eventVisible(tc.id, tc.ev); got != tc.want {
				t.Errorf("eventVisible(%v, %q) = %v, want %v", tc.id, tc.ev.Type, got, tc.want)
			}
		})
	}
}

// TestStreamEvents_OwnerScoping opens the SSE stream as a logs-granted user and
// verifies request lifecycle events for that user's own keys arrive, while
// another user's request events and admin-operational events are dropped before
// hitting the wire.
func TestStreamEvents_OwnerScoping(t *testing.T) {
	h, apiRouter := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()
	if _, err := pool.Exec(context.Background(), `TRUNCATE users, webauthn_sessions CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	userRepo := user.NewRepository(pool)
	webauthnRepo := webauthn.NewRepository(pool)
	sm := webauthn.NewSessionManager(webauthnRepo)
	h.SetWebAuthnSessionManager(sm)
	h.SetUserAuth(userRepo, webauthnRepo)

	id := createUserViaAPI(t, apiRouter, "tailer", "password123", "user", []string{"logs"})
	token := mintUserToken(t, sm, id)

	// The SSE route lives on its own Timeout-exempt router in main.go.
	er := chi.NewRouter()
	h.RegisterEvents(er)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/events", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)

	go func() {
		er.ServeHTTP(rec, req)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)

	// Own request (owner_user_id == this user) is visible; another user's request
	// and operational/discovery events are not.
	events.Publish(events.Event{Type: "request.completed", Severity: "info", Message: "mine done", Metadata: map[string]interface{}{"owner_user_id": id}})
	events.Publish(events.Event{Type: "request.completed", Severity: "info", Message: "theirs done", Metadata: map[string]interface{}{"owner_user_id": uuid.NewString()}})
	events.Publish(events.Event{Type: "backup.created", Severity: "success", Message: "backup done"})
	events.Publish(events.Event{Type: "request.discovery.provider_starting", Severity: "info", Message: "discovering"})
	time.Sleep(100 * time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler goroutine did not exit after context cancellation")
	}

	body := rec.Body.String()
	if !strings.Contains(body, "mine done") {
		t.Errorf("logs-granted user missing own request.completed, got: %s", body)
	}
	if strings.Contains(body, "theirs done") {
		t.Errorf("another user's request event leaked to logs-granted user: %s", body)
	}
	if strings.Contains(body, "backup.created") {
		t.Errorf("admin-operational event leaked to logs-granted user: %s", body)
	}
	if strings.Contains(body, "provider_starting") {
		t.Errorf("discovery progress leaked to logs-granted user: %s", body)
	}
}
