package adminauth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// newTestSessionManager returns a session manager backed by the in-memory
// store the OIDC suite uses (no database), with no sessions pre-created.
func newTestSessionManager(t *testing.T) *webauthn.SessionManager {
	t.Helper()
	return webauthn.NewSessionManager(newMemStore())
}

func TestTokenExchange_MintsJarCookie(t *testing.T) {
	sessionMgr := newTestSessionManager(t)
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return token == "sekrit" }}
	h := TokenExchange(adminMgr, sessionMgr, nil, authcookie.FrontDesk, "never")

	r := httptest.NewRequest(http.MethodPost, "/api/auth/admin-exchange",
		strings.NewReader(`{"admin_token":"sekrit"}`))
	rec := httptest.NewRecorder()
	h(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", rec.Code, rec.Body.String())
	}
	var sessionCookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == "fd_session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil || sessionCookie.Value == "" || !sessionCookie.HttpOnly {
		t.Fatalf("want a non-empty HttpOnly fd_session cookie, got %+v", rec.Result().Cookies())
	}
	if strings.Contains(rec.Body.String(), sessionCookie.Value) || strings.Contains(rec.Body.String(), "sekrit") {
		t.Fatal("neither the session token nor the admin token may appear in the body")
	}
	// The minted session must validate as the admin identity.
	if uid, ok := sessionMgr.TokenUser(r.Context(), sessionCookie.Value); !ok || string(uid) != "admin" {
		t.Fatalf("minted session TokenUser = %q, %v; want admin, true", uid, ok)
	}
}

func TestTokenExchange_RefusesWhenTotpEnabled(t *testing.T) {
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return token == "sekrit" }}
	h := TokenExchange(adminMgr, newTestSessionManager(t),
		func() bool { return true }, authcookie.FrontDesk, "never")
	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"admin_token":"sekrit"}`))
	rec := httptest.NewRecorder()
	h(rec, r)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (raw token is only a first factor with 2FA on)", rec.Code)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("no cookie may be minted when TOTP gates the login")
	}
}

func TestTokenExchange_NilSessionManager_ReturnsServerErrorWithoutValidating(t *testing.T) {
	adminMgr := &mockAdminAuth{validateFn: func(string) bool {
		t.Error("Validate must not be called before the nil sessionMgr guard")
		return false
	}}
	h := TokenExchange(adminMgr, nil, nil, authcookie.FrontDesk, "never")

	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(`{"admin_token":"x"}`))
	rec := httptest.NewRecorder()
	h(rec, r)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (nil sessionMgr)", rec.Code)
	}
	if len(rec.Result().Cookies()) != 0 {
		t.Fatal("no Set-Cookie header may be sent when sessionMgr is nil")
	}
}

func TestTokenExchange_RejectsBadToken(t *testing.T) {
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return token == "sekrit" }}
	h := TokenExchange(adminMgr, newTestSessionManager(t), nil, authcookie.FrontDesk, "never")
	for _, body := range []string{`{"admin_token":"wrong"}`, `{}`, `not-json`} {
		r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader(body))
		rec := httptest.NewRecorder()
		h(rec, r)
		if rec.Code == http.StatusOK {
			t.Fatalf("body %q: got 200, want an error", body)
		}
		if len(rec.Result().Cookies()) != 0 {
			t.Fatalf("body %q: no cookie may be set on failure", body)
		}
	}
}
