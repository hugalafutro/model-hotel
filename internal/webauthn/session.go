package webauthn

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"io"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// errInvalidLoginState is returned by ConsumeLoginState when the record is
// missing, of the wrong type, or expired. Kept unexported and opaque so callers
// can't distinguish the cases (no oracle for a probing attacker).
var errInvalidLoginState = errors.New("invalid or expired login state")

// SessionManager handles WebAuthn-based admin session authentication.
// It validates bearer tokens stored as WebAuthn session records of type "auth_token",
// following the same hash-then-lookup pattern as admin.Manager (SHA-256 + constant-time compare).
//
// It depends on the SessionStore interface (not the concrete *Repository) so the
// same login logic can run over Postgres in the main server or SQLite in the HA
// Front Desk control plane.
type SessionManager struct {
	store SessionStore
}

// NewSessionManager creates a new SessionManager backed by the given session
// store. The main server passes *Repository (Postgres); Front Desk passes its
// SQLite store. Both satisfy SessionStore.
func NewSessionManager(store SessionStore) *SessionManager {
	return &SessionManager{store: store}
}

// Validate checks whether the given token is a valid, non-expired auth token
// session. It hashes the token with SHA-256 before DB lookup (no plaintext
// tokens stored) and uses constant-time comparison for the hash match.
// The ctx parameter propagates request deadlines and tracing.
func (m *SessionManager) Validate(ctx context.Context, token string) bool {
	if token == "" {
		return false
	}

	// Hash the token first — eliminates the timing oracle between UUID-parse
	// failures and DB lookup, and matches the project's hash-before-store
	// security model (admin token, virtual keys).
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	session, err := m.store.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		return false
	}

	if session.ExpiresAt.Before(time.Now()) {
		return false
	}

	// Constant-time compare as defense in depth (the DB lookup is already by
	// hash, but this prevents any theoretical timing leak from the comparison
	// itself if the DB ever returns multiple rows).
	if session.TokenHash == nil {
		return false
	}

	if subtle.ConstantTimeCompare([]byte(tokenHash), []byte(*session.TokenHash)) != 1 {
		return false
	}

	return true
}

// CreateAuthToken creates a new 30-day admin authentication session.
// It generates a cryptographically random token, stores only its SHA-256 hash
// in the database, and returns the raw token to the caller.
// The ctx parameter propagates request deadlines and tracing.
// credentialID links the auth token to the passkey used for login, so that
// deleting the passkey can cascade-revoke its derived sessions.
func (m *SessionManager) CreateAuthToken(ctx context.Context, userID, credentialID []byte) (string, error) {
	// Generate a high-entropy random token (32 bytes = 256 bits).
	tokenBytes := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, tokenBytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(tokenBytes)

	// Hash the token for storage — the raw token is never persisted.
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	id, err := uuid.NewRandom()
	if err != nil {
		return "", err
	}

	challenge, err := generateChallenge(32)
	if err != nil {
		return "", err
	}

	// Auth tokens don't need meaningful WebAuthn session data, but the column
	// is NOT NULL, so store a minimal JSON object.
	sessionData := []byte(`{"type":"auth_token"}`)

	session := &SessionRecord{
		ID:           id,
		Challenge:    challenge,
		SessionData:  sessionData,
		Type:         "auth_token",
		UserID:       userID,
		TokenHash:    &tokenHash,
		CredentialID: credentialID,
		ExpiresAt:    time.Now().Add(30 * 24 * time.Hour),
	}

	if err := m.store.CreateSession(ctx, session); err != nil {
		return "", err
	}

	return token, nil
}

// CreateLoginState stores a short-lived OIDC login-state record holding the
// per-login state/nonce/PKCE-verifier blob, keyed by a fresh random id. It
// returns that id, which the caller sets in a cookie so the callback can find
// the record. The record carries Type "oidc_login" and a short ExpiresAt; it is
// never an auth token (TokenHash stays nil) so Validate can never accept it.
// Reuses the same SessionStore as auth tokens, so it ports to Front Desk's
// SQLite store unchanged.
func (m *SessionManager) CreateLoginState(ctx context.Context, data []byte, ttl time.Duration) (uuid.UUID, error) {
	id, err := uuid.NewRandom()
	if err != nil {
		return uuid.Nil, err
	}
	challenge, err := generateChallenge(32)
	if err != nil {
		return uuid.Nil, err
	}
	session := &SessionRecord{
		ID:          id,
		Challenge:   challenge,
		SessionData: data,
		Type:        "oidc_login",
		UserID:      []byte("admin"),
		ExpiresAt:   time.Now().Add(ttl),
	}
	if err := m.store.CreateSession(ctx, session); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}

// ConsumeLoginState fetches the OIDC login-state record by id and deletes it,
// enforcing single use: a replayed callback finds nothing the second time. It
// returns the stored blob only when the record exists, is of type "oidc_login",
// and has not expired. The delete runs regardless of expiry so stale records
// don't linger until the hourly cleanup.
func (m *SessionManager) ConsumeLoginState(ctx context.Context, id uuid.UUID) ([]byte, error) {
	session, err := m.store.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	// The delete is the atomic single-use claim: DeleteSession reports an error
	// (ErrNotFound / 0 rows affected) when no row was removed, so under a
	// concurrent replay only the goroutine whose DELETE actually removed the row
	// proceeds; any other reader that saw the same row before the delete is
	// rejected here. This closes the read-then-delete TOCTOU on the guard.
	if delErr := m.store.DeleteSession(ctx, id); delErr != nil {
		return nil, errInvalidLoginState
	}
	if session.Type != "oidc_login" {
		return nil, errInvalidLoginState
	}
	if session.ExpiresAt.Before(time.Now()) {
		return nil, errInvalidLoginState
	}
	return session.SessionData, nil
}

// RevokeAuthToken deletes an auth token session by hashing the token and
// looking up the session by its token_hash. Returns true if a session was
// found and deleted.
func (m *SessionManager) RevokeAuthToken(ctx context.Context, token string) bool {
	if token == "" {
		return false
	}

	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	session, err := m.store.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		return false
	}

	if err := m.store.DeleteSession(ctx, session.ID); err != nil {
		debuglog.Error("webauthn: failed to revoke auth token", "error", err)
		return false
	}

	return true
}

// generateChallenge returns a hex-encoded random challenge of the given byte length.
func generateChallenge(length int) (string, error) {
	buf := make([]byte, length)
	if _, err := io.ReadFull(rand.Reader, buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
