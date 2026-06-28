package frontdesk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/admin"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

const testFrontdeskToken = "test-frontdesk-token"

func newTestServer(t *testing.T) (*Server, *Store) {
	t.Helper()
	store := newTestStore(t)
	bus := events.NewBus()
	poller := NewPoller(store, bus, "")

	adminMgr, _, err := admin.New(t.TempDir(), testFrontdeskToken)
	if err != nil {
		t.Fatalf("admin.New: %v", err)
	}
	rp, err := webauthn.NewRelyingParty("localhost", "Front Desk", []string{"http://localhost"})
	if err != nil {
		t.Fatalf("NewRelyingParty: %v", err)
	}
	srv := NewServer(ServerConfig{
		Store:        store,
		Poller:       poller,
		Bus:          bus,
		AdminMgr:     adminMgr,
		MasterKey:    testMasterKey,
		RelyingParty: rp,
		IPLimiter:    ratelimit.NewIPLimiter(1000, 1000, nil, nil),
	})
	return srv, store
}

// do issues a request against the server, optionally with the admin bearer token.
func do(t *testing.T, srv *Server, method, path, body string, auth bool) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *strings.Reader
	if body == "" {
		rdr = strings.NewReader("")
	} else {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if auth {
		req.Header.Set("Authorization", "Bearer "+testFrontdeskToken)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestServerAuthGate(t *testing.T) {
	srv, _ := newTestServer(t)

	// No token: control-plane endpoints are 401.
	if rec := do(t, srv, http.MethodGet, "/api/members", "", false); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauth /api/members = %d, want 401", rec.Code)
	}
	// Wrong token: 401.
	req := httptest.NewRequest(http.MethodGet, "/api/members", http.NoBody)
	req.Header.Set("Authorization", "Bearer nope")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("wrong-token /api/members = %d, want 401", rec.Code)
	}
	// Correct token: 200.
	if rec := do(t, srv, http.MethodGet, "/api/members", "", true); rec.Code != http.StatusOK {
		t.Errorf("auth /api/members = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestServerTraefikConfigUnauthenticatedAndRecordsPoll(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()
	_, _ = store.CreateMember(ctx, "a", "http://a:8081", "")
	_, _ = store.CreateMember(ctx, "b", "http://b:8081", "")
	if err := store.SetMemberState(ctx, mustMemberID(t, store, "http://b:8081"), StateDrained); err != nil {
		t.Fatal(err)
	}

	// Unauthenticated access is allowed (compose-internal endpoint).
	rec := do(t, srv, http.MethodGet, "/traefik/config", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("/traefik/config = %d, want 200", rec.Code)
	}
	var cfg TraefikConfig
	if err := json.Unmarshal(rec.Body.Bytes(), &cfg); err != nil {
		t.Fatalf("decode config: %v", err)
	}
	if got := len(cfg.HTTP.Services[traefikServiceName].LoadBalancer.Servers); got != 1 {
		t.Errorf("expected 1 active server (b is drained), got %d", got)
	}

	// The poll was recorded (watchdog won't immediately fire).
	srv.poller.mu.RLock()
	recorded := !srv.poller.lastConfigPollAt.IsZero()
	srv.poller.mu.RUnlock()
	if !recorded {
		t.Error("handleTraefikConfig should record the poll time")
	}
}

func TestServerMemberCRUD(t *testing.T) {
	srv, _ := newTestServer(t)

	// Create.
	rec := do(t, srv, http.MethodPost, "/api/members", `{"name":"hotel-1","url":"http://h1:8081"}`, true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var created Member
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == "" || created.Name != "hotel-1" {
		t.Fatalf("created member: %+v", created)
	}

	// Duplicate URL -> 400.
	if rec := do(t, srv, http.MethodPost, "/api/members", `{"name":"dup","url":"http://h1:8081"}`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("duplicate = %d, want 400", rec.Code)
	}
	// Bad URL -> 400.
	if rec := do(t, srv, http.MethodPost, "/api/members", `{"name":"x","url":"ftp://nope"}`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("bad url = %d, want 400", rec.Code)
	}

	// List shows it with a status field.
	rec = do(t, srv, http.MethodGet, "/api/members", "", true)
	var views []memberView
	_ = json.Unmarshal(rec.Body.Bytes(), &views)
	if len(views) != 1 || views[0].Name != "hotel-1" {
		t.Fatalf("list: %+v", views)
	}

	// Rename via PATCH.
	if rec := do(t, srv, http.MethodPatch, "/api/members/"+created.ID, `{"name":"renamed"}`, true); rec.Code != http.StatusOK {
		t.Fatalf("patch = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// Set state to drained.
	if rec := do(t, srv, http.MethodPost, "/api/members/"+created.ID+"/state", `{"state":"drained"}`, true); rec.Code != http.StatusOK {
		t.Fatalf("state = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// Delete.
	if rec := do(t, srv, http.MethodDelete, "/api/members/"+created.ID, "", true); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204", rec.Code)
	}
	if rec := do(t, srv, http.MethodDelete, "/api/members/"+created.ID, "", true); rec.Code != http.StatusNotFound {
		t.Errorf("delete missing = %d, want 404", rec.Code)
	}
}

func TestServerSettings(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := do(t, srv, http.MethodGet, "/api/settings", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("get settings = %d", rec.Code)
	}
	body := `{"health_poll_secs":9,"traefik_poll_secs":9,"traefik_stale_secs":40,"event_retention_days":30,"retry_attempts":3,"session_idle_timeout_minutes":30}`
	if rec := do(t, srv, http.MethodPut, "/api/settings", body, true); rec.Code != http.StatusOK {
		t.Fatalf("put settings = %d; body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, srv, http.MethodGet, "/api/settings", "", true)
	var got Settings
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.HealthPollSecs != 9 || got.RetryAttempts != 3 || got.SessionIdleTimeoutMinutes != 30 {
		t.Errorf("settings not updated: %+v", got)
	}

	// Out-of-range session idle timeout -> 400 (bounds 0..240).
	if rec := do(t, srv, http.MethodPut, "/api/settings", `{"health_poll_secs":9,"traefik_poll_secs":9,"traefik_stale_secs":40,"event_retention_days":30,"retry_attempts":3,"session_idle_timeout_minutes":241}`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("session_idle_timeout_minutes=241 = %d, want 400", rec.Code)
	}

	// A JSON null for the int field is a no-op in encoding/json: it must preserve
	// the stored value (30 above), NOT silently zero it to "never auto-logout".
	// This is the partial-merge contract; an omitted field behaves identically.
	if rec := do(t, srv, http.MethodPut, "/api/settings", `{"session_idle_timeout_minutes":null}`, true); rec.Code != http.StatusOK {
		t.Fatalf("put null session timeout = %d; body=%s", rec.Code, rec.Body.String())
	}
	rec = do(t, srv, http.MethodGet, "/api/settings", "", true)
	got = Settings{}
	_ = json.Unmarshal(rec.Body.Bytes(), &got)
	if got.SessionIdleTimeoutMinutes != 30 {
		t.Errorf("null preserved value: got %d, want 30 (unchanged)", got.SessionIdleTimeoutMinutes)
	}

	// Invalid settings -> 400.
	if rec := do(t, srv, http.MethodPut, "/api/settings", `{"health_poll_secs":0,"traefik_poll_secs":1,"traefik_stale_secs":1,"event_retention_days":1,"retry_attempts":1}`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("invalid settings = %d, want 400", rec.Code)
	}
}

func TestServerLogout(t *testing.T) {
	srv, _ := newTestServer(t)

	// Unauthenticated logout is refused by the auth gate.
	if rec := do(t, srv, http.MethodPost, "/api/logout", "", false); rec.Code != http.StatusUnauthorized {
		t.Errorf("unauth logout = %d, want 401", rec.Code)
	}

	// Authenticated with the raw FRONTDESK_TOKEN (no server session row): the
	// revoke is a harmless no-op and the route still returns 200 success.
	rec := do(t, srv, http.MethodPost, "/api/logout", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout = %d; body=%s", rec.Code, rec.Body.String())
	}
	var resp map[string]bool
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse logout response: %v", err)
	}
	if !resp["success"] {
		t.Errorf("logout success = %v, want true", resp["success"])
	}
}

func TestServerAutoSyncPrimaryGate(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()
	m1, err := store.CreateMember(ctx, "hotel-1", "https://h1.example.com", "tok1")
	if err != nil {
		t.Fatalf("create m1: %v", err)
	}
	m2, err := store.CreateMember(ctx, "hotel-2", "https://h2.example.com", "tok2")
	if err != nil {
		t.Fatalf("create m2: %v", err)
	}

	put := func(body string) *httptest.ResponseRecorder {
		return do(t, srv, http.MethodPut, "/api/fleet/autosync", body, true)
	}
	primaryNow := func() string {
		rec := do(t, srv, http.MethodGet, "/api/fleet/autosync", "", true)
		var cfg struct {
			PrimaryID string `json:"primary_id"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &cfg)
		return cfg.PrimaryID
	}

	// First selection (none configured yet) needs no confirmation.
	if rec := put(`{"enabled":false,"primary_id":"` + m1.ID + `"}`); rec.Code != http.StatusOK {
		t.Fatalf("initial primary = %d; body=%s", rec.Code, rec.Body.String())
	}

	// Repointing an already-set primary without the admin token is refused.
	if rec := put(`{"enabled":false,"primary_id":"` + m2.ID + `"}`); rec.Code != http.StatusForbidden {
		t.Errorf("repoint without token = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	// A wrong token is equally refused.
	if rec := put(`{"enabled":false,"primary_id":"` + m2.ID + `","confirm_token":"nope"}`); rec.Code != http.StatusForbidden {
		t.Errorf("repoint wrong token = %d, want 403", rec.Code)
	}
	if got := primaryNow(); got != m1.ID {
		t.Errorf("primary changed despite refusal: got %q, want %q", got, m1.ID)
	}
	// The correct admin token lets the repoint through.
	if rec := put(`{"enabled":false,"primary_id":"` + m2.ID + `","confirm_token":"` + testFrontdeskToken + `"}`); rec.Code != http.StatusOK {
		t.Fatalf("repoint with token = %d; body=%s", rec.Code, rec.Body.String())
	}
	if got := primaryNow(); got != m2.ID {
		t.Errorf("primary after confirmed repoint: got %q, want %q", got, m2.ID)
	}

	// Clearing the primary is gated the same way.
	if rec := put(`{"enabled":false,"primary_id":""}`); rec.Code != http.StatusForbidden {
		t.Errorf("clear without token = %d, want 403", rec.Code)
	}
	if rec := put(`{"enabled":false,"primary_id":"","confirm_token":"` + testFrontdeskToken + `"}`); rec.Code != http.StatusOK {
		t.Fatalf("clear with token = %d; body=%s", rec.Code, rec.Body.String())
	}
	if got := primaryNow(); got != "" {
		t.Errorf("primary after confirmed clear: got %q, want empty", got)
	}
}

func TestServerEventsAndStatus(t *testing.T) {
	srv, _ := newTestServer(t)

	// Creating a member emits an event.
	_ = do(t, srv, http.MethodPost, "/api/members", `{"name":"h","url":"http://h:8081"}`, true)

	rec := do(t, srv, http.MethodGet, "/api/events", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("events = %d", rec.Code)
	}
	var resp struct {
		Events []Event `json:"events"`
		Total  int     `json:"total"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Total < 1 || len(resp.Events) < 1 {
		t.Errorf("expected at least one event, got %+v", resp)
	}

	// traefik-status returns the (empty) poller snapshot without error.
	if rec := do(t, srv, http.MethodGet, "/api/traefik-status", "", true); rec.Code != http.StatusOK {
		t.Errorf("traefik-status = %d, want 200", rec.Code)
	}
}

func TestClampEventsLimit(t *testing.T) {
	cases := []struct{ in, want int }{
		{-1, defaultEventsLimit},
		{0, defaultEventsLimit},
		{1, 1},
		{100, 100},
		{maxEventsLimit, maxEventsLimit},
		{maxEventsLimit + 1, maxEventsLimit},
		{100000, maxEventsLimit},
	}
	for _, c := range cases {
		if got := clampEventsLimit(c.in); got != c.want {
			t.Errorf("clampEventsLimit(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestServerEventsTimeFilter(t *testing.T) {
	srv, _ := newTestServer(t)
	// Creating a member emits one event "now".
	_ = do(t, srv, http.MethodPost, "/api/members", `{"name":"h","url":"http://h:8081"}`, true)

	count := func(query string) int {
		rec := do(t, srv, http.MethodGet, "/api/events?"+query, "", true)
		if rec.Code != http.StatusOK {
			t.Fatalf("events = %d", rec.Code)
		}
		var resp struct {
			Total int `json:"total"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &resp)
		return resp.Total
	}

	future := url.QueryEscape(time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
	past := url.QueryEscape(time.Now().Add(-time.Hour).UTC().Format(time.RFC3339))

	if n := count("since=" + past); n < 1 {
		t.Errorf("since=past should include the event, got %d", n)
	}
	if n := count("since=" + future); n != 0 {
		t.Errorf("since=future should exclude the event, got %d", n)
	}
	if n := count("until=" + past); n != 0 {
		t.Errorf("until=past should exclude the event, got %d", n)
	}
	// A malformed bound is ignored (treated as no bound), not an error.
	if n := count("since=not-a-time"); n < 1 {
		t.Errorf("malformed since should be ignored, got %d", n)
	}
}

func TestServerWebAuthnAvailablePublic(t *testing.T) {
	srv, _ := newTestServer(t)
	// The login-surface availability probe is public (no auth) and reports the RP.
	rec := do(t, srv, http.MethodGet, "/api/webauthn/available", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("/api/webauthn/available = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"enabled":true`) {
		t.Errorf("expected enabled:true, got %s", rec.Body.String())
	}
}

func mustMemberID(t *testing.T, store *Store, url string) string {
	t.Helper()
	members, err := store.ListMembers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range members {
		if m.URL == url {
			return m.ID
		}
	}
	t.Fatalf("member with url %q not found", url)
	return ""
}
