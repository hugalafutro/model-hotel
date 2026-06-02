// Package webauthn provides WebAuthn/FIDO2 credential storage and authentication for Model Hotel.
package webauthn

import (
	"context"
	"errors"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	gowa "github.com/go-webauthn/webauthn/webauthn"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// ---------------------------------------------------------------------------
// Column lists for query consistency
// ---------------------------------------------------------------------------

const credentialColumns = `id, public_key, attestation_type, attestation_format, transport, flags_byte, sign_count, aaguid, attestation_object, attestation_client_data, attestation_client_data_hash, attestation_public_key_algo, authenticator_data, created_at, updated_at`

const sessionColumns = `id, challenge, session_data, type, user_id, token_hash, expires_at, created_at`

// ---------------------------------------------------------------------------
// Domain types
// ---------------------------------------------------------------------------

// CredentialRecord is the database representation of a WebAuthn credential,
// mapping to the webauthn_credentials table (migration 039).
type CredentialRecord struct {
	ID                        []byte    `json:"id"`
	PublicKey                 []byte    `json:"public_key"`
	AttestationType           string    `json:"attestation_type"`
	AttestationFormat         string    `json:"attestation_format"`
	Transport                 []string  `json:"transport"`
	FlagsByte                 byte      `json:"flags_byte"`
	SignCount                 uint32    `json:"sign_count"`
	AAGUID                    uuid.UUID `json:"aaguid"`
	AttestationObject         []byte    `json:"attestation_object"`
	AttestationClientData     []byte    `json:"attestation_client_data"`
	AttestationClientDataHash []byte    `json:"attestation_client_data_hash"`
	AttestationPublicKeyAlgo  int64     `json:"attestation_public_key_algo"`
	AuthenticatorData         []byte    `json:"authenticator_data"`
	CreatedAt                 time.Time `json:"created_at"`
	UpdatedAt                 time.Time `json:"updated_at"`
}

// ToWebAuthnCredential converts a CredentialRecord into a webauthn.Credential
// suitable for use with the go-webauthn library.
func (r *CredentialRecord) ToWebAuthnCredential() gowa.Credential {
	var transports []protocol.AuthenticatorTransport
	for _, t := range r.Transport {
		transports = append(transports, protocol.AuthenticatorTransport(t))
	}

	aaguidBytes, err := r.AAGUID.MarshalBinary()
	if err != nil {
		debuglog.Error("webauthn: failed to marshal AAGUID", "aaguid", r.AAGUID, "error", err)
		aaguidBytes = make([]byte, 16)
	}

	return gowa.Credential{
		ID:                r.ID,
		PublicKey:         r.PublicKey,
		AttestationType:   r.AttestationType,
		AttestationFormat: r.AttestationFormat,
		Transport:         transports,
		Flags:             gowa.NewCredentialFlags(protocol.AuthenticatorFlags(r.FlagsByte)),
		Authenticator: gowa.Authenticator{
			AAGUID:    aaguidBytes,
			SignCount: r.SignCount,
		},
		Attestation: gowa.CredentialAttestation{
			ClientDataJSON:     r.AttestationClientData,
			ClientDataHash:     r.AttestationClientDataHash,
			AuthenticatorData:  r.AuthenticatorData,
			PublicKeyAlgorithm: r.AttestationPublicKeyAlgo,
			Object:             r.AttestationObject,
		},
	}
}

// FromWebAuthnCredential converts a webauthn.Credential into a CredentialRecord
// for database storage.
func FromWebAuthnCredential(c *gowa.Credential) *CredentialRecord {
	aaguid, err := uuid.FromBytes(c.Authenticator.AAGUID)
	if err != nil {
		debuglog.Error("webauthn: failed to parse AAGUID from credential", "error", err)
		aaguid = uuid.Nil
	}

	var transports []string
	for _, t := range c.Transport {
		transports = append(transports, string(t))
	}

	return &CredentialRecord{
		ID:                        c.ID,
		PublicKey:                 c.PublicKey,
		AttestationType:           c.AttestationType,
		AttestationFormat:         c.AttestationFormat,
		Transport:                 transports,
		FlagsByte:                 byte(c.Flags.ProtocolValue()),
		SignCount:                 c.Authenticator.SignCount,
		AAGUID:                    aaguid,
		AttestationObject:         c.Attestation.Object,
		AttestationClientData:     c.Attestation.ClientDataJSON,
		AttestationClientDataHash: c.Attestation.ClientDataHash,
		AttestationPublicKeyAlgo:  c.Attestation.PublicKeyAlgorithm,
		AuthenticatorData:         c.Attestation.AuthenticatorData,
	}
}

// SessionRecord is the database representation of a WebAuthn session,
// mapping to the webauthn_sessions table (migration 039).
// TokenHash stores the SHA-256 hash of auth token sessions (type "auth_token"),
// consistent with the project's hash-before-store security model.
// Nil for registration/login sessions (no auth token).
type SessionRecord struct {
	ID          uuid.UUID `json:"id"`
	Challenge   string    `json:"challenge"`
	SessionData []byte    `json:"session_data"`
	Type        string    `json:"type"`
	UserID      []byte    `json:"user_id"`
	TokenHash   *string   `json:"token_hash,omitempty"`
	ExpiresAt   time.Time `json:"expires_at"`
	CreatedAt   time.Time `json:"created_at"`
}

// AdminUser implements the webauthn.User interface for Model Hotel's single
// administrator account. It uses a fixed "admin" identity.
type AdminUser struct {
	id          []byte
	name        string
	displayName string
	credentials []gowa.Credential
}

// NewAdminUser creates a new AdminUser with the fixed admin identity.
func NewAdminUser() *AdminUser {
	return &AdminUser{
		id:          []byte("admin"),
		name:        "admin",
		displayName: "Administrator",
	}
}

// SetCredentials sets the credentials for this user. Call before passing to
// BeginRegistration/BeginLogin.
func (u *AdminUser) SetCredentials(creds []gowa.Credential) {
	u.credentials = creds
}

// WebAuthnID returns the fixed administrator user handle.
func (u *AdminUser) WebAuthnID() []byte { return u.id }

// WebAuthnName returns the fixed administrator name.
func (u *AdminUser) WebAuthnName() string { return u.name }

// WebAuthnDisplayName returns the fixed administrator display name.
func (u *AdminUser) WebAuthnDisplayName() string { return u.displayName }

// WebAuthnCredentials returns the credentials associated with this user.
func (u *AdminUser) WebAuthnCredentials() []gowa.Credential { return u.credentials }

// ---------------------------------------------------------------------------
// Repository
// ---------------------------------------------------------------------------

// rowsScan allows tests to override rows.Scan for error-path coverage.
var rowsScan = func(rows pgx.Rows, dest ...any) error {
	return rows.Scan(dest...)
}

// Repository provides database access for WebAuthn credentials and sessions.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a new WebAuthn repository.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// ---------------------------------------------------------------------------
// Credential CRUD
// ---------------------------------------------------------------------------

// StoreCredential inserts a new WebAuthn credential record.
func (r *Repository) StoreCredential(ctx context.Context, cred *CredentialRecord) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO webauthn_credentials (id, public_key, attestation_type, attestation_format, transport, flags_byte, sign_count, aaguid, attestation_object, attestation_client_data, attestation_client_data_hash, attestation_public_key_algo, authenticator_data)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		 ON CONFLICT (id) DO UPDATE SET
		   public_key = EXCLUDED.public_key,
		   attestation_type = EXCLUDED.attestation_type,
		   attestation_format = EXCLUDED.attestation_format,
		   transport = EXCLUDED.transport,
		   flags_byte = EXCLUDED.flags_byte,
		   sign_count = EXCLUDED.sign_count,
		   aaguid = EXCLUDED.aaguid,
		   attestation_object = EXCLUDED.attestation_object,
		   attestation_client_data = EXCLUDED.attestation_client_data,
		   attestation_client_data_hash = EXCLUDED.attestation_client_data_hash,
		   attestation_public_key_algo = EXCLUDED.attestation_public_key_algo,
		   authenticator_data = EXCLUDED.authenticator_data,
		   updated_at = NOW()`,
		cred.ID, cred.PublicKey, cred.AttestationType, cred.AttestationFormat, cred.Transport, cred.FlagsByte, cred.SignCount, cred.AAGUID, cred.AttestationObject, cred.AttestationClientData, cred.AttestationClientDataHash, cred.AttestationPublicKeyAlgo, cred.AuthenticatorData,
	)
	if err != nil {
		debuglog.Error("webauthn: failed to store credential", "credential_id_len", len(cred.ID), "error", err)
		return err
	}

	debuglog.Info("webauthn: stored credential", "credential_id_len", len(cred.ID))
	return nil
}

// ListCredentials returns all WebAuthn credentials.
func (r *Repository) ListCredentials(ctx context.Context) ([]*CredentialRecord, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+credentialColumns+` FROM webauthn_credentials ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var creds []*CredentialRecord
	for rows.Next() {
		var cred CredentialRecord
		if err := rowsScan(rows,
			&cred.ID, &cred.PublicKey, &cred.AttestationType, &cred.AttestationFormat, &cred.Transport,
			&cred.FlagsByte, &cred.SignCount, &cred.AAGUID, &cred.AttestationObject, &cred.AttestationClientData,
			&cred.AttestationClientDataHash, &cred.AttestationPublicKeyAlgo, &cred.AuthenticatorData,
			&cred.CreatedAt, &cred.UpdatedAt,
		); err != nil {
			return nil, err
		}
		creds = append(creds, &cred)
	}
	return creds, rows.Err()
}

