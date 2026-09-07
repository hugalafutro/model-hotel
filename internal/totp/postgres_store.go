package totp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pgStore is the PostgreSQL implementation of Store. One instance serves either
// the single admin enrollment (admin_totp, the row id = 1) or one user's
// enrollment (user_totp, scoped by user_id), so the atomic single-use,
// atomic-disable and recovery-code guarantees are written once and both callers
// inherit them.
//
// Table names, the row predicate and the log/error label are constants supplied
// by the two constructors below, never user input; only keyArgs carries a value,
// and it travels as a bound parameter.
type pgStore struct {
	db *pgxpool.Pool
	// table and recoveryTable are the config and recovery-code tables.
	table, recoveryTable string
	// keyCol is the row's key column ("id" or "user_id") and keyVal the SQL
	// literal or placeholder it is matched against ("1" or "$1"); together they
	// form the row predicate. keyArgs holds the values those placeholders
	// consume and is prepended to every argument list. keyed reports whether the
	// recovery table is scoped by the same key.
	keyCol, keyVal string
	keyArgs        []any
	keyed          bool
	// label distinguishes the two stores in wrapped errors ("" or "user ").
	label string
}

// Compile-time assertion that pgStore satisfies Store.
var _ Store = (*pgStore)(nil)

// NewPostgresStore creates a Postgres-backed store for the single admin TOTP
// enrollment.
func NewPostgresStore(pool *pgxpool.Pool) Store {
	return &pgStore{
		db:            pool,
		table:         "admin_totp",
		recoveryTable: "admin_totp_recovery",
		keyCol:        "id",
		keyVal:        "1",
	}
}

// NewUserPostgresStore creates a Postgres-backed store bound to one user's TOTP
// enrollment (migration 052). The stateless Repository (crypto + policy) is
// reused verbatim via NewRepositoryWithStore.
func NewUserPostgresStore(pool *pgxpool.Pool, userID uuid.UUID) Store {
	return &pgStore{
		db:            pool,
		table:         "user_totp",
		recoveryTable: "user_totp_recovery",
		keyCol:        "user_id",
		keyVal:        "$1",
		keyArgs:       []any{userID},
		keyed:         true,
		label:         "user ",
	}
}

// args prepends the bound key values to a statement's own arguments.
func (s *pgStore) args(extra ...any) []any {
	return append(append(make([]any, 0, len(s.keyArgs)+len(extra)), s.keyArgs...), extra...)
}

// ph renders the placeholder for the i-th (1-based) non-key argument.
func (s *pgStore) ph(i int) string {
	return "$" + strconv.Itoa(len(s.keyArgs)+i)
}

// keyCond renders the row predicate the key column and its bound value form.
func (s *pgStore) keyCond() string {
	return s.keyCol + " = " + s.keyVal
}

// where builds the config-table predicate, ANDing any extra conditions.
func (s *pgStore) where(extra ...string) string {
	return " WHERE " + strings.Join(append([]string{s.keyCond()}, extra...), " AND ")
}

// recoveryWhere builds the recovery-table predicate. The admin recovery table
// has no key column, so it is filtered only by the extra conditions.
func (s *pgStore) recoveryWhere(extra ...string) string {
	conds := extra
	if s.keyed {
		conds = append([]string{s.keyCond()}, extra...)
	}
	if len(conds) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(conds, " AND ")
}

func (s *pgStore) errf(what string, err error) error {
	return fmt.Errorf("totp: %s%s: %w", s.label, what, err)
}

// UpsertEnrollment stores or replaces the provisional secret. The ON CONFLICT
// clause resets enabled/confirmed_at/last_used_step so a half-finished or live
// enrollment cleanly restarts and requires re-verification.
func (s *pgStore) UpsertEnrollment(ctx context.Context, cipher, nonce, salt []byte) error {
	_, err := s.db.Exec(ctx,
		`INSERT INTO `+s.table+` (`+s.keyCol+`, secret_cipher, secret_nonce, secret_salt, enabled, confirmed_at)
		 VALUES (`+s.keyVal+`, `+s.ph(1)+`, `+s.ph(2)+`, `+s.ph(3)+`, FALSE, NULL)
		 ON CONFLICT (`+s.keyCol+`) DO UPDATE SET
		   secret_cipher = EXCLUDED.secret_cipher,
		   secret_nonce  = EXCLUDED.secret_nonce,
		   secret_salt   = EXCLUDED.secret_salt,
		   enabled       = FALSE,
		   confirmed_at  = NULL,
		   last_used_step = NULL`,
		s.args(cipher, nonce, salt)...,
	)
	if err != nil {
		return s.errf("upsert enrollment", err)
	}
	return nil
}

