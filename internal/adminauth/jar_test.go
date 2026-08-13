package adminauth

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	totpsvc "github.com/hugalafutro/model-hotel/internal/totp"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// newTestSessionWithAdminToken returns a session manager backed by the in-memory
// store the OIDC suite uses (no database) holding one valid admin session token.
func newTestSessionWithAdminToken(t *testing.T) (*webauthn.SessionManager, string) {
	t.Helper()
	sessionMgr := webauthn.NewSessionManager(newMemStore())
	tok, err := sessionMgr.CreateAuthToken(context.Background(), []byte("admin"), nil, webauthn.SessionMeta{})
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}
	return sessionMgr, tok
}

// TestRequireAdminOrSession_HonorsJarNames proves the cookie branch reads the
// jar's own session cookie name and ignores the other app's.
func TestRequireAdminOrSession_HonorsJarNames(t *testing.T) {
	sessionMgr, adminTok := newTestSessionWithAdminToken(t)

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	// mockAdminAuth with no validateFn rejects every raw admin token, so only the
	// cookie/session branch can admit.
	gate := RequireAdminOrSession(&mockAdminAuth{}, sessionMgr, nil, authcookie.FrontDesk, next)

	// fd_session admits.
	r := httptest.NewRequest(http.MethodGet, "/x", http.NoBody)
	r.AddCookie(&http.Cookie{Name: "fd_session", Value: adminTok})
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("fd_session cookie: got %d, want 200", rec.Code)
	}

	// The same token under the dashboard's cookie name must NOT admit.
	r2 := httptest.NewRequest(http.MethodGet, "/x", http.NoBody)
	r2.AddCookie(&http.Cookie{Name: "mh_session", Value: adminTok})
	rec2 := httptest.NewRecorder()
	gate.ServeHTTP(rec2, r2)
	if rec2.Code != http.StatusUnauthorized {
		t.Fatalf("mh_session cookie against FrontDesk jar: got %d, want 401", rec2.Code)
	}
}

// TestRequireAdminOrSession_JarCSRF proves the CSRF double-submit check on an
// unsafe method reads the jar's own CSRF cookie name.
func TestRequireAdminOrSession_JarCSRF(t *testing.T) {
	sessionMgr, adminTok := newTestSessionWithAdminToken(t)

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	gate := RequireAdminOrSession(&mockAdminAuth{}, sessionMgr, nil, authcookie.FrontDesk, next)

	// A matching fd_csrf cookie + header admits.
	r := httptest.NewRequest(http.MethodPost, "/x", http.NoBody)
	r.AddCookie(&http.Cookie{Name: "fd_session", Value: adminTok})
	r.AddCookie(&http.Cookie{Name: "fd_csrf", Value: "csrf-value"})
	r.Header.Set(authcookie.CSRFHeader, "csrf-value")
	rec := httptest.NewRecorder()
	gate.ServeHTTP(rec, r)
	if rec.Code != http.StatusOK {
		t.Fatalf("fd_csrf double-submit: got %d, want 200", rec.Code)
	}

	// The dashboard's CSRF cookie name does not satisfy the Front Desk jar.
	r2 := httptest.NewRequest(http.MethodPost, "/x", http.NoBody)
	r2.AddCookie(&http.Cookie{Name: "fd_session", Value: adminTok})
	r2.AddCookie(&http.Cookie{Name: "mh_csrf", Value: "csrf-value"})
	r2.Header.Set(authcookie.CSRFHeader, "csrf-value")
	rec2 := httptest.NewRecorder()
	gate.ServeHTTP(rec2, r2)
	if rec2.Code != http.StatusForbidden {
		t.Fatalf("mh_csrf cookie against FrontDesk jar: got %d, want 403", rec2.Code)
	}
}

// --- handler-internal mint/clear sites honor the handler's own jar ---
//
// The gate test above covers middleware.go. These cover the five places a
// handler writes or reads a session cookie itself (TotpHandler.EnrollVerify and
// .Login, WebAuthnHandler.respondLoginSuccess and .Logout, OIDCHandler.Callback):
// with the Front Desk jar they must emit fd_session/fd_csrf and never
// mh_session, which is exactly what reverting to the package-level (Dashboard)
// functions would break.

// cookieNamed returns the Set-Cookie entry with the given name, or nil.
func cookieNamed(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, c := range w.Result().Cookies() {
		if c.Name == name {
			return c
		}
	}
	return nil
}