// GetCredentialByID retrieves a WebAuthn credential by its ID.
func (r *Repository) GetCredentialByID(ctx context.Context, id []byte) (*CredentialRecord, error) {
	var cred CredentialRecord
	err := r.pool.QueryRow(ctx,
		`SELECT `+credentialColumns+` FROM webauthn_credentials WHERE id = $1`, id,
	).Scan(
		&cred.ID, &cred.PublicKey, &cred.AttestationType, &cred.AttestationFormat, &cred.Transport,
		&cred.FlagsByte, &cred.SignCount, &cred.AAGUID, &cred.AttestationObject, &cred.AttestationClientData,
		&cred.AttestationClientDataHash, &cred.AttestationPublicKeyAlgo, &cred.AuthenticatorData,
		&cred.CreatedAt, &cred.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &cred, nil
}

// DeleteCredential removes a WebAuthn credential by its ID.
func (r *Repository) DeleteCredential(ctx context.Context, id []byte) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM webauthn_credentials WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	debuglog.Info("webauthn: deleted credential", "credential_id_len", len(id))
	return nil
}

// UpdateSignCount updates the signature counter for a credential.
func (r *Repository) UpdateSignCount(ctx context.Context, id []byte, signCount uint32) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE webauthn_credentials SET sign_count = $1, updated_at = NOW() WHERE id = $2`,
		signCount, id,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------------------
// Session CRUD
// ---------------------------------------------------------------------------

// CreateSession inserts a new WebAuthn session record.
func (r *Repository) CreateSession(ctx context.Context, session *SessionRecord) error {
	_, err := r.pool.Exec(ctx,
		`INSERT INTO webauthn_sessions (id, challenge, session_data, type, user_id, token_hash, expires_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		session.ID, session.Challenge, session.SessionData, session.Type, session.UserID, session.TokenHash, session.ExpiresAt,
	)
	if err != nil {
		debuglog.Error("webauthn: failed to create session", "session_id", session.ID, "type", session.Type, "error", err)
		return err
	}
	return nil
}

