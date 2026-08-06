package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/authcookie"
	"github.com/hugalafutro/model-hotel/internal/user"
)

// The route, exercised through the real auth middleware rather than by calling
// the handler directly, because the bug this guards lives in the gap between
// them: resolveCredentials falls through to the bearer when the session cookie
// is invalid, so the middleware can authenticate one credential while a handler
// that reads the request itself sees another.
//
// Presenting a valid bearer alongside a junk cookie used to make the handler
// hand the manager the junk cookie, which was not a session, which meant "this
// caller must hold the raw admin token" and revoked every session under the
// admin handle. Any authenticated account could do it.
func TestRevokeOtherSessionsRoute_JunkCookieCannotTargetAdmin(t *testing.T) {
	r, userRepo, sessionMgr := setupUsersTest(t)
	ctx := context.Background()

	// A plain, lowest-privilege account and a session for it.
	victimAdminToken, err := sessionMgr.CreateAuthToken(ctx, []byte("admin"), nil)
	if err != nil {
		t.Fatalf("mint admin session: %v", err)
	}
	u := seedRouteUser(t, userRepo, "mallory", "user")
	attackerToken, err := sessionMgr.CreateAuthToken(ctx, []byte(u.ID.String()), nil)
	if err != nil {
		t.Fatalf("mint user session: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/auth/sessions/revoke-others", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+attackerToken)
	req.AddCookie(&http.Cookie{Name: authcookie.SessionCookie, Value: "garbage-not-a-session"})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var result revokeOthersResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if result.Revoked != 0 {
		t.Errorf("revoked %d sessions; a user account reached beyond its own identity", result.Revoked)
	}
	if _, ok := sessionMgr.TokenUser(ctx, victimAdminToken); !ok {
		t.Error("the admin session was revoked by a non-admin account")
	}
	if _, ok := sessionMgr.TokenUser(ctx, attackerToken); !ok {
		t.Error("the caller's own authenticating session was revoked")
	}
}

// The ordinary path still works through the middleware: a user's own other
// sessions go, and the one they called from stays.
func TestRevokeOtherSessionsRoute_EndsOnlyTheCallersOwnOtherSessions(t *testing.T) {
	r, userRepo, sessionMgr := setupUsersTest(t)
	ctx := context.Background()

	u := seedRouteUser(t, userRepo, "alice", "user")
	mine, err := sessionMgr.CreateAuthToken(ctx, []byte(u.ID.String()), nil)
	if err != nil {
		t.Fatal(err)
	}
	phone, err := sessionMgr.CreateAuthToken(ctx, []byte(u.ID.String()), nil)
	if err != nil {
		t.Fatal(err)
	}
	adminSession, err := sessionMgr.CreateAuthToken(ctx, []byte("admin"), nil)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/auth/sessions/revoke-others", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+mine)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	if _, ok := sessionMgr.TokenUser(ctx, mine); !ok {
		t.Error("the caller's own session was revoked")
	}
	if _, ok := sessionMgr.TokenUser(ctx, phone); ok {
		t.Error("the caller's other session survived")
	}
	if _, ok := sessionMgr.TokenUser(ctx, adminSession); !ok {
		t.Error("a different identity's session was revoked")
	}
}

// seedRouteUser creates an enabled account directly, so the test does not
// depend on the create-user API surface it is not exercising.
func seedRouteUser(t *testing.T, repo *user.Repository, username, role string) *user.User {
	t.Helper()
	hash, err := user.HashPassword(context.Background(), "correct-horse-1")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	u, err := repo.Create(context.Background(), username, username, nil, hash, user.Role(role), nil, user.Limits{}, nil)
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u
}
