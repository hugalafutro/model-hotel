package webauthn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

// maxAuthTokenTTL is the ceiling this deployment accepts for a browser session.
// Logging in does not revoke an existing session, so a stolen token stays usable
// until it expires and this TTL is the only bound on that exposure. Raising it
// past this ceiling is a security decision, not a tuning knob, and must fail
// here first. The literal is duplicated deliberately: deriving it from
// AuthTokenTTL would make the assertion tautological.
const maxAuthTokenTTL = 7 * 24 * time.Hour

func TestAuthTokenTTL_WithinSecurityCeiling(t *testing.T) {
	if AuthTokenTTL > maxAuthTokenTTL {
		t.Errorf("AuthTokenTTL = %v, exceeds the %v ceiling: a stolen session survives that long unrevoked", AuthTokenTTL, maxAuthTokenTTL)
	}
}

// TestCreateAuthToken_StampsExpiryFromTTL locks the minted session's expiry to
// the shared constant, so a literal duration reintroduced here cannot silently
// change how long sessions live. The matching cookie-side guard lives in
// adminauth's TestUserLogin_CookieMaxAgeMatchesSessionTTL.
func TestCreateAuthToken_StampsExpiryFromTTL(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	before := time.Now()
	token, err := mgr.CreateAuthToken(ctx, []byte("admin"), nil, SessionMeta{})
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