// GetSession retrieves a WebAuthn session by its ID.
func (r *Repository) GetSession(ctx context.Context, id uuid.UUID) (*SessionRecord, error) {
	var s SessionRecord
	err := r.pool.QueryRow(ctx,
		`SELECT `+sessionColumns+` FROM webauthn_sessions WHERE id = $1`, id,
	).Scan(&s.ID, &s.Challenge, &s.SessionData, &s.Type, &s.UserID, &s.TokenHash, &s.ExpiresAt, &s.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &s, nil
}

// GetSessionByTokenHash retrieves an auth_token session by its SHA-256 hash.
// Used by SessionManager.Validate to look up sessions without storing plaintext tokens.
func (r *Repository) GetSessionByTokenHash(ctx context.Context, tokenHash string) (*SessionRecord, error) {
	var s SessionRecord
	err := r.pool.QueryRow(ctx,
		`SELECT `+sessionColumns+` FROM webauthn_sessions WHERE token_hash = $1 AND type = 'auth_token'`,
		tokenHash,
	).Scan(&s.ID, &s.Challenge, &s.SessionData, &s.Type, &s.UserID, &s.TokenHash, &s.ExpiresAt, &s.CreatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return &s, nil
}

// DeleteSession removes a WebAuthn session by its ID.
func (r *Repository) DeleteSession(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM webauthn_sessions WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// CleanupExpiredSessions removes sessions that have passed their expiry time.
func (r *Repository) CleanupExpiredSessions(ctx context.Context) (int64, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM webauthn_sessions WHERE expires_at < NOW()`)
	if err != nil {
		debuglog.Error("webauthn: failed to cleanup expired sessions", "error", err)
		return 0, err
	}
	n := tag.RowsAffected()
	if n > 0 {
		debuglog.Info("webauthn: cleaned up expired sessions", "count", n)
	}
	return n, nil
}

// ---------------------------------------------------------------------------
// Sentinels
// ---------------------------------------------------------------------------

// ErrNotFound is returned when a credential or session is not found.
var ErrNotFound = &notFoundError{}

type notFoundError struct{}

func (e *notFoundError) Error() string { return "webauthn record not found" }

// ---------------------------------------------------------------------------
// Relying Party
// ---------------------------------------------------------------------------

// NewRelyingParty creates a new WebAuthn Relying Party instance with the given
// configuration.
func NewRelyingParty(rpID, rpDisplayName string, rpOrigins []string) (*gowa.WebAuthn, error) {
	return gowa.New(&gowa.Config{
		RPID:          rpID,
		RPDisplayName: rpDisplayName,
		RPOrigins:     rpOrigins,
	})
}
