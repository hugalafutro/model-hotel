package webauthn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// mintSession creates an auth-token session for the test and removes it again
// on cleanup, so the aged rows these tests leave behind never leak into the
// package's other tests (the repo is a shared Postgres database).
func mintSession(t *testing.T, repo *Repository, mgr *SessionManager, user string) string {
	t.Helper()
	token, err := mgr.CreateAuthToken(context.Background(), []byte(user), nil, SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	id := sessionByToken(t, repo, token).ID
	t.Cleanup(func() { _ = repo.DeleteSession(context.Background(), id) })
	return token
}

// ageSession rewrites a session's timestamps so a test can stand at any point
// of its life without waiting: created_at moves back by createdAgo, expires_at
// to now+expiresIn (what the last slide would have left), and last_seen_at
// back by lastSeenAgo so the throttled touch fires (or not).
func ageSession(t *testing.T, repo *Repository, token string, createdAgo, expiresIn, lastSeenAgo time.Duration) {
	t.Helper()
	sum := sha256.Sum256([]byte(token))
	if _, err := repo.pool.Exec(context.Background(),
		`UPDATE webauthn_sessions
		    SET created_at = now() - $2::interval,
		        expires_at = now() + $3::interval,
		        last_seen_at = now() - $4::interval
		  WHERE token_hash = $1`,
		hex.EncodeToString(sum[:]),
		createdAgo.String(), expiresIn.String(), lastSeenAgo.String()); err != nil {
		t.Fatalf("age session: %v", err)
	}
}

// within reports whether two instants agree to Postgres's microsecond storage
// precision plus a little slack.
func within(a, b time.Time, tol time.Duration) bool {
	d := a.Sub(b)
	return d > -tol && d < tol
}

// A session in use slides: once the throttled last-seen touch fires, expiry
// moves out to now + AuthTokenTTL and the caller is told so it can re-issue the
// cookie with the new lifetime.
func TestAuthenticate_ExtendsExpiryOnTouch(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	token := mintSession(t, repo, mgr, "admin-slide")
	// Two days in, last used (and slid) an hour ago, so the window has an hour
	// of room to grow.
	ageSession(t, repo, token, 48*time.Hour, AuthTokenTTL-time.Hour, time.Hour)
	before := sessionByToken(t, repo, token).ExpiresAt

	res, ok := mgr.Authenticate(ctx, token)
	if !ok {
		t.Fatal("token should validate")
	}
	if !res.Extended {
		t.Fatal("Extended = false, want true: the touch fired and the window had room to grow")
	}
	after := sessionByToken(t, repo, token).ExpiresAt
	if !after.After(before) {
		t.Fatalf("expires_at %v did not move past %v", after, before)
	}
	want := time.Now().Add(AuthTokenTTL)
	if d := after.Sub(want); d < -time.Minute || d > time.Minute {
		t.Errorf("expires_at = %v, want about now+TTL (%v)", after, want)
	}
	if !within(res.ExpiresAt, after, time.Millisecond) {
		t.Errorf("result ExpiresAt %v != stored %v", res.ExpiresAt, after)
	}
}

// Sliding rides the same throttle as the last-seen stamp: a request inside the
// window neither writes nor reports an extension, so a busy tab does not turn
// every call into an UPDATE plus a Set-Cookie.
func TestAuthenticate_NoExtensionInsideThrottle(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	token := mintSession(t, repo, mgr, "admin-throttled")
	if _, ok := mgr.Authenticate(ctx, token); !ok {
		t.Fatal("first validation should pass")
	}
	before := sessionByToken(t, repo, token).ExpiresAt

	res, ok := mgr.Authenticate(ctx, token)
	if !ok {
		t.Fatal("second validation should pass")
	}
	if res.Extended {
		t.Error("Extended = true within the throttle window")
	}
	if !sessionByToken(t, repo, token).ExpiresAt.Equal(before) {
		t.Error("expires_at moved within the throttle window")
	}
}

// The first request after login finds no last-seen stamp, so the touch fires,
// but the expiry is still within milliseconds of now+TTL: nothing worth an
// UPDATE and a Set-Cookie, so nothing is extended or reported.
func TestAuthenticate_FirstRequestAfterLoginDoesNotExtend(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	token := mintSession(t, repo, mgr, "admin-fresh")
	before := sessionByToken(t, repo, token).ExpiresAt

	res, ok := mgr.Authenticate(ctx, token)
	if !ok {
		t.Fatal("token should validate")
	}
	if res.Extended {
		t.Error("Extended = true on the first request after login")
	}
	if !sessionByToken(t, repo, token).ExpiresAt.Equal(before) {
		t.Error("expires_at moved on the first request after login")
	}
}

// The absolute cap: expiry never passes created_at + AuthTokenMaxLifetime no
// matter how active the session is, so a stolen cookie in constant use still
// dies. Near the cap the extension is clamped; at the cap nothing extends.
func TestAuthenticate_ClampsToMaxLifetime(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	token := mintSession(t, repo, mgr, "admin-cap")
	// One day short of the cap, last slid to twelve hours out: a full TTL would
	// overshoot, so the extension lands exactly on the cap.
	ageSession(t, repo, token, AuthTokenMaxLifetime-24*time.Hour, 12*time.Hour, time.Hour)
	created := sessionByToken(t, repo, token).CreatedAt

	res, ok := mgr.Authenticate(ctx, token)
	if !ok {
		t.Fatal("token should validate")
	}
	if !res.Extended {
		t.Fatal("Extended = false, want true: there was a day of room left")
	}
	lifetimeEnd := created.Add(AuthTokenMaxLifetime)
	got := sessionByToken(t, repo, token).ExpiresAt
	if d := got.Sub(lifetimeEnd); d < -time.Second || d > time.Second {
		t.Errorf("expires_at = %v, want clamped to the cap %v", got, lifetimeEnd)
	}

	// Sitting on the cap already (an hour left, expiry == cap): nothing to
	// extend, and the caller is not told to re-issue anything.
	ageSession(t, repo, token, AuthTokenMaxLifetime-time.Hour, time.Hour, time.Hour)
	before := sessionByToken(t, repo, token).ExpiresAt
	res, ok = mgr.Authenticate(ctx, token)
	if !ok {
		t.Fatal("token should still validate: the cap is an hour away")
	}
	if res.Extended {
		t.Error("Extended = true at the cap")
	}
	if !sessionByToken(t, repo, token).ExpiresAt.Equal(before) {
		t.Error("expires_at moved past the cap")
	}
}

// A session that outlived the cap is dead even if a stale expires_at says
// otherwise (rows minted under an older, longer TTL, or a clock the store did
// not clamp): the cap is enforced on validation, not only on extension.
func TestAuthenticate_RejectsPastMaxLifetime(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	token := mintSession(t, repo, mgr, "admin-dead")
	ageSession(t, repo, token, AuthTokenMaxLifetime+time.Hour, 24*time.Hour, time.Hour)
	if _, ok := mgr.Authenticate(ctx, token); ok {
		t.Error("a session older than the max lifetime validated")
	}
}

// A row minted under a longer fixed TTL (before sliding expiry) keeps its
// later expiry: sliding only ever moves expires_at forward, never back.
func TestAuthenticate_NeverShortensAnExistingExpiry(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	token := mintSession(t, repo, mgr, "admin-legacy")
	// Fresh row with a 7-day expiry and an aged last-seen so the touch fires.
	ageSession(t, repo, token, time.Hour, 7*24*time.Hour, time.Hour)
	before := sessionByToken(t, repo, token).ExpiresAt

	res, ok := mgr.Authenticate(ctx, token)
	if !ok {
		t.Fatal("token should validate")
	}
	if res.Extended {
		t.Error("Extended = true although now+TTL is earlier than the stored expiry")
	}
	if !sessionByToken(t, repo, token).ExpiresAt.Equal(before) {
		t.Error("expires_at was pulled back")
	}
}

// A failed extension write must not fail an otherwise valid authentication,
// mirroring the last-seen stamp; the caller is simply not told to re-issue.
func TestAuthenticate_ExtendFailureDoesNotFailValidation(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	token := mintSession(t, repo, mgr, "admin-extfail")
	ageSession(t, repo, token, 48*time.Hour, AuthTokenTTL-time.Hour, time.Hour)

	failing := NewSessionManager(&extendFailingStore{SessionStore: repo})
	res, ok := failing.Authenticate(ctx, token)
	if !ok {
		t.Fatal("validation failed because the extension write failed")
	}
	if res.Extended {
		t.Error("Extended = true although the write failed")
	}
}

// TokenUser keeps its contract as the thin wrapper every caller already uses.
func TestTokenUser_StillReturnsUserID(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	token := mintSession(t, repo, mgr, "admin-wrap")
	uid, ok := mgr.TokenUser(ctx, token)
	if !ok || string(uid) != "admin-wrap" {
		t.Errorf("TokenUser = (%q, %v), want (admin-wrap, true)", uid, ok)
	}
}

// extendFailingStore delegates everything to the wrapped store but refuses
// expiry extensions, isolating Authenticate's error branch.
type extendFailingStore struct {
	SessionStore
}

func (s *extendFailingStore) ExtendSession(context.Context, uuid.UUID, time.Time) error {
	return errors.New("simulated extend failure")
}

// Verify is the pure lookup the SSE heartbeats use: it admits a live session
// but neither stamps last-seen nor slides the expiry, however stale both are.
func TestVerify_WritesNothing(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	token := mintSession(t, repo, mgr, "admin-verify")
	ageSession(t, repo, token, 48*time.Hour, AuthTokenTTL-time.Hour, time.Hour)
	before := sessionByToken(t, repo, token)

	res, ok := mgr.Verify(ctx, token)
	if !ok || string(res.UserID) != "admin-verify" {
		t.Fatalf("Verify = (%+v, %v), want the live session", res, ok)
	}
	if res.Extended {
		t.Error("Verify reported an extension")
	}
	after := sessionByToken(t, repo, token)
	if !after.ExpiresAt.Equal(before.ExpiresAt) {
		t.Errorf("Verify slid expires_at from %v to %v", before.ExpiresAt, after.ExpiresAt)
	}
	if after.LastSeenAt == nil || before.LastSeenAt == nil || !after.LastSeenAt.Equal(*before.LastSeenAt) {
		t.Errorf("Verify stamped last_seen_at (%v -> %v)", before.LastSeenAt, after.LastSeenAt)
	}
	// And a dead session is still refused.
	ageSession(t, repo, token, AuthTokenMaxLifetime+time.Hour, 24*time.Hour, time.Hour)
	if _, ok := mgr.Verify(ctx, token); ok {
		t.Error("Verify admitted a session past the max lifetime")
	}
}

// The mint path leaves created_at to the store (Postgres default now()), and the
// cap reads it back: a scan or column regression there would silently uncap
// sliding, so pin that a freshly minted row carries a real creation time.
func TestCreateAuthToken_StoresCreatedAt(t *testing.T) {
	repo := newTestRepo(t)
	mgr := NewSessionManager(repo)

	token := mintSession(t, repo, mgr, "admin-created")
	s := sessionByToken(t, repo, token)
	if s.CreatedAt.IsZero() {
		t.Fatal("CreatedAt is zero on a freshly minted session")
	}
	if d := time.Since(s.CreatedAt); d < 0 || d > time.Minute {
		t.Errorf("CreatedAt = %v, want roughly now", s.CreatedAt)
	}
}

// A slid session is still the operator's live session: the active-sessions
// list keeps showing it, with the moved expiry.
func TestListAuthSessions_IncludesSlidSession(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	token := mintSession(t, repo, mgr, "admin-listslid")
	ageSession(t, repo, token, 48*time.Hour, AuthTokenTTL-time.Hour, time.Hour)
	res, ok := mgr.Authenticate(ctx, token)
	if !ok || !res.Extended {
		t.Fatalf("Authenticate = (%+v, %v), want a slid session", res, ok)
	}

	rows, err := repo.ListAuthSessionsForUser(ctx, []byte("admin-listslid"))
	if err != nil {
		t.Fatalf("ListAuthSessionsForUser: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("listed %d sessions, want the one slid session", len(rows))
	}
	if !within(rows[0].ExpiresAt, res.ExpiresAt, time.Millisecond) {
		t.Errorf("listed expiry %v, want the slid %v", rows[0].ExpiresAt, res.ExpiresAt)
	}
}

// TestAuthenticate_FutureLastSeenStillSlides covers the negative-duration hole
// in the throttle. last_seen_at is a timestamptz read back off the row, so a
// backwards step of the server's clock can leave it dated ahead of now.
// `now.Sub(*LastSeenAt) >= lastSeenThrottle` is then FALSE for the resulting
// negative duration, so the touch never fires - and because that touch is the
// only writer of last_seen_at, nothing corrects the stamp either. The session
// stops sliding entirely and expires at its original deadline while in active
// use, which for a user is an unexplained logout.
func TestAuthenticate_FutureLastSeenStillSlides(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	token := mintSession(t, repo, mgr, "admin-future-lastseen")
	// A negative "ago" puts last_seen_at in the future.
	ageSession(t, repo, token, 48*time.Hour, AuthTokenTTL-time.Hour, -2*time.Hour)
	before := sessionByToken(t, repo, token).ExpiresAt

	res, ok := mgr.Authenticate(ctx, token)
	if !ok {
		t.Fatal("token should validate")
	}
	if !res.Extended {
		t.Fatal("Extended = false: a future last_seen_at froze the slide, so the session expires while in use")
	}
	if after := sessionByToken(t, repo, token).ExpiresAt; !after.After(before) {
		t.Errorf("expires_at %v did not move past %v", after, before)
	}
}

// TestAuthenticate_FutureLastSeenSelfCorrects pins why refusing a future stamp
// costs at most one extra write per session rather than a write per request:
// the touch it releases rewrites last_seen_at with the current clock, so the
// throttle closes again immediately. Without this the guard would look like a
// write-amplification risk under a fleet-wide clock step.
func TestAuthenticate_FutureLastSeenSelfCorrects(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	token := mintSession(t, repo, mgr, "admin-selfcorrect-lastseen")
	ageSession(t, repo, token, 48*time.Hour, AuthTokenTTL-time.Hour, -2*time.Hour)

	if res, ok := mgr.Authenticate(ctx, token); !ok || !res.Extended {
		t.Fatalf("first call: ok=%v extended=%v, want both true", ok, res.Extended)
	}
	if seen := sessionByToken(t, repo, token).LastSeenAt; seen == nil || seen.After(time.Now().Add(time.Minute)) {
		t.Fatalf("the touch must rewrite last_seen_at to the current clock, got %v", seen)
	}
	// Second call: the stamp is sane again, so the throttle holds.
	res, ok := mgr.Authenticate(ctx, token)
	if !ok {
		t.Fatal("second call should validate")
	}
	if res.Extended {
		t.Error("Extended = true on the second call: the corrected stamp must re-close the throttle")
	}
}
