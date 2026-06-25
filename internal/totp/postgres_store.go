package totp

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore is the PostgreSQL implementation of Store, used by the main
// server. It holds the same SQL the TOTP repository carried inline before the
// storage interface was extracted, so behavior is unchanged.
type PostgresStore struct {
	db *pgxpool.Pool
}

// Compile-time assertion that PostgresStore satisfies Store.
var _ Store = (*PostgresStore)(nil)

// NewPostgresStore creates a Postgres-backed TOTP store.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore {
	return &PostgresStore{db: pool}
}

// UpsertEnrollment stores or replaces the provisional secret. The ON CONFLICT
// clause resets enabled/confirmed_at/last_used_step so a half-finished or live
// enrollment cleanly restarts and requires re-verification.
func (s *PostgresStore) UpsertEnrollment(ctx context.Context, cipher, nonce, salt []byte) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO admin_totp (id, secret_cipher, secret_nonce, secret_salt, enabled, confirmed_at)
		 VALUES (1, $1, $2, $3, FALSE, NULL)
		 ON CONFLICT (id) DO UPDATE SET
		   secret_cipher = EXCLUDED.secret_cipher,
		   secret_nonce  = EXCLUDED.secret_nonce,
		   secret_salt   = EXCLUDED.secret_salt,
		   enabled       = FALSE,
		   confirmed_at  = NULL,
		   last_used_step = NULL`,
		cipher, nonce, salt,
	)
	if err != nil {
		return fmt.Errorf("totp: upsert enrollment: %w", err)
	}
	return nil
}

// LoadSecret returns the stored secret, ok=false when none is enrolled.
func (s *PostgresStore) LoadSecret(ctx context.Context) (EncryptedSecret, bool, error) {
	var sec EncryptedSecret
	err := s.db.QueryRow(ctx,
		`SELECT secret_cipher, secret_nonce, secret_salt FROM admin_totp WHERE id = 1`,
	).Scan(&sec.Cipher, &sec.Nonce, &sec.Salt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return EncryptedSecret{}, false, nil
		}
		return EncryptedSecret{}, false, fmt.Errorf("totp: load secret: %w", err)
	}
	return sec, true, nil
}

// RecordUsedStep atomically advances last_used_step. The conditional UPDATE
// makes a concurrent replay of the same step impossible: exactly one caller gets
// RowsAffected()==1; a replayed or older step gets 0.
func (s *PostgresStore) RecordUsedStep(ctx context.Context, step int64) (bool, error) {
	tag, err := s.db.Exec(ctx,
		`UPDATE admin_totp SET last_used_step = $1
		 WHERE id = 1 AND (last_used_step IS NULL OR last_used_step < $1)`,
		step,
	)
	if err != nil {
		return false, fmt.Errorf("totp: record used step: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// Enable flips the row to enabled=true; returns false when there was no
// provisional enrollment to enable.
func (s *PostgresStore) Enable(ctx context.Context) (bool, error) {
	tag, err := s.db.Exec(ctx,
		`UPDATE admin_totp SET enabled = TRUE, confirmed_at = NOW() WHERE id = 1`,
	)
	if err != nil {
		return false, fmt.Errorf("totp: enable: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

// Disable deletes the config and all recovery codes in one transaction so a
// failure cannot leave recovery codes behind with no secret (or vice versa).
func (s *PostgresStore) Disable(ctx context.Context) error {
	tx, err := s.db.Begin(ctx)
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
	return nil
}

// DisableIfAuthorized runs the load -> authorize -> delete sequence in a single
// transaction. The recoveryUnused probe handed to authorize queries within the
// same transaction, so the authorization decision and the deletes are atomic.
func (s *PostgresStore) DisableIfAuthorized(ctx context.Context, authorize DisableAuthorizer) (bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("totp: disable (begin): %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var sec EncryptedSecret
	var lastUsedStep *int64
	err = tx.QueryRow(ctx,
		`SELECT secret_cipher, secret_nonce, secret_salt, last_used_step FROM admin_totp WHERE id = 1`,
	).Scan(&sec.Cipher, &sec.Nonce, &sec.Salt, &lastUsedStep)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("totp: disable (load secret): %w", err)
	}

	recoveryUnused := func(codeHash string) (bool, error) {
		var n int
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM admin_totp_recovery WHERE code_hash = $1 AND used_at IS NULL`,
			codeHash,
		).Scan(&n); err != nil {
			return false, fmt.Errorf("totp: disable (check recovery): %w", err)
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

	if _, err := tx.Exec(ctx, `DELETE FROM admin_totp WHERE id = 1`); err != nil {
		return false, fmt.Errorf("totp: disable (delete config): %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM admin_totp_recovery`); err != nil {
		return false, fmt.Errorf("totp: disable (delete recovery): %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("totp: disable (commit): %w", err)
	}
	return true, nil
}

// IsEnabled reports whether TOTP is active; (false, nil) when not enrolled.
func (s *PostgresStore) IsEnabled(ctx context.Context) (bool, error) {
	var enabled bool
	err := s.db.QueryRow(ctx,
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

// EnabledAt returns confirmed_at when enrolled AND enabled; ok=false otherwise.
func (s *PostgresStore) EnabledAt(ctx context.Context) (time.Time, bool, error) {
	var confirmedAt *time.Time
	err := s.db.QueryRow(ctx,
		`SELECT confirmed_at FROM admin_totp WHERE id = 1 AND enabled`,
	).Scan(&confirmedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, fmt.Errorf("totp: enabled_at: %w", err)
	}
	if confirmedAt == nil {
		return time.Time{}, false, nil
	}
	return *confirmedAt, true, nil
}

// RecoveryCounts returns the number of unused and total recovery codes.
func (s *PostgresStore) RecoveryCounts(ctx context.Context) (int, int, error) {
	var remaining, total int
	if err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FILTER (WHERE used_at IS NULL), COUNT(*) FROM admin_totp_recovery`,
	).Scan(&remaining, &total); err != nil {
		return 0, 0, fmt.Errorf("totp: recovery counts: %w", err)
	}
	return remaining, total, nil
}

// LastUsedStep returns the last accepted TOTP step (may be nil); ok=false when
// no enrollment row exists.
func (s *PostgresStore) LastUsedStep(ctx context.Context) (*int64, bool, error) {
	var step *int64
	err := s.db.QueryRow(ctx,
		`SELECT last_used_step FROM admin_totp WHERE id = 1`,
	).Scan(&step)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("totp: last used: %w", err)
	}
	return step, true, nil
}

// ReplaceRecoveryCodes atomically deletes all recovery codes and inserts the
// given hashes as the new set.
func (s *PostgresStore) ReplaceRecoveryCodes(ctx context.Context, codeHashes []string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("totp: recovery codes (begin): %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM admin_totp_recovery`); err != nil {
		return fmt.Errorf("totp: recovery codes (delete): %w", err)
	}

	if len(codeHashes) > 0 {
		// Build a single batched INSERT: VALUES ($1), ($2), ..., ($N).
		placeholders := make([]string, len(codeHashes))
		args := make([]any, len(codeHashes))
		for i, h := range codeHashes {
			placeholders[i] = fmt.Sprintf("($%d)", i+1)
			args[i] = h
		}
		query := "INSERT INTO admin_totp_recovery (code_hash) VALUES " + strings.Join(placeholders, ", ")
		if _, err := tx.Exec(ctx, query, args...); err != nil {
			return fmt.Errorf("totp: recovery codes (insert): %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("totp: recovery codes (commit): %w", err)
	}
	return nil
}

// ConsumeRecoveryCode atomically marks a single unused code (by hash) as used.
// The UPDATE ... WHERE used_at IS NULL makes double-use impossible.
func (s *PostgresStore) ConsumeRecoveryCode(ctx context.Context, codeHash string) (bool, error) {
	tag, err := s.db.Exec(ctx,
		`UPDATE admin_totp_recovery SET used_at = NOW() WHERE code_hash = $1 AND used_at IS NULL`,
		codeHash,
	)
	if err != nil {
		return false, fmt.Errorf("totp: consume recovery code: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}
