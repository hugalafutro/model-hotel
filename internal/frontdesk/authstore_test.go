package frontdesk

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"

	totppkg "github.com/hugalafutro/model-hotel/internal/totp"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

func newCred(id string) *webauthn.CredentialRecord {
	return &webauthn.CredentialRecord{
		ID:              []byte(id),
		Name:            id,
		PublicKey:       []byte("pubkey-" + id),
		AttestationType: "none",
		Transport:       []string{"usb", "nfc"},
		FlagsByte:       0x45,
		SignCount:       3,
		AAGUID:          uuid.New(),
	}
}

func TestWebAuthnStoreCredentialCRUD(t *testing.T) {
	s := newTestStore(t)
	store := NewWebAuthnStore(s)
	ctx := context.Background()

	cred := newCred("cred-a")
	if err := store.StoreCredential(ctx, cred); err != nil {
		t.Fatalf("StoreCredential: %v", err)
	}

	got, err := store.GetCredentialByID(ctx, []byte("cred-a"))
	if err != nil {
		t.Fatalf("GetCredentialByID: %v", err)
	}
	if got.Name != "cred-a" || got.FlagsByte != 0x45 || got.SignCount != 3 {
		t.Errorf("scalar round-trip wrong: %+v", got)
	}
	if len(got.Transport) != 2 || got.Transport[0] != "usb" {
		t.Errorf("transport round-trip: %v", got.Transport)
	}
	if got.AAGUID != cred.AAGUID {
		t.Errorf("aaguid round-trip: %v != %v", got.AAGUID, cred.AAGUID)
	}

	if err := store.RenameCredential(ctx, []byte("cred-a"), "renamed"); err != nil {
		t.Fatalf("RenameCredential: %v", err)
	}
	if err := store.UpdateSignCount(ctx, []byte("cred-a"), 99); err != nil {
		t.Fatalf("UpdateSignCount: %v", err)
	}
	got, _ = store.GetCredentialByID(ctx, []byte("cred-a"))
	if got.Name != "renamed" || got.SignCount != 99 {
		t.Errorf("after update: name=%q signCount=%d", got.Name, got.SignCount)
	}

	list, err := store.ListCredentials(ctx)
	if err != nil || len(list) != 1 {
		t.Fatalf("ListCredentials: len=%d err=%v", len(list), err)
	}

	// Not-found mapping.
	if _, err := store.GetCredentialByID(ctx, []byte("missing")); !errors.Is(err, webauthn.ErrNotFound) {
		t.Errorf("GetCredentialByID(missing): want ErrNotFound, got %v", err)
	}
	if err := store.RenameCredential(ctx, []byte("missing"), "x"); !errors.Is(err, webauthn.ErrNotFound) {
		t.Errorf("RenameCredential(missing): want ErrNotFound, got %v", err)
	}
	if err := store.UpdateSignCount(ctx, []byte("missing"), 1); !errors.Is(err, webauthn.ErrNotFound) {
		t.Errorf("UpdateSignCount(missing): want ErrNotFound, got %v", err)
	}
}

func TestWebAuthnStoreUpsert(t *testing.T) {
	s := newTestStore(t)
	store := NewWebAuthnStore(s)
	ctx := context.Background()

	cred := newCred("cred-up")
	_ = store.StoreCredential(ctx, cred)
	cred.Name = "second-write"
	cred.SignCount = 50
	if err := store.StoreCredential(ctx, cred); err != nil {
		t.Fatalf("re-StoreCredential (upsert): %v", err)
	}
	list, _ := store.ListCredentials(ctx)
	if len(list) != 1 {
		t.Fatalf("upsert created duplicate: %d rows", len(list))
	}
	if list[0].Name != "second-write" || list[0].SignCount != 50 {
		t.Errorf("upsert did not update: %+v", list[0])
	}
}

// TestSessionManagerOverSQLite exercises the reused SessionManager end to end
// over the SQLite SessionStore: create an auth token, validate it, revoke it.
func TestSessionManagerOverSQLite(t *testing.T) {
	s := newTestStore(t)
	store := NewWebAuthnStore(s)
	mgr := webauthn.NewSessionManager(store)
	ctx := context.Background()

	token, err := mgr.CreateAuthToken(ctx, []byte("admin"), []byte("cred-x"), webauthn.SessionMeta{})
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}
	if !mgr.Validate(ctx, token) {
		t.Fatal("freshly created token should validate")
	}
	if mgr.Validate(ctx, "not-a-real-token") {
		t.Fatal("bogus token must not validate")
	}
	if !mgr.RevokeAuthToken(ctx, token) {
		t.Fatal("RevokeAuthToken should return true")
	}
	if mgr.Validate(ctx, token) {
		t.Fatal("revoked token must not validate")
	}
}

