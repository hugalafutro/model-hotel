package adminauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	webauthnx "github.com/go-webauthn/webauthn/webauthn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// newTestWebAuthnHandler creates a WebAuthnHandler with the given dependencies
func newTestWebAuthnHandler(
	repo *webauthn.Repository,
	rp *webauthnx.WebAuthn,
	sessionMgr *webauthn.SessionManager,
	adminMgr AdminAuthenticator,
) *WebAuthnHandler {
	return &WebAuthnHandler{
		webauthnRepo: repo,
		relyingParty: rp,
		sessionMgr:   sessionMgr,
		adminMgr:     adminMgr,
		ipLimiter:    mockIPLimiter{},
		totpEnabled:  func() bool { return false },
		jar:          authcookie.Dashboard,
	}
}

// mockIPLimiter implements IPLimiterMiddleware for tests
type mockIPLimiter struct{}

func (m mockIPLimiter) Middleware(next http.Handler) http.Handler {
	return next
}

func (m mockIPLimiter) ClientIP(r *http.Request) string {
	return r.RemoteAddr
}

// availStubStore embeds webauthn.Store so it satisfies the interface, and
// overrides only ListCredentials (the sole method Available calls). Any other
// method would panic on the nil embedded interface, which is the intended guard:
// Available must not touch the rest of the store.
type availStubStore struct {
	webauthn.Store
	creds []*webauthn.CredentialRecord
	err   error
}

func (s availStubStore) ListCredentials(context.Context) ([]*webauthn.CredentialRecord, error) {
	return s.creds, s.err
}

// TestLogout_NoAuthHeader tests that missing auth header returns 401
func TestWebAuthnHandler_Logout_NoAuthHeader(t *testing.T) {
	h := newTestWebAuthnHandler(nil, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/webauthn/logout", http.NoBody)
	w := httptest.NewRecorder()

	h.Logout(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusUnauthorized, w.Code, w.Body.String())
	}
}

// TestAdminOrSessionAuth_NoAuth tests that missing auth returns 401
func TestAdminOrSessionAuth_NoAuth(t *testing.T) {
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return false }}
	sessionMgr := webauthn.NewSessionManager(nil)
	h := newTestWebAuthnHandler(nil, nil, sessionMgr, adminMgr)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := h.adminOrSessionAuth(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

// TestAdminOrSessionAuth_InvalidToken tests that invalid token returns 401
func TestAdminOrSessionAuth_InvalidToken(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Fatal("test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	defer pool.Close()

	repo := webauthn.NewRepository(pool)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return token == "admin-token" }}
	sessionMgr := webauthn.NewSessionManager(repo)
	h := newTestWebAuthnHandler(repo, nil, sessionMgr, adminMgr)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	wrapped := h.adminOrSessionAuth(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer wrong-token")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d, got %d", http.StatusUnauthorized, w.Code)
	}
}

// TestAdminOrSessionAuth_AdminToken tests that admin token passes auth
func TestAdminOrSessionAuth_AdminToken(t *testing.T) {
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return token == "admin-token" }}
	sessionMgr := webauthn.NewSessionManager(nil)
	h := newTestWebAuthnHandler(nil, nil, sessionMgr, adminMgr)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	wrapped := h.adminOrSessionAuth(handler)

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer admin-token")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

