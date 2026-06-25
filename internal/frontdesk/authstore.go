package frontdesk

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/totp"
	"github.com/hugalafutro/model-hotel/internal/webauthn"
)

// This file holds the SQLite implementations of the webauthn and totp storage
// contracts extracted in the auth-interface refactor. They mirror the Postgres
// implementations (internal/webauthn/webauthn.go, internal/totp/postgres_store.go)
// row-for-row, so the reused SessionManager and totp.Repository behave
// identically. Keeping these here (rather than in webauthn/totp) keeps the
// modernc.org/sqlite dependency out of the main server's import graph.

// ---------------------------------------------------------------------------
// WebAuthn SQLite store
// ---------------------------------------------------------------------------

// WebAuthnStore is the SQLite-backed webauthn.Store, sharing the control plane's
// database handle.
type WebAuthnStore struct {
	db *sql.DB
}

// Compile-time assertion that WebAuthnStore satisfies the full Store contract.
var _ webauthn.Store = (*WebAuthnStore)(nil)

// NewWebAuthnStore returns a SQLite webauthn store over the Front Desk database.
func NewWebAuthnStore(store *Store) *WebAuthnStore {
	return &WebAuthnStore{db: store.db}
}

const waCredentialColumns = `id, name, public_key, attestation_type, attestation_format, transport, flags_byte, sign_count, aaguid, attestation_object, attestation_client_data, attestation_client_data_hash, attestation_public_key_algo, authenticator_data, created_at, updated_at`

const waSessionColumns = `id, challenge, session_data, type, user_id, token_hash, credential_id, expires_at, created_at`

// StoreCredential inserts or replaces a credential (upsert by id).
func (s *WebAuthnStore) StoreCredential(ctx context.Context, cred *webauthn.CredentialRecord) error {
	transport, err := json.Marshal(cred.Transport)
	if err != nil {
		return fmt.Errorf("frontdesk: marshal transport: %w", err)
	}
	now := time.Now().UTC().UnixNano()
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO webauthn_credentials
		   (id, name, public_key, attestation_type, attestation_format, transport, flags_byte, sign_count, aaguid, attestation_object, attestation_client_data, attestation_client_data_hash, attestation_public_key_algo, authenticator_data, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT (id) DO UPDATE SET
		   name = excluded.name,
		   public_key = excluded.public_key,
		   attestation_type = excluded.attestation_type,
		   attestation_format = excluded.attestation_format,
		   transport = excluded.transport,
		   flags_byte = excluded.flags_byte,
		   sign_count = excluded.sign_count,
		   aaguid = excluded.aaguid,
		   attestation_object = excluded.attestation_object,
		   attestation_client_data = excluded.attestation_client_data,
		   attestation_client_data_hash = excluded.attestation_client_data_hash,
		   attestation_public_key_algo = excluded.attestation_public_key_algo,
		   authenticator_data = excluded.authenticator_data,
		   updated_at = excluded.updated_at`,
		cred.ID, cred.Name, cred.PublicKey, cred.AttestationType, cred.AttestationFormat, string(transport),
		int64(cred.FlagsByte), int64(cred.SignCount), cred.AAGUID.String(), cred.AttestationObject,
		cred.AttestationClientData, cred.AttestationClientDataHash, cred.AttestationPublicKeyAlgo,
		cred.AuthenticatorData, now, now,
	)
	if err != nil {
		return fmt.Errorf("frontdesk: store credential: %w", err)
	}
	return nil
}

// ListCredentials returns all credentials, newest first.
func (s *WebAuthnStore) ListCredentials(ctx context.Context) ([]*webauthn.CredentialRecord, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+waCredentialColumns+` FROM webauthn_credentials ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("frontdesk: list credentials: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var creds []*webauthn.CredentialRecord
	for rows.Next() {
		cred, err := scanCredential(rows)
		if err != nil {
			return nil, err
		}
		creds = append(creds, cred)
	}
	return creds, rows.Err()
}

// GetCredentialByID returns one credential, or webauthn.ErrNotFound.
func (s *WebAuthnStore) GetCredentialByID(ctx context.Context, id []byte) (*webauthn.CredentialRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+waCredentialColumns+` FROM webauthn_credentials WHERE id = ?`, id)
	cred, err := scanCredential(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, webauthn.ErrNotFound
		}
		return nil, err
	}
	return cred, nil
}

