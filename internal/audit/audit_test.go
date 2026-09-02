package audit

import (
	"context"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/clientip"
	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/user"
)

var testDB *db.DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	testDBURL, setupErr := db.SetupTestDB("audit")
	if setupErr != nil {
		log.Printf("failed to setup test DB: %v", setupErr)
		os.Exit(1)
	}
	defer db.CleanupTestDB("audit")

	var err error
	testDB, err = db.New(ctx, testDBURL, 25, 5)
	if err != nil {
		log.Printf("failed to initialize test DB: %v", err)
		os.Exit(1) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
	}
	defer testDB.Close()

	os.Exit(m.Run()) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
}

// newRecorder returns a Recorder over the package test DB with a clean
// audit_log, truncated again on cleanup so state never leaks between tests.
func newRecorder(t *testing.T, retention func() int) *Recorder {
	t.Helper()
	truncate := func() {
		_, _ = testDB.Pool().Exec(context.Background(), `TRUNCATE audit_log`)
	}
	truncate()
	t.Cleanup(truncate)
	return New(testDB.Pool(), retention)
}

// eventually polls cond until it is true or a short deadline elapses, for
// assertions on rows written by the middleware's background record goroutine.
func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within deadline")
}

func countRows(t *testing.T, where string, args ...any) int {
	t.Helper()
	var n int
	if err := testDB.Pool().QueryRow(context.Background(),
		`SELECT COUNT(*) FROM audit_log WHERE `+where, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestActorOf(t *testing.T) {
	uid := uuid.New()
	cases := []struct {
		name  string
		id    *user.Identity
		actor string
		role  string
	}{
		{"nil identity", nil, "unknown", ""},
		{"users row", &user.Identity{Role: user.RoleUser, Username: "alice", UserID: &uid}, "alice", "user"},
		{"legacy admin", user.AdminIdentity(), "admin", "admin"},
		{"anonymous admin session", &user.Identity{Role: user.RoleAdmin}, "admin", "admin"},
		{"anonymous non-admin", &user.Identity{Role: user.RoleUser}, "unknown", "user"},
	}
	for _, tc := range cases {
		actor, role := actorOf(tc.id)
		if actor != tc.actor || role != tc.role {
			t.Errorf("%s: actorOf = (%q, %q), want (%q, %q)", tc.name, actor, role, tc.actor, tc.role)
		}
	}
}

func TestMiddlewareRecordsThroughChi(t *testing.T) {
	rec := newRecorder(t, nil)
	r := chi.NewRouter()
	r.Use(rec.Middleware)
	r.Delete("/things/{id}", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	r.Get("/things/{id}", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	// A handler that writes a body without an explicit WriteHeader must be
	// recorded as 200 (the middleware defaults a 0 status to 200).
	r.Post("/implicit", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("created-ish"))
	})
	// A handler that writes nothing at all also counts as 200.
	r.Put("/silent", func(_ http.ResponseWriter, _ *http.Request) {})

	uid := uuid.New()
	ident := &user.Identity{Role: user.RoleUser, Username: "carol", UserID: &uid}
	do := func(method, path string) {
		req := httptest.NewRequest(method, path, http.NoBody)
		req = req.WithContext(user.WithIdentity(req.Context(), ident))
		r.ServeHTTP(httptest.NewRecorder(), req)
	}
	do(http.MethodDelete, "/things/9b8ff239-11a2-4c8a-b3f5-1d0c9f5c1a2b")
	do(http.MethodGet, "/things/9b8ff239-11a2-4c8a-b3f5-1d0c9f5c1a2b")
	do(http.MethodPost, "/implicit")
	do(http.MethodPut, "/silent")

	// The middleware records on a background goroutine, so wait for the three
	// mutations to land (the GET must never be recorded).
	eventually(t, func() bool { return countRows(t, "1=1") == 3 })
	if n := countRows(t, "1=1"); n != 3 {
		t.Fatalf("recorded %d rows, want 3 (GET must not be recorded)", n)
	}
	if n := countRows(t, `route = '/things/{id}' AND entity_id = '9b8ff239-11a2-4c8a-b3f5-1d0c9f5c1a2b' AND status_code = 204 AND actor = 'carol'`); n != 1 {
		t.Errorf("chi route/entity row missing")
	}
	if n := countRows(t, `path = '/implicit' AND status_code = 200`); n != 1 {
		t.Errorf("implicit-200 row missing")
	}
	if n := countRows(t, `path = '/silent' AND status_code = 200`); n != 1 {
		t.Errorf("silent-200 row missing")
	}
}

// TestMiddlewareRecordsResolvedClientIP pins remote_addr to the trusted-proxy
// resolved client address from clientip.Middleware. The raw socket peer would
// be the docker bridge gateway behind the compose NAT, which cannot answer
// "who did this" — the whole point of the column.
func TestMiddlewareRecordsResolvedClientIP(t *testing.T) {
	rec := newRecorder(t, nil)
	_, cidr, _ := net.ParseCIDR("172.16.0.0/12")
	r := chi.NewRouter()
	r.Use(clientip.Middleware([]*net.IPNet{cidr}))
	r.Use(rec.Middleware)
	r.Post("/things", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	req := httptest.NewRequest(http.MethodPost, "/things", http.NoBody)
	req = req.WithContext(user.WithIdentity(req.Context(), &user.Identity{Role: user.RoleAdmin}))
	req.RemoteAddr = "172.27.0.1:50506"
	req.Header.Set("X-Forwarded-For", "203.0.113.9")
	r.ServeHTTP(httptest.NewRecorder(), req)

	eventually(t, func() bool { return countRows(t, "1=1") == 1 })
	if n := countRows(t, `remote_addr = '203.0.113.9'`); n != 1 {
		t.Errorf("audit row must store the resolved client IP, not the socket peer")
	}
}

// TestMiddlewareRecordsNonIDEntityParam covers routes that name their entity
// parameter something other than "id" (the circuit-breaker reset uses
// {provider_id}). Without the fallback these rows land with an empty entity_id
// and the trail cannot say which provider an operator acted on. A parameter
// that names no entity ({filename}) must still record nothing rather than
// something misleading.
func TestMiddlewareRecordsNonIDEntityParam(t *testing.T) {
	rec := newRecorder(t, nil)
	r := chi.NewRouter()
	r.Use(rec.Middleware)
	r.Route("/api", func(r chi.Router) {
		r.Post("/failover-groups/circuit-breaker/{provider_id}/reset", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		r.Post("/things/{model_uuid}/act", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
		r.Delete("/backups/{filename}", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
	})

	providerID := "3f2a91c4-55de-4d7e-9d1a-6b0f7c2e8a10"
	modelUUID := "7c1d0e55-2b34-4a99-8f7e-15ab3c9d0e42"
	ident := &user.Identity{Role: user.RoleAdmin}
	do := func(method, path string) {
		req := httptest.NewRequest(method, path, http.NoBody)
		req = req.WithContext(user.WithIdentity(req.Context(), ident))
		r.ServeHTTP(httptest.NewRecorder(), req)
	}
	do(http.MethodPost, "/api/failover-groups/circuit-breaker/"+providerID+"/reset")
	do(http.MethodPost, "/api/things/"+modelUUID+"/act")
	do(http.MethodDelete, "/api/backups/model-hotel-2026-07-27.dump")

	eventually(t, func() bool { return countRows(t, "1=1") == 3 })

	if n := countRows(t, `route = '/api/failover-groups/circuit-breaker/{provider_id}/reset' AND entity_id = $1 AND status_code = 200`, providerID); n != 1 {
		t.Errorf("circuit-breaker reset row must record the provider id as entity_id")
	}
	if n := countRows(t, `route = '/api/things/{model_uuid}/act' AND entity_id = $1`, modelUUID); n != 1 {
		t.Errorf("{model_uuid} route must record the uuid as entity_id")
	}
	// entity_id is stored as NULLIF(..., ''), so "no entity" lands as NULL.
	if n := countRows(t, `route = '/api/backups/{filename}' AND entity_id IS NULL`); n != 1 {
		t.Errorf("a non-entity parameter must leave entity_id empty, not guess")
	}
}

func TestMiddlewareSkipsFleetHeartbeat(t *testing.T) {
	rec := newRecorder(t, nil)
	r := chi.NewRouter()
	r.Use(rec.Middleware)
	// Mount under /api so the resolved route pattern is /api/fleet/announce,
	// exactly as the server wires the member-side fleet handler.
	r.Route("/api", func(r chi.Router) {
		r.Post("/fleet/announce", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
		// A non-heartbeat mutation under the same prefix must still be audited.
		r.Post("/settings", func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	ident := &user.Identity{Role: user.RoleAdmin}
	do := func(path string) {
		req := httptest.NewRequest(http.MethodPost, path, http.NoBody)
		req = req.WithContext(user.WithIdentity(req.Context(), ident))
		r.ServeHTTP(httptest.NewRecorder(), req)
	}
	// Fire the heartbeat several times; none of them may be recorded.
	for range 5 {
		do("/api/fleet/announce")
	}
	do("/api/settings")

	// Only the settings mutation lands. Wait first so a would-be announce row
	// has time to appear (proving absence, not just a timing race).
	eventually(t, func() bool { return countRows(t, `path = '/api/settings'`) == 1 })
	if n := countRows(t, `path = '/api/fleet/announce'`); n != 0 {
		t.Errorf("fleet announce was audited (%d rows), want 0", n)
	}
	if n := countRows(t, "1=1"); n != 1 {
		t.Errorf("total audit rows = %d, want 1 (only /api/settings)", n)
	}
}

func TestIsAuditExempt(t *testing.T) {
	cases := map[string]bool{
		"/api/fleet/announce":         true,
		"/api/config/quota-snapshots": true,  // the quota relay, a fleet write no operator chose
		"/api/backups/prune-preview":  true,  // a read that is POST only for its body
		"/api/backups/prune":          false, // the write next to it stays audited
		"/api/backups":                false,
		"/api/settings":               false,
		"/fleet/announce":             false, // unmounted pattern is not the real route
		"/api/fleet/members":          false,
		"":                            false,
	}
	for route, want := range cases {
		if got := isAuditExempt(route); got != want {
			t.Errorf("isAuditExempt(%q) = %v, want %v", route, got, want)
		}
	}
}

func TestWaitDrainsBackgroundRecords(t *testing.T) {
	rec := newRecorder(t, nil)
	r := chi.NewRouter()
	r.Use(rec.Middleware)
	r.Post("/things", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})

	ident := &user.Identity{Role: user.RoleAdmin}
	const n = 5
	for range n {
		req := httptest.NewRequest(http.MethodPost, "/things", http.NoBody)
		req = req.WithContext(user.WithIdentity(req.Context(), ident))
		r.ServeHTTP(httptest.NewRecorder(), req)
	}

	// Wait drains the in-flight record goroutines: every row is present
	// afterwards with no polling, which is the shutdown-flush contract.
	rec.Wait()

	if got := countRows(t, "1=1"); got != n {
		t.Errorf("after Wait: %d rows, want %d", got, n)
	}
}

func TestListFiltersCursorAndLimits(t *testing.T) {
	rec := newRecorder(t, nil)
	for range 5 {
		rec.record(Entry{
			Actor: "alice", ActorRole: "user", Method: http.MethodPost,
			Route: "/x", Path: "/x", StatusCode: 201, RemoteAddr: "10.0.0.1",
		})
	}
	rec.record(Entry{
		Actor: "bob", ActorRole: "user", Method: http.MethodDelete,
		Route: "/y", Path: "/y", EntityID: uuid.NewString(), StatusCode: 204, RemoteAddr: "10.0.0.2",
	})
	ctx := context.Background()

	all, err := rec.List(ctx, ListParams{})
	if err != nil || len(all) != 6 {
		t.Fatalf("list all: %d entries, err=%v", len(all), err)
	}
	byActor, _ := rec.List(ctx, ListParams{Actor: "bob"})
	if len(byActor) != 1 || byActor[0].Method != http.MethodDelete || byActor[0].EntityID == "" {
		t.Errorf("actor filter = %+v", byActor)
	}
	byMethod, _ := rec.List(ctx, ListParams{Method: "delete"}) // case-insensitive
	if len(byMethod) != 1 {
		t.Errorf("method filter = %d entries", len(byMethod))
	}
	// A non-audited method filter is ignored rather than injected.
	bogus, _ := rec.List(ctx, ListParams{Method: "TRACE"})
	if len(bogus) != 6 {
		t.Errorf("bogus method filter = %d entries, want all 6", len(bogus))
	}
	// Time-window filters.
	windowed, _ := rec.List(ctx, ListParams{From: time.Now().Add(-time.Minute), To: time.Now().Add(time.Minute)})
	if len(windowed) != 6 {
		t.Errorf("window filter = %d entries", len(windowed))
	}
	past, _ := rec.List(ctx, ListParams{To: time.Now().Add(-time.Minute)})
	if len(past) != 0 {
		t.Errorf("past window = %d entries", len(past))
	}

	// Limit is clamped and List returns limit+1 rows for has_more detection.
	clamped, _ := rec.List(ctx, ListParams{Limit: 300})
	if len(clamped) != 6 {
		t.Errorf("clamped list = %d entries", len(clamped))
	}
	page1, _ := rec.List(ctx, ListParams{Limit: 2})
	if len(page1) != 3 { // 2 requested + 1 lookahead
		t.Fatalf("page1 = %d entries, want 3", len(page1))
	}
	page2, _ := rec.List(ctx, ListParams{Limit: 2, CursorCreatedAt: page1[1].CreatedAt, CursorID: page1[1].ID})
	if len(page2) != 3 {
		t.Fatalf("page2 = %d entries, want 3", len(page2))
	}
	for _, e := range page2 {
		if e.ID == page1[0].ID || e.ID == page1[1].ID {
			t.Errorf("cursor page overlaps: %s", e.ID)
		}
	}

	// Offset paging (page-numbered view): skipping the first two rows returns the
	// next window and never the rows it skipped.
	offsetPage, _ := rec.List(ctx, ListParams{Limit: 2, Offset: 2})
	if len(offsetPage) != 3 { // 2 requested + 1 lookahead
		t.Fatalf("offset page = %d entries, want 3", len(offsetPage))
	}
	for _, e := range offsetPage {
		if e.ID == page1[0].ID || e.ID == page1[1].ID {
			t.Errorf("offset page overlaps the first two rows: %s", e.ID)
		}
	}

	// Count honors the same filters, without the cursor.
	if n := rec.Count(ctx, ListParams{Actor: "alice"}); n != 5 {
		t.Errorf("count actor=alice = %d", n)
	}
	if n := rec.Count(ctx, ListParams{Method: "POST", From: time.Now().Add(-time.Minute), To: time.Now().Add(time.Minute)}); n != 5 {
		t.Errorf("count windowed POST = %d", n)
	}
}

func TestPurgeCutoffAndAll(t *testing.T) {
	rec := newRecorder(t, nil)
	if _, err := testDB.Pool().Exec(context.Background(),
		`INSERT INTO audit_log (created_at, actor, actor_role, method, route, path, status_code)
		 VALUES (NOW() - INTERVAL '10 days', 'old', 'admin', 'POST', '/x', '/x', 200),
		        (NOW(), 'fresh', 'admin', 'POST', '/x', '/x', 200)`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := rec.Purge(context.Background(), time.Now().Add(-24*time.Hour), false); err != nil {
		t.Fatalf("purge cutoff: %v", err)
	}
	if n := countRows(t, "1=1"); n != 1 {
		t.Fatalf("after cutoff purge: %d rows, want 1", n)
	}
	if err := rec.Purge(context.Background(), time.Time{}, true); err != nil {
		t.Fatalf("purge all: %v", err)
	}
	if n := countRows(t, "1=1"); n != 0 {
		t.Fatalf("after full purge: %d rows, want 0", n)
	}
}

func TestPruneRunsOncePerInterval(t *testing.T) {
	rec := newRecorder(t, func() int { return 1 })
	seedOld := func() {
		if _, err := testDB.Pool().Exec(context.Background(),
			`INSERT INTO audit_log (created_at, actor, actor_role, method, route, path, status_code)
			 VALUES (NOW() - INTERVAL '10 days', 'stale', 'admin', 'POST', '/x', '/x', 200)`); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	seedOld()
	rec.record(Entry{Actor: "a", ActorRole: "user", Method: "POST", Route: "/r", Path: "/r", StatusCode: 200})
	if n := countRows(t, `actor = 'stale'`); n != 0 {
		t.Fatalf("first insert did not prune: %d stale rows", n)
	}
	// Within the same interval the sweep must not run again.
	seedOld()
	rec.record(Entry{Actor: "b", ActorRole: "user", Method: "POST", Route: "/r", Path: "/r", StatusCode: 200})
	if n := countRows(t, `actor = 'stale'`); n != 1 {
		t.Errorf("second insert pruned within the throttle interval (stale rows = %d)", n)
	}
	// An invalid retention value falls back to the default window, which the
	// 10-day-old row is inside of - nothing may be deleted.
	rec2 := newRecorder(t, func() int { return -3 })
	seedOld()
	rec2.record(Entry{Actor: "c", ActorRole: "user", Method: "POST", Route: "/r", Path: "/r", StatusCode: 200})
	if n := countRows(t, `actor = 'stale'`); n != 1 {
		t.Errorf("default-retention prune deleted a row inside the window (stale rows = %d)", n)
	}
}

func TestStatusRecorderUnwrap(t *testing.T) {
	w := httptest.NewRecorder()
	sw := &statusRecorder{ResponseWriter: w}
	if sw.Unwrap() != w {
		t.Error("Unwrap did not return the underlying writer")
	}
	// Double WriteHeader keeps the first status.
	sw.WriteHeader(http.StatusTeapot)
	sw.WriteHeader(http.StatusOK)
	if sw.status != http.StatusTeapot {
		t.Errorf("status = %d, want 418", sw.status)
	}
}
