package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

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
