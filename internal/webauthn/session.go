package webauthn

import (
	"bytes"
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

// AuthTokenTTL is the lifetime of a minted auth-token session, shared by the
// server-side session expiry and the session cookie MaxAge so the two cannot
// drift apart. All login front-ends that mint sessions use this single value.
//
// Nothing revokes a session when its owner logs in again, so this TTL is the
// only bound on how long a stolen token stays usable. Keep it short enough that
// the exposure window is survivable; session_ttl_test.go enforces the ceiling.
//
// User-facing copy quotes this value as "1 day": settings.sessionTimeout.hint
// in web/src/i18n/locales/ (29 locales) and settings.sessionTimeoutHint in
// frontdesk/web/src/i18n/locales/ (11 locales), plus README.md and
// wiki/Security.md and wiki/Configuration.md. Changing the constant without
// sweeping those reintroduces the label-lies-to-the-operator bug the copy was
// written to fix.
const AuthTokenTTL = 24 * time.Hour

// lastSeenThrottle bounds how often a session's last_seen_at is rewritten.
// Validation runs on every admin request; without the throttle each one would
// carry a DB write. "Active within the last few minutes" is all the
// active-sessions list needs.
const lastSeenThrottle = 5 * time.Minute

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
	_, ok := m.TokenUser(ctx, token)
	return ok
}

// TokenUser validates the token exactly like Validate and additionally returns
// the UserID the session was minted with: []byte("admin") for legacy admin
// logins, a user UUID string for multi-user password logins. ok is false when
// the token is invalid or expired.
func (m *SessionManager) TokenUser(ctx context.Context, token string) ([]byte, bool) {
	if token == "" {
		return nil, false
	}

	// Hash the token first — eliminates the timing oracle between UUID-parse
	// failures and DB lookup, and matches the project's hash-before-store
	// security model (admin token, virtual keys).
	hash := sha256.Sum256([]byte(token))
	tokenHash := hex.EncodeToString(hash[:])

	session, err := m.store.GetSessionByTokenHash(ctx, tokenHash)
	if err != nil {
		return nil, false
	}

	if session.ExpiresAt.Before(time.Now()) {
		return nil, false
	}

	// Constant-time compare as defense in depth (the DB lookup is already by
	// hash, but this prevents any theoretical timing leak from the comparison
	// itself if the DB ever returns multiple rows).
	if session.TokenHash == nil {
		return nil, false
	}

	if subtle.ConstantTimeCompare([]byte(tokenHash), []byte(*session.TokenHash)) != 1 {
		return nil, false
	}

	// Stamp last-seen so the active-sessions list can show which devices are
	// still in use, throttled so the hot path does not gain a write per
	// request. Best-effort: a failed stamp must not fail an otherwise valid
	// authentication.
	if session.LastSeenAt == nil || time.Since(*session.LastSeenAt) >= lastSeenThrottle {
		if err := m.store.TouchSessionLastSeen(ctx, session.ID, time.Now()); err != nil {
			debuglog.Error("webauthn: failed to stamp session last-seen", "error", err)
		}
	}

	return session.UserID, true
}

// SessionMeta is the device metadata captured when a session is minted: the
// login request's User-Agent and client IP. It is display metadata for the
// operator's active-sessions list, never an authorization input, so a spoofed
// header only mislabels the attacker's own row.
type SessionMeta struct {
	UserAgent string
	IP        string
}

// AuthSessionInfo is one row of the operator-facing active-sessions list:
// everything the dashboard shows about a live session, and nothing it must not
// see (no token, no token hash).
type AuthSessionInfo struct {
	ID         uuid.UUID  `json:"id"`
	UserAgent  string     `json:"user_agent"`
	IP         string     `json:"ip"`
	CreatedAt  time.Time  `json:"created_at"`
	LastSeenAt *time.Time `json:"last_seen_at,omitempty"`
	Current    bool       `json:"current"`
}

// ErrCurrentSession is returned by RevokeSessionByID when the target is the
// session the request itself rides on. Distinct from ErrNotFound so the API
// can tell the operator "use logout for that" instead of a puzzling 404.
var ErrCurrentSession = errors.New("cannot revoke the current session")