// LoadSecret returns the stored secret, ok=false when none is enrolled.
func (s *pgStore) LoadSecret(ctx context.Context) (EncryptedSecret, bool, error) {
	var sec EncryptedSecret
	err := s.db.QueryRow(ctx,
		`SELECT secret_cipher, secret_nonce, secret_salt FROM `+s.table+s.where(),
		s.args()...,
	).Scan(&sec.Cipher, &sec.Nonce, &sec.Salt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return EncryptedSecret{}, false, nil
		}
		return EncryptedSecret{}, false, s.errf("load secret", err)
	}
	return sec, true, nil
}

// RecordUsedStep atomically advances last_used_step. The conditional UPDATE
// makes a concurrent replay of the same step impossible: exactly one caller gets
// RowsAffected()==1; a replayed or older step gets 0.
func (s *pgStore) RecordUsedStep(ctx context.Context, step int64) (bool, error) {
	tag, err := s.db.Exec(ctx,
		`UPDATE `+s.table+` SET last_used_step = `+s.ph(1)+
			s.where(`(last_used_step IS NULL OR last_used_step < `+s.ph(1)+`)`),
		s.args(step)...,
	)
	if err != nil {
		return false, s.errf("record used step", err)
	}
	return tag.RowsAffected() == 1, nil
}

// Enable flips the row to enabled=true; returns false when there was no
// provisional enrollment to enable.
func (s *pgStore) Enable(ctx context.Context) (bool, error) {
	tag, err := s.db.Exec(ctx,
		`UPDATE `+s.table+` SET enabled = TRUE, confirmed_at = NOW()`+s.where(),
		s.args()...,
	)
	if err != nil {
		return false, s.errf("enable", err)
	}
	return tag.RowsAffected() > 0, nil
}

// deleteAll removes the config row and every recovery code inside tx.
func (s *pgStore) deleteAll(ctx context.Context, tx pgx.Tx, what string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM `+s.table+s.where(), s.args()...); err != nil {
		return s.errf(what+" (delete config)", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM `+s.recoveryTable+s.recoveryWhere(), s.args()...); err != nil {
		return s.errf(what+" (delete recovery)", err)
	}
	return nil
}

// Disable deletes the config and all recovery codes in one transaction so a
// failure cannot leave recovery codes behind with no secret (or vice versa).
func (s *pgStore) Disable(ctx context.Context) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return s.errf("disable (begin)", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := s.deleteAll(ctx, tx, "disable"); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return s.errf("disable (commit)", err)
	}
	return nil
}

// DisableIfAuthorized runs the load -> authorize -> delete sequence in a single
// transaction. The recoveryUnused probe handed to authorize queries within the
// same transaction, so the authorization decision and the deletes are atomic.
func (s *pgStore) DisableIfAuthorized(ctx context.Context, authorize DisableAuthorizer) (bool, error) {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return false, s.errf("disable (begin)", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var sec EncryptedSecret
	var lastUsedStep *int64
	err = tx.QueryRow(ctx,
		`SELECT secret_cipher, secret_nonce, secret_salt, last_used_step FROM `+s.table+s.where(),
		s.args()...,
	).Scan(&sec.Cipher, &sec.Nonce, &sec.Salt, &lastUsedStep)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, s.errf("disable (load secret)", err)
	}

	recoveryUnused := func(codeHash string) (bool, error) {
		var n int
		if err := tx.QueryRow(ctx,
			`SELECT COUNT(*) FROM `+s.recoveryTable+s.recoveryWhere(`code_hash = `+s.ph(1), `used_at IS NULL`),
			s.args(codeHash)...,
		).Scan(&n); err != nil {
			return false, s.errf("disable (check recovery)", err)
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

	if err := s.deleteAll(ctx, tx, "disable"); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, s.errf("disable (commit)", err)
	}
	return true, nil
}

// IsEnabled reports whether TOTP is active; (false, nil) when not enrolled.
func (s *pgStore) IsEnabled(ctx context.Context) (bool, error) {
	var enabled bool
	err := s.db.QueryRow(ctx,
		`SELECT enabled FROM `+s.table+s.where(), s.args()...,
	).Scan(&enabled)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, s.errf("is_enabled", err)
	}
	return enabled, nil
}

