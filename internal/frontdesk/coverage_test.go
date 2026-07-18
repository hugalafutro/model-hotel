package frontdesk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/hugalafutro/model-hotel/internal/events"
)

// TestPollerRunTicksAndStops exercises Run + tickLoop and one tick of every poll
// loop, then confirms Run returns once the context is cancelled.
func TestPollerRunTicksAndStops(t *testing.T) {
	traefik := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("{}"))
	}))
	defer traefik.Close()

	member := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == memberHealthPath {
			_, _ = w.Write([]byte("OK"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer member.Close()

	p, store, _ := newTestPoller(t, traefik.URL)
	if _, err := store.CreateMember(context.Background(), "m1", member.URL, ""); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after the context was cancelled")
	}
	if snap := p.Snapshot(); len(snap) == 0 {
		t.Error("expected at least one member status after a health tick")
	}
}

// TestPollerPollsHandleStoreErrors confirms the poll loops degrade gracefully
// (log and return, never panic) when the store is unavailable, and that
// settings() falls back to defaults.
func TestPollerPollsHandleStoreErrors(t *testing.T) {
	p, store, _ := newTestPoller(t, "")
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	ctx := context.Background()
	p.PollHealthOnce(ctx)
	p.PollVersionsOnce(ctx)
	p.PollTraefikOnce(ctx)
	p.checkConfigStaleness(ctx)
	if s := p.settings(ctx); s.HealthPollSecs == 0 {
		t.Error("settings() should fall back to defaults when the store errors")
	}
}

// TestPollHealthOnceReportsDown covers the per-member health loop and the
// down-transition event path. The threshold is set to 1 so a single poll
// confirms down (the debounce itself is covered in poller_test.go).
func TestPollHealthOnceReportsDown(t *testing.T) {
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer down.Close()

	p, store, _ := newTestPoller(t, "")
	ctx := context.Background()
	m, err := store.CreateMember(ctx, "m1", down.URL, "")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	set, err := store.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	set.HealthFailThreshold = 1
	if err := store.UpdateSettings(ctx, set); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}

	p.PollHealthOnce(ctx)

	st, ok := p.Snapshot()[m.ID]
	if !ok || !st.Health.Known || st.Health.Healthy {
		t.Fatalf("expected a known-unhealthy status, got %+v (ok=%v)", st, ok)
	}
	evs, _, err := store.ListEvents(ctx, EventFilter{Type: "health.down", Limit: 10})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) == 0 {
		t.Error("expected a health.down event to be recorded")
	}
}

