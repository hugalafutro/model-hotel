package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
)

// The handler must hand the manager the token the request actually carried, or
// the manager cannot tell which session to keep and would sign the caller out
// of the page they clicked from.
func TestRevokeOtherSessions_PassesTheCallersSessionCookie(t *testing.T) {
	h := &Handler{}
	var gotToken string
	var gotFallback []byte
	h.SetWebAuthnSessionManager(&mockWebAuthnSessionMgr{
		revokeOthersFn: func(_ context.Context, currentToken string, fallbackUser []byte) (int64, error) {
			gotToken, gotFallback = currentToken, fallbackUser
			return 3, nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/auth/sessions/revoke-others", http.NoBody)
	req.AddCookie(&http.Cookie{Name: authcookie.SessionCookie, Value: "session-abc"})
	w := httptest.NewRecorder()
	h.RevokeOtherSessions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	if gotToken != "session-abc" {
		t.Errorf("token = %q, want the session cookie's value", gotToken)
	}
	if string(gotFallback) != "admin" {
		t.Errorf("fallback identity = %q, want admin", gotFallback)
	}

	var result revokeOthersResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result.Revoked != 3 {
		t.Errorf("revoked = %d, want 3", result.Revoked)
	}
}

// A caller on the raw admin token holds no session cookie; the bearer token is
// still what identifies them, so it must reach the manager.
func TestRevokeOtherSessions_FallsBackToTheBearerToken(t *testing.T) {
	h := &Handler{}
	var gotToken string
	h.SetWebAuthnSessionManager(&mockWebAuthnSessionMgr{
		revokeOthersFn: func(_ context.Context, currentToken string, _ []byte) (int64, error) {
			gotToken = currentToken
			return 0, nil
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/auth/sessions/revoke-others", http.NoBody)
	req.Header.Set("Authorization", "Bearer raw-admin-token")
	w := httptest.NewRecorder()
	h.RevokeOtherSessions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	if gotToken != "raw-admin-token" {
		t.Errorf("token = %q, want the bearer token", gotToken)
	}
}

func TestRevokeOtherSessions_ReportsStoreFailure(t *testing.T) {
	h := &Handler{}
	h.SetWebAuthnSessionManager(&mockWebAuthnSessionMgr{
		revokeOthersFn: func(context.Context, string, []byte) (int64, error) {
			return 0, context.DeadlineExceeded
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/auth/sessions/revoke-others", http.NoBody)
	w := httptest.NewRecorder()
	h.RevokeOtherSessions(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500: a failed revoke must not report success", w.Code)
	}
}

// With no session manager wired there is nothing to revoke, and claiming
// success would tell an operator their other sessions are gone when they are
// not.
func TestRevokeOtherSessions_WithoutSessionManager(t *testing.T) {
	h := &Handler{}

	req := httptest.NewRequest(http.MethodPost, "/auth/sessions/revoke-others", http.NoBody)
	w := httptest.NewRecorder()
	h.RevokeOtherSessions(w, req)

	if w.Code != http.StatusPreconditionFailed {
		t.Errorf("status = %d, want 412", w.Code)
	}
}