// DeleteCredential removes a credential and revokes its derived auth_token
// sessions in one transaction, mirroring the Postgres cascade.
func (s *WebAuthnStore) DeleteCredential(ctx context.Context, id []byte) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("frontdesk: delete credential (begin): %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM webauthn_sessions WHERE credential_id = ? AND type = 'auth_token'`, id,
	); err != nil {
		return fmt.Errorf("frontdesk: delete credential (revoke sessions): %w", err)
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM webauthn_credentials WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("frontdesk: delete credential: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return webauthn.ErrNotFound
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("frontdesk: delete credential (commit): %w", err)
	}
	return nil
}

// RenameCredential updates a credential's display name.
func (s *WebAuthnStore) RenameCredential(ctx context.Context, id []byte, name string) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE webauthn_credentials SET name = ?, updated_at = ? WHERE id = ?`,
		name, time.Now().UTC().UnixNano(), id,
	)
	return waAffected(res, err)
}

// UpdateSignCount updates a credential's signature counter.
func (s *WebAuthnStore) UpdateSignCount(ctx context.Context, id []byte, signCount uint32) error {
	res, err := s.db.ExecContext(ctx,
		`UPDATE webauthn_credentials SET sign_count = ?, updated_at = ? WHERE id = ?`,
		int64(signCount), time.Now().UTC().UnixNano(), id,
	)
	return waAffected(res, err)
}

// CreateSession inserts a session record.
func (s *WebAuthnStore) CreateSession(ctx context.Context, session *webauthn.SessionRecord) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO webauthn_sessions (id, challenge, session_data, type, user_id, token_hash, credential_id, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID.String(), session.Challenge, session.SessionData, session.Type, session.UserID,
		session.TokenHash, session.CredentialID, session.ExpiresAt.UTC().UnixNano(), time.Now().UTC().UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("frontdesk: create session: %w", err)
	}
	return nil
}

// GetSession returns a session by id, or webauthn.ErrNotFound.
func (s *WebAuthnStore) GetSession(ctx context.Context, id uuid.UUID) (*webauthn.SessionRecord, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+waSessionColumns+` FROM webauthn_sessions WHERE id = ?`, id.String())
	return scanSessionResult(row)
}

// GetSessionByTokenHash returns an auth_token session by hash, or webauthn.ErrNotFound.
func (s *WebAuthnStore) GetSessionByTokenHash(ctx context.Context, tokenHash string) (*webauthn.SessionRecord, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+waSessionColumns+` FROM webauthn_sessions WHERE token_hash = ? AND type = 'auth_token'`, tokenHash,
	)
	return scanSessionResult(row)
}

// DeleteSession removes a session by id.
func (s *WebAuthnStore) DeleteSession(ctx context.Context, id uuid.UUID) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM webauthn_sessions WHERE id = ?`, id.String())
	return waAffected(res, err)
}

