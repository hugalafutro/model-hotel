package frontdesk

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	otptotp "github.com/pquerna/otp/totp"

	"github.com/hugalafutro/model-hotel/internal/admin"
	"github.com/hugalafutro/model-hotel/internal/adminauth"
	"github.com/hugalafutro/model-hotel/internal/events"
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

const testFrontdeskToken = "test-frontdesk-token"

var memberServerSeq atomic.Int32

// systemMemberServer stands in for a member instance: it answers GET
// /api/system/ with a fleet block reporting the given is_primary plus a unique
// instance_id, which is what Front Desk's same-host repoint guard and add-time
// dedup query. Other paths return 200 empty so unrelated probes (health/
// settings) do not error. Each call gets a distinct instance_id, so two servers
// look like two different hosts unless you use systemMemberServerID.
func systemMemberServer(t *testing.T, selfReportsPrimary bool) *httptest.Server {
	t.Helper()
	id := fmt.Sprintf("iid-%d", memberServerSeq.Add(1))
	return systemMemberServerID(t, selfReportsPrimary, id)
}

// systemMemberServerID is systemMemberServer with an explicit instance_id, so
// two stand-ins can be made to look like the same physical host.
func systemMemberServerID(t *testing.T, selfReportsPrimary bool, instanceID string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/system") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"fleet":{"is_primary":%s},"instance_id":%q}`,
				strconv.FormatBool(selfReportsPrimary), instanceID)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTestServer(t *testing.T) (*Server, *Store) {
	t.Helper()
	return newTestServerUI(t, nil)
}

// newTestServerUI is newTestServer with an embedded SPA mounted, so tests can
// exercise the "/" UI surface (e.g. security headers on the framed page).
func newTestServerUI(t *testing.T, ui fs.FS) (*Server, *Store) {
	t.Helper()
	// A limit no test can reach, so unrelated suites never see a 429.
	return newTestServerLimited(t, ui, ratelimit.NewIPLimiter(1000, 1000, nil, nil))
}

// newTestServerLimited is newTestServerUI with a caller-chosen per-IP limiter,
// for tests that assert which routes the limiter actually covers.
func newTestServerLimited(t *testing.T, ui fs.FS, limiter adminauth.IPLimiterMiddleware) (*Server, *Store) {
	t.Helper()
	return newTestServerCfg(t, ui, limiter, "")
}

// newTestServerTraefikToken builds a server whose /traefik/config is bearer
// gated, for tests about the gated half of that endpoint.
func newTestServerTraefikToken(t *testing.T, token string) (*Server, *Store) {
	t.Helper()
	return newTestServerCfg(t, nil, ratelimit.NewIPLimiter(1000, 1000, nil, nil), token)
}

// newTestServerHealthzLimited builds a server whose liveness probe has its own
// budget, for the tests that assert the probe is bounded and that its flood
// does not spend the login limiter's allowance.
func newTestServerHealthzLimited(t *testing.T, healthz adminauth.IPLimiterMiddleware) (*Server, *Store) {
	t.Helper()
	return newTestServerCfgFull(t, nil, ratelimit.NewIPLimiter(1000, 1000, nil, nil), healthz, "")
}

func newTestServerCfg(t *testing.T, ui fs.FS, limiter adminauth.IPLimiterMiddleware, traefikToken string) (*Server, *Store) {
	t.Helper()
	// A probe limit no unrelated test can reach.
	return newTestServerCfgFull(t, ui, limiter, ratelimit.NewIPLimiter(1000, 1000, nil, nil), traefikToken)
}

func newTestServerCfgFull(t *testing.T, ui fs.FS, limiter, healthz adminauth.IPLimiterMiddleware, traefikToken string) (*Server, *Store) {
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
		Store:          store,
		Poller:         poller,
		Bus:            bus,
		AdminMgr:       adminMgr,
		MasterKey:      testMasterKey,
		RelyingParty:   rp,
		IPLimiter:      limiter,
		HealthzLimiter: healthz,
		UI:             ui,
		TraefikToken:   traefikToken,
	})
	// Drain any detached background goroutine (e.g. an auto-sync kick) before
	// the store and its temp dir are torn down. Registered here so it runs
	// (LIFO) ahead of the store.Close / TempDir cleanups queued earlier.
	t.Cleanup(srv.Wait)
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