// TestServerSettingsEventsAndMemberMutations drives the settings, events, and
// member-mutation handlers (happy and error paths) over HTTP.
func TestServerSettingsEventsAndMemberMutations(t *testing.T) {
	srv, store := newTestServer(t)

	// An add now requires a verified reply: point it at a stand-in that answers
	// the token probe and self-reports is_primary=false.
	host := systemMemberServer(t, false)
	rec := do(t, srv, http.MethodPost, "/api/members",
		fmt.Sprintf(`{"name":"m1","url":%q,"token":"tok"}`, host.URL), true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create member = %d: %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created member: %v", err)
	}
	id := created.ID

	if rec := do(t, srv, http.MethodGet, "/api/settings", "", true); rec.Code != http.StatusOK {
		t.Errorf("get settings = %d", rec.Code)
	}
	if rec := do(t, srv, http.MethodPut, "/api/settings",
		`{"health_poll_secs":10,"traefik_poll_secs":10,"traefik_stale_secs":30,"event_retention_days":30,"retry_attempts":3}`,
		true); rec.Code != http.StatusOK {
		t.Errorf("put settings = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, srv, http.MethodPut, "/api/settings", `{not json`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("put settings (bad json) = %d, want 400", rec.Code)
	}
	if rec := do(t, srv, http.MethodPut, "/api/settings",
		`{"health_poll_secs":0,"traefik_poll_secs":10,"traefik_stale_secs":30}`, true); rec.Code == http.StatusOK {
		t.Errorf("put settings (below-minimum) should be rejected, got %d", rec.Code)
	}

	if rec := do(t, srv, http.MethodGet, "/api/events?limit=10&type=member.added", "", true); rec.Code != http.StatusOK {
		t.Errorf("list events = %d", rec.Code)
	}

	if rec := do(t, srv, http.MethodPatch, "/api/members/"+id, `{"name":"renamed"}`, true); rec.Code != http.StatusOK {
		t.Errorf("patch member = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, srv, http.MethodPatch, "/api/members/"+id, `{"token":""}`, true); rec.Code != http.StatusOK {
		t.Errorf("patch member (clear token) = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, srv, http.MethodPatch, "/api/members/does-not-exist", `{"name":"x"}`, true); rec.Code == http.StatusOK {
		t.Errorf("patch missing member should fail, got %d", rec.Code)
	}
	// A second active member so draining m1 is allowed (the last active member
	// cannot be drained). Added via the store to skip the add-time host handshake.
	if _, err := store.CreateMember(context.Background(), "m2", "http://m2:8081", ""); err != nil {
		t.Fatalf("second member: %v", err)
	}
	if rec := do(t, srv, http.MethodPost, "/api/members/"+id+"/state", `{"state":"drained"}`, true); rec.Code != http.StatusOK {
		t.Errorf("set member state = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, srv, http.MethodGet, "/api/traefik-status", "", true); rec.Code != http.StatusOK {
		t.Errorf("traefik status = %d", rec.Code)
	}
	if rec := do(t, srv, http.MethodDelete, "/api/members/"+id, "", true); rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
		t.Errorf("delete member = %d", rec.Code)
	}
}

// TestServerAccessorsAndHelpers covers the small exported accessors and helpers.
func TestServerAccessorsAndHelpers(t *testing.T) {
	srv, store := newTestServer(t)
	if srv.SessionManager() == nil {
		t.Error("SessionManager() returned nil")
	}
	if store.DB() == nil {
		t.Error("DB() returned nil")
	}
	// No real SPA is embedded in tests (only the placeholder), so this is nil; the
	// call still exercises the function.
	_ = EmbeddedUI()

	if got := atoiDefault("", 5); got != 5 {
		t.Errorf("atoiDefault(\"\", 5) = %d, want 5", got)
	}
	if got := atoiDefault("notanumber", 7); got != 7 {
		t.Errorf("atoiDefault(invalid, 7) = %d, want 7", got)
	}
	if got := atoiDefault("42", 5); got != 42 {
		t.Errorf("atoiDefault(\"42\", 5) = %d, want 42", got)
	}
}

// TestTOTPStoreDisable covers TOTPStore.Disable, which runs its delete
// transaction even when nothing is enrolled.
func TestTOTPStoreDisable(t *testing.T) {
	store := newTestStore(t)
	if err := NewTOTPStore(store).Disable(context.Background()); err != nil {
		t.Fatalf("Disable: %v", err)
	}
}

// TestServerHandlersErrorWhenStoreClosed drives every control-plane handler with
// a dead store so the writeError/error branches (not hit on the happy path) run.
func TestServerHandlersErrorWhenStoreClosed(t *testing.T) {
	srv, store := newTestServer(t)
	m, err := store.CreateMember(context.Background(), "m1", "http://m1:8081", "")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close store: %v", err)
	}

	cases := []struct {
		method, path, body string
		auth               bool
	}{
		{http.MethodGet, "/api/members", "", true},
		// Token present so the handler passes the token-required check and reaches
		// the (dead) store, exercising createMember's writeError branch.
		{http.MethodPost, "/api/members", `{"name":"x","url":"http://x:8081","token":"tok"}`, true},
		{http.MethodPatch, "/api/members/" + m.ID, `{"name":"y"}`, true},
		{http.MethodPost, "/api/members/" + m.ID + "/state", `{"state":"drained"}`, true},
		{http.MethodDelete, "/api/members/" + m.ID, "", true},
		{http.MethodGet, "/api/settings", "", true},
		{http.MethodPut, "/api/settings", `{"health_poll_secs":1,"traefik_poll_secs":1,"traefik_stale_secs":1}`, true},
		{http.MethodGet, "/api/events", "", true},
		{http.MethodPost, "/api/config/sync", `{"primary_id":"` + m.ID + `"}`, true},
		{http.MethodGet, "/traefik/config", "", false}, // unauthenticated, compose-internal
	}
	for _, c := range cases {
		rec := do(t, srv, c.method, c.path, c.body, c.auth)
		if rec.Code < 400 {
			t.Errorf("%s %s with a dead store = %d, want an error status", c.method, c.path, rec.Code)
		}
	}
}

// TestHandleTraefikConfig covers the unauthenticated, compose-internal config
// endpoint that Traefik's HTTP provider polls.
func TestHandleTraefikConfig(t *testing.T) {
	srv, store := newTestServer(t)
	if _, err := store.CreateMember(context.Background(), "m1", "http://m1:8081", ""); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	rec := do(t, srv, http.MethodGet, "/traefik/config", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("traefik config = %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "http") {
		t.Errorf("unexpected traefik config body: %s", rec.Body.String())
	}
}

// TestSSEStreamsAndStops covers the SSE handler: it sets up the stream, delivers
// a published event, and returns when the request context is cancelled.
func TestSSEStreamsAndStops(t *testing.T) {
	srv, _ := newTestServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/sse", http.NoBody).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+testFrontdeskToken)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		srv.ServeHTTP(rec, req)
		close(done)
	}()

	// Give the handler time to subscribe, then publish an event so the data
	// branch runs, then cancel so the handler exits via ctx.Done().
	time.Sleep(40 * time.Millisecond)
	srv.bus.Publish(events.Event{Type: "member.added", Severity: "info", Source: "test", Message: "hi"})
	time.Sleep(40 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sse did not return after context cancel")
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content-type = %q, want text/event-stream", ct)
	}
}

// TestSPAHandlerServesAssetsAndIndex covers spaHandler: a concrete asset is
// served directly, and any unknown route falls back to index.html.
func TestSPAHandlerServesAssetsAndIndex(t *testing.T) {
	fsys := fstest.MapFS{
		"index.html":    &fstest.MapFile{Data: []byte("<!doctype html><title>fd</title>")},
		"assets/app.js": &fstest.MapFile{Data: []byte("console.log(1)")},
	}
	h := spaHandler(fsys)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app.js", http.NoBody))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "console.log") {
		t.Errorf("asset request = %d body=%q", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/some/spa/route", http.NoBody))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<title>fd</title>") {
		t.Errorf("spa fallback = %d body=%q", rec.Code, rec.Body.String())
	}
}
