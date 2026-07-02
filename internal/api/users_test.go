package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/user"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// setupUsersTest wires the full multi-user stack (real Postgres repos, real
// session manager) behind the API router, mirroring main.go's wiring.
func setupUsersTest(t *testing.T) (chi.Router, *user.Repository, *webauthn.SessionManager) {
	t.Helper()
	h, r := newTestHandlerWithRouter(t)

	pool := h.Pool().Pool()
	if _, err := pool.Exec(context.Background(), `TRUNCATE users, webauthn_sessions CASCADE`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	userRepo := user.NewRepository(pool)
	webauthnRepo := webauthn.NewRepository(pool)
	sessionMgr := webauthn.NewSessionManager(webauthnRepo)
	h.SetWebAuthnSessionManager(sessionMgr)
	h.SetUserAuth(userRepo, webauthnRepo)
	return r, userRepo, sessionMgr
}

// doJSON performs an authenticated request and returns the recorder.
func doJSON(t *testing.T, r chi.Router, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequest(method, path, http.NoBody)
	} else {
		req = httptest.NewRequest(method, path, bytes.NewReader([]byte(body)))
	}
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

const envAdminToken = "test-admin-token"

// createUserViaAPI creates a user as the env admin and returns its ID.
func createUserViaAPI(t *testing.T, r chi.Router, username, password, role string, grants []string) string {
	t.Helper()
	g, _ := json.Marshal(grants)
	body := fmt.Sprintf(`{"username":%q,"display_name":"Test","password":%q,"role":%q,"grants":%s}`, username, password, role, g)
	w := doJSON(t, r, http.MethodPost, "/users", envAdminToken, body)
	if w.Code != http.StatusCreated {
		t.Fatalf("create user: status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("bad create body: %v", err)
	}
	return resp.ID
}

func TestUsersAPI_AdminCRUD(t *testing.T) {
	r, _, _ := setupUsersTest(t)

	id := createUserViaAPI(t, r, "bob", "password123", "user", []string{"chat", "logs"})

	// Password hash must never serialize.
	w := doJSON(t, r, http.MethodGet, "/users", envAdminToken, "")
	if w.Code != http.StatusOK {
		t.Fatalf("list: %d", w.Code)
	}
	if bytes.Contains(w.Body.Bytes(), []byte("argon2")) || bytes.Contains(w.Body.Bytes(), []byte("password_hash")) {
		t.Fatal("password hash leaked in list response")
	}

	// Duplicate username -> 409.
	g, _ := json.Marshal([]string{})
	dup := fmt.Sprintf(`{"username":"bob","password":"password123","role":"user","grants":%s}`, g)
	if w := doJSON(t, r, http.MethodPost, "/users", envAdminToken, dup); w.Code != http.StatusConflict {
		t.Errorf("duplicate create: %d, want 409", w.Code)
	}

	// Validation failures.
	for name, body := range map[string]string{
		"unknown grant":  `{"username":"x","password":"password123","role":"user","grants":["nonsense"]}`,
		"bad role":       `{"username":"x","password":"password123","role":"root","grants":[]}`,
		"short password": `{"username":"x","password":"short","role":"user","grants":[]}`,
		"no username":    `{"username":"","password":"password123","role":"user","grants":[]}`,
	} {
		if w := doJSON(t, r, http.MethodPost, "/users", envAdminToken, body); w.Code != http.StatusBadRequest {
			t.Errorf("%s: %d, want 400", name, w.Code)
		}
	}

	// Update flips role and grants.
	up := `{"username":"bob","display_name":"Bobby","role":"user","grants":["usage"],"enabled":true}`
	w = doJSON(t, r, http.MethodPut, "/users/"+id, envAdminToken, up)
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d, body %s", w.Code, w.Body.String())
	}
	var updated struct {
		DisplayName string   `json:"display_name"`
		Grants      []string `json:"grants"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.DisplayName != "Bobby" || len(updated.Grants) != 1 || updated.Grants[0] != "usage" {
		t.Errorf("update not applied: %+v", updated)
	}

	// Delete -> 204, then 404.
	if w := doJSON(t, r, http.MethodDelete, "/users/"+id, envAdminToken, ""); w.Code != http.StatusNoContent {
		t.Errorf("delete: %d", w.Code)
	}
	if w := doJSON(t, r, http.MethodDelete, "/users/"+id, envAdminToken, ""); w.Code != http.StatusNotFound {
		t.Errorf("re-delete: %d, want 404", w.Code)
	}
}

func TestUsersAPI_GrantCatalog(t *testing.T) {
	r, _, _ := setupUsersTest(t)
	w := doJSON(t, r, http.MethodGet, "/users/grants", envAdminToken, "")
	if w.Code != http.StatusOK {
		t.Fatalf("grants: %d", w.Code)
	}
	var resp struct {
		Grants []string `json:"grants"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Grants) != len(user.AllGrants()) {
		t.Errorf("catalog size = %d, want %d", len(resp.Grants), len(user.AllGrants()))
	}
}

// mintUserToken logs the user in at the session layer (the HTTP login
// endpoint lives in adminauth and has its own tests).
func mintUserToken(t *testing.T, sm *webauthn.SessionManager, id string) string {
	t.Helper()
	token, err := sm.CreateAuthToken(context.Background(), []byte(id), nil)
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}
	return token
}