// EnabledAt returns confirmed_at when enrolled AND enabled; ok=false otherwise.
func (s *pgStore) EnabledAt(ctx context.Context) (time.Time, bool, error) {
	var confirmedAt *time.Time
	err := s.db.QueryRow(ctx,
		`SELECT confirmed_at FROM `+s.table+s.where(`enabled`), s.args()...,
	).Scan(&confirmedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, s.errf("enabled_at", err)
	}
	if confirmedAt == nil {
		return time.Time{}, false, nil
	}
	return *confirmedAt, true, nil
}

// RecoveryCounts returns the number of unused and total recovery codes.
func (s *pgStore) RecoveryCounts(ctx context.Context) (int, int, error) {
	var remaining, total int
	if err := s.db.QueryRow(ctx,
		`SELECT COUNT(*) FILTER (WHERE used_at IS NULL), COUNT(*) FROM `+s.recoveryTable+s.recoveryWhere(),
		s.args()...,
	).Scan(&remaining, &total); err != nil {
		return 0, 0, s.errf("recovery counts", err)
	}
	return remaining, total, nil
}

// LastUsedStep returns the last accepted TOTP step (may be nil); ok=false when
// no enrollment row exists.
func (s *pgStore) LastUsedStep(ctx context.Context) (*int64, bool, error) {
	var step *int64
	err := s.db.QueryRow(ctx,
		`SELECT last_used_step FROM `+s.table+s.where(), s.args()...,
	).Scan(&step)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, false, nil
		}
		return nil, false, s.errf("last used", err)
	}
	return step, true, nil
}

// ReplaceRecoveryCodes atomically deletes all recovery codes and inserts the
// given hashes as the new set.
func (s *pgStore) ReplaceRecoveryCodes(ctx context.Context, codeHashes []string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return s.errf("recovery codes (begin)", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `DELETE FROM `+s.recoveryTable+s.recoveryWhere(), s.args()...); err != nil {
		return s.errf("recovery codes (delete)", err)
	}

	if len(codeHashes) > 0 {
		// One row per hash from a single text[] argument, carrying the bound
		// key value in the projection when the recovery table is keyed.
		cols, keyVal := "code_hash", ""
		if s.keyed {
			cols, keyVal = s.keyCol+", code_hash", s.keyVal+", "
		}
		query := "INSERT INTO " + s.recoveryTable + " (" + cols + ") SELECT " + keyVal +
			"code FROM unnest(" + s.ph(1) + "::text[]) AS code"
		if _, err := tx.Exec(ctx, query, s.args(codeHashes)...); err != nil {
			return s.errf("recovery codes (insert)", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return s.errf("recovery codes (commit)", err)
	}
	return nil
}

// ConsumeRecoveryCode atomically marks a single unused code (by hash) as used.
// The UPDATE ... WHERE used_at IS NULL makes double-use impossible.
func (s *pgStore) ConsumeRecoveryCode(ctx context.Context, codeHash string) (bool, error) {
	tag, err := s.db.Exec(ctx,
		`UPDATE `+s.recoveryTable+` SET used_at = NOW()`+
			s.recoveryWhere(`code_hash = `+s.ph(1), `used_at IS NULL`),
		s.args(codeHash)...,
	)
	if err != nil {
		return false, s.errf("consume recovery code", err)
	}
	return tag.RowsAffected() == 1, nil
}