// assertFrontDeskJarCookies asserts the response minted the Front Desk pair and
// no dashboard cookie.
func assertFrontDeskJarCookies(t *testing.T, w *httptest.ResponseRecorder, where string) {
	t.Helper()
	if c := cookieNamed(w, "fd_session"); c == nil {
		t.Errorf("%s: no fd_session cookie, got %+v", where, w.Result().Cookies())
	} else if !c.HttpOnly {
		t.Errorf("%s: fd_session must be HttpOnly", where)
	}
	if cookieNamed(w, "fd_csrf") == nil {
		t.Errorf("%s: no fd_csrf cookie, got %+v", where, w.Result().Cookies())
	}
	if c := cookieNamed(w, authcookie.SessionCookie); c != nil {
		t.Errorf("%s: Front Desk jar must not set %s, got %+v", where, authcookie.SessionCookie, c)
	}
}

// newFrontDeskTotpHandler mirrors newTotpTestHandler but wires the Front Desk
// jar with cookie mode on, so the handler's own mint sites are observable.
func newFrontDeskTotpHandler(t *testing.T) *TotpHandler {
	t.Helper()
	truncateTOTPTables(t)

	totpRepo := totpsvc.NewRepository(apiTestDB.Pool(), testMasterKey)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return token == "admin-token" }}
	wrepo := webauthn.NewRepository(apiTestDB.Pool())
	sessionMgr := webauthn.NewSessionManager(wrepo)

	shim := &totpEnabledShim{repo: totpRepo, adminMgr: adminMgr, sessionMgr: sessionMgr}
	shim.totpEnabled.Store(false)
	t.Cleanup(func() { truncateTOTPTables(t) })

	return NewTotpHandler(totpRepo, adminMgr, sessionMgr, mockIPLimiter{}, false,
		shim.TotpEnabled, shim.RefreshTotpEnabled, "never", true, authcookie.FrontDesk)
}

// TestTotpMintSites_HonorHandlerJar pins both TOTP mint sites (EnrollVerify and
// Login) to the handler's jar: with authcookie.FrontDesk the session rides
// fd_session, never mh_session.
func TestTotpMintSites_HonorHandlerJar(t *testing.T) {
	th := newFrontDeskTotpHandler(t)

	req := httptest.NewRequest(http.MethodPost, "/totp/enroll/start", http.NoBody)
	req.Header.Set("Authorization", "Bearer admin-token")
	w := httptest.NewRecorder()
	serveTotpRouter(th).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("enroll/start: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var startResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &startResp); err != nil {
		t.Fatalf("decode enroll/start: %v", err)
	}
	secret := startResp["secret"]

	// EnrollVerify mint site (totp.go). Step -1 keeps the login below on a
	// distinct, later single-use step.
	vreq := httptest.NewRequest(http.MethodPost, "/totp/enroll/verify",
		bytes.NewReader([]byte(`{"code":"`+codeForStep(t, secret, -1)+`"}`)))
	vreq.Header.Set("Authorization", "Bearer admin-token")
	vreq.Header.Set("Content-Type", "application/json")
	vw := httptest.NewRecorder()
	serveTotpRouter(th).ServeHTTP(vw, vreq)
	if vw.Code != http.StatusOK {
		t.Fatalf("enroll/verify: expected 200, got %d: %s", vw.Code, vw.Body.String())
	}
	assertFrontDeskJarCookies(t, vw, "enroll/verify")

	// Login mint site (totp.go).
	lreq := httptest.NewRequest(http.MethodPost, "/totp/login",
		bytes.NewReader([]byte(`{"token":"admin-token","code":"`+validCode(t, secret)+`"}`)))
	lreq.Header.Set("Content-Type", "application/json")
	lw := httptest.NewRecorder()
	serveTotpRouter(th).ServeHTTP(lw, lreq)
	if lw.Code != http.StatusOK {
		t.Fatalf("login: expected 200, got %d: %s", lw.Code, lw.Body.String())
	}
	assertFrontDeskJarCookies(t, lw, "login")

	tok := cookieNamed(lw, "fd_session")
	if tok == nil {
		t.Fatal("login: no fd_session cookie to validate")
	}
	if _, ok := th.sessionMgr.TokenUser(context.Background(), tok.Value); !ok {
		t.Error("login: fd_session token does not validate against the session manager")
	}
}

