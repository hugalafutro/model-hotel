package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
)

// logoutHandler builds a mock-backed Handler for the always-available logout
// endpoint, tracking whether and with what token RevokeAuthToken was called.
func logoutHandler(t *testing.T, revoked *string, revokeCalled *bool) *Handler {
	t.Helper()
	h := testHandler(nil, nil, nil, nil, nil)
	h.SetWebAuthnSessionManager(&mockWebAuthnSessionMgr{
		revokeFn: func(_ context.Context, token string) bool {
			if revokeCalled != nil {
				*revokeCalled = true
			}
			if revoked != nil {
				*revoked = token
			}
			return true
		},
	})
	return h
}

func TestAuthLogout_CookieToken_ClearsCookieAndRevokes(t *testing.T) {
	var revokedTok string
	var called bool
	h := logoutHandler(t, &revokedTok, &called)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", http.NoBody)
	req.AddCookie(&http.Cookie{Name: authcookie.SessionCookie, Value: "cookie-tok"})
	rec := httptest.NewRecorder()

	h.AuthLogout(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("logout = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatal("expected RevokeAuthToken to be called")
	}
	if revokedTok != "cookie-tok" {
		t.Errorf("revoked token = %q, want %q", revokedTok, "cookie-tok")
	}
	c := cookieByName(rec, authcookie.SessionCookie)
	if c == nil {
		t.Fatal("expected an mh_session cookie to be set (cleared)")
	}
	if c.MaxAge >= 0 {
		t.Errorf("session cookie MaxAge = %d, want negative (expiring)", c.MaxAge)
	}
}

func TestAuthLogout_BearerToken_ClearsCookieAndRevokes(t *testing.T) {
	var revokedTok string
	var called bool
	h := logoutHandler(t, &revokedTok, &called)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", http.NoBody)
	req.Header.Set("Authorization", "Bearer bearer-tok")
	rec := httptest.NewRecorder()

	h.AuthLogout(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("logout = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if !called {
		t.Fatal("expected RevokeAuthToken to be called")
	}
	if revokedTok != "bearer-tok" {
		t.Errorf("revoked token = %q, want %q", revokedTok, "bearer-tok")
	}
	c := cookieByName(rec, authcookie.SessionCookie)
	if c == nil {
		t.Fatal("expected an mh_session cookie to be set (cleared)")
	}
	if c.MaxAge >= 0 {
		t.Errorf("session cookie MaxAge = %d, want negative (expiring)", c.MaxAge)
	}
}

func TestAuthLogout_NoToken_StillClearsCookieIdempotently(t *testing.T) {
	var called bool
	h := logoutHandler(t, nil, &called)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/logout", http.NoBody)
	rec := httptest.NewRecorder()

	h.AuthLogout(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("logout = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if called {
		t.Error("RevokeAuthToken should not be called when no token is presented")
	}
	c := cookieByName(rec, authcookie.SessionCookie)
	if c == nil {
		t.Fatal("expected an mh_session cookie clear even with no token (idempotent)")
	}
	if c.MaxAge >= 0 {
		t.Errorf("session cookie MaxAge = %d, want negative (expiring)", c.MaxAge)
	}
}
