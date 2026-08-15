package webauthn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

// maxAuthTokenTTL and maxAuthTokenLifetime are the security ceilings on the
// browser-session idle window and absolute cap, independent of the values in
// force. Logging in does not revoke an existing session, so a stolen token
// stays usable until it expires: the idle window bounds a token nobody is
// using, the cap bounds one in constant use, and together they are the only
// bound on that exposure. Raising a ceiling is a security decision, not a
// tuning knob.
const (
	maxAuthTokenTTL      = 7 * 24 * time.Hour
	maxAuthTokenLifetime = 90 * 24 * time.Hour
)

// pinnedAuthTokenTTL and pinnedAuthTokenLifetime are the values in force,
// pinned so any change is a deliberate two-line edit (constant + pin) and
// cannot slip through unnoticed: every change must also sweep the user-facing
// copy listed on the AuthTokenTTL doc comment, which quotes them as "3 days"
// and "30 days".
const (
	pinnedAuthTokenTTL      = 72 * time.Hour
	pinnedAuthTokenLifetime = 30 * 24 * time.Hour
)

func TestAuthTokenTTL_WithinSecurityCeiling(t *testing.T) {
	if AuthTokenTTL > maxAuthTokenTTL {
		t.Errorf("AuthTokenTTL = %v, exceeds the %v ceiling: an idle stolen session survives that long unrevoked", AuthTokenTTL, maxAuthTokenTTL)
	}
	if AuthTokenMaxLifetime > maxAuthTokenLifetime {
		t.Errorf("AuthTokenMaxLifetime = %v, exceeds the %v ceiling: a stolen session in use survives that long unrevoked", AuthTokenMaxLifetime, maxAuthTokenLifetime)
	}
	if AuthTokenTTL > AuthTokenMaxLifetime {
		t.Errorf("AuthTokenTTL %v exceeds AuthTokenMaxLifetime %v: the idle window can never be longer than the cap", AuthTokenTTL, AuthTokenMaxLifetime)
	}
}

func TestAuthTokenTTL_IsPinned(t *testing.T) {
	if AuthTokenTTL != pinnedAuthTokenTTL {
		t.Errorf("AuthTokenTTL = %v, want %v: update the pin and sweep the copy that quotes the value", AuthTokenTTL, pinnedAuthTokenTTL)
	}
	if AuthTokenMaxLifetime != pinnedAuthTokenLifetime {
		t.Errorf("AuthTokenMaxLifetime = %v, want %v: update the pin and sweep the copy that quotes the value", AuthTokenMaxLifetime, pinnedAuthTokenLifetime)
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
