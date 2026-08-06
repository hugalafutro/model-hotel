package webauthn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func sessionExists(t *testing.T, repo *Repository, token string) bool {
	t.Helper()
	sum := sha256.Sum256([]byte(token))
	_, err := repo.GetSessionByTokenHash(context.Background(), hex.EncodeToString(sum[:]))
	return err == nil
}

// The point of the action: every other session for this identity dies while the
// one the operator is using survives, so signing out other devices does not sign
// them out of the page they clicked it on.
func TestRevokeOtherSessions_KeepsTheCallersOwnSession(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	identity := []byte("admin-keeps-own")
	mine, err := mgr.CreateAuthToken(ctx, identity, nil)
	if err != nil {
		t.Fatal(err)
	}
	phone, err := mgr.CreateAuthToken(ctx, identity, nil)
	if err != nil {
		t.Fatal(err)
	}
	laptop, err := mgr.CreateAuthToken(ctx, identity, nil)
	if err != nil {
		t.Fatal(err)
	}

	revoked, err := mgr.RevokeOtherSessions(ctx, mine, identity)
	if err != nil {
		t.Fatalf("RevokeOtherSessions: %v", err)
	}
	if revoked != 2 {
		t.Errorf("revoked = %d, want 2", revoked)
	}
	if !sessionExists(t, repo, mine) {
		t.Error("the caller's own session was revoked; they would be logged out by their own click")
	}
	if sessionExists(t, repo, phone) || sessionExists(t, repo, laptop) {
		t.Error("another session survived")
	}
}

// Sessions belonging to someone else are not this operator's to revoke.
func TestRevokeOtherSessions_LeavesOtherIdentitiesAlone(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	identity := []byte("admin-scoped")
	mine, err := mgr.CreateAuthToken(ctx, identity, nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := mgr.CreateAuthToken(ctx, []byte("2f9c3a1e-0000-4000-8000-000000000001"), nil)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := mgr.RevokeOtherSessions(ctx, mine, identity); err != nil {
		t.Fatalf("RevokeOtherSessions: %v", err)
	}
	if !sessionExists(t, repo, other) {
		t.Error("revoked a different identity's session")
	}
}

// An operator authenticated with the raw admin token holds no session, so there
// is nothing to keep and every admin session goes. Without this the action would
// silently do nothing in exactly the case an operator reaches for it: they still
// have the admin token but suspect a browser session is stolen.
func TestRevokeOtherSessions_RawAdminTokenRevokesAll(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	identity := []byte("admin-raw-token")
	browser, err := mgr.CreateAuthToken(ctx, identity, nil)
	if err != nil {
		t.Fatal(err)
	}

	revoked, err := mgr.RevokeOtherSessions(ctx, "not-a-session-token", identity)
	if err != nil {
		t.Fatalf("RevokeOtherSessions: %v", err)
	}
	if revoked != 1 {
		t.Errorf("revoked = %d, want 1", revoked)
	}
	if sessionExists(t, repo, browser) {
		t.Error("session survived a revoke-all from the raw admin token")
	}
}

func TestRevokeOtherSessions_NoOtherSessionsIsNotAnError(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	identity := []byte("admin-solo")
	mine, err := mgr.CreateAuthToken(ctx, identity, nil)
	if err != nil {
		t.Fatal(err)
	}

	revoked, err := mgr.RevokeOtherSessions(ctx, mine, identity)
	if err != nil {
		t.Fatalf("RevokeOtherSessions: %v", err)
	}
	if revoked != 0 {
		t.Errorf("revoked = %d, want 0", revoked)
	}
	if !sessionExists(t, repo, mine) {
		t.Error("the only session was revoked")
	}
}
