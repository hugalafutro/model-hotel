package totp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// UserPostgresStore is the per-user Store implementation over the user_totp /
// user_totp_recovery tables (migration 052). One instance is bound to a single
// user id, so the stateless Repository (crypto + policy) is reused verbatim via
// NewRepositoryWithStore -- the single-use, atomic-disable, and recovery-code
// guarantees all carry over unchanged from the admin store.
type UserPostgresStore struct {
	db     *pgxpool.Pool
	userID uuid.UUID
}

// Compile-time assertion that UserPostgresStore satisfies Store.
var _ Store = (*UserPostgresStore)(nil)

// NewUserPostgresStore creates a Postgres-backed TOTP store bound to one user.
func NewUserPostgresStore(pool *pgxpool.Pool, userID uuid.UUID) *UserPostgresStore {
	return &UserPostgresStore{db: pool, userID: userID}
}

// UpsertEnrollment stores or replaces the user's provisional secret, resetting
// enabled/confirmed_at/last_used_step so a half-finished or live enrollment
// cleanly restarts and requires re-verification.
func (s *UserPostgresStore) UpsertEnrollment(ctx context.Context, cipher, nonce, salt []byte) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO user_totp (user_id, secret_cipher, secret_nonce, secret_salt, enabled, confirmed_at)
		 VALUES ($1, $2, $3, $4, FALSE, NULL)
		 ON CONFLICT (user_id) DO UPDATE SET
		   secret_cipher = EXCLUDED.secret_cipher,
		   secret_nonce  = EXCLUDED.secret_nonce,
		   secret_salt   = EXCLUDED.secret_salt,
		   enabled       = FALSE,
		   confirmed_at  = NULL,
		   last_used_step = NULL`,
		s.userID, cipher, nonce, salt,
	)
	if err != nil {
		return fmt.Errorf("totp: user upsert enrollment: %w", err)
	}
	return nil
}

// LoadSecret returns the user's stored secret, ok=false when none is enrolled.
func (s *UserPostgresStore) LoadSecret(ctx context.Context) (EncryptedSecret, bool, error) {
	var sec EncryptedSecret
	err := s.db.QueryRow(ctx,
		`SELECT secret_cipher, secret_nonce, secret_salt FROM user_totp WHERE user_id = $1`,
		s.userID,
	).Scan(&sec.Cipher, &sec.Nonce, &sec.Salt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return EncryptedSecret{}, false, nil
		}
		return EncryptedSecret{}, false, fmt.Errorf("totp: user load secret: %w", err)
	}
	return sec, true, nil
}

// RecordUsedStep atomically advances the user's last_used_step; the conditional
// UPDATE makes a concurrent replay of the same step impossible.
func (s *UserPostgresStore) RecordUsedStep(ctx context.Context, step int64) (bool, error) {
	tag, err := s.db.Exec(ctx,
		`UPDATE user_totp SET last_used_step = $2
		 WHERE user_id = $1 AND (last_used_step IS NULL OR last_used_step < $2)`,
		s.userID, step,
	)
	if err != nil {
		return false, fmt.Errorf("totp: user record used step: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// Enable flips the user's row to enabled=true; returns false when there was no
// provisional enrollment to enable.
func (s *UserPostgresStore) Enable(ctx context.Context) (bool, error) {
	tag, err := s.db.Exec(ctx,
		`UPDATE user_totp SET enabled = TRUE, confirmed_at = NOW() WHERE user_id = $1`,
		s.userID,
	)
	if err != nil {
		return false, fmt.Errorf("totp: user enable: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// Disable deletes the user's config and recovery codes in one transaction.
func (s *UserPostgresStore) Disable(ctx context.Context) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("totp: user disable (begin): %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM user_totp WHERE user_id = $1`, s.userID); err != nil {
		return fmt.Errorf("totp: user disable (delete config): %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_totp_recovery WHERE user_id = $1`, s.userID); err != nil {
		return fmt.Errorf("totp: user disable (delete recovery): %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("totp: user disable (commit): %w", err)
	}
	return nil
}

// DisableIfAuthorized runs the load -> authorize -> delete sequence in a single
// transaction, scoped to the bound user. The recoveryUnused probe queries within
// the same transaction, so the authorization decision and the deletes are atomic.
func (s *UserPostgresStore) DisableIfAuthorized(ctx context.Context, authorize DisableAuthorizer) (bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("totp: user disable (begin): %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var sec EncryptedSecret
	var lastUsedStep *int64
	err = tx.QueryRow(ctx,
		`SELECT secret_cipher, secret_nonce, secret_salt, last_used_step FROM user_totp WHERE user_id = $1`,
		s.userID,
	).Scan(&sec.Cipher, &sec.Nonce, &sec.Salt, &lastUsedStep)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("totp: user disable (load secret): %w", err)
	}

	recoveryUnused := func(codeHash string) (bool, error) {
		var n int
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM user_totp_recovery WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL`,
			s.userID, codeHash,
		).Scan(&n); err != nil {
			return false, fmt.Errorf("totp: user disable (check recovery): %w", err)
		}
		return n == 1, nil
	}

	authorized, err := authorize(sec, lastUsedStep, recoveryUnused)
	if err != nil {
		return false, err
	}
	if !authorized {
		return false, nil
	}

	if _, err := tx.Exec(ctx, `DELETE FROM user_totp WHERE user_id = $1`, s.userID); err != nil {
		return false, fmt.Errorf("totp: user disable (delete config): %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_totp_recovery WHERE user_id = $1`, s.userID); err != nil {
		return false, fmt.Errorf("totp: user disable (delete recovery): %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("totp: user disable (commit): %w", err)
	}
	return true, nil
}

// IsEnabled reports whether the user's TOTP is active; (false, nil) when not enrolled.
func (s *UserPostgresStore) IsEnabled(ctx context.Context) (bool, error) {
	var enabled bool
	err := s.db.QueryRow(ctx,
		`SELECT enabled FROM user_totp WHERE user_id = $1`, s.userID,
	).Scan(&enabled)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("totp: user is_enabled: %w", err)
	}
	return enabled, nil
}

// EnabledAt returns confirmed_at when enrolled AND enabled; ok=false otherwise.
func (s *UserPostgresStore) EnabledAt(ctx context.Context) (time.Time, bool, error) {
	var confirmedAt *time.Time
	err := s.db.QueryRow(ctx,
		`SELECT confirmed_at FROM user_totp WHERE user_id = $1 AND enabled`, s.userID,
	).Scan(&confirmedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, fmt.Errorf("totp: user enabled_at: %w", err)
	}
	if confirmedAt == nil {
		return time.Time{}, false, nil
	}
	return *confirmedAt, true, nil
}

// RecoveryCounts returns the number of unused and total recovery codes.
func (s *UserPostgresStore) RecoveryCounts(ctx context.Context) (int, int, error) {
	var remaining, total int
	if err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FILTER (WHERE used_at IS NULL), COUNT(*) FROM user_totp_recovery WHERE user_id = $1`,
		s.userID,
	).Scan(&remaining, &total); err != nil {
		return 0, 0, fmt.Errorf("totp: user recovery counts: %w", err)
	}
	return remaining, total, nil
}

// LastUsedStep returns the last accepted TOTP step (may be nil); ok=false when
// no enrollment row exists.
func (s *UserPostgresStore) LastUsedStep(ctx context.Context) (*int64, bool, error) {
	var step *int64
	err := s.db.QueryRow(ctx,
		`SELECT last_used_step FROM user_totp WHERE user_id = $1`, s.userID,
	).Scan(&step)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("totp: user last used: %w", err)
	}
	return step, true, nil
}

// ReplaceRecoveryCodes atomically deletes the user's recovery codes and inserts
// the given hashes as the new set.
func (s *UserPostgresStore) ReplaceRecoveryCodes(ctx context.Context, codeHashes []string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("totp: user recovery codes (begin): %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM user_totp_recovery WHERE user_id = $1`, s.userID); err != nil {
		return fmt.Errorf("totp: user recovery codes (delete): %w", err)
	}

	if len(codeHashes) > 0 {
		// Build a single batched INSERT: VALUES ($1, $2), ($1, $3), ...
		placeholders := make([]string, len(codeHashes))
		args := make([]any, 0, len(codeHashes)+1)
		args = append(args, s.userID)
		for i, h := range codeHashes {
			placeholders[i] = fmt.Sprintf("($1, $%d)", i+2)
			args = append(args, h)
		}
		query := "INSERT INTO user_totp_recovery (user_id, code_hash) VALUES " + strings.Join(placeholders, ", ")
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			return fmt.Errorf("totp: user recovery codes (insert): %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("totp: user recovery codes (commit): %w", err)
	}
	return nil
}

// ConsumeRecoveryCode atomically marks one of the user's unused codes (by hash)
// as used. The UPDATE ... WHERE used_at IS NULL makes double-use impossible.
func (s *UserPostgresStore) ConsumeRecoveryCode(ctx context.Context, codeHash string) (bool, error) {
	tag, err := s.db.Exec(ctx,
		`UPDATE user_totp_recovery SET used_at = NOW() WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL`,
		s.userID, codeHash,
	)
	if err != nil {
		return false, fmt.Errorf("totp: user consume recovery code: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}