// TestAdminOrSessionAuth_SessionToken tests that session token passes auth
func TestAdminOrSessionAuth_SessionToken(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Fatal("test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	defer pool.Close()

	repo := webauthn.NewRepository(pool)
	sessionMgr := webauthn.NewSessionManager(repo)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return false }}
	h := newTestWebAuthnHandler(repo, nil, sessionMgr, adminMgr)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("success"))
	})

	wrapped := h.adminOrSessionAuth(handler)

	// Create a session token
	token, err := sessionMgr.CreateAuthToken(context.Background(), []byte("admin"), nil, webauthn.SessionMeta{})
	if err != nil {
		t.Fatalf("failed to create session token: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

// newCookieSessionEnv builds a repo-backed SessionManager wired through
// adminOrSessionAuth and mints a session token for the given user handle.
// It returns the wrapped handler and the minted token. The admin token is
// never valid here, so only the cookie/session paths can admit a request.
func newCookieSessionEnv(t *testing.T, userID []byte) (http.Handler, string) {
	t.Helper()
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}
	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	t.Cleanup(pool.Close)

	repo := webauthn.NewRepository(pool)
	sessionMgr := webauthn.NewSessionManager(repo)
	adminMgr := &mockAdminAuth{validateFn: func(string) bool { return false }}
	h := newTestWebAuthnHandler(repo, nil, sessionMgr, adminMgr)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := h.adminOrSessionAuth(handler)

	token, err := sessionMgr.CreateAuthToken(context.Background(), userID, nil, webauthn.SessionMeta{})
	if err != nil {
		t.Fatalf("failed to create session token: %v", err)
	}
	return wrapped, token
}

// TestAdminOrSession_Cookie_AdminGet_Works verifies an admin session ridden on
// the mh_session cookie admits a safe GET (no CSRF header required).
func TestAdminOrSession_Cookie_AdminGet_Works(t *testing.T) {
	wrapped, token := newCookieSessionEnv(t, []byte("admin"))

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.AddCookie(&http.Cookie{Name: authcookie.SessionCookie, Value: token})
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

// TestAdminOrSession_Cookie_Post_RequiresCSRF verifies an admin cookie session
// on an unsafe method is rejected with 403 when the CSRF header is absent.
func TestAdminOrSession_Cookie_Post_RequiresCSRF(t *testing.T) {
	wrapped, token := newCookieSessionEnv(t, []byte("admin"))

	req := httptest.NewRequest(http.MethodPost, "/test", http.NoBody)
	req.AddCookie(&http.Cookie{Name: authcookie.SessionCookie, Value: token})
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Errorf("expected status %d (CSRF required), got %d; body: %s", http.StatusForbidden, w.Code, w.Body.String())
	}
}

// TestAdminOrSession_Cookie_Post_WithCSRF verifies an admin cookie session on
// an unsafe method passes when the CSRF cookie and header match.
func TestAdminOrSession_Cookie_Post_WithCSRF(t *testing.T) {
	wrapped, token := newCookieSessionEnv(t, []byte("admin"))

	req := httptest.NewRequest(http.MethodPost, "/test", http.NoBody)
	req.AddCookie(&http.Cookie{Name: authcookie.SessionCookie, Value: token})
	req.AddCookie(&http.Cookie{Name: authcookie.CSRFCookie, Value: "csrf-tok"})
	req.Header.Set(authcookie.CSRFHeader, "csrf-tok")
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d (matching CSRF), got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}
}

// TestAdminOrSession_Cookie_UserSessionRejected verifies the admin-only gate:
// a valid but non-admin (UUID) session cookie must NOT reach next. With no
// bearer header and no admin cookie identity, it falls through to the header
// logic and is rejected with 401.
func TestAdminOrSession_Cookie_UserSessionRejected(t *testing.T) {
	wrapped, token := newCookieSessionEnv(t, []byte("11111111-1111-1111-1111-111111111111"))

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.AddCookie(&http.Cookie{Name: authcookie.SessionCookie, Value: token})
	w := httptest.NewRecorder()

	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d (non-admin session rejected), got %d; body: %s", http.StatusUnauthorized, w.Code, w.Body.String())
	}
}