// TestShutdownDrainsBackgroundBeforeClosingTheStore: the ordering Shutdown
// exists for. A tracked background goroutine finishing its last store read must
// find the store still open; closing first would leave that read querying a
// closed handle, which is the unowned-read race the rearm watcher's join avoids
// inside a pass.
func TestShutdownDrainsBackgroundBeforeClosingTheStore(t *testing.T) {
	srv, store := newTestServer(t)

	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	var readErr error
	srv.StartBackground(ctx, func(c context.Context) {
		close(started)
		<-c.Done()
		// The last read on the way out, as every poll loop does.
		_, readErr = store.AutoSyncGen(context.Background())
	})
	<-started
	cancel() // what the signal handler does before the process winds down

	if err := srv.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if readErr != nil {
		t.Errorf("background read on the way out failed: %v; the store closed before the drain finished", readErr)
	}
	if _, err := store.AutoSyncGen(context.Background()); err == nil {
		t.Error("store still usable after Shutdown; want it closed")
	}
}

// TestShutdownBoundsTheDrain: a background goroutine that ignores its
// cancellation delays exit by the caller's budget, not forever. Shutdown returns
// when the context does and closes the store regardless, so a stuck loop cannot
// hang the process.
func TestShutdownBoundsTheDrain(t *testing.T) {
	srv, store := newTestServer(t)

	release := make(chan struct{})
	releaseStuck := sync.OnceFunc(func() { close(release) })
	// The server's own Wait cleanup joins this goroutine, so it is released on
	// every exit from the test, failing ones included.
	defer releaseStuck()
	started := make(chan struct{})
	srv.StartBackground(context.Background(), func(context.Context) {
		close(started)
		<-release
	})
	<-started

	drainCtx, drainCancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer drainCancel()
	if err := srv.Shutdown(drainCtx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if _, err := store.AutoSyncGen(context.Background()); err == nil {
		t.Error("store still usable after a timed-out drain; want it closed anyway")
	}
}

// TestStartBackgroundRefusedOnceShutdownBegins: an http.Server drain returns on
// its own deadline without stopping the handlers still in flight, so a handler
// can reach a registration after Shutdown has parked its waiter. Registering then
// would trip sync.WaitGroup's "Add called concurrently with Wait" panic, so the
// answer is a refusal: the work is not started and the caller is told.
func TestStartBackgroundRefusedOnceShutdownBegins(t *testing.T) {
	srv, _ := newTestServer(t)

	if err := srv.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}

	ran := make(chan struct{})
	if srv.StartBackground(t.Context(), func(context.Context) { close(ran) }) {
		t.Error("StartBackground reported the work started after Shutdown; want a refusal")
	}
	select {
	case <-ran:
		t.Error("refused background work ran anyway")
	default:
	}

	// The timeout variant prepares a context before it registers, so a refusal has
	// to release it rather than leave it to the deadline. govet's lostcancel and
	// the race detector both watch this path; the assertion here is that the
	// refusal is reported, and that the context comes back already cancelled.
	var kickCtx context.Context
	if srv.StartBackgroundTimeout(t.Context(), time.Minute, func(c context.Context) { kickCtx = c }) {
		t.Error("StartBackgroundTimeout reported the work started after Shutdown; want a refusal")
	}
	if kickCtx != nil {
		t.Error("refused timed background work ran anyway")
	}
}

