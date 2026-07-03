package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/audit"
	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// setupAuditTest wires the full handler with an audit recorder mounted (must
// happen before Register, which installs the middleware) plus the multi-user
// stack, over clean audit/users/keys tables.
func setupAuditTest(t *testing.T) (chi.Router, func(id string) string, func(name string, grants []string) string) {
	t.Helper()
	h := newTestHandler(t)
	pool := h.Pool().Pool()
	if _, err := pool.Exec(context.Background(), `TRUNCATE audit_log, users, webauthn_sessions, virtual_keys CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	userRepo := user.NewRepository(pool)
	webauthnRepo := webauthn.NewRepository(pool)
	sessionMgr := webauthn.NewSessionManager(webauthnRepo)
	h.SetWebAuthnSessionManager(sessionMgr)
	h.SetUserAuth(userRepo, webauthnRepo)
	h.SetAudit(audit.New(pool, nil))

	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.Register(r)

	loginAs := func(id string) string {
		token, err := sessionMgr.CreateAuthToken(context.Background(), []byte(id), nil)
		if err != nil {
			t.Fatalf("CreateAuthToken: %v", err)
		}
		return token
	}
	mkUser := func(name string, grants []string) string {
		g, _ := json.Marshal(grants)
		w := doJSON(t, r, http.MethodPost, "/users", envAdminToken,
			fmt.Sprintf(`{"username":%q,"password":"password123","role":"user","grants":%s}`, name, g))
		if w.Code != http.StatusCreated {
			t.Fatalf("create user %s: %d %s", name, w.Code, w.Body.String())
		}
		var resp struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode user: %v", err)
		}
		return resp.ID
	}
	return r, loginAs, mkUser
}

func listAudit(t *testing.T, r chi.Router, path, token string) AuditListResponse {
	t.Helper()
	w := doJSON(t, r, http.MethodGet, path, token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET %s: %d %s", path, w.Code, w.Body.String())
	}
	var resp AuditListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode audit list: %v", err)
	}
	return resp
}

func TestAudit_RecordsMutationsWithActor(t *testing.T) {
	r, loginAs, mkUser := setupAuditTest(t)

	// One admin mutation (the user create) and one non-admin mutation.
	uid := mkUser("audited-user", []string{string(user.GrantVirtualKeys)})
	userToken := loginAs(uid)
	if w := doJSON(t, r, http.MethodPost, "/virtual-keys", userToken, `{"name":"audited-key"}`); w.Code != http.StatusCreated {
		t.Fatalf("create key: %d %s", w.Code, w.Body.String())
	}
	// Reads are never audited.
	if w := doJSON(t, r, http.MethodGet, "/virtual-keys", userToken, ""); w.Code != http.StatusOK {
		t.Fatalf("list keys: %d", w.Code)
	}

	resp := listAudit(t, r, "/audit", envAdminToken)
	if len(resp.Entries) != 2 {
		t.Fatalf("entries = %d, want 2 (got %+v)", len(resp.Entries), resp.Entries)
	}
	// Newest first: the key create by the user, then the user create by the
	// env-token admin.
	keyEntry, userEntry := resp.Entries[0], resp.Entries[1]
	if keyEntry.Actor != "audited-user" || keyEntry.ActorRole != "user" ||
		keyEntry.Method != http.MethodPost || keyEntry.StatusCode != http.StatusCreated {
		t.Errorf("key entry = %+v", keyEntry)
	}
	if userEntry.Actor != "admin" || userEntry.ActorRole != "admin" ||
		userEntry.Route != "/users/" && userEntry.Route != "/users" {
		t.Errorf("user entry = %+v", userEntry)
	}
}

func TestAudit_EntityIDAndFailedAttempts(t *testing.T) {
	r, _, mkUser := setupAuditTest(t)
	uid := mkUser("entity-user", nil)

	// A mutation on a specific entity records its id.
	if w := doJSON(t, r, http.MethodPost, "/users/"+uid+"/password", envAdminToken, `{"password":"password456"}`); w.Code != http.StatusOK {
		t.Fatalf("set password: %d %s", w.Code, w.Body.String())
	}
	// An unauthenticated mutation dies at the auth gate before the recorder
	// and must not pollute the trail (asserted by the exact match below).
	if w := doJSON(t, r, http.MethodDelete, "/users/"+uid, "not-a-valid-token", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("bogus delete: %d, want 401", w.Code)
	}
	if resp := listAudit(t, r, "/audit?method=DELETE", envAdminToken); len(resp.Entries) != 0 {
		t.Errorf("unauthenticated request reached the trail: %+v", resp.Entries)
	}

	resp := listAudit(t, r, "/audit?method=POST", envAdminToken)
	var found bool
	for _, e := range resp.Entries {
		if e.EntityID == uid && e.Route == "/users/{id}/password" {
			found = true
			// The password itself must never appear anywhere in the row.
			raw, _ := json.Marshal(e)
			if strings.Contains(string(raw), "password456") {
				t.Errorf("audit row leaked request body: %s", raw)
			}
		}
	}
	if !found {
		t.Errorf("no entry with entity id %s: %+v", uid, resp.Entries)
	}
}

func TestAudit_AdminOnlyAndFilters(t *testing.T) {
	r, loginAs, mkUser := setupAuditTest(t)
	uid := mkUser("no-peek", []string{string(user.GrantLogs)})
	userToken := loginAs(uid)

	// Grant holders cannot read the trail.
	if w := doJSON(t, r, http.MethodGet, "/audit", userToken, ""); w.Code != http.StatusForbidden {
		t.Fatalf("non-admin audit read: %d, want 403", w.Code)
	}

	// Actor filter narrows to the matching rows.
	resp := listAudit(t, r, "/audit?actor=admin", envAdminToken)
	if len(resp.Entries) != 1 {
		t.Fatalf("actor filter: %d entries, want 1", len(resp.Entries))
	}
	if resp := listAudit(t, r, "/audit?actor=nobody", envAdminToken); len(resp.Entries) != 0 {
		t.Fatalf("bogus actor filter: %d entries, want 0", len(resp.Entries))
	}
}

func TestAudit_CursorPagination(t *testing.T) {
	r, _, mkUser := setupAuditTest(t)
	for i := range 5 {
		mkUser(fmt.Sprintf("page-user-%d", i), nil)
	}

	first := listAudit(t, r, "/audit?limit=2", envAdminToken)
	if len(first.Entries) != 2 || !first.HasMore || first.NextCursor == "" || first.Total != 5 {
		t.Fatalf("first page: %d entries, has_more=%v, total=%d", len(first.Entries), first.HasMore, first.Total)
	}
	seen := map[string]bool{}
	for _, e := range first.Entries {
		seen[e.ID] = true
	}
	second := listAudit(t, r, "/audit?limit=2&cursor="+first.NextCursor, envAdminToken)
	if len(second.Entries) != 2 {
		t.Fatalf("second page: %d entries", len(second.Entries))
	}
	for _, e := range second.Entries {
		if seen[e.ID] {
			t.Errorf("page overlap on %s", e.ID)
		}
		seen[e.ID] = true
	}
	third := listAudit(t, r, "/audit?limit=2&cursor="+second.NextCursor, envAdminToken)
	if len(third.Entries) != 1 || third.HasMore {
		t.Fatalf("third page: %d entries, has_more=%v", len(third.Entries), third.HasMore)
	}
}

func TestAudit_PurgeLeavesItsOwnTrail(t *testing.T) {
	r, _, mkUser := setupAuditTest(t)
	mkUser("purge-fodder", nil)

	if w := doJSON(t, r, http.MethodDelete, "/audit/purge", envAdminToken, `{"older_than":"all"}`); w.Code != http.StatusNoContent {
		t.Fatalf("purge: %d %s", w.Code, w.Body.String())
	}
	// The wipe removed everything before it, and then recorded itself: a
	// cleared trail always shows who cleared it.
	resp := listAudit(t, r, "/audit", envAdminToken)
	if len(resp.Entries) != 1 || resp.Entries[0].Route != "/audit/purge" {
		t.Fatalf("post-purge trail = %+v", resp.Entries)
	}
	// Bad vocabulary is a 400.
	if w := doJSON(t, r, http.MethodDelete, "/audit/purge", envAdminToken, `{"older_than":"yesterday"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("bad purge vocab: %d, want 400", w.Code)
	}
}

func TestAudit_RetentionPrune(t *testing.T) {
	if apiTestDB == nil {
		t.Fatal("test database not available")
	}
	pool := apiTestDB.Pool()
	if _, err := pool.Exec(context.Background(), `TRUNCATE audit_log`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	// Seed a row far past the retention window.
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO audit_log (created_at, actor, actor_role, method, route, path, status_code)
		 VALUES (NOW() - INTERVAL '10 days', 'old-actor', 'admin', 'POST', '/x', '/x', 200)`); err != nil {
		t.Fatalf("seed: %v", err)
	}

	// A recorder with a 1-day retention prunes on its first insert.
	rec := audit.New(pool, func() int { return 1 })
	req := httptest.NewRequest(http.MethodPost, "/prune-trigger", http.NoBody)
	rec.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(httptest.NewRecorder(), req)

	var oldRows int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM audit_log WHERE actor = 'old-actor'`).Scan(&oldRows); err != nil {
		t.Fatalf("count: %v", err)
	}
	if oldRows != 0 {
		t.Errorf("retention prune left %d old rows", oldRows)
	}
	// The triggering request itself was recorded and survived the prune.
	var fresh int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM audit_log WHERE path = '/prune-trigger'`).Scan(&fresh); err != nil {
		t.Fatalf("count fresh: %v", err)
	}
	if fresh != 1 {
		t.Errorf("trigger row count = %d, want 1", fresh)
	}
}

func TestAudit_SurfaceNotWired(t *testing.T) {
	// A handler without SetAudit answers 404 on both endpoints instead of
	// nil-panicking.
	h := newTestHandler(t)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.Register(r)
	if w := doJSON(t, r, http.MethodGet, "/audit", envAdminToken, ""); w.Code != http.StatusNotFound {
		t.Errorf("unwired list: %d, want 404", w.Code)
	}
	if w := doJSON(t, r, http.MethodDelete, "/audit/purge", envAdminToken, `{"older_than":"all"}`); w.Code != http.StatusNotFound {
		t.Errorf("unwired purge: %d, want 404", w.Code)
	}
}

func TestAudit_QueryParamEdges(t *testing.T) {
	r, _, mkUser := setupAuditTest(t)
	mkUser("param-user", nil)

	// Time-window filters narrow the list.
	past := time.Now().Add(-time.Minute).UTC().Format(time.RFC3339)
	future := time.Now().Add(time.Minute).UTC().Format(time.RFC3339)
	if resp := listAudit(t, r, "/audit?from="+url.QueryEscape(past)+"&to="+url.QueryEscape(future), envAdminToken); len(resp.Entries) != 1 {
		t.Errorf("window filter: %d entries, want 1", len(resp.Entries))
	}
	if resp := listAudit(t, r, "/audit?to="+url.QueryEscape(past), envAdminToken); len(resp.Entries) != 0 {
		t.Errorf("past window: %d entries, want 0", len(resp.Entries))
	}
	// Out-of-range limits are clamped rather than refused.
	if resp := listAudit(t, r, "/audit?limit=0", envAdminToken); len(resp.Entries) != 1 {
		t.Errorf("limit=0: %d entries", len(resp.Entries))
	}
	if resp := listAudit(t, r, "/audit?limit=9999", envAdminToken); len(resp.Entries) != 1 {
		t.Errorf("limit=9999: %d entries", len(resp.Entries))
	}
	// A garbage cursor is a 400.
	if w := doJSON(t, r, http.MethodGet, "/audit?cursor=%25%25not-base64", envAdminToken, ""); w.Code != http.StatusBadRequest {
		t.Errorf("bad cursor: %d, want 400", w.Code)
	}
	// A malformed purge body is a 400.
	if w := doJSON(t, r, http.MethodDelete, "/audit/purge", envAdminToken, `{not json`); w.Code != http.StatusBadRequest {
		t.Errorf("bad purge body: %d, want 400", w.Code)
	}
}