func TestGrantEnforcement_UserRole(t *testing.T) {
	r, _, sm := setupUsersTest(t)
	id := createUserViaAPI(t, r, "carol", "password123", "user", []string{"chat"})
	token := mintUserToken(t, sm, id)

	// /auth/me reflects the identity.
	w := doJSON(t, r, http.MethodGet, "/auth/me", token, "")
	if w.Code != http.StatusOK {
		t.Fatalf("me: %d, body %s", w.Code, w.Body.String())
	}
	var me struct {
		Username string   `json:"username"`
		Role     string   `json:"role"`
		Grants   []string `json:"grants"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me.Username != "carol" || me.Role != "user" || len(me.Grants) != 1 || me.Grants[0] != "chat" {
		t.Errorf("unexpected /auth/me: %+v", me)
	}

	// The sidebar system widget is visible to every role, so /system must be
	// readable regardless of grants.
	if w := doJSON(t, r, http.MethodGet, "/system", token, ""); w.Code != http.StatusOK {
		t.Errorf("GET /system as grant-limited user: %d, want 200", w.Code)
	}

	// Chat grant unlocks the models list (chat UI's picker)...
	if w := doJSON(t, r, http.MethodGet, "/models", token, ""); w.Code != http.StatusOK {
		t.Errorf("GET /models with chat grant: %d, want 200", w.Code)
	}
	// ...but nothing else.
	for _, path := range []string{"/users", "/providers", "/logs", "/stats", "/settings", "/virtual-keys"} {
		if w := doJSON(t, r, http.MethodGet, path, token, ""); w.Code != http.StatusForbidden {
			t.Errorf("GET %s with chat grant: %d, want 403", path, w.Code)
		}
	}
	// Model mutations stay admin-only even with the chat grant.
	if w := doJSON(t, r, http.MethodDelete, "/models/00000000-0000-0000-0000-000000000001", token, ""); w.Code != http.StatusForbidden {
		t.Errorf("DELETE /models with chat grant: %d, want 403", w.Code)
	}

	// The usage grant covers the whole Dashboard: stats plus the model and
	// provider lists its count pills read.
	obsID := createUserViaAPI(t, r, "dave", "password123", "user", []string{"usage"})
	obsToken := mintUserToken(t, sm, obsID)
	for _, path := range []string{"/stats", "/models", "/providers"} {
		if w := doJSON(t, r, http.MethodGet, path, obsToken, ""); w.Code != http.StatusOK {
			t.Errorf("GET %s with usage grant: %d, want 200", path, w.Code)
		}
	}
	for _, path := range []string{"/virtual-keys", "/settings", "/users"} {
		if w := doJSON(t, r, http.MethodGet, path, obsToken, ""); w.Code != http.StatusForbidden {
			t.Errorf("GET %s with usage grant: %d, want 403", path, w.Code)
		}
	}
}

func TestGrantEnforcement_EnvAdminAndAdminRole(t *testing.T) {
	r, _, sm := setupUsersTest(t)

	// Env admin token passes everything.
	if w := doJSON(t, r, http.MethodGet, "/users", envAdminToken, ""); w.Code != http.StatusOK {
		t.Errorf("env admin GET /users: %d", w.Code)
	}
	w := doJSON(t, r, http.MethodGet, "/auth/me", envAdminToken, "")
	var me struct {
		Username string `json:"username"`
		Role     string `json:"role"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me.Username != "admin" || me.Role != "admin" {
		t.Errorf("env admin /auth/me: %+v", me)
	}

	// An admin-role user passes admin-only surfaces too.
	id := createUserViaAPI(t, r, "root2", "password123", "admin", nil)
	token := mintUserToken(t, sm, id)
	for _, path := range []string{"/users", "/providers", "/logs", "/stats", "/settings"} {
		if w := doJSON(t, r, http.MethodGet, path, token, ""); w.Code != http.StatusOK {
			t.Errorf("admin-role GET %s: %d, want 200 (body %s)", path, w.Code, w.Body.String())
		}
	}
}

func TestGrantEnforcement_DisabledUserLockedOut(t *testing.T) {
	r, _, sm := setupUsersTest(t)
	id := createUserViaAPI(t, r, "dave", "password123", "user", []string{"chat"})
	token := mintUserToken(t, sm, id)

	if w := doJSON(t, r, http.MethodGet, "/auth/me", token, ""); w.Code != http.StatusOK {
		t.Fatalf("pre-disable me: %d", w.Code)
	}

	up := `{"username":"dave","role":"user","grants":["chat"],"enabled":false}`
	if w := doJSON(t, r, http.MethodPut, "/users/"+id, envAdminToken, up); w.Code != http.StatusOK {
		t.Fatalf("disable: %d", w.Code)
	}

	// Old token is dead: revoked AND the middleware re-checks enabled.
	if w := doJSON(t, r, http.MethodGet, "/auth/me", token, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("disabled user still authenticated: %d", w.Code)
	}
}

func TestGrantEnforcement_PasswordResetRevokesSessions(t *testing.T) {
	r, _, sm := setupUsersTest(t)
	id := createUserViaAPI(t, r, "erin", "password123", "user", []string{"chat"})
	token := mintUserToken(t, sm, id)

	if w := doJSON(t, r, http.MethodPost, "/users/"+id+"/password", envAdminToken, `{"password":"newpassword1"}`); w.Code != http.StatusOK {
		t.Fatalf("password reset: %d", w.Code)
	}
	if w := doJSON(t, r, http.MethodGet, "/auth/me", token, ""); w.Code != http.StatusUnauthorized {
		t.Errorf("session survived password reset: %d", w.Code)
	}
}

func TestUsersAPI_SelfProtection(t *testing.T) {
	r, _, sm := setupUsersTest(t)
	id := createUserViaAPI(t, r, "frank", "password123", "admin", nil)
	token := mintUserToken(t, sm, id)

	// Self-delete refused.
	if w := doJSON(t, r, http.MethodDelete, "/users/"+id, token, ""); w.Code != http.StatusConflict {
		t.Errorf("self-delete: %d, want 409", w.Code)
	}
	// Self-disable refused.
	up := `{"username":"frank","role":"admin","grants":[],"enabled":false}`
	if w := doJSON(t, r, http.MethodPut, "/users/"+id, token, up); w.Code != http.StatusConflict {
		t.Errorf("self-disable: %d, want 409", w.Code)
	}
	// Self-demote refused.
	up = `{"username":"frank","role":"user","grants":[],"enabled":true}`
	if w := doJSON(t, r, http.MethodPut, "/users/"+id, token, up); w.Code != http.StatusConflict {
		t.Errorf("self-demote: %d, want 409", w.Code)
	}
	// The env admin can still disable them (break-glass beats self-protection).
	if w := doJSON(t, r, http.MethodPut, "/users/"+id, envAdminToken, up); w.Code != http.StatusOK {
		t.Errorf("env admin demote: %d, want 200", w.Code)
	}
}

// TestResolveIdentity_UnknownHandleRejected verifies that only the exact
// legacy "admin" handle maps to the admin identity; any other non-UUID
// session handle is rejected instead of silently escalating.
func TestResolveIdentity_UnknownHandleRejected(t *testing.T) {
	r, _, sm := setupUsersTest(t)

	if w := doJSON(t, r, http.MethodGet, "/auth/me", mintUserToken(t, sm, "admin"), ""); w.Code != http.StatusOK {
		t.Errorf("legacy admin handle: %d, want 200", w.Code)
	}
	for _, handle := range []string{"Admin", "administrator", "not-a-uuid", ""} {
		if w := doJSON(t, r, http.MethodGet, "/auth/me", mintUserToken(t, sm, handle), ""); w.Code != http.StatusUnauthorized {
			t.Errorf("handle %q: %d, want 401", handle, w.Code)
		}
	}

	// A well-formed UUID handle whose users row does not exist (deleted account,
	// or a token that outlived its user) must fail closed, not panic on the nil
	// user returned by the lookup.
	ghost := uuid.NewString()
	if w := doJSON(t, r, http.MethodGet, "/auth/me", mintUserToken(t, sm, ghost), ""); w.Code != http.StatusUnauthorized {
		t.Errorf("orphaned UUID handle: %d, want 401", w.Code)
	}
}

// doJSONCtx is doJSON with a caller-supplied context, so a cancelled context can
// drive the repository queries into failure and exercise the handlers' generic
// 500 branches (the env admin token needs no DB, so auth still passes).
func doJSONCtx(ctx context.Context, t *testing.T, r chi.Router, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body == "" {
		req = httptest.NewRequestWithContext(ctx, method, path, http.NoBody)
	} else {
		req = httptest.NewRequestWithContext(ctx, method, path, bytes.NewReader([]byte(body)))
	}
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// TestUsersAPI_ErrorPaths walks the validation, malformed-input, not-found, and
// conflict branches of every user handler so a broken request fails with the
// right status instead of a 500 or a panic.
func TestUsersAPI_ErrorPaths(t *testing.T) {
	r, _, _ := setupUsersTest(t)
	id := createUserViaAPI(t, r, "erin", "password123", "user", []string{"usage"})
	other := createUserViaAPI(t, r, "erica", "password123", "user", nil)
	ghost := uuid.NewString()

	// validate() rejections not already covered by AdminCRUD.
	badCreate := map[string]string{
		"whitespace username": `{"username":"a b","password":"password123","role":"user","grants":[]}`,
		"too-long username":   `{"username":"` + strings.Repeat("x", 65) + `","password":"password123","role":"user","grants":[]}`,
		"too-long display":    `{"username":"z","display_name":"` + strings.Repeat("d", 129) + `","password":"password123","role":"user","grants":[]}`,
	}
	for name, body := range badCreate {
		if w := doJSON(t, r, http.MethodPost, "/users", envAdminToken, body); w.Code != http.StatusBadRequest {
			t.Errorf("create %s: %d, want 400", name, w.Code)
		}
	}

	// Malformed JSON bodies -> 400 on every writing handler.
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/users"},
		{http.MethodPut, "/users/" + id},
		{http.MethodPost, "/users/" + id + "/password"},
	} {
		if w := doJSON(t, r, tc.method, tc.path, envAdminToken, `{"username":`); w.Code != http.StatusBadRequest {
			t.Errorf("%s %s malformed body: %d, want 400", tc.method, tc.path, w.Code)
		}
	}

	// Non-UUID path param -> 400 before any DB work.
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPut, "/users/not-a-uuid", `{"username":"z","role":"user","grants":[],"enabled":true}`},
		{http.MethodPost, "/users/not-a-uuid/password", `{"password":"password123"}`},
		{http.MethodDelete, "/users/not-a-uuid", ""},
	} {
		if w := doJSON(t, r, tc.method, tc.path, envAdminToken, tc.body); w.Code != http.StatusBadRequest {
			t.Errorf("%s %s bad uuid: %d, want 400", tc.method, tc.path, w.Code)
		}
	}

	// Update validation error (bad role) -> 400.
	if w := doJSON(t, r, http.MethodPut, "/users/"+id, envAdminToken,
		`{"username":"erin","role":"root","grants":[],"enabled":true}`); w.Code != http.StatusBadRequest {
		t.Errorf("update bad role: %d, want 400", w.Code)
	}

	// Update / SetPassword against a missing row -> 404.
	if w := doJSON(t, r, http.MethodPut, "/users/"+ghost, envAdminToken,
		`{"username":"nobody","role":"user","grants":[],"enabled":true}`); w.Code != http.StatusNotFound {
		t.Errorf("update missing: %d, want 404", w.Code)
	}
	if w := doJSON(t, r, http.MethodPost, "/users/"+ghost+"/password", envAdminToken,
		`{"password":"password123"}`); w.Code != http.StatusNotFound {
		t.Errorf("setpassword missing: %d, want 404", w.Code)
	}

	// Rename erin onto erica's username -> unique violation 409.
	if w := doJSON(t, r, http.MethodPut, "/users/"+id, envAdminToken,
		`{"username":"erica","role":"user","grants":[],"enabled":true}`); w.Code != http.StatusConflict {
		t.Errorf("update dup username: %d, want 409", w.Code)
	}

	// SetPassword below the minimum length -> 400.
	if w := doJSON(t, r, http.MethodPost, "/users/"+id+"/password", envAdminToken,
		`{"password":"short"}`); w.Code != http.StatusBadRequest {
		t.Errorf("setpassword short: %d, want 400", w.Code)
	}

	// SetPassword happy path -> 200 (and revokes sessions).
	if w := doJSON(t, r, http.MethodPost, "/users/"+other+"/password", envAdminToken,
		`{"password":"brand-new-pass"}`); w.Code != http.StatusOK {
		t.Errorf("setpassword ok: %d, want 200", w.Code)
	}
}

