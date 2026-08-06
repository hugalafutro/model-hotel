package webauthn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

// maxAuthTokenTTL is the ceiling this deployment accepts for a browser session.
// A stolen session token stays usable until it expires and nothing at login
// revokes it (see plans/2026-08-06-session-lifetime-plan.md), so the TTL is the
// only bound on that exposure. Raising it past this ceiling is a security
// decision, not a tuning knob, and must fail here first.
const maxAuthTokenTTL = 7 * 24 * time.Hour

func TestAuthTokenTTL_WithinSecurityCeiling(t *testing.T) {
	if AuthTokenTTL > maxAuthTokenTTL {
		t.Errorf("AuthTokenTTL = %v, exceeds the %v ceiling: a stolen session survives that long unrevoked", AuthTokenTTL, maxAuthTokenTTL)
	}
}

// TestCreateAuthToken_StampsExpiryFromTTL locks the minted session's expiry to
// the shared constant. Cookie MaxAge at every login front-end reads the same
// value, so a hardcoded expiry here would silently desync the cookie from the
// server-side session.
func TestCreateAuthToken_StampsExpiryFromTTL(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	before := time.Now()
	token, err := mgr.CreateAuthToken(ctx, []byte("admin"), nil)
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}
	after := time.Now()

	sum := sha256.Sum256([]byte(token))
	session, err := repo.GetSessionByTokenHash(ctx, hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatalf("GetSessionByTokenHash: %v", err)
	}

	earliest := before.Add(AuthTokenTTL)
	latest := after.Add(AuthTokenTTL)
	if session.ExpiresAt.Before(earliest.Add(-time.Second)) || session.ExpiresAt.After(latest.Add(time.Second)) {
		t.Errorf("ExpiresAt = %v, want now+AuthTokenTTL (between %v and %v)", session.ExpiresAt, earliest, latest)
	}
}