// TestDeleteCredentialCascadesSessions verifies the transactional cascade:
// deleting a credential revokes the auth_token sessions derived from it.
func TestDeleteCredentialCascadesSessions(t *testing.T) {
	s := newTestStore(t)
	store := NewWebAuthnStore(s)
	mgr := webauthn.NewSessionManager(store)
	ctx := context.Background()

	if err := store.StoreCredential(ctx, newCred("cred-del")); err != nil {
		t.Fatalf("StoreCredential: %v", err)
	}
	token, err := mgr.CreateAuthToken(ctx, []byte("admin"), []byte("cred-del"), webauthn.SessionMeta{})
	if err != nil {
		t.Fatalf("CreateAuthToken: %v", err)
	}
	if !mgr.Validate(ctx, token) {
		t.Fatal("token should validate before delete")
	}

	if err := store.DeleteCredential(ctx, []byte("cred-del")); err != nil {
		t.Fatalf("DeleteCredential: %v", err)
	}
	if mgr.Validate(ctx, token) {
		t.Fatal("session derived from a deleted credential must be revoked")
	}
	if err := store.DeleteCredential(ctx, []byte("cred-del")); !errors.Is(err, webauthn.ErrNotFound) {
		t.Errorf("second delete: want ErrNotFound, got %v", err)
	}
}

func TestCleanupExpiredSessions(t *testing.T) {
	s := newTestStore(t)
	store := NewWebAuthnStore(s)
	ctx := context.Background()

	expired := &webauthn.SessionRecord{
		ID: uuid.New(), Challenge: "c", SessionData: []byte("{}"), Type: "login",
		ExpiresAt: time.Now().Add(-1 * time.Hour),
	}
	live := &webauthn.SessionRecord{
		ID: uuid.New(), Challenge: "c", SessionData: []byte("{}"), Type: "login",
		ExpiresAt: time.Now().Add(1 * time.Hour),
	}
	_ = store.CreateSession(ctx, expired)
	_ = store.CreateSession(ctx, live)

	n, err := store.CleanupExpiredSessions(ctx)
	if err != nil {
		t.Fatalf("CleanupExpiredSessions: %v", err)
	}
	if n != 1 {
		t.Errorf("cleaned %d, want 1", n)
	}
	if _, err := store.GetSession(ctx, expired.ID); !errors.Is(err, webauthn.ErrNotFound) {
		t.Errorf("expired session should be gone, got %v", err)
	}
	if _, err := store.GetSession(ctx, live.ID); err != nil {
		t.Errorf("live session should remain, got %v", err)
	}
}

// TestTOTPRepositoryOverSQLite exercises the reused totp.Repository end to end
// over the SQLite Store: enroll, verify (with single-use replay rejection),
// enable, recovery codes, and disable-with-code.
func TestTOTPRepositoryOverSQLite(t *testing.T) {
	s := newTestStore(t)
	repo := totppkg.NewRepositoryWithStore(NewTOTPStore(s), testMasterKey)
	ctx := context.Background()

	if enabled, _ := repo.IsEnabled(ctx); enabled {
		t.Fatal("should not be enabled before enrollment")
	}

	_, secret, err := repo.Enroll(ctx)
	if err != nil {
		t.Fatalf("Enroll: %v", err)
	}

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("GenerateCode: %v", err)
	}
	ok, err := repo.Verify(ctx, code)
	if err != nil || !ok {
		t.Fatalf("Verify: ok=%v err=%v", ok, err)
	}
	// Single-use: the same code/step must not be accepted twice.
	if ok, _ := repo.Verify(ctx, code); ok {
		t.Fatal("replayed code must be rejected (single-use)")
	}

	if err := repo.Enable(ctx); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if enabled, _ := repo.IsEnabled(ctx); !enabled {
		t.Fatal("should be enabled after Enable")
	}
	if _, ok, _ := repo.EnabledAt(ctx); !ok {
		t.Fatal("EnabledAt should report ok after enable")
	}

	codes, err := repo.GenerateRecoveryCodes(ctx)
	if err != nil || len(codes) != 10 {
		t.Fatalf("GenerateRecoveryCodes: len=%d err=%v", len(codes), err)
	}
	info, _ := repo.Info(ctx)
	if info.RecoveryRemaining != 10 || info.RecoveryTotal != 10 {
		t.Errorf("recovery counts: %+v", info)
	}
	// Consume one recovery code; double-use must fail.
	if ok, _ := repo.ConsumeRecoveryCode(ctx, codes[0]); !ok {
		t.Fatal("first recovery-code use should succeed")
	}
	if ok, _ := repo.ConsumeRecoveryCode(ctx, codes[0]); ok {
		t.Fatal("recovery code must be single-use")
	}
	info, _ = repo.Info(ctx)
	if info.RecoveryRemaining != 9 {
		t.Errorf("recovery remaining = %d, want 9", info.RecoveryRemaining)
	}

	// Disable with a fresh recovery code, then confirm TOTP is gone.
	if ok, err := repo.DisableWithCode(ctx, codes[1]); err != nil || !ok {
		t.Fatalf("DisableWithCode: ok=%v err=%v", ok, err)
	}
	if enabled, _ := repo.IsEnabled(ctx); enabled {
		t.Fatal("should be disabled after DisableWithCode")
	}
}

