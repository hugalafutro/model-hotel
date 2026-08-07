package adminauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// newTestSessionWithAdminToken returns a session manager backed by the in-memory
// store the OIDC suite uses (no database) holding one valid admin session token.
func newTestSessionWithAdminToken(t *testing.T) (*webauthn.SessionManager, string) {
	t.Helper()
	sessionMgr := webauthn.NewSessionManager(newMemStore())
	tok, err := sessionMgr.CreateAuthToken(context.Background(), []byte("admin"), nil)
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