// TestWebAuthnHandler_Logout_WithValidToken tests logout with valid session token
func TestWebAuthnHandler_Logout_WithValidToken(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Fatal("test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	defer pool.Close()

	repo := webauthn.NewRepository(pool)
	sessionMgr := webauthn.NewSessionManager(repo)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return false }}
	h := newTestWebAuthnHandler(repo, nil, sessionMgr, adminMgr)

	// Create a session token
	token, err := sessionMgr.CreateAuthToken(context.Background(), []byte("admin"), nil, webauthn.SessionMeta{})
	if err != nil {
		t.Fatalf("failed to create session token: %v", err)
	}

	// Verify token is valid before logout
	if !sessionMgr.Validate(context.Background(), token) {
		t.Fatal("token should be valid before logout")
	}

	req := httptest.NewRequest(http.MethodPost, "/webauthn/logout", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	h.Logout(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	// Verify token is revoked after logout
	if sessionMgr.Validate(context.Background(), token) {
		t.Error("token should be invalid after logout")
	}
}

// TestWebAuthnLoginFinish_SetsSessionCookie verifies that in cookie mode the
// passkey login success response sets the HttpOnly session cookie carrying the
// minted token and returns {"success": true} with no token in the body.
func TestWebAuthnLoginFinish_SetsSessionCookie(t *testing.T) {
	h := newTestWebAuthnHandler(nil, nil, nil, nil)
	h.useCookieAuth = true
	h.cookieSecure = "never"

	req := httptest.NewRequest(http.MethodPost, "/webauthn/login/finish", http.NoBody)
	w := httptest.NewRecorder()

	h.respondLoginSuccess(w, req, "session-token-123")

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}
	if got := sessionCookie(t, w); got != "session-token-123" {
		t.Errorf("session cookie value = %q, want %q", got, "session-token-123")
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if _, ok := resp["token"]; ok {
		t.Errorf("cookie-mode response must not include token in body: %v", resp)
	}
	if resp["success"] != true {
		t.Errorf("expected success:true, got %v", resp)
	}
}

// TestWebAuthnLoginFinish_LegacyReturnsToken verifies that with cookie auth
// disabled (Front Desk) the login response carries the token in the body and
// sets no session cookie, preserving the legacy contract byte-for-byte.
func TestWebAuthnLoginFinish_LegacyReturnsToken(t *testing.T) {
	h := newTestWebAuthnHandler(nil, nil, nil, nil)
	// useCookieAuth defaults to false (header-bearer mode).

	req := httptest.NewRequest(http.MethodPost, "/webauthn/login/finish", http.NoBody)
	w := httptest.NewRecorder()

	h.respondLoginSuccess(w, req, "legacy-token")

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode body: %v", err)
	}
	if resp["token"] != "legacy-token" {
		t.Errorf("expected token in body, got %v", resp)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == authcookie.SessionCookie {
			t.Errorf("legacy mode must not set session cookie, got %+v", c)
		}
	}
}

// TestWebAuthnHandler_Logout_CookieMode_ClearsCookie verifies that in cookie
// mode logout revokes the token read from the session cookie and emits an
// expiring session cookie (MaxAge < 0).
func TestWebAuthnHandler_Logout_CookieMode_ClearsCookie(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Fatal("test database not available")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	defer pool.Close()

	repo := webauthn.NewRepository(pool)
	sessionMgr := webauthn.NewSessionManager(repo)
	adminMgr := &mockAdminAuth{validateFn: func(string) bool { return false }}
	h := newTestWebAuthnHandler(repo, nil, sessionMgr, adminMgr)
	h.useCookieAuth = true
	h.cookieSecure = "never"

	token, err := sessionMgr.CreateAuthToken(context.Background(), []byte("admin"), nil, webauthn.SessionMeta{})
	if err != nil {
		t.Fatalf("failed to create session token: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/webauthn/logout", http.NoBody)
	req.AddCookie(&http.Cookie{Name: authcookie.SessionCookie, Value: token})
	w := httptest.NewRecorder()

	h.Logout(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}
	if sessionMgr.Validate(context.Background(), token) {
		t.Error("token should be invalid after cookie-mode logout")
	}
	var cleared *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == authcookie.SessionCookie {
			cleared = c
		}
	}
	if cleared == nil {
		t.Fatalf("expected expiring %s cookie, got %+v", authcookie.SessionCookie, w.Result().Cookies())
	}
	if cleared.MaxAge >= 0 {
		t.Errorf("expected session cookie MaxAge < 0, got %d", cleared.MaxAge)
	}
}

