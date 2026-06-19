// Package totp manages the single admin TOTP (RFC 6238) second-factor
// configuration: an AES-GCM encrypted secret plus single-use recovery codes.
//
// The plaintext TOTP secret never persists to disk; it is encrypted with the
// process MASTER_KEY via internal/auth (the same AES-256-GCM + Argon2id key
// derivation that protects provider API keys). Recovery codes are stored as
// SHA-256 hashes and are single-use enforced atomically.
//
// No-content logging rule: secret, otpauth URI, recovery codes, and submitted
// codes are never logged. Only events are (e.g. "totp: enabled").
package totp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pquerna/otp/totp"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// Repository manages the single admin TOTP config (encrypted secret) and
// single-use recovery codes. The plaintext secret never persists; it is
// AES-GCM encrypted with the process MASTER_KEY like provider API keys.
type Repository struct {
	db        *pgxpool.Pool
	masterKey string
}

// NewRepository creates a new TOTP repository backed by the given connection
// pool. The masterKey is used to encrypt/decrypt the TOTP secret at rest.
func NewRepository(pool *pgxpool.Pool, masterKey string) *Repository {
	return &Repository{db: pool, masterKey: masterKey}
}

// Enroll generates a new TOTP secret, encrypts it with MASTER_KEY, and stores a
// provisional row (enabled=false, confirmed_at=NULL). It returns the otpauth
// URI (for QR rendering) and the base32 secret (for manual entry).
//
// Re-enrolling overwrites any prior provisional or enabled secret: the ON
// CONFLICT clause replaces secret_cipher/nonce/salt and resets enabled to
// false and confirmed_at to NULL, so a half-finished enrollment cleanly
// restarts and a live enrollment requires re-verification.
func (r *Repository) Enroll(ctx context.Context) (uri, secret string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Model Hotel",
		AccountName: "admin",
	})
	if err != nil {
		return "", "", fmt.Errorf("totp: generate secret: %w", err)
	}
	secret = key.Secret()

	kp, err := auth.Encrypt(secret, r.masterKey)
	if err != nil {
		return "", "", fmt.Errorf("totp: encrypt secret: %w", err)
	}

	_, err = r.db.Exec(ctx,
		`INSERT INTO admin_totp (id, secret_cipher, secret_nonce, secret_salt, enabled, confirmed_at)
		 VALUES (1, $1, $2, $3, FALSE, NULL)
		 ON CONFLICT (id) DO UPDATE SET
		   secret_cipher = EXCLUDED.secret_cipher,
		   secret_nonce  = EXCLUDED.secret_nonce,
		   secret_salt   = EXCLUDED.secret_salt,
		   enabled       = FALSE,
		   confirmed_at  = NULL`,
		kp.Ciphertext, kp.Nonce, kp.Salt,
	)
	if err != nil {
		return "", "", fmt.Errorf("totp: upsert enrollment: %w", err)
	}

	debuglog.Info("totp: enrollment started")
	return key.URL(), secret, nil
}

// Verify checks a 6-digit code against the stored secret. Returns
// (false, nil) when no enrollment exists and (false, err) on decrypt failure.
// totp.Validate uses the standard TOTP defaults (period 30, skew 1 step, 6
// digits, SHA-1) and performs a constant-time comparison internally.
func (r *Repository) Verify(ctx context.Context, code string) (bool, error) {
	var cipher, nonce, salt []byte
	err := r.db.QueryRow(ctx,
		`SELECT secret_cipher, secret_nonce, secret_salt FROM admin_totp WHERE id = 1`,
	).Scan(&cipher, &nonce, &salt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("totp: load secret: %w", err)
	}

	secret, err := auth.Decrypt(cipher, nonce, salt, r.masterKey)
	if err != nil {
		return false, fmt.Errorf("totp: decrypt secret: %w", err)
	}

	return totp.Validate(code, secret), nil
}