// TestUsersAPI_RepositoryFailures drives each handler's generic 500 branch by
// cancelling the request context so the underlying query fails.
func TestUsersAPI_RepositoryFailures(t *testing.T) {
	r, _, _ := setupUsersTest(t)
	id := createUserViaAPI(t, r, "gina", "password123", "user", nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // every repo query below observes a cancelled context

	cases := []struct{ name, method, path, body string }{
		{"list", http.MethodGet, "/users", ""},
		{"create", http.MethodPost, "/users", `{"username":"hank","password":"password123","role":"user","grants":[]}`},
		{"update", http.MethodPut, "/users/" + id, `{"username":"gina","role":"user","grants":[],"enabled":true}`},
		{"setpassword", http.MethodPost, "/users/" + id + "/password", `{"password":"password123"}`},
		{"delete", http.MethodDelete, "/users/" + id, ""},
	}
	for _, tc := range cases {
		if w := doJSONCtx(ctx, t, r, tc.method, tc.path, envAdminToken, tc.body); w.Code != http.StatusInternalServerError {
			t.Errorf("%s with cancelled ctx: %d, want 500", tc.name, w.Code)
		}
	}
}

// TestRequireGrant_ExportedGuard exercises the exported RequireGrant wrapper
// that main.go mounts on the admin-chat group: a caller with the grant passes,
// one without is refused with 403.
func TestRequireGrant_ExportedGuard(t *testing.T) {
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

	chatID := createUserViaAPI(t, apiRouter, "cody", "password123", "user", []string{"chat"})
	usageID := createUserViaAPI(t, apiRouter, "uma", "password123", "user", []string{"usage"})

	gr := chi.NewRouter()
	gr.Use(h.AuthMiddleware)
	gr.Group(func(g chi.Router) {
		g.Use(h.RequireGrant(user.GrantChat))
		g.Get("/probe", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	})

	if w := doJSON(t, gr, http.MethodGet, "/probe", mintUserToken(t, sm, chatID), ""); w.Code != http.StatusOK {
		t.Errorf("chat-granted caller: %d, want 200", w.Code)
	}
	if w := doJSON(t, gr, http.MethodGet, "/probe", mintUserToken(t, sm, usageID), ""); w.Code != http.StatusForbidden {
		t.Errorf("ungranted caller: %d, want 403", w.Code)
	}
}