// TestWebAuthnHandler_Logout_LegacyMode_NoSetCookie verifies the Front Desk
// legacy logout path: it revokes the bearer token and emits no Set-Cookie
// header so the response stays byte-identical to the pre-cookie contract.
func TestWebAuthnHandler_Logout_LegacyMode_NoSetCookie(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Fatal("test database not available")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	defer pool.Close()

	repo := webauthn.NewRepository(pool)
	sessionMgr := webauthn.NewSessionManager(repo)
	adminMgr := &mockAdminAuth{validateFn: func(string) bool { return false }}
	h := newTestWebAuthnHandler(repo, nil, sessionMgr, adminMgr)
	// useCookieAuth defaults to false (header-bearer mode).

	token, err := sessionMgr.CreateAuthToken(context.Background(), []byte("admin"), nil, webauthn.SessionMeta{})
	if err != nil {
		t.Fatalf("failed to create session token: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/webauthn/logout", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	h.Logout(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}
	if sessionMgr.Validate(context.Background(), token) {
		t.Error("token should be invalid after logout")
	}
	if sc := w.Header().Values("Set-Cookie"); len(sc) != 0 {
		t.Errorf("legacy logout must not emit Set-Cookie, got %v", sc)
	}
}

// TestListCredentials_Success tests listing credentials with valid auth
func TestListCredentials_Success(t *testing.T) {
	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Fatal("test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatal("test database not available")
	}
	defer pool.Close()

	repo := webauthn.NewRepository(pool)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return token == "admin-token" }}
	h := newTestWebAuthnHandler(repo, nil, nil, adminMgr)

	req := httptest.NewRequest(http.MethodGet, "/webauthn/credentials", http.NoBody)
	req.Header.Set("Authorization", "Bearer admin-token")
	w := httptest.NewRecorder()

	h.ListCredentials(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var resp []credentialResponse
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Should be empty array since no credentials exist
	if len(resp) != 0 {
		t.Errorf("expected 0 credentials, got %d", len(resp))
	}
}

// --- NewWebAuthnHandler constructor tests ---

// TestWebAuthnHandler_NewWebAuthnHandler_NilParams tests the constructor with nil params
func TestWebAuthnHandler_NewWebAuthnHandler_NilParams(t *testing.T) {
	h := NewWebAuthnHandler(nil, nil, nil, nil, nil, false, nil, false, "", authcookie.Dashboard)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.webauthnRepo != nil {
		t.Error("expected nil webauthnRepo")
	}
	if h.relyingParty != nil {
		t.Error("expected nil relyingParty")
	}
	if h.sessionMgr != nil {
		t.Error("expected nil sessionMgr")
	}
	if h.adminMgr != nil {
		t.Error("expected nil adminMgr")
	}
	if h.ipLimiter != nil {
		t.Error("expected nil ipLimiter")
	}
}

// TestWebAuthnHandler_NewWebAuthnHandler_NonNilParams tests the constructor with provided params
func TestWebAuthnHandler_NewWebAuthnHandler_NonNilParams(t *testing.T) {
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return true }}
	limiter := mockIPLimiter{}
	h := NewWebAuthnHandler(nil, nil, nil, adminMgr, limiter, false, nil, false, "", authcookie.Dashboard)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
	if h.adminMgr == nil {
		t.Error("expected non-nil adminMgr")
	}
	if h.ipLimiter == nil {
		t.Error("expected non-nil ipLimiter")
	}
}

// buildFakeRegistrationCredential returns a create() response for a credential
// whose id is credentialIDB64, issued against no challenge: it parses, and the
// library then rejects it at the challenge check.
func buildFakeRegistrationCredential(t *testing.T, credentialIDB64 string) json.RawMessage {
	t.Helper()
	return softAuthenticatorWithID(t, credentialIDB64).attestation(t, "")
}

// buildFakeLoginCredential is the get() counterpart: it parses, and the library
// rejects it at the challenge check.
func buildFakeLoginCredential(t *testing.T, credentialIDB64 string) json.RawMessage {
	t.Helper()
	return softAuthenticatorWithID(t, credentialIDB64).assertion(t, "", 0)
}

// --- TOTP gate tests for adminOrSessionAuth (B1) ---

// TestWebAuthnAdminOrSessionAuth_TotpOn_RejectsRawToken verifies that with
// TOTP enabled, a raw admin token is rejected by adminOrSessionAuth (so the
// second factor cannot be bypassed to manage passkeys).
func TestWebAuthnAdminOrSessionAuth_TotpOn_RejectsRawToken(t *testing.T) {
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return token == "admin-token" }}
	h := newTestWebAuthnHandler(nil, nil, nil, adminMgr)
	h.totpEnabled = func() bool { return true }

	wrapped := h.adminOrSessionAuth(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("handler should NOT be called: raw token must be rejected under TOTP")
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer admin-token")
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 (raw token rejected under TOTP), got %d; body: %s", w.Code, w.Body.String())
	}
}

// TestWebAuthnAdminOrSessionAuth_TotpOff_AcceptsRawToken verifies that with
// TOTP disabled, the raw admin token passes adminOrSessionAuth (unchanged behavior).
func TestWebAuthnAdminOrSessionAuth_TotpOff_AcceptsRawToken(t *testing.T) {
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return token == "admin-token" }}
	h := newTestWebAuthnHandler(nil, nil, nil, adminMgr)
	// newTestWebAuthnHandler already defaults totpEnabled to false.

	wrapped := h.adminOrSessionAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer admin-token")
	w := httptest.NewRecorder()
	wrapped.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (raw token accepted under TOTP off), got %d", w.Code)
	}
}