// TestShutdownWaitsForTheSSEReauthTicker: a stream's re-auth ticker reads the
// session and device tables, so the drain has to cover it. Shutdown must not
// return, and so must not close the store, while one is still ticking; the
// request context is what ends it, exactly as a disconnecting client would.
func TestShutdownWaitsForTheSSEReauthTicker(t *testing.T) {
	srv, _ := newTestServer(t)

	req, cancelReq := sseRequest(t)
	req.Header.Set("Authorization", "Bearer "+testFrontdeskToken)
	rec := newSSERecorder()
	streamDone := startStreamWith(t, srv, req, rec, sseTick)
	awaitWrite(t, rec, "keep-alive") // the ticker is running and re-checking

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- srv.Shutdown(t.Context()) }()
	// t.Context() outlives the test body, so this drain has no deadline of its own
	// to escape through: only the ticker returning can release it.
	select {
	case <-shutdownDone:
		t.Fatal("Shutdown returned while an SSE re-auth ticker was still running")
	case <-time.After(50 * time.Millisecond):
	}

	cancelReq()
	awaitClosed(t, streamDone, "after its request context was cancelled")
	if err := <-shutdownDone; err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
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
	srv, store := newTestServer(t)
	// A member is only added once it replies and verifies (token accepted, not the
	// primary), so point the add at a stand-in that answers 200 and self-reports
	// is_primary=false.
	host := systemMemberServer(t, false)
	body := func(name, url string) string {
		return fmt.Sprintf(`{"name":%q,"url":%q,"token":"tok"}`, name, url)
	}

	// Create.
	rec := do(t, srv, http.MethodPost, "/api/members", body("hotel-1", host.URL), true)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var created Member
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.ID == "" || created.Name != "hotel-1" {
		t.Fatalf("created member: %+v", created)
	}

	// Duplicate URL -> 400.
	if rec := do(t, srv, http.MethodPost, "/api/members", body("dup", host.URL), true); rec.Code != http.StatusBadRequest {
		t.Errorf("duplicate = %d, want 400", rec.Code)
	}
	// Bad URL -> 400.
	if rec := do(t, srv, http.MethodPost, "/api/members", `{"name":"x","url":"ftp://nope","token":"tok"}`, true); rec.Code != http.StatusBadRequest {
		t.Errorf("bad url = %d, want 400", rec.Code)
	}
	// Missing token -> 400 (a member must verify before it is added).
	if rec := do(t, srv, http.MethodPost, "/api/members", fmt.Sprintf(`{"name":"y","url":%q}`, host.URL), true); rec.Code != http.StatusBadRequest {
		t.Errorf("missing token = %d, want 400", rec.Code)
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

	// A second active member so the drain below is allowed: the last active member
	// cannot be drained. Added via the store to skip the add-time host handshake.
	second, err := store.CreateMember(context.Background(), "hotel-2", "http://hotel-2:8081", "")
	if err != nil {
		t.Fatalf("second member: %v", err)
	}

	// Set state to drained (allowed: hotel-2 stays active).
	if rec := do(t, srv, http.MethodPost, "/api/members/"+created.ID+"/state", `{"state":"drained"}`, true); rec.Code != http.StatusOK {
		t.Fatalf("state = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	// Draining the now-last active member (hotel-2) is refused with 409 carrying
	// the stable machine code (not just the English text) so clients route on it.
	drainRec := do(t, srv, http.MethodPost, "/api/members/"+second.ID+"/state", `{"state":"drained"}`, true)
	if drainRec.Code != http.StatusConflict {
		t.Fatalf("drain last active = %d, want 409; body=%s", drainRec.Code, drainRec.Body.String())
	}
	var coded map[string]string
	if err := json.Unmarshal(drainRec.Body.Bytes(), &coded); err != nil {
		t.Fatalf("decode 409 body: %v; body=%s", err, drainRec.Body.String())
	}
	if coded["code"] != "last_active_member" {
		t.Errorf("409 code = %q, want %q; body=%s", coded["code"], "last_active_member", drainRec.Body.String())
	}

	// The state change is attributed in its audit event. An admin bearer carries
	// no paired device, so it is recorded as the dashboard.
	rec = do(t, srv, http.MethodGet, "/api/events?type=member.state_changed", "", true)
	var stateEvents struct {
		Events []Event `json:"events"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &stateEvents)
	if len(stateEvents.Events) == 0 {
		t.Fatal("expected a member.state_changed event")
	}
	if got := stateEvents.Events[0].Metadata["initiated_by"]; got != "the dashboard" {
		t.Errorf("state_changed initiated_by = %v, want %q", got, "the dashboard")
	}

	// Delete.
	if rec := do(t, srv, http.MethodDelete, "/api/members/"+created.ID, "", true); rec.Code != http.StatusNoContent {
		t.Fatalf("delete = %d, want 204", rec.Code)
	}
	if rec := do(t, srv, http.MethodDelete, "/api/members/"+created.ID, "", true); rec.Code != http.StatusNotFound {
		t.Errorf("delete missing = %d, want 404", rec.Code)
	}
}

func TestListMembersAttachesNewestEvent(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	withEvents, err := store.CreateMember(ctx, "hotel-1", "https://h1.example", "")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	noEvents, err := store.CreateMember(ctx, "hotel-2", "https://h2.example", "")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	if _, err := store.InsertEvent(ctx, Event{
		Type: "health.up", Severity: "success", Source: "poller", Message: "up", MemberID: withEvents.ID,
	}); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}

	rec := do(t, srv, http.MethodGet, "/api/members", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/members = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var views []memberView
	if err := json.Unmarshal(rec.Body.Bytes(), &views); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byID := make(map[string]memberView, len(views))
	for _, v := range views {
		byID[v.ID] = v
	}
	if got := byID[withEvents.ID].NewestEvent; got == nil || got.Type != "health.up" {
		t.Errorf("member with events: newest_event = %+v, want type health.up", got)
	}
	// A member with no events omits the field entirely (omitempty), so a client
	// can tell a genuinely event-free member from an older Front Desk that never
	// sends it.
	if byID[noEvents.ID].NewestEvent != nil {
		t.Errorf("member with no events should omit newest_event, got %+v", byID[noEvents.ID].NewestEvent)
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

	// Called with the raw FRONTDESK_TOKEN (no server session row): the
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
	// Members point at stand-in servers that self-report is_primary=false, so the
	// authorised repoint below passes the same-host guard (they are genuinely
	// different hosts) and the test never touches the real network.
	s1 := systemMemberServer(t, false)
	s2 := systemMemberServer(t, false)
	m1, err := store.CreateMember(ctx, "hotel-1", s1.URL, "tok1")
	if err != nil {
		t.Fatalf("create m1: %v", err)
	}
	m2, err := store.CreateMember(ctx, "hotel-2", s2.URL, "tok2")
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

	// Re-selecting the same primary row with a valid token is a harmless no-op
	// server-side (the wizard blocks it client-side): the same-host guard returns
	// early without probing the host, and the primary is unchanged.
	if rec := put(`{"enabled":false,"primary_id":"` + m2.ID + `","confirm_token":"` + testFrontdeskToken + `"}`); rec.Code != http.StatusOK {
		t.Fatalf("same-row re-select = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := primaryNow(); got != m2.ID {
		t.Errorf("primary after same-row re-select: got %q, want %q", got, m2.ID)
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

// TestGetAutoSyncFleetState confirms the fleet state machine's verdict rides on
// the GET /api/fleet/autosync payload (the one endpoint Bellhop already polls):
// a confirmed-down member surfaces fleet_state=="degraded" with a "member_down"
// reason code, while an all-healthy fleet is "ok" and omits the reasons key
// entirely (omitempty).
func TestGetAutoSyncFleetState(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	m1, err := store.CreateMember(ctx, "hotel-1", "https://h1.example", "tok1")
	if err != nil {
		t.Fatalf("create m1: %v", err)
	}
	m2, err := store.CreateMember(ctx, "hotel-2", "https://h2.example", "tok2")
	if err != nil {
		t.Fatalf("create m2: %v", err)
	}

	getBody := func() map[string]any {
		t.Helper()
		rec := do(t, srv, http.MethodGet, "/api/fleet/autosync", "", true)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /api/fleet/autosync = %d; body=%s", rec.Code, rec.Body.String())
		}
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		return body
	}

	// One member confirmed down -> degraded with a member_down reason code.
	setHealth(srv, m1.ID, false)
	setHealth(srv, m2.ID, true)
	body := getBody()
	if got := body["fleet_state"]; got != "degraded" {
		t.Errorf("fleet_state = %v, want degraded", got)
	}
	reasons, ok := body["fleet_state_reasons"].([]any)
	if !ok {
		t.Fatalf("fleet_state_reasons missing or wrong type: %#v", body["fleet_state_reasons"])
	}
	found := false
	for _, r := range reasons {
		if s, _ := r.(string); s == "member_down" {
			found = true
		}
	}
	if !found {
		t.Errorf("fleet_state_reasons = %v, want to contain member_down", reasons)
	}

	// All members healthy -> ok, and the reasons key is omitted entirely.
	setHealth(srv, m1.ID, true)
	body = getBody()
	if got := body["fleet_state"]; got != "ok" {
		t.Errorf("fleet_state = %v, want ok", got)
	}
	if _, present := body["fleet_state_reasons"]; present {
		t.Errorf("fleet_state_reasons should be absent when ok, got %v", body["fleet_state_reasons"])
	}
}

// TestServerRepointSameHostRejected covers the same-host guard: repointing the
// primary onto a member row that is actually the same physical host (it
// self-reports is_primary=true) is refused with 409 even with a valid token, so
// the source of truth cannot be "changed" to itself under a different URL.
func TestServerRepointSameHostRejected(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	// m1 is the designated primary. m2 is a second member row whose host reports
	// itself as already-primary, i.e. the same instance reached under another URL.
	s1 := systemMemberServer(t, false)
	sameHost := systemMemberServer(t, true)
	m1, err := store.CreateMember(ctx, "hotel-1", s1.URL, "tok1")
	if err != nil {
		t.Fatalf("create m1: %v", err)
	}
	m2, err := store.CreateMember(ctx, "hotel-1-lan", sameHost.URL, "tok2")
	if err != nil {
		t.Fatalf("create m2: %v", err)
	}

	if rec := do(t, srv, http.MethodPut, "/api/fleet/autosync",
		`{"enabled":false,"primary_id":"`+m1.ID+`"}`, true); rec.Code != http.StatusOK {
		t.Fatalf("designate primary = %d; body=%s", rec.Code, rec.Body.String())
	}

	// Authorised repoint onto the same host -> 409, primary unchanged.
	rec := do(t, srv, http.MethodPut, "/api/fleet/autosync",
		`{"enabled":false,"primary_id":"`+m2.ID+`","confirm_token":"`+testFrontdeskToken+`"}`, true)
	if rec.Code != http.StatusConflict {
		t.Fatalf("repoint to same host = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	cfg, _ := store.GetAutoSync(ctx)
	if cfg.PrimaryID != m1.ID {
		t.Errorf("primary changed to %q despite same-host rejection, want %q", cfg.PrimaryID, m1.ID)
	}
}

func TestServerCannotDeletePrimary(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	pm, err := store.CreateMember(ctx, "primary", "https://p.example.com", "ptok")
	if err != nil {
		t.Fatalf("create primary: %v", err)
	}
	om, err := store.CreateMember(ctx, "other", "https://o.example.com", "")
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	// A third member so removing `other` is a plain removal rather than a
	// two-member disband (see TestServerDeleteFromTwoMemberFleetDisbands).
	if _, err := store.CreateMember(ctx, "third", "https://t.example.com", ""); err != nil {
		t.Fatalf("create third: %v", err)
	}

	// Designate pm as the fleet primary (first selection needs no token).
	rec := do(t, srv, http.MethodPut, "/api/fleet/autosync",
		`{"enabled":false,"primary_id":"`+pm.ID+`"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("designate primary = %d; body=%s", rec.Code, rec.Body.String())
	}

	// Deleting a non-primary member needs no token and succeeds.
	if rec := do(t, srv, http.MethodDelete, "/api/members/"+om.ID, "", true); rec.Code != http.StatusNoContent {
		t.Errorf("delete non-primary = %d, want 204", rec.Code)
	}

	// The primary is the config source of truth and cannot be removed here at all:
	// there is no token that unlocks it. It must be changed via the Fleet Sync
	// wizard (a repoint) first. Every removal attempt is refused with 409.
	if rec := do(t, srv, http.MethodDelete, "/api/members/"+pm.ID, "", true); rec.Code != http.StatusConflict {
		t.Errorf("delete primary (no body) = %d, want 409", rec.Code)
	}
	// Even the valid admin token does not unlock a primary deletion anymore.
	if rec := do(t, srv, http.MethodDelete, "/api/members/"+pm.ID, `{"confirm_token":"`+testFrontdeskToken+`"}`, true); rec.Code != http.StatusConflict {
		t.Errorf("delete primary (valid token) = %d, want 409", rec.Code)
	}

	// The primary is still there and still designated after the refused deletes.
	if _, err := store.GetMember(ctx, pm.ID); err != nil {
		t.Errorf("primary member should still exist: err=%v", err)
	}
	cfg, _ := store.GetAutoSync(ctx)
	if cfg.PrimaryID != pm.ID {
		t.Errorf("primary_id = %q after refused deletes, want %q", cfg.PrimaryID, pm.ID)
	}
}

// TestServerDeleteFromTwoMemberFleetDisbands covers the fleet-size invariant
// over HTTP: a fleet is never allowed to shrink to one member, so removing the
// non-primary of a two-member fleet removes BOTH rows, clears the designation
// and records a fleet.disbanded event.
func TestServerDeleteFromTwoMemberFleetDisbands(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	pm, err := store.CreateMember(ctx, "primary", "https://p.example.com", "ptok")
	if err != nil {
		t.Fatalf("create primary: %v", err)
	}
	om, err := store.CreateMember(ctx, "other", "https://o.example.com", "")
	if err != nil {
		t.Fatalf("create other: %v", err)
	}
	if rec := do(t, srv, http.MethodPut, "/api/fleet/autosync",
		`{"enabled":false,"primary_id":"`+pm.ID+`"}`, true); rec.Code != http.StatusOK {
		t.Fatalf("designate primary = %d; body=%s", rec.Code, rec.Body.String())
	}

	if rec := do(t, srv, http.MethodDelete, "/api/members/"+om.ID, "", true); rec.Code != http.StatusNoContent {
		t.Fatalf("disbanding delete = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}

	members, err := store.ListMembers(ctx)
	if err != nil {
		t.Fatalf("list members: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("members remain after disband: %+v", members)
	}
	cfg, _ := store.GetAutoSync(ctx)
	if cfg.Enabled || cfg.PrimaryID != "" {
		t.Errorf("auto-sync survived the disband: %+v", cfg)
	}

	rec := do(t, srv, http.MethodGet, "/api/events?type=fleet.disbanded", "", true)
	var events struct {
		Events []Event `json:"events"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &events)
	if len(events.Events) == 0 {
		t.Fatal("expected a fleet.disbanded event")
	}
	// The disband is one event, not a member.removed per row: the roster did not
	// shrink, the fleet ceased to exist.
	rec = do(t, srv, http.MethodGet, "/api/events?type=member.removed", "", true)
	events.Events = nil
	_ = json.Unmarshal(rec.Body.Bytes(), &events)
	if len(events.Events) != 0 {
		t.Errorf("disband also emitted member.removed: %+v", events.Events)
	}
}

// TestServerRefusesDesignatingPrimaryOfLoneMember: a fleet below two members is
// not allowed to exist, so the wizard cannot designate a primary while only one
// member row exists (the door that used to create wedged one-member "fleets").
func TestServerRefusesDesignatingPrimaryOfLoneMember(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	lm, err := store.CreateMember(ctx, "lone", "https://l.example.com", "ltok")
	if err != nil {
		t.Fatalf("create lone: %v", err)
	}
	rec := do(t, srv, http.MethodPut, "/api/fleet/autosync",
		`{"enabled":false,"primary_id":"`+lm.ID+`"}`, true)
	if rec.Code != http.StatusConflict {
		t.Fatalf("designate lone primary = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	// Carries a stable code so the wizard can say "add a second member" instead
	// of misreporting it as the same-host repoint guard (both are 409s).
	var coded map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &coded); err != nil || coded["code"] != "fleet_too_small" {
		t.Errorf("409 code = %q (err=%v), want fleet_too_small; body=%s", coded["code"], err, rec.Body.String())
	}
	cfg, _ := store.GetAutoSync(ctx)
	if cfg.PrimaryID != "" {
		t.Errorf("primary_id = %q, want empty after refusal", cfg.PrimaryID)
	}
}

// TestServerDeleteMembershipChangedReturns409 covers the membership-changed
// refusal over HTTP: the store's guards refuse a delete whose roster moved
// underneath it, and the server maps that to a coded 409 so the dashboard can
// tell the operator to look again (a RAISE(IGNORE) trigger plays the
// concurrent operator, exactly like the store-level test).
func TestServerDeleteMembershipChangedReturns409(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()
	m1, err := store.CreateMember(ctx, "m1", "https://m1.example.com", "")
	if err != nil {
		t.Fatalf("create m1: %v", err)
	}
	if _, err := store.CreateMember(ctx, "m2", "https://m2.example.com", ""); err != nil {
		t.Fatalf("create m2: %v", err)
	}
	if _, err := store.db.ExecContext(ctx,
		`CREATE TRIGGER boom BEFORE DELETE ON members BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	rec := do(t, srv, http.MethodDelete, "/api/members/"+m1.ID, "", true)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	var coded map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &coded); err != nil || coded["code"] != "membership_changed" {
		t.Errorf("409 code = %q (err=%v), want membership_changed; body=%s", coded["code"], err, rec.Body.String())
	}
}

// TestServerAutoSyncToggleSurvivesLegacyLoneFleet: a one-member fleet with a
// designated primary predates the two-member floor. The floor only guards NEW
// designations, so pausing/resuming auto-sync (the wizard PUTs the primary back
// unchanged) must keep working on that legacy state.
func TestServerAutoSyncToggleSurvivesLegacyLoneFleet(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	lm, err := store.CreateMember(ctx, "lone", "https://l.example.com", "ltok")
	if err != nil {
		t.Fatalf("create lone: %v", err)
	}
	// Seed the legacy designation below the handler (the door the floor closed).
	if err := store.SetAutoSync(ctx, true, lm.ID); err != nil {
		t.Fatalf("seed legacy designation: %v", err)
	}

	rec := do(t, srv, http.MethodPut, "/api/fleet/autosync",
		`{"enabled":false,"primary_id":"`+lm.ID+`"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle on legacy lone fleet = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	cfg, _ := store.GetAutoSync(ctx)
	if cfg.Enabled || cfg.PrimaryID != lm.ID {
		t.Errorf("after toggle: %+v, want disabled with the designation kept", cfg)
	}
}

// TestDeleteLastActiveMemberReturns409 covers the delete door of the routing-pool
// guard over HTTP in a 3+ fleet: with the primary and a spare drained, removing
// the sole active replica is refused with 409 carrying the stable
// last_active_member code. (At two members the same delete disbands instead.)
func TestDeleteLastActiveMemberReturns409(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()
	pm, err := store.CreateMember(ctx, "primary", "https://p.example.com", "ptok")
	if err != nil {
		t.Fatalf("create primary: %v", err)
	}
	rm, err := store.CreateMember(ctx, "replica", "https://r.example.com", "rtok")
	if err != nil {
		t.Fatalf("create replica: %v", err)
	}
	sm, err := store.CreateMember(ctx, "spare", "https://s.example.com", "")
	if err != nil {
		t.Fatalf("create spare: %v", err)
	}
	if err := store.SetMemberState(ctx, sm.ID, StateDrained); err != nil {
		t.Fatalf("drain spare: %v", err)
	}
	if rec := do(t, srv, http.MethodPut, "/api/fleet/autosync",
		`{"enabled":false,"primary_id":"`+pm.ID+`"}`, true); rec.Code != http.StatusOK {
		t.Fatalf("designate primary = %d; body=%s", rec.Code, rec.Body.String())
	}
	// Drain the primary so the replica is the only active member.
	if err := store.SetMemberState(ctx, pm.ID, StateDrained); err != nil {
		t.Fatalf("drain primary: %v", err)
	}
	rec := do(t, srv, http.MethodDelete, "/api/members/"+rm.ID, "", true)
	if rec.Code != http.StatusConflict {
		t.Fatalf("delete last active = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	var coded map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &coded); err != nil {
		t.Fatalf("decode 409 body: %v; body=%s", err, rec.Body.String())
	}
	if coded["code"] != "last_active_member" {
		t.Errorf("409 code = %q, want %q; body=%s", coded["code"], "last_active_member", rec.Body.String())
	}
}

func TestServerEventsAndStatus(t *testing.T) {
	srv, _ := newTestServer(t)

	// Creating a member emits an event (a verified add against a live stand-in).
	host := systemMemberServer(t, false)
	_ = do(t, srv, http.MethodPost, "/api/members",
		fmt.Sprintf(`{"name":"h","url":%q,"token":"tok"}`, host.URL), true)

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
	// Creating a member emits one event "now" (a verified add against a stand-in).
	host := systemMemberServer(t, false)
	_ = do(t, srv, http.MethodPost, "/api/members",
		fmt.Sprintf(`{"name":"h","url":%q,"token":"tok"}`, host.URL), true)

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

// totpCodeForStep returns a TOTP code offset by `steps` 30-second windows from
// now. Verify accepts one code per step (single use) within a skew=1 window, so
// a test that chains enroll-verify and login has to spend distinct steps:
// enroll -1, login 0. The initial wait keeps generation and server-side
// validation inside the same 30s window, so an edge step cannot fall out of the
// skew window when the clock crosses a boundary between the two reads.
func totpCodeForStep(t *testing.T, secret string, steps int) string {
	t.Helper()
	if rem := time.Until(time.Now().Truncate(30 * time.Second).Add(30 * time.Second)); rem < time.Second {
		time.Sleep(rem + 50*time.Millisecond)
	}
	midWindow := time.Now().Truncate(30 * time.Second).Add(15 * time.Second)
	code, err := otptotp.GenerateCode(secret, midWindow.Add(time.Duration(steps)*30*time.Second))
	if err != nil {
		t.Fatalf("GenerateCode(step %d): %v", steps, err)
	}
	return code
}

// fdCookies picks the Front Desk session/CSRF pair out of a response.
func fdCookies(rec *httptest.ResponseRecorder) (session, csrf *http.Cookie) {
	for _, c := range rec.Result().Cookies() {
		switch c.Name {
		case "fd_session":
			session = c
		case "fd_csrf":
			csrf = c
		}
	}
	return session, csrf
}

// TestCookieAuth_ExchangeLoginCSRFAndLogout walks the browser contract end to
// end: the exchange mints the cookie pair, the cookie authenticates reads,
// mutations demand the CSRF header, and logout clears both cookies and revokes
// the session server-side.
func TestCookieAuth_ExchangeLoginCSRFAndLogout(t *testing.T) {
	srv, _ := newTestServer(t)

	// 1. Exchange the raw token for cookies.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/auth/admin-exchange",
		strings.NewReader(`{"admin_token":"`+testFrontdeskToken+`"}`))
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("exchange: %d %s", rec.Code, rec.Body.String())
	}
	session, csrf := fdCookies(rec)
	if session == nil || !session.HttpOnly || csrf == nil || csrf.HttpOnly {
		t.Fatalf("want HttpOnly fd_session + readable fd_csrf, got %+v", rec.Result().Cookies())
	}
	if strings.Contains(rec.Body.String(), session.Value) {
		t.Fatal("session token must not be echoed in the body")
	}

	withCookies := func(r *http.Request) {
		r.AddCookie(session)
		r.AddCookie(csrf)
	}

	// 2. Cookie authenticates a read with no Authorization header.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/members", http.NoBody)
	withCookies(req)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cookie read: %d", rec.Code)
	}

	// 3. Mutation without the CSRF header is refused...
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/quota/refresh", http.NoBody)
	withCookies(req)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("mutation without CSRF header: %d, want 403", rec.Code)
	}

	// 4. ...and passes with it.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/quota/refresh", http.NoBody)
	withCookies(req)
	req.Header.Set("X-CSRF-Token", csrf.Value)
	srv.ServeHTTP(rec, req)
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
		t.Fatalf("mutation with CSRF header: %d", rec.Code)
	}

	// 5. Logout clears both cookies and revokes the session server-side.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/api/logout", http.NoBody)
	withCookies(req)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("logout: %d", rec.Code)
	}
	cleared := 0
	for _, c := range rec.Result().Cookies() {
		if (c.Name == "fd_session" || c.Name == "fd_csrf") && c.MaxAge < 0 {
			cleared++
		}
	}
	if cleared != 2 {
		t.Fatalf("logout must expire both cookies, expired %d", cleared)
	}
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/api/members", http.NoBody)
	withCookies(req)
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("revoked session still authenticates: %d", rec.Code)
	}
}

// TestCookieAuth_HeaderBearerPathUnaffected pins the header-bearer contract for
// raw FRONTDESK_TOKEN M2M callers: an Authorization header alone still mutates,
// with no CSRF header. (Paired-device bearers take the same branch; their
// coverage lives in the device tests.)
func TestCookieAuth_HeaderBearerPathUnaffected(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := do(t, srv, http.MethodPost, "/api/quota/refresh", "", true)
	if rec.Code == http.StatusForbidden || rec.Code == http.StatusUnauthorized {
		t.Fatalf("header bearer mutation without CSRF: %d, want success", rec.Code)
	}
}

// TestLogout_WorksUnauthenticated pins logout as auth-exempt: an expired or
// absent session can still log out, so the SPA always converges on the login
// screen instead of getting stuck on a 401. A caller presenting no credential
// at all also gets no Set-Cookie back: that request is indistinguishable from a
// cross-site POST (SameSite=Strict strips both cookies), so emitting cookie
// deletions for it would hand any third-party page a forced logout.
func TestLogout_WorksUnauthenticated(t *testing.T) {
	srv, _ := newTestServer(t)
	rec := do(t, srv, http.MethodPost, "/api/logout", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("unauthenticated logout: %d, want 200", rec.Code)
	}
	if got := rec.Result().Cookies(); len(got) != 0 {
		t.Fatalf("credential-less logout must emit no Set-Cookie, got %+v", got)
	}
}

// TestTotpLogin_CookieMode_NoTokenInBody drives the real TOTP enroll/login
// ceremony over the Front Desk router and pins the cookie contract: the session
// arrives as an HttpOnly fd_session cookie and never in the JSON body.
func TestTotpLogin_CookieMode_NoTokenInBody(t *testing.T) {
	srv, _ := newTestServer(t)

	rec := do(t, srv, http.MethodPost, "/api/totp/enroll/start", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("enroll/start: %d %s", rec.Code, rec.Body.String())
	}
	var start map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &start); err != nil {
		t.Fatalf("decode enroll/start: %v", err)
	}
	secret := start["secret"]
	if secret == "" {
		t.Fatalf("enroll/start returned no secret: %s", rec.Body.String())
	}

	rec = do(t, srv, http.MethodPost, "/api/totp/enroll/verify",
		`{"code":"`+totpCodeForStep(t, secret, -1)+`"}`, true)
	if rec.Code != http.StatusOK {
		t.Fatalf("enroll/verify: %d %s", rec.Code, rec.Body.String())
	}

	rec = do(t, srv, http.MethodPost, "/api/totp/login",
		`{"token":"`+testFrontdeskToken+`","code":"`+totpCodeForStep(t, secret, 0)+`"}`, false)
	if rec.Code != http.StatusOK {
		t.Fatalf("totp login: %d %s", rec.Code, rec.Body.String())
	}
	sessionCookie, _ := fdCookies(rec)
	if sessionCookie == nil || sessionCookie.Value == "" || !sessionCookie.HttpOnly {
		t.Fatalf("TOTP login must set an HttpOnly fd_session cookie, got %+v", rec.Result().Cookies())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("login body: %v", err)
	}
	if _, hasToken := body["token"]; hasToken {
		t.Fatal("cookie-mode login body must not carry the session token")
	}
	// The cookie is a working session, not just a well-shaped one.
	req := httptest.NewRequest(http.MethodGet, "/api/members", http.NoBody)
	req.AddCookie(sessionCookie)
	authed := httptest.NewRecorder()
	srv.ServeHTTP(authed, req)
	if authed.Code != http.StatusOK {
		t.Fatalf("fd_session from TOTP login does not authenticate: %d", authed.Code)
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