// Enable flips the single admin_totp row to enabled=true and stamps confirmed_at.
// Returns an error if no provisional enrollment exists (rows affected = 0).
func (r *Repository) Enable(ctx context.Context) error {
	tag, err := r.db.Exec(ctx,
		`UPDATE admin_totp SET enabled = TRUE, confirmed_at = NOW() WHERE id = 1`,
	)
	if err != nil {
		return fmt.Errorf("totp: enable: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("totp: no provisional enrollment to enable")
	}
	debuglog.Info("totp: enabled")
	return nil
}

// Disable deletes the TOTP config and all recovery codes, returning the login
// to single-factor (raw admin token) behavior. Both deletes run in one
// transaction so a failure cannot leave recovery codes behind with no secret
// (or vice versa).
func (r *Repository) Disable(ctx context.Context) error {
	tx, err := r.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("totp: disable (begin): %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM admin_totp WHERE id = 1`); err != nil {
		return fmt.Errorf("totp: disable (delete config): %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM admin_totp_recovery`); err != nil {
		return fmt.Errorf("totp: disable (delete recovery): %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("totp: disable (commit): %w", err)
	}
	debuglog.Info("totp: disabled")
	return nil
}

// IsEnabled reports whether TOTP 2FA is currently active. Returns (false, nil)
// when no enrollment exists.
func (r *Repository) IsEnabled(ctx context.Context) (bool, error) {
	var enabled bool
	err := r.db.QueryRow(ctx,
		`SELECT enabled FROM admin_totp WHERE id = 1`,
	).Scan(&enabled)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("totp: is_enabled: %w", err)
	}
	return enabled, nil
}

// GenerateRecoveryCodes generates 10 single-use recovery codes, stores their
// SHA-256 hashes (replacing any existing set), and returns the plaintext codes
// in order for one-time display. The codes are never logged.
//
// Each code is 16 base32 chars formatted as XXXX-XXXX-XXXX-XXXX (10 bytes of
// crypto/rand -> 16 base32 chars with no padding, uppercased).
func (r *Repository) GenerateRecoveryCodes(ctx context.Context) ([]string, error) {
	codes := make([]string, 0, 10)
	hashes := make([]string, 0, 10)
	for range 10 {
		code, err := generateRecoveryCode()
		if err != nil {
			return nil, fmt.Errorf("totp: generate recovery code: %w", err)
		}
		codes = append(codes, code)
		hashes = append(hashes, sha256hex(code))
	}

	tx, err := r.db.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("totp: recovery codes (begin): %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM admin_totp_recovery`); err != nil {
		return nil, fmt.Errorf("totp: recovery codes (delete): %w", err)
	}

	// Build a single batched INSERT: VALUES ($1), ($2), ..., ($10).
	placeholders := make([]string, len(hashes))
	args := make([]any, len(hashes))
	for i, h := range hashes {
		placeholders[i] = fmt.Sprintf("($%d)", i+1)
		args[i] = h
	}
	query := "INSERT INTO admin_totp_recovery (code_hash) VALUES " + strings.Join(placeholders, ", ")
	if _, err := tx.Exec(ctx, query, args...); err != nil {
		return nil, fmt.Errorf("totp: recovery codes (insert): %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("totp: recovery codes (commit): %w", err)
	}

	return codes, nil
}

// ConsumeRecoveryCode marks a recovery code as used if it is valid and unused.
// Returns ok=true only when exactly one row matched (valid hash, unused).
// The atomic UPDATE ... WHERE used_at IS NULL makes double-use impossible.
func (r *Repository) ConsumeRecoveryCode(ctx context.Context, code string) (bool, error) {
	hash := sha256hex(normalizeRecoveryCode(code))
	tag, err := r.db.Exec(ctx,
		`UPDATE admin_totp_recovery SET used_at = NOW() WHERE code_hash = $1 AND used_at IS NULL`,
		hash,
	)
	if err != nil {
		return false, fmt.Errorf("totp: consume recovery code: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// generateRecoveryCode returns a 16-char base32 code grouped as
// XXXX-XXXX-XXXX-XXXX from 10 bytes of crypto/rand entropy.
func generateRecoveryCode() (string, error) {
	buf := make([]byte, 10)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	enc := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(buf)
	enc = strings.ToUpper(enc)
	// Pad/truncate to exactly 16 chars (10 bytes -> 16 base32 chars by design,
	// but be defensive in case the encoder ever changes).
	for len(enc) < 16 {
		enc += base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString([]byte{0})[:1]
	}
	if len(enc) > 16 {
		enc = enc[:16]
	}
	return enc[0:4] + "-" + enc[4:8] + "-" + enc[8:12] + "-" + enc[12:16], nil
}

// normalizeRecoveryCode canonicalizes user-entered codes so a valid code still
// matches when typed lowercase, with spaces, or without the grouping dashes.
// It uppercases, drops every non-alphanumeric rune, and regroups exactly 16
// characters as XXXX-XXXX-XXXX-XXXX (the stored format). Non-16-length input is
// returned cleaned-but-ungrouped, which simply will not match any stored hash.
func normalizeRecoveryCode(code string) string {
	var b strings.Builder
	for _, c := range strings.ToUpper(code) {
		if (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			b.WriteRune(c)
		}
	}
	s := b.String()
	if len(s) == 16 {
		return s[0:4] + "-" + s[4:8] + "-" + s[8:12] + "-" + s[12:16]
	}
	return s
}

// sha256hex returns the lowercase hex SHA-256 digest of s.
func sha256hex(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}