// CleanupExpiredSessions removes sessions past their expiry.
func (s *WebAuthnStore) CleanupExpiredSessions(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM webauthn_sessions WHERE expires_at < ?`, time.Now().UTC().UnixNano())
	if err != nil {
		return 0, fmt.Errorf("frontdesk: cleanup expired sessions: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func scanCredential(sc scanner) (*webauthn.CredentialRecord, error) {
	var (
		cred      webauthn.CredentialRecord
		transport string
		flags     int64
		signCount int64
		aaguid    string
		algo      sql.NullInt64
		createdAt int64
		updatedAt int64
	)
	if err := sc.Scan(
		&cred.ID, &cred.Name, &cred.PublicKey, &cred.AttestationType, &cred.AttestationFormat, &transport,
		&flags, &signCount, &aaguid, &cred.AttestationObject, &cred.AttestationClientData,
		&cred.AttestationClientDataHash, &algo, &cred.AuthenticatorData, &createdAt, &updatedAt,
	); err != nil {
		return nil, err
	}
	if transport != "" {
		if err := json.Unmarshal([]byte(transport), &cred.Transport); err != nil {
			return nil, fmt.Errorf("frontdesk: unmarshal transport: %w", err)
		}
	}
	// These columns are written from a byte and a uint32 respectively (see
	// StoreCredential), so the stored values are always within range.
	cred.FlagsByte = byte(flags & 0xff)             //nolint:gosec // value originates from a byte
	cred.SignCount = uint32(signCount & 0xffffffff) //nolint:gosec // value originates from a uint32
	if parsed, err := uuid.Parse(aaguid); err == nil {
		cred.AAGUID = parsed
	}
	if algo.Valid {
		cred.AttestationPublicKeyAlgo = algo.Int64
	}
	cred.CreatedAt = time.Unix(0, createdAt).UTC()
	cred.UpdatedAt = time.Unix(0, updatedAt).UTC()
	return &cred, nil
}

func scanSessionResult(sc scanner) (*webauthn.SessionRecord, error) {
	var (
		s         webauthn.SessionRecord
		idStr     string
		expiresAt int64
		createdAt int64
	)
	if err := sc.Scan(&idStr, &s.Challenge, &s.SessionData, &s.Type, &s.UserID, &s.TokenHash, &s.CredentialID, &expiresAt, &createdAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, webauthn.ErrNotFound
		}
		return nil, err
	}
	parsed, err := uuid.Parse(idStr)
	if err != nil {
		return nil, fmt.Errorf("frontdesk: parse session id: %w", err)
	}
	s.ID = parsed
	s.ExpiresAt = time.Unix(0, expiresAt).UTC()
	s.CreatedAt = time.Unix(0, createdAt).UTC()
	return &s, nil
}

func waAffected(res sql.Result, err error) error {
	if err != nil {
		return fmt.Errorf("frontdesk: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return webauthn.ErrNotFound
	}
	return nil
}

// ---------------------------------------------------------------------------
// TOTP SQLite store
// ---------------------------------------------------------------------------

// TOTPStore is the SQLite-backed totp.Store, sharing the control plane's
// database handle. It owns SQL and transactions only; totp.Repository owns the
// crypto and policy.
type TOTPStore struct {
	db *sql.DB
}

// Compile-time assertion that TOTPStore satisfies totp.Store.
var _ totp.Store = (*TOTPStore)(nil)

// NewTOTPStore returns a SQLite totp store over the Front Desk database.
func NewTOTPStore(store *Store) *TOTPStore {
	return &TOTPStore{db: store.db}
}

// UpsertEnrollment stores or replaces the provisional secret, resetting the
// enabled/confirmed/last-used state so a half-finished enrollment restarts clean.
func (s *TOTPStore) UpsertEnrollment(ctx context.Context, cipher, nonce, salt []byte) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO admin_totp (id, secret_cipher, secret_nonce, secret_salt, enabled, created_at, confirmed_at, last_used_step)
		 VALUES (1, ?, ?, ?, 0, ?, NULL, NULL)
		 ON CONFLICT (id) DO UPDATE SET
		   secret_cipher = excluded.secret_cipher,
		   secret_nonce = excluded.secret_nonce,
		   secret_salt = excluded.secret_salt,
		   enabled = 0,
		   confirmed_at = NULL,
		   last_used_step = NULL`,
		cipher, nonce, salt, time.Now().UTC().UnixNano(),
	)
	if err != nil {
		return fmt.Errorf("totp: upsert enrollment: %w", err)
	}
	return nil
}

// LoadSecret returns the stored secret, ok=false when none is enrolled.
func (s *TOTPStore) LoadSecret(ctx context.Context) (totp.EncryptedSecret, bool, error) {
	var sec totp.EncryptedSecret
	err := s.db.QueryRowContext(ctx,
		`SELECT secret_cipher, secret_nonce, secret_salt FROM admin_totp WHERE id = 1`,
	).Scan(&sec.Cipher, &sec.Nonce, &sec.Salt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return totp.EncryptedSecret{}, false, nil
		}
		return totp.EncryptedSecret{}, false, fmt.Errorf("totp: load secret: %w", err)
	}
	return sec, true, nil
}

// RecordUsedStep atomically advances last_used_step only when strictly greater
// (or NULL). Returns true iff exactly one row was updated (single-use signal).
func (s *TOTPStore) RecordUsedStep(ctx context.Context, step int64) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE admin_totp SET last_used_step = ? WHERE id = 1 AND (last_used_step IS NULL OR last_used_step < ?)`,
		step, step,
	)
	if err != nil {
		return false, fmt.Errorf("totp: record used step: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// Enable flips the row to enabled and stamps confirmed_at. Returns false when
// there was no provisional enrollment.
func (s *TOTPStore) Enable(ctx context.Context) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE admin_totp SET enabled = 1, confirmed_at = ? WHERE id = 1`, time.Now().UTC().UnixNano(),
	)
	if err != nil {
		return false, fmt.Errorf("totp: enable: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// Disable deletes the config and all recovery codes in one transaction.
func (s *TOTPStore) Disable(ctx context.Context) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM admin_totp WHERE id = 1`); err != nil {
			return fmt.Errorf("totp: disable (delete config): %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM admin_totp_recovery`); err != nil {
			return fmt.Errorf("totp: disable (delete recovery): %w", err)
		}
		return nil
	})
}