// CreateAuthToken creates a new admin authentication session lasting AuthTokenTTL.
// It generates a cryptographically random token, stores only its SHA-256 hash
// in the database, and returns the raw token to the caller.
// The ctx parameter propagates request deadlines and tracing.
// credentialID links the auth token to the passkey used for login, so that
// deleting the passkey can cascade-revoke its derived sessions.
// meta carries the login request's device metadata into the stored session.
func (m *SessionManager) CreateAuthToken(ctx context.Context, userID, credentialID []byte, meta SessionMeta) (string, error) {
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
		ExpiresAt:    time.Now().Add(AuthTokenTTL),
		UserAgent:    meta.UserAgent,
		IP:           meta.IP,
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

// RevokeOtherSessions signs out every session belonging to identity except the
// one the call was made from, so the operator is not logged out by their own
// click.
//
// identity is the caller's session handle as the authentication layer resolved
// it, never something derived from the request here. Deriving it from a token
// this function picked itself would let a caller authenticated by one
// credential aim the revoke at an identity carried by another: present a valid
// bearer alongside a junk cookie and the junk cookie decides whose sessions
// die.
//
// candidateTokens are the credentials the request actually carried, in no
// particular order. A session is kept only when it hashes to one of them AND
// belongs to identity, so an unrecognized or foreign token can never widen the
// blast radius; it only means nothing is kept. That is the correct outcome for
// an operator on the raw admin token, which mints no session: they hold the
// credential but no browser session, and every session for their identity goes.
//
// A store failure while looking a candidate up is returned rather than swallowed:
// treating "could not check" as "not the caller's session" would silently turn a
// timeout into signing the caller out of the page they clicked from.
func (m *SessionManager) RevokeOtherSessions(ctx context.Context, identity []byte, candidateTokens ...string) (int64, error) {
	if len(identity) == 0 {
		return 0, nil
	}

	keepHash := ""
	for _, token := range candidateTokens {
		if token == "" {
			continue
		}
		sum := sha256.Sum256([]byte(token))
		hash := hex.EncodeToString(sum[:])
		session, err := m.store.GetSessionByTokenHash(ctx, hash)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return 0, err
		}
		// An expired row is not a session the caller can still be using: the
		// middleware rejects it. Sparing it would keep a dead row while
		// revoking the live session the request actually authenticated with,
		// logging the caller out of the page they clicked from.
		if session.ExpiresAt.Before(time.Now()) {
			continue
		}
		if bytes.Equal(session.UserID, identity) {
			keepHash = hash
			break
		}
	}
	return m.store.DeleteOtherSessionsForUser(ctx, identity, keepHash)
}

// ListAuthSessions returns the live auth-token sessions belonging to identity,
// marking as current the one whose token the request itself carried. The
// current session leads the list and the rest follow newest first, so the
// operator's anchor row ("this is me") never sits pages deep in a long list.
//
// candidateTokens follow the same contract as RevokeOtherSessions: they are
// whatever credentials the request carried, and a foreign or junk token simply
// matches no row of this identity, so nothing is marked current. Rows are
// already scoped to identity before matching, which is what makes the hash
// comparison safe as a "current" test: a stolen token from another identity
// cannot label a row here. Token hashes never leave this function; the caller
// sees only the boolean.
func (m *SessionManager) ListAuthSessions(ctx context.Context, identity []byte, candidateTokens ...string) ([]AuthSessionInfo, error) {
	if len(identity) == 0 {
		return nil, nil
	}
	records, err := m.store.ListAuthSessionsForUser(ctx, identity)
	if err != nil {
		return nil, err
	}

	currentHash := ""
	for _, token := range candidateTokens {
		if token == "" {
			continue
		}
		sum := sha256.Sum256([]byte(token))
		hash := hex.EncodeToString(sum[:])
		for _, rec := range records {
			if rec.TokenHash != nil && *rec.TokenHash == hash {
				currentHash = hash
				break
			}
		}
		if currentHash != "" {
			break
		}
	}

	sessions := make([]AuthSessionInfo, 0, len(records))
	for _, rec := range records {
		info := AuthSessionInfo{
			ID:         rec.ID,
			UserAgent:  rec.UserAgent,
			IP:         rec.IP,
			CreatedAt:  rec.CreatedAt,
			LastSeenAt: rec.LastSeenAt,
			Current:    currentHash != "" && rec.TokenHash != nil && *rec.TokenHash == currentHash,
		}
		if info.Current {
			// Lead with the calling session; the store already ordered the rest
			// newest first, and prepending one element preserves that.
			sessions = append([]AuthSessionInfo{info}, sessions...)
			continue
		}
		sessions = append(sessions, info)
	}
	return sessions, nil
}

// RevokeSessionByID deletes one of identity's auth-token sessions by id.
//
// A session that does not exist, is not an auth token, or belongs to a
// different identity all read as ErrNotFound: a distinct answer for "exists
// but is not yours" would confirm the id to a probing caller. The session the
// request itself rides on (matched against candidateTokens the same way
// RevokeOtherSessions spares it) is refused with ErrCurrentSession instead of
// silently signing the caller out of the page they clicked from.
func (m *SessionManager) RevokeSessionByID(ctx context.Context, identity []byte, id uuid.UUID, candidateTokens ...string) error {
	if len(identity) == 0 {
		return ErrNotFound
	}
	session, err := m.store.GetSession(ctx, id)
	if err != nil {
		return err
	}
	if session.Type != "auth_token" || !bytes.Equal(session.UserID, identity) {
		return ErrNotFound
	}
	if session.TokenHash != nil {
		for _, token := range candidateTokens {
			if token == "" {
				continue
			}
			sum := sha256.Sum256([]byte(token))
			if hex.EncodeToString(sum[:]) == *session.TokenHash {
				return ErrCurrentSession
			}
		}
	}
	return m.store.DeleteSession(ctx, id)
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