// Front Desk shares the session store, so signing other sessions out has to
// work here too. The SQLite side gets its own coverage because its predicate
// (IS NOT) is a different construct from the Postgres one (IS DISTINCT FROM),
// and a wrong one silently keeps the very sessions the operator asked to end.
func TestDeleteOtherSessionsForUser(t *testing.T) {
	s := newTestStore(t)
	store := NewWebAuthnStore(s)
	ctx := context.Background()

	mineHash := "hash-mine"
	otherHash := "hash-other"
	strangerHash := "hash-stranger"
	nullHashSession := &webauthn.SessionRecord{
		ID: uuid.New(), Challenge: "c", SessionData: []byte("{}"), Type: "auth_token",
		UserID: []byte("admin"), ExpiresAt: time.Now().Add(time.Hour),
	}
	ceremony := &webauthn.SessionRecord{
		ID: uuid.New(), Challenge: "c", SessionData: []byte("{}"), Type: "login",
		UserID: []byte("admin"), ExpiresAt: time.Now().Add(time.Hour),
	}
	mine := &webauthn.SessionRecord{
		ID: uuid.New(), Challenge: "c", SessionData: []byte("{}"), Type: "auth_token",
		UserID: []byte("admin"), TokenHash: &mineHash, ExpiresAt: time.Now().Add(time.Hour),
	}
	other := &webauthn.SessionRecord{
		ID: uuid.New(), Challenge: "c", SessionData: []byte("{}"), Type: "auth_token",
		UserID: []byte("admin"), TokenHash: &otherHash, ExpiresAt: time.Now().Add(time.Hour),
	}
	stranger := &webauthn.SessionRecord{
		ID: uuid.New(), Challenge: "c", SessionData: []byte("{}"), Type: "auth_token",
		UserID: []byte("someone-else"), TokenHash: &strangerHash, ExpiresAt: time.Now().Add(time.Hour),
	}
	for _, rec := range []*webauthn.SessionRecord{nullHashSession, ceremony, mine, other, stranger} {
		if err := store.CreateSession(ctx, rec); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
	}

	n, err := store.DeleteOtherSessionsForUser(ctx, []byte("admin"), mineHash)
	if err != nil {
		t.Fatalf("DeleteOtherSessionsForUser: %v", err)
	}
	// The other auth_token session and the one with no token hash: a NULL hash
	// is not the caller's, so it must not be mistaken for a session to keep.
	if n != 2 {
		t.Errorf("revoked %d, want 2", n)
	}
	if _, err := store.GetSession(ctx, mine.ID); err != nil {
		t.Errorf("the caller's own session was revoked: %v", err)
	}
	if _, err := store.GetSession(ctx, ceremony.ID); err != nil {
		t.Errorf("an in-flight login ceremony was torn down: %v", err)
	}
	if _, err := store.GetSession(ctx, stranger.ID); err != nil {
		t.Errorf("another identity's session was revoked: %v", err)
	}
	if _, err := store.GetSession(ctx, other.ID); !errors.Is(err, webauthn.ErrNotFound) {
		t.Errorf("another session of the same identity survived, got %v", err)
	}
	if _, err := store.GetSession(ctx, nullHashSession.ID); !errors.Is(err, webauthn.ErrNotFound) {
		t.Errorf("a session with no token hash survived, got %v", err)
	}
}

