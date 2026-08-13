package webauthn

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func sessionByToken(t *testing.T, repo *Repository, token string) *SessionRecord {
	t.Helper()
	sum := sha256.Sum256([]byte(token))
	s, err := repo.GetSessionByTokenHash(context.Background(), hex.EncodeToString(sum[:]))
	if err != nil {
		t.Fatalf("GetSessionByTokenHash: %v", err)
	}
	return s
}

// The device metadata captured at login is what the sessions list shows the
// operator; losing it between mint and store would leave every row an
// "unknown device".
func TestCreateAuthToken_StoresDeviceMeta(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	token, err := mgr.CreateAuthToken(ctx, []byte("admin-meta"), nil, SessionMeta{
		UserAgent: "Mozilla/5.0 (X11; Linux x86_64) Firefox/141.0",
		IP:        "203.0.113.7",
	})
	if err != nil {
		t.Fatal(err)
	}

	s := sessionByToken(t, repo, token)
	if s.UserAgent != "Mozilla/5.0 (X11; Linux x86_64) Firefox/141.0" {
		t.Errorf("UserAgent = %q, want the minted user agent", s.UserAgent)
	}
	if s.IP != "203.0.113.7" {
		t.Errorf("IP = %q, want 203.0.113.7", s.IP)
	}
	if s.LastSeenAt != nil {
		t.Errorf("LastSeenAt = %v, want nil before first validation", s.LastSeenAt)
	}
}

