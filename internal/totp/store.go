package totp

import (
	"context"
	"time"
)

// This file defines the persistence contract for TOTP so the second-factor
// crypto in this package is decoupled from any single database.
//
// PostgresStore (postgres_store.go) is the implementation used by the main
// server. The HA "Front Desk" control plane supplies its own SQLite Store, so
// it reuses all of the RFC 6238 verification, single-use enforcement, and
// recovery-code logic in Repository without a Postgres dependency.
//
// The Store owns SQL and transactions; Repository owns crypto and policy. The
// atomic operations (single-use step acceptance, atomic disable, recovery-code
// consumption) are expressed as whole methods so each backend performs them in
// one transaction and the security guarantees survive the abstraction.

// EncryptedSecret is the AES-GCM encrypted TOTP secret as stored at rest.
type EncryptedSecret struct {
	Cipher []byte
	Nonce  []byte
	Salt   []byte
}

// DisableAuthorizer decides, inside the disable transaction, whether a presented
// code authorizes disabling TOTP. It receives the loaded secret and the last
// accepted step (for replay rejection) and a recoveryUnused probe that reports
// whether a recovery-code hash exists and is unused, queried in the SAME
// transaction. Returning (true, nil) commits the disable; (false, nil) leaves
// everything unchanged.
type DisableAuthorizer func(secret EncryptedSecret, lastUsedStep *int64, recoveryUnused func(codeHash string) (bool, error)) (bool, error)

// Store is the TOTP persistence contract. All methods operate on the single
// admin_totp row (id = 1) and the admin_totp_recovery table.
type Store interface {
	// UpsertEnrollment stores or replaces the provisional secret, resetting
	// enabled to false, confirmed_at to NULL, and last_used_step to NULL.
	UpsertEnrollment(ctx context.Context, cipher, nonce, salt []byte) error

	// LoadSecret returns the stored secret, with ok=false when none is enrolled.
	LoadSecret(ctx context.Context) (secret EncryptedSecret, ok bool, err error)

	// RecordUsedStep atomically advances last_used_step to step only when it is
	// strictly greater (or currently NULL). Returns true iff exactly one row was
	// updated, which is the single-use (RFC 6238 5.2) acceptance signal.
	RecordUsedStep(ctx context.Context, step int64) (bool, error)

	// Enable flips the row to enabled=true and stamps confirmed_at. Returns false
	// when there was no provisional enrollment to enable.
	Enable(ctx context.Context) (bool, error)

	// Disable deletes the config and all recovery codes in one transaction.
	Disable(ctx context.Context) error

	// DisableIfAuthorized loads the secret and last_used_step, invokes authorize
	// (which decides via TOTP crypto or the recoveryUnused probe), and only when
	// it returns true deletes the config and recovery codes, all in one
	// transaction. Returns (false, nil) when no enrollment exists or authorize
	// declines.
	DisableIfAuthorized(ctx context.Context, authorize DisableAuthorizer) (bool, error)

	// IsEnabled reports whether TOTP is active; (false, nil) when not enrolled.
	IsEnabled(ctx context.Context) (bool, error)

	// EnabledAt returns confirmed_at when enrolled AND enabled; ok=false otherwise.
	EnabledAt(ctx context.Context) (confirmedAt time.Time, ok bool, err error)

	// RecoveryCounts returns the number of unused and total recovery codes.
	RecoveryCounts(ctx context.Context) (remaining, total int, err error)

	// LastUsedStep returns the last accepted TOTP step (may be nil when never
	// used); ok=false when no enrollment row exists.
	LastUsedStep(ctx context.Context) (step *int64, ok bool, err error)

	// ReplaceRecoveryCodes atomically deletes all recovery codes and inserts the
	// given hashes as the new set.
	ReplaceRecoveryCodes(ctx context.Context, codeHashes []string) error

	// ConsumeRecoveryCode atomically marks a single unused code (by hash) as
	// used. Returns true iff exactly one row matched, making double-use
	// impossible.
	ConsumeRecoveryCode(ctx context.Context, codeHash string) (bool, error)
}