// An empty keepTokenHash keeps nothing, which is how a caller holding the raw
// admin token (and therefore no session of its own) ends all of them.
func TestDeleteOtherSessionsForUser_EmptyKeepHashRevokesAll(t *testing.T) {
	s := newTestStore(t)
	store := NewWebAuthnStore(s)
	ctx := context.Background()

	hash := "hash-a"
	rec := &webauthn.SessionRecord{
		ID: uuid.New(), Challenge: "c", SessionData: []byte("{}"), Type: "auth_token",
		UserID: []byte("admin"), TokenHash: &hash, ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := store.CreateSession(ctx, rec); err != nil {
		t.Fatal(err)
	}

	n, err := store.DeleteOtherSessionsForUser(ctx, []byte("admin"), "")
	if err != nil {
		t.Fatalf("DeleteOtherSessionsForUser: %v", err)
	}
	if n != 1 {
		t.Errorf("revoked %d, want 1", n)
	}
}

// Device metadata must survive the SQLite round-trip, or Front Desk's
// active-sessions list would show every login as an unknown device.
func TestSQLiteSessionMetaRoundTrip(t *testing.T) {
	s := newTestStore(t)
	store := NewWebAuthnStore(s)
	ctx := context.Background()

	hash := "hash-meta"
	rec := &webauthn.SessionRecord{
		ID: uuid.New(), Challenge: "c", SessionData: []byte("{}"), Type: "auth_token",
		UserID: []byte("admin"), TokenHash: &hash, ExpiresAt: time.Now().Add(time.Hour),
		UserAgent: "Mozilla/5.0 Firefox/141.0", IP: "203.0.113.7",
	}
	if err := store.CreateSession(ctx, rec); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	got, err := store.GetSession(ctx, rec.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.UserAgent != "Mozilla/5.0 Firefox/141.0" || got.IP != "203.0.113.7" {
		t.Errorf("meta round-trip lost: ua=%q ip=%q", got.UserAgent, got.IP)
	}
	if got.LastSeenAt != nil {
		t.Errorf("LastSeenAt = %v, want nil before first touch", got.LastSeenAt)
	}
}

// The SQLite side of the sessions list: only this identity's live auth tokens,
// newest first, ceremonies and expired rows excluded.
func TestSQLiteListAuthSessionsForUser(t *testing.T) {
	s := newTestStore(t)
	store := NewWebAuthnStore(s)
	ctx := context.Background()

	hashOld, hashNew, hashExp, hashStranger := "h-old", "h-new", "h-exp", "h-stranger"
	older := &webauthn.SessionRecord{
		ID: uuid.New(), Challenge: "c", SessionData: []byte("{}"), Type: "auth_token",
		UserID: []byte("admin"), TokenHash: &hashOld, ExpiresAt: time.Now().Add(time.Hour),
		UserAgent: "older",
	}
	ceremony := &webauthn.SessionRecord{
		ID: uuid.New(), Challenge: "c", SessionData: []byte("{}"), Type: "login",
		UserID: []byte("admin"), ExpiresAt: time.Now().Add(time.Hour),
	}
	expired := &webauthn.SessionRecord{
		ID: uuid.New(), Challenge: "c", SessionData: []byte("{}"), Type: "auth_token",
		UserID: []byte("admin"), TokenHash: &hashExp, ExpiresAt: time.Now().Add(-time.Hour),
	}
	stranger := &webauthn.SessionRecord{
		ID: uuid.New(), Challenge: "c", SessionData: []byte("{}"), Type: "auth_token",
		UserID: []byte("someone-else"), TokenHash: &hashStranger, ExpiresAt: time.Now().Add(time.Hour),
	}
	for _, rec := range []*webauthn.SessionRecord{older, ceremony, expired, stranger} {
		if err := store.CreateSession(ctx, rec); err != nil {
			t.Fatalf("CreateSession: %v", err)
		}
	}
	// A strictly later created_at for the newer row, so the ordering assertion
	// cannot pass by insertion accident.
	time.Sleep(5 * time.Millisecond)
	newer := &webauthn.SessionRecord{
		ID: uuid.New(), Challenge: "c", SessionData: []byte("{}"), Type: "auth_token",
		UserID: []byte("admin"), TokenHash: &hashNew, ExpiresAt: time.Now().Add(time.Hour),
		UserAgent: "newer",
	}
	if err := store.CreateSession(ctx, newer); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	sessions, err := store.ListAuthSessionsForUser(ctx, []byte("admin"))
	if err != nil {
		t.Fatalf("ListAuthSessionsForUser: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("len = %d, want 2 (ceremony, expired, and foreign rows excluded)", len(sessions))
	}
	if sessions[0].UserAgent != "newer" || sessions[1].UserAgent != "older" {
		t.Errorf("order = [%q, %q], want newest first", sessions[0].UserAgent, sessions[1].UserAgent)
	}
}

// A row the list cannot scan (an id that is not a UUID reaches uuid.Parse)
// surfaces an error rather than a silently truncated list.
func TestSQLiteListAuthSessions_ScanErrorSurfaces(t *testing.T) {
	s := newTestStore(t)
	store := NewWebAuthnStore(s)
	ctx := context.Background()

	// user_id as a blob parameter, matching how CreateSession binds it: SQLite
	// compares TEXT and BLOB as unequal, so a TEXT literal would just be
	// filtered out instead of exercising the scan path.
	if _, err := s.db.ExecContext(ctx,
		`INSERT INTO webauthn_sessions (id, challenge, session_data, type, user_id, token_hash, expires_at, created_at)
		 VALUES ('not-a-uuid', 'c', X'7B7D', 'auth_token', ?, 'h-bad', ?, ?)`,
		[]byte("admin"), time.Now().Add(time.Hour).UTC().UnixNano(), time.Now().UTC().UnixNano(),
	); err != nil {
		t.Fatalf("seed malformed row: %v", err)
	}

	if _, err := store.ListAuthSessionsForUser(ctx, []byte("admin")); err == nil {
		t.Error("a malformed row should surface an error")
	}
}

// A last-seen stamp the database refuses surfaces an error to the manager
// (which logs and moves on) — and, one level up, must not fail validation.
// The trigger stands in for any UPDATE-time failure without closing the DB.
func TestSQLiteTouchSessionLastSeen_UpdateFailureSurfaces(t *testing.T) {
	s := newTestStore(t)
	store := NewWebAuthnStore(s)
	mgr := webauthn.NewSessionManager(store)
	ctx := context.Background()

	token, err := mgr.CreateAuthToken(ctx, []byte("admin"), nil, webauthn.SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.ExecContext(ctx,
		`CREATE TRIGGER refuse_touch BEFORE UPDATE OF last_seen_at ON webauthn_sessions
		 BEGIN SELECT RAISE(ABORT, 'simulated update failure'); END`,
	); err != nil {
		t.Fatalf("create trigger: %v", err)
	}

	if err := store.TouchSessionLastSeen(ctx, uuid.New(), time.Now()); err != nil {
		t.Errorf("touching a missing row does not UPDATE anything and must stay quiet, got %v", err)
	}

	// The stamp fails, validation must not: TokenUser logs and moves on.
	if _, ok := mgr.TokenUser(ctx, token); !ok {
		t.Error("validation failed because the last-seen stamp failed")
	}
	got, err := store.GetSession(ctx, sessionIDByHash(t, s, token))
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.LastSeenAt != nil {
		t.Error("the refused stamp landed anyway")
	}
}

// sessionIDByHash resolves a raw token to its session row id.
func sessionIDByHash(t *testing.T, s *Store, token string) uuid.UUID {
	t.Helper()
	sum := sha256.Sum256([]byte(token))
	var idStr string
	if err := s.db.QueryRow(
		`SELECT id FROM webauthn_sessions WHERE token_hash = ?`, hex.EncodeToString(sum[:]),
	).Scan(&idStr); err != nil {
		t.Fatalf("resolve session id: %v", err)
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		t.Fatalf("parse id: %v", err)
	}
	return id
}

// Touch stamps last_seen_at; a row revoked in between is not an error.
func TestSQLiteTouchSessionLastSeen(t *testing.T) {
	s := newTestStore(t)
	store := NewWebAuthnStore(s)
	ctx := context.Background()

	hash := "hash-touch"
	rec := &webauthn.SessionRecord{
		ID: uuid.New(), Challenge: "c", SessionData: []byte("{}"), Type: "auth_token",
		UserID: []byte("admin"), TokenHash: &hash, ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := store.CreateSession(ctx, rec); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	at := time.Now().Truncate(time.Millisecond)
	if err := store.TouchSessionLastSeen(ctx, rec.ID, at); err != nil {
		t.Fatalf("TouchSessionLastSeen: %v", err)
	}
	got, err := store.GetSession(ctx, rec.ID)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.LastSeenAt == nil || !got.LastSeenAt.Equal(at) {
		t.Errorf("LastSeenAt = %v, want %v", got.LastSeenAt, at)
	}

	if err := store.TouchSessionLastSeen(ctx, uuid.New(), at); err != nil {
		t.Errorf("touching a missing session should not error, got %v", err)
	}
}

func TestDeleteOtherSessionsForUser_ClosedDBIsAnError(t *testing.T) {
	s := newTestStore(t)
	store := NewWebAuthnStore(s)
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if _, err := store.DeleteOtherSessionsForUser(context.Background(), []byte("admin"), ""); err == nil {
		t.Error("a failed revoke must report an error rather than claim success")
	}
}

// TestSQLiteExtendSession: the sliding-expiry write moves expires_at and
// nothing else, a missing row is not an error, and a session slid this way
// still authenticates through the shared manager (the SQLite store honours the
// same contract as Postgres, so Front Desk sessions slide too).
func TestSQLiteExtendSession(t *testing.T) {
	s := newTestStore(t)
	store := NewWebAuthnStore(s)
	mgr := webauthn.NewSessionManager(store)
	ctx := context.Background()

	token, err := mgr.CreateAuthToken(ctx, []byte("admin"), nil, webauthn.SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	id := sessionIDByHash(t, s, token)
	before, err := store.GetSession(ctx, id)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}

	later := before.ExpiresAt.Add(36 * time.Hour).Truncate(time.Microsecond)
	if err := store.ExtendSession(ctx, id, later); err != nil {
		t.Fatalf("ExtendSession: %v", err)
	}
	after, err := store.GetSession(ctx, id)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if !after.ExpiresAt.Equal(later) {
		t.Errorf("expires_at = %v, want %v", after.ExpiresAt, later)
	}
	if !after.CreatedAt.Equal(before.CreatedAt) || after.LastSeenAt != nil && before.LastSeenAt == nil {
		t.Error("ExtendSession touched columns other than expires_at")
	}
	if _, ok := mgr.TokenUser(ctx, token); !ok {
		t.Error("an extended session no longer validates")
	}

	if err := store.ExtendSession(ctx, uuid.New(), later); err != nil {
		t.Errorf("extending a missing row must stay quiet, got %v", err)
	}
}

// TestSQLiteSlidingExpiryEndToEnd drives the manager over the SQLite store:
// an aged Front Desk session slides on use, exactly as the Postgres-backed
// dashboard session does.
func TestSQLiteSlidingExpiryEndToEnd(t *testing.T) {
	s := newTestStore(t)
	store := NewWebAuthnStore(s)
	mgr := webauthn.NewSessionManager(store)
	ctx := context.Background()

	token, err := mgr.CreateAuthToken(ctx, []byte("admin"), nil, webauthn.SessionMeta{})
	if err != nil {
		t.Fatal(err)
	}
	id := sessionIDByHash(t, s, token)
	// Two days in, last used an hour ago, expiry as that use left it.
	seen := time.Now().Add(-time.Hour)
	if _, err := s.db.ExecContext(ctx,
		`UPDATE webauthn_sessions SET created_at = ?, last_seen_at = ?, expires_at = ? WHERE id = ?`,
		time.Now().Add(-48*time.Hour).UTC().UnixNano(), seen.UTC().UnixNano(),
		seen.Add(webauthn.AuthTokenTTL).UTC().UnixNano(), id.String()); err != nil {
		t.Fatalf("age session: %v", err)
	}

	res, ok := mgr.Authenticate(ctx, token)
	if !ok || !res.Extended {
		t.Fatalf("Authenticate = (%+v, %v), want a slid session", res, ok)
	}
	got, err := store.GetSession(ctx, id)
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	want := time.Now().Add(webauthn.AuthTokenTTL)
	if d := got.ExpiresAt.Sub(want); d < -time.Minute || d > time.Minute {
		t.Errorf("expires_at = %v, want about now+TTL (%v)", got.ExpiresAt, want)
	}
}