// A session's last-seen stamp is what tells the operator a stolen token is
// still in use. The first validation must set it.
func TestTokenUser_StampsLastSeen(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	token, err := mgr.CreateAuthToken(ctx, []byte("admin-lastseen"), nil, SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := mgr.TokenUser(ctx, token); !ok {
		t.Fatal("token should validate")
	}
	s := sessionByToken(t, repo, token)
	if s.LastSeenAt == nil {
		t.Fatal("LastSeenAt still nil after a successful validation")
	}
	if time.Since(*s.LastSeenAt) > time.Minute {
		t.Errorf("LastSeenAt = %v, want roughly now", *s.LastSeenAt)
	}
}

// Validation runs on every admin request; the stamp must not turn each one
// into a DB write. A validation right after the last stamp leaves it alone.
func TestTokenUser_ThrottlesLastSeenWrites(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	token, err := mgr.CreateAuthToken(ctx, []byte("admin-throttle"), nil, SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := mgr.TokenUser(ctx, token); !ok {
		t.Fatal("token should validate")
	}
	first := sessionByToken(t, repo, token).LastSeenAt
	if first == nil {
		t.Fatal("LastSeenAt not set by first validation")
	}

	if _, ok := mgr.TokenUser(ctx, token); !ok {
		t.Fatal("token should still validate")
	}
	second := sessionByToken(t, repo, token).LastSeenAt
	if second == nil || !second.Equal(*first) {
		t.Errorf("LastSeenAt moved from %v to %v within the throttle window", first, second)
	}
}

// Once the stamp is older than the throttle window, the next validation
// refreshes it; otherwise "last seen" would freeze at first use.
func TestTokenUser_RestampsAfterThrottleWindow(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	token, err := mgr.CreateAuthToken(ctx, []byte("admin-restamp"), nil, SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := mgr.TokenUser(ctx, token); !ok {
		t.Fatal("token should validate")
	}

	sum := sha256.Sum256([]byte(token))
	if _, err := repo.pool.Exec(ctx,
		`UPDATE webauthn_sessions SET last_seen_at = now() - interval '1 hour' WHERE token_hash = $1`,
		hex.EncodeToString(sum[:])); err != nil {
		t.Fatalf("age the stamp: %v", err)
	}

	if _, ok := mgr.TokenUser(ctx, token); !ok {
		t.Fatal("token should still validate")
	}
	s := sessionByToken(t, repo, token)
	if s.LastSeenAt == nil || time.Since(*s.LastSeenAt) > time.Minute {
		t.Errorf("LastSeenAt = %v, want refreshed to roughly now", s.LastSeenAt)
	}
}

// The list shows the caller their own live sessions and nothing else: the
// request's own session is flagged current, expired rows are dead already, and
// another identity's sessions are not this operator's business.
func TestListAuthSessions_ScopesMarksAndSkips(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	identity := []byte("admin-list")
	mine, err := mgr.CreateAuthToken(ctx, identity, nil, SessionMeta{UserAgent: "here", IP: "198.51.100.1"})
	if err != nil {
		t.Fatal(err)
	}
	phone, err := mgr.CreateAuthToken(ctx, identity, nil, SessionMeta{UserAgent: "phone", IP: "198.51.100.2"})
	if err != nil {
		t.Fatal(err)
	}
	expired, err := mgr.CreateAuthToken(ctx, identity, nil, SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	expiredSum := sha256.Sum256([]byte(expired))
	if _, err := repo.pool.Exec(ctx,
		`UPDATE webauthn_sessions SET expires_at = now() - interval '1 hour' WHERE token_hash = $1`,
		hex.EncodeToString(expiredSum[:])); err != nil {
		t.Fatalf("age the session: %v", err)
	}
	// TestCleanupExpiredSessions counts the DB's expired rows exactly; don't
	// leak this one into its tally.
	t.Cleanup(func() {
		_, _ = repo.pool.Exec(context.Background(),
			`DELETE FROM webauthn_sessions WHERE token_hash = $1`, hex.EncodeToString(expiredSum[:]))
	})
	if _, err := mgr.CreateAuthToken(ctx, []byte("someone-else"), nil, SessionMeta{}); err != nil {
		t.Fatal(err)
	}

	sessions, err := mgr.ListAuthSessions(ctx, identity, mine)
	if err != nil {
		t.Fatalf("ListAuthSessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("len = %d, want 2 (expired and foreign rows excluded)", len(sessions))
	}

	byUA := map[string]AuthSessionInfo{}
	for _, s := range sessions {
		byUA[s.UserAgent] = s
	}
	cur, ok := byUA["here"]
	if !ok || !cur.Current {
		t.Errorf("the caller's own session is not marked current: %+v", sessions)
	}
	// The calling session leads the list regardless of age ("here" was minted
	// before "phone", so newest-first alone would bury it): the operator's
	// anchor row must not sit pages deep in a long list.
	if !sessions[0].Current {
		t.Errorf("the current session is not first: %+v", sessions)
	}
	if cur.IP != "198.51.100.1" {
		t.Errorf("current session IP = %q, want 198.51.100.1", cur.IP)
	}
	other, ok := byUA["phone"]
	if !ok || other.Current {
		t.Errorf("the other session is missing or wrongly current: %+v", sessions)
	}
	if other.ID == uuid.Nil || other.CreatedAt.IsZero() {
		t.Errorf("listing lost ID/CreatedAt: %+v", other)
	}
	_ = sessionByToken(t, repo, phone)
}

// An operator on the raw admin token has no session of their own; no row is
// current, and a foreign token must not mark anything.
func TestListAuthSessions_ForeignTokenMarksNothingCurrent(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	identity := []byte("admin-list-raw")
	if _, err := mgr.CreateAuthToken(ctx, identity, nil, SessionMeta{}); err != nil {
		t.Fatal(err)
	}
	stranger, err := mgr.CreateAuthToken(ctx, []byte("stranger-list"), nil, SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}

	sessions, err := mgr.ListAuthSessions(ctx, identity, "junk-token", stranger)
	if err != nil {
		t.Fatalf("ListAuthSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("len = %d, want 1", len(sessions))
	}
	if sessions[0].Current {
		t.Error("a session is marked current though the caller holds no session for this identity")
	}
}

// The per-row action: revoking one of your own other sessions kills exactly
// that session.
func TestRevokeSessionByID_RevokesOwnOtherSession(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	identity := []byte("admin-revoke-one")
	mine, err := mgr.CreateAuthToken(ctx, identity, nil, SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	phone, err := mgr.CreateAuthToken(ctx, identity, nil, SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	phoneID := sessionByToken(t, repo, phone).ID

	if err := mgr.RevokeSessionByID(ctx, identity, phoneID, mine); err != nil {
		t.Fatalf("RevokeSessionByID: %v", err)
	}
	if sessionExists(t, repo, phone) {
		t.Error("the targeted session survived")
	}
	if !sessionExists(t, repo, mine) {
		t.Error("the caller's own session died as collateral")
	}
}

// Aiming the per-row revoke at the session the request rides on is refused
// with a distinct error: the UI hides that button, so reaching this means a
// stale list or a crafted request, and either way "sign out this device" must
// not silently mean "sign me out".
func TestRevokeSessionByID_RefusesCurrentSession(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	identity := []byte("admin-revoke-self")
	mine, err := mgr.CreateAuthToken(ctx, identity, nil, SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	mineID := sessionByToken(t, repo, mine).ID

	err = mgr.RevokeSessionByID(ctx, identity, mineID, mine)
	if !errors.Is(err, ErrCurrentSession) {
		t.Fatalf("err = %v, want ErrCurrentSession", err)
	}
	if !sessionExists(t, repo, mine) {
		t.Error("the current session was revoked despite the refusal")
	}
}

// Another identity's session id must read as not-found — revoking it would
// cross identities, and a distinct error would confirm the id exists.
func TestRevokeSessionByID_ForeignSessionReadsNotFound(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	stranger, err := mgr.CreateAuthToken(ctx, []byte("stranger-revoke"), nil, SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	strangerID := sessionByToken(t, repo, stranger).ID

	err = mgr.RevokeSessionByID(ctx, []byte("admin-revoke-foreign"), strangerID)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
	if !sessionExists(t, repo, stranger) {
		t.Error("a foreign identity's session was revoked")
	}
}

// No identity means no rows: the store must not be queried with an empty user
// id, which matches nothing today but is one schema change away from matching
// the wrong rows.
func TestListAuthSessions_NoIdentityListsNothing(t *testing.T) {
	repo := newTestRepo(t)
	mgr := NewSessionManager(repo)

	sessions, err := mgr.ListAuthSessions(context.Background(), nil, "some-token")
	if err != nil {
		t.Fatalf("ListAuthSessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("len = %d, want 0", len(sessions))
	}
}

// A store failure surfaces rather than reading as "no sessions", which would
// tell an operator a stolen session is gone when nothing was checked.
func TestListAuthSessions_StoreFailureSurfaces(t *testing.T) {
	repo := newTestRepo(t)
	mgr := NewSessionManager(repo)

	canceled, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := mgr.ListAuthSessions(canceled, []byte("admin"), "some-token"); err == nil {
		t.Error("a canceled context should surface an error")
	}
}

// Empty candidate slots are the normal shape of a bearer-only or cookie-only
// request (the other credential is ""); they must be skipped, not hashed.
func TestListAuthSessions_SkipsEmptyCandidates(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	identity := []byte("admin-empty-candidates")
	mine, err := mgr.CreateAuthToken(ctx, identity, nil, SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}

	sessions, err := mgr.ListAuthSessions(ctx, identity, "", mine)
	if err != nil {
		t.Fatalf("ListAuthSessions: %v", err)
	}
	if len(sessions) != 1 || !sessions[0].Current {
		t.Errorf("sessions = %+v, want the one session marked current", sessions)
	}
}

func TestRevokeSessionByID_NoIdentityReadsNotFound(t *testing.T) {
	repo := newTestRepo(t)
	mgr := NewSessionManager(repo)

	if err := mgr.RevokeSessionByID(context.Background(), nil, uuid.New()); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

// The empty candidate slot of a bearer-only request must not read as "not the
// current session was offered": the real token beside it still spares it.
func TestRevokeSessionByID_SkipsEmptyCandidates(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	identity := []byte("admin-revoke-empty-cand")
	mine, err := mgr.CreateAuthToken(ctx, identity, nil, SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	mineID := sessionByToken(t, repo, mine).ID

	if err := mgr.RevokeSessionByID(ctx, identity, mineID, "", mine); !errors.Is(err, ErrCurrentSession) {
		t.Fatalf("err = %v, want ErrCurrentSession", err)
	}
}

// A failed last-seen stamp must not fail an otherwise valid authentication:
// the operator's session working matters more than its freshness metadata.
func TestTokenUser_StampFailureDoesNotFailValidation(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	token, err := mgr.CreateAuthToken(ctx, []byte("admin-stamp-fail"), nil, SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}

	// A store whose Touch always fails, over the same underlying repo.
	failing := NewSessionManager(&touchFailingStore{SessionStore: repo})
	if _, ok := failing.TokenUser(ctx, token); !ok {
		t.Error("validation failed because the last-seen stamp failed")
	}
}

// touchFailingStore delegates everything to the wrapped store but refuses
// last-seen stamps, isolating TokenUser's error branch.
type touchFailingStore struct {
	SessionStore
}

func (s *touchFailingStore) TouchSessionLastSeen(context.Context, uuid.UUID, time.Time) error {
	return errors.New("simulated touch failure")
}

// The Postgres list surfaces a row it cannot scan rather than returning a
// silently truncated list.
func TestListAuthSessionsForUser_ScanError(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()
	mgr := NewSessionManager(repo)

	identity := []byte("admin-list-scan-error")
	if _, err := mgr.CreateAuthToken(ctx, identity, nil, SessionMeta{}); err != nil {
		t.Fatal(err)
	}

	// NOTE: mutates a package-level variable; do not use t.Parallel() here.
	origRowsScan := rowsScan
	rowsScan = func(pgx.Rows, ...any) error {
		return errors.New("simulated scan error")
	}
	defer func() { rowsScan = origRowsScan }()

	if _, err := repo.ListAuthSessionsForUser(ctx, identity); err == nil {
		t.Error("a scan failure should surface an error")
	}
}

func TestRevokeSessionByID_MissingReadsNotFound(t *testing.T) {
	repo := newTestRepo(t)
	mgr := NewSessionManager(repo)

	err := mgr.RevokeSessionByID(context.Background(), []byte("admin-revoke-missing"), uuid.New())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}
