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
	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

func TestEventVisible(t *testing.T) {
	logsUser := &user.Identity{Role: user.RoleUser, Grants: []string{"logs"}}
	chatUser := &user.Identity{Role: user.RoleUser, Grants: []string{"chat"}}

	cases := []struct {
		name      string
		id        *user.Identity
		eventType string
		want      bool
	}{
		{"admin sees request lifecycle", user.AdminIdentity(), "request.completed", true},
		{"admin sees operational", user.AdminIdentity(), "backup.created", true},
		{"logs grant sees request lifecycle", logsUser, "request.completed", true},
		{"logs grant sees streaming", logsUser, "request.streaming", true},
		{"logs grant denied discovery progress", logsUser, "request.discovery.provider_starting", false},
		{"logs grant denied operational", logsUser, "backup.created", false},
		{"logs grant denied fleet", logsUser, "member.state_changed", false},
		{"chat grant denied request lifecycle", chatUser, "request.started", false},
		{"future type default-denied", logsUser, "shiny.new_event", false},
		{"nil identity denied", nil, "request.completed", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := eventVisible(tc.id, tc.eventType); got != tc.want {
				t.Errorf("eventVisible(%v, %q) = %v, want %v", tc.id, tc.eventType, got, tc.want)
			}
		})
	}
}

// TestStreamEvents_GrantFiltering opens the SSE stream as a logs-granted user
// and verifies request lifecycle events arrive while admin-operational events
// are dropped before hitting the wire.
func TestStreamEvents_GrantFiltering(t *testing.T) {
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

	events.Publish(events.Event{Type: "request.completed", Severity: "info", Message: "req done"})
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
	if !strings.Contains(body, "request.completed") {
		t.Errorf("logs-granted user missing request.completed, got: %s", body)
	}
	if strings.Contains(body, "backup.created") {
		t.Errorf("admin-operational event leaked to logs-granted user: %s", body)
	}
	if strings.Contains(body, "provider_starting") {
		t.Errorf("discovery progress leaked to logs-granted user: %s", body)
	}
}