// DisableIfAuthorized loads the secret and last_used_step, invokes authorize,
// and only when it returns true deletes the config and recovery codes, all in
// one transaction.
func (s *TOTPStore) DisableIfAuthorized(ctx context.Context, authorize totp.DisableAuthorizer) (bool, error) {
	var authorized bool
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var sec totp.EncryptedSecret
		var lastUsedStep *int64
		err := tx.QueryRowContext(ctx,
			`SELECT secret_cipher, secret_nonce, secret_salt, last_used_step FROM admin_totp WHERE id = 1`,
		).Scan(&sec.Cipher, &sec.Nonce, &sec.Salt, &lastUsedStep)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil // not enrolled: authorized stays false
			}
			return fmt.Errorf("totp: disable (load secret): %w", err)
		}

		recoveryUnused := func(codeHash string) (bool, error) {
			var n int
			if err := tx.QueryRowContext(ctx,
				`SELECT COUNT(*) FROM admin_totp_recovery WHERE code_hash = ? AND used_at IS NULL`, codeHash,
			).Scan(&n); err != nil {
				return false, fmt.Errorf("totp: disable (check recovery): %w", err)
			}
			return n == 1, nil
		}

		ok, err := authorize(sec, lastUsedStep, recoveryUnused)
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM admin_totp WHERE id = 1`); err != nil {
			return fmt.Errorf("totp: disable (delete config): %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM admin_totp_recovery`); err != nil {
			return fmt.Errorf("totp: disable (delete recovery): %w", err)
		}
		authorized = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return authorized, nil
}

// IsEnabled reports whether TOTP is active; (false, nil) when not enrolled.
func (s *TOTPStore) IsEnabled(ctx context.Context) (bool, error) {
	var enabled int
	err := s.db.QueryRowContext(ctx, `SELECT enabled FROM admin_totp WHERE id = 1`).Scan(&enabled)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("totp: is enabled: %w", err)
	}
	return enabled != 0, nil
}

// EnabledAt returns confirmed_at when enrolled AND enabled; ok=false otherwise.
func (s *TOTPStore) EnabledAt(ctx context.Context) (time.Time, bool, error) {
	var enabled int
	var confirmedAt *int64
	err := s.db.QueryRowContext(ctx, `SELECT enabled, confirmed_at FROM admin_totp WHERE id = 1`).Scan(&enabled, &confirmedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, fmt.Errorf("totp: enabled at: %w", err)
	}
	if enabled == 0 || confirmedAt == nil {
		return time.Time{}, false, nil
	}
	return time.Unix(0, *confirmedAt).UTC(), true, nil
}

// RecoveryCounts returns the number of unused and total recovery codes.
func (s *TOTPStore) RecoveryCounts(ctx context.Context) (int, int, error) {
	var remaining, total int
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FILTER (WHERE used_at IS NULL), COUNT(*) FROM admin_totp_recovery`,
	).Scan(&remaining, &total)
	if err != nil {
		return 0, 0, fmt.Errorf("totp: recovery counts: %w", err)
	}
	return remaining, total, nil
}

// LastUsedStep returns the last accepted TOTP step; ok=false when no enrollment.
func (s *TOTPStore) LastUsedStep(ctx context.Context) (*int64, bool, error) {
	var step *int64
	err := s.db.QueryRowContext(ctx, `SELECT last_used_step FROM admin_totp WHERE id = 1`).Scan(&step)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("totp: last used step: %w", err)
	}
	return step, true, nil
}

// ReplaceRecoveryCodes atomically replaces the recovery-code set.
func (s *TOTPStore) ReplaceRecoveryCodes(ctx context.Context, codeHashes []string) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM admin_totp_recovery`); err != nil {
			return fmt.Errorf("totp: recovery codes (delete): %w", err)
		}
		for _, h := range codeHashes {
			if _, err := tx.ExecContext(ctx, `INSERT INTO admin_totp_recovery (code_hash, used_at) VALUES (?, NULL)`, h); err != nil {
				return fmt.Errorf("totp: recovery codes (insert): %w", err)
			}
		}
		return nil
	})
}

// ConsumeRecoveryCode atomically marks a single unused code as used. Returns
// true iff exactly one row matched, making double-use impossible.
func (s *TOTPStore) ConsumeRecoveryCode(ctx context.Context, codeHash string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE admin_totp_recovery SET used_at = ? WHERE code_hash = ? AND used_at IS NULL`,
		time.Now().UTC().UnixNano(), codeHash,
	)
	if err != nil {
		return false, fmt.Errorf("totp: consume recovery code: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

func (s *TOTPStore) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("totp: begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("totp: commit tx: %w", err)
	}
	return nil
}