// TestWebAuthnLogout_HonorsHandlerJar pins WebAuthnHandler.Logout's cookie-first
// read and its ClearSession to the handler's jar: a Front Desk handler reads the
// token from fd_session and expires fd_session, never mh_session.
func TestWebAuthnLogout_HonorsHandlerJar(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}
	repo := webauthn.NewRepository(apiTestDB.Pool())
	sessionMgr := webauthn.NewSessionManager(repo)
	h := newTestWebAuthnHandler(repo, nil, sessionMgr, &mockAdminAuth{})
	h.useCookieAuth = true
	h.cookieSecure = "never"
	h.jar = authcookie.FrontDesk

	token, err := sessionMgr.CreateAuthToken(context.Background(), []byte("admin"), nil, webauthn.SessionMeta{})
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/webauthn/logout", http.NoBody)
	req.AddCookie(&http.Cookie{Name: "fd_session", Value: token})
	w := httptest.NewRecorder()
	h.Logout(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// Cookie-first read honored the jar: the token was found and revoked even
	// though no Authorization header was sent.
	if sessionMgr.Validate(context.Background(), token) {
		t.Error("token must be revoked after a cookie-mode logout read from fd_session")
	}
	cleared := cookieNamed(w, "fd_session")
	if cleared == nil {
		t.Fatalf("expected an expiring fd_session cookie, got %+v", w.Result().Cookies())
	}
	if cleared.MaxAge >= 0 {
		t.Errorf("expected fd_session MaxAge < 0, got %d", cleared.MaxAge)
	}
	if cookieNamed(w, "fd_csrf") == nil {
		t.Errorf("expected an expiring fd_csrf cookie, got %+v", w.Result().Cookies())
	}
	if c := cookieNamed(w, authcookie.SessionCookie); c != nil {
		t.Errorf("Front Desk jar must not clear %s, got %+v", authcookie.SessionCookie, c)
	}
}

// TestOIDCCallback_HonorsHandlerJar pins the OIDC callback mint site to the
// handler's jar: cookie mode with the Front Desk jar sets fd_session and
// redirects cleanly, never touching mh_session.
func TestOIDCCallback_HonorsHandlerJar(t *testing.T) {
	idp := newMockIDP(t, oidcTestClientID)
	h, _, sessionMgr := newOIDCTestHandlerMode(t, idp, "admin@example.com", true, "never")
	h.jar = authcookie.FrontDesk

	loc, cookie := runStart(t, h)
	state := loc.Query().Get("state")
	idp.configure(loc.Query().Get("nonce"), "admin@example.com", true)

	req := httptest.NewRequest(http.MethodGet,
		"/api/auth/oidc/callback?"+url.Values{"state": {state}, "code": {"auth-code"}}.Encode(),
		http.NoBody)
	req.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.Callback(w, req)

	if got := w.Header().Get("Location"); got != "/" {
		t.Fatalf("cookie mode must redirect cleanly to /, got %q", got)
	}
	assertFrontDeskJarCookies(t, w, "oidc callback")

	session := cookieNamed(w, "fd_session")
	if session == nil {
		t.Fatal("oidc callback: no fd_session cookie to validate")
	}
	if !sessionMgr.Validate(context.Background(), session.Value) {
		t.Error("oidc callback: fd_session token failed Validate")
	}
}

// TestWebAuthnLoginSuccess_HonorsHandlerJar pins the passkey login mint site to
// the handler's jar: the minted token rides fd_session, never mh_session.
func TestWebAuthnLoginSuccess_HonorsHandlerJar(t *testing.T) {
	h := newTestWebAuthnHandler(nil, nil, nil, nil)
	h.useCookieAuth = true
	h.cookieSecure = "never"
	h.jar = authcookie.FrontDesk

	req := httptest.NewRequest(http.MethodPost, "/webauthn/login/finish", http.NoBody)
	w := httptest.NewRecorder()
	h.respondLoginSuccess(w, req, "session-token-123")

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	assertFrontDeskJarCookies(t, w, "passkey login")
	if c := cookieNamed(w, "fd_session"); c != nil && c.Value != "session-token-123" {
		t.Errorf("fd_session value = %q, want %q", c.Value, "session-token-123")
	}
}
