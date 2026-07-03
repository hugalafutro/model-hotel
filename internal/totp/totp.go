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
//
// Persistence lives behind the Store interface (see store.go); this file holds
// the crypto and policy. The main server uses PostgresStore; the HA Front Desk
// control plane supplies a SQLite Store and reuses everything here unchanged.
package totp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pquerna/otp/totp"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// totpPeriodSeconds is the TOTP step size (RFC 6238 default).
const totpPeriodSeconds = 30

// Repository manages the single admin TOTP config (encrypted secret) and
// single-use recovery codes. The plaintext secret never persists; it is
// AES-GCM encrypted with the process MASTER_KEY like provider API keys.
//
// It owns the crypto and policy and delegates all persistence to a Store, so
// the same logic runs over Postgres (main server) or SQLite (Front Desk).
type Repository struct {
	store     Store
	masterKey string
}

// NewRepository creates a TOTP repository backed by the given connection pool
// (PostgresStore). The masterKey encrypts/decrypts the TOTP secret at rest.
func NewRepository(pool *pgxpool.Pool, masterKey string) *Repository {
	return &Repository{store: NewPostgresStore(pool), masterKey: masterKey}
}

// NewRepositoryWithStore creates a TOTP repository over an arbitrary Store
// implementation (e.g. a SQLite store in the HA Front Desk control plane).
func NewRepositoryWithStore(store Store, masterKey string) *Repository {
	return &Repository{store: store, masterKey: masterKey}
}

// Enroll generates a new TOTP secret, encrypts it with MASTER_KEY, and stores a
// provisional row (enabled=false, confirmed_at=NULL). It returns the otpauth
// URI (for QR rendering) and the base32 secret (for manual entry). The admin
// flow labels the QR "admin"; per-user enrollments use EnrollAs directly.
//
// Re-enrolling overwrites any prior provisional or enabled secret, so a
// half-finished enrollment cleanly restarts and a live enrollment requires
// re-verification (the Store's upsert resets enabled/confirmed_at/last_used_step).
func (r *Repository) Enroll(ctx context.Context) (uri, secret string, err error) {
	return r.EnrollAs(ctx, "admin")
}

// EnrollAs is Enroll with a caller-chosen otpauth account label (the username
// shown in the authenticator app for per-user enrollments).
func (r *Repository) EnrollAs(ctx context.Context, accountName string) (uri, secret string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Model Hotel",
		AccountName: accountName,
	})
	if err != nil {
		return "", "", fmt.Errorf("totp: generate secret: %w", err)
	}
	secret = key.Secret()

	kp, err := auth.Encrypt(secret, r.masterKey)
	if err != nil {
		return "", "", fmt.Errorf("totp: encrypt secret: %w", err)
	}

	if err := r.store.UpsertEnrollment(ctx, kp.Ciphertext, kp.Nonce, kp.Salt); err != nil {
		return "", "", err
	}

	debuglog.Info("totp: enrollment started")
	return key.URL(), secret, nil
}

// Verify checks a 6-digit code against the stored secret using the standard
// TOTP defaults (period 30, skew 1 step, 6 digits, SHA-1) with constant-time
// comparison, and enforces single use (RFC 6238 §5.2): a code is accepted only
// once per 30s step. Returns (false, nil) for an invalid or replayed code, or
// when no enrollment exists, and (false, err) on decrypt/DB failure.
func (r *Repository) Verify(ctx context.Context, code string) (bool, error) {
	sec, ok, err := r.store.LoadSecret(ctx)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}

	secret, err := auth.Decrypt(sec.Cipher, sec.Nonce, sec.Salt, r.masterKey)
	if err != nil {
		return false, fmt.Errorf("totp: decrypt secret: %w", err)
	}

	// Find which 30s step the code belongs to within the skew=1 window
	// (previous / current / next), using constant-time comparison. This mirrors
	// totp.Validate's acceptance set; a no-match means an invalid code.
	matched := matchStep(secret, code)
	if matched < 0 {
		return false, nil
	}

	// Single-use (RFC 6238 §5.2): the Store's atomic conditional update accepts
	// the step only if it is newer than the last accepted one, so a concurrent
	// replay of the same step is impossible.
	return r.store.RecordUsedStep(ctx, matched)
}

// Enable flips the single admin_totp row to enabled=true and stamps confirmed_at.
// Returns an error if no provisional enrollment exists.
func (r *Repository) Enable(ctx context.Context) error {
	enabled, err := r.store.Enable(ctx)
	if err != nil {
		return err
	}
	if !enabled {
		return fmt.Errorf("totp: no provisional enrollment to enable")
	}
	debuglog.Info("totp: enabled")
	return nil
}

// Disable deletes the TOTP config and all recovery codes, returning the login
// to single-factor (raw admin token) behavior. The Store performs both deletes
// in one transaction so a failure cannot leave recovery codes behind with no
// secret (or vice versa).
func (r *Repository) Disable(ctx context.Context) error {
	if err := r.store.Disable(ctx); err != nil {
		return err
	}
	debuglog.Info("totp: disabled")
	return nil
}

// DisableWithCode authorizes with a current TOTP code or an unused recovery
// code and disables TOTP atomically: either the whole operation commits (config
// + recovery codes deleted) or nothing changes. Because disable wipes the entire
// config, the code only needs to be CHECKED, not consumed -- this avoids
// spending a recovery code or burning a TOTP step when the delete itself fails.
// Returns (false, nil) for an invalid or used code (nothing changed) or when
// TOTP is not enrolled.
func (r *Repository) DisableWithCode(ctx context.Context, code string) (bool, error) {
	ok, err := r.store.DisableIfAuthorized(ctx, func(sec EncryptedSecret, lastUsedStep *int64, recoveryUnused func(string) (bool, error)) (bool, error) {
		// Authorize with a current TOTP code (within the skew window) or an unused
		// recovery code. The code is only checked, not consumed -- the whole config
		// is about to be deleted -- but a TOTP step already accepted (e.g. by a
		// prior login) is rejected so a used code cannot be replayed to disable.
		authorized := false
		if secret, derr := auth.Decrypt(sec.Cipher, sec.Nonce, sec.Salt, r.masterKey); derr == nil {
			if step := matchStep(secret, code); step >= 0 {
				if lastUsedStep == nil || step > *lastUsedStep {
					authorized = true
				}
			}
		}
		if !authorized {
			return recoveryUnused(sha256hex(normalizeRecoveryCode(code)))
		}
		return true, nil
	})
	if err != nil {
		return false, err
	}
	if ok {
		debuglog.Info("totp: disabled")
	}
	return ok, nil
}

// IsEnabled reports whether TOTP 2FA is currently active. Returns (false, nil)
// when no enrollment exists.
func (r *Repository) IsEnabled(ctx context.Context) (bool, error) {
	return r.store.IsEnabled(ctx)
}

// EnabledAt returns when TOTP was last confirmed (the confirmed_at stamp set by
// Enable). The bool is false when no enrollment exists or it is not yet
// confirmed, so callers can omit the timestamp from a disabled-state response.
func (r *Repository) EnabledAt(ctx context.Context) (time.Time, bool, error) {
	return r.store.EnabledAt(ctx)
}

// SecurityInfo summarizes the active TOTP enrollment for the settings panel.
type SecurityInfo struct {
	RecoveryRemaining int       // unused recovery codes
	RecoveryTotal     int       // recovery codes issued in the current set
	LastUsed          time.Time // last accepted TOTP step, derived to a time; zero if never
}

// Info returns recovery-code usage plus the last accepted TOTP step converted
// back to wall-clock time. LastUsed reflects TOTP-code acceptance (enroll or
// login) and not recovery-code use; it is the zero time when no code has been
// accepted yet. Read-only and cheap, but it touches the DB, so it lives behind
// the admin-gated /totp/info rather than the public, polled /totp/status.
func (r *Repository) Info(ctx context.Context) (SecurityInfo, error) {
	var info SecurityInfo

	remaining, total, err := r.store.RecoveryCounts(ctx)
	if err != nil {
		return SecurityInfo{}, err
	}
	info.RecoveryRemaining = remaining
	info.RecoveryTotal = total

	step, ok, err := r.store.LastUsedStep(ctx)
	if err != nil {
		return SecurityInfo{}, err
	}
	if ok && step != nil && *step > 0 {
		info.LastUsed = time.Unix(*step*int64(totpPeriodSeconds), 0).UTC()
	}
	return info, nil
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

	if err := r.store.ReplaceRecoveryCodes(ctx, hashes); err != nil {
		return nil, err
	}
	return codes, nil
}

// ConsumeRecoveryCode marks a recovery code as used if it is valid and unused.
// Returns ok=true only when exactly one row matched (valid hash, unused).
// The Store's atomic UPDATE ... WHERE used_at IS NULL makes double-use impossible.
func (r *Repository) ConsumeRecoveryCode(ctx context.Context, code string) (bool, error) {
	return r.store.ConsumeRecoveryCode(ctx, sha256hex(normalizeRecoveryCode(code)))
}

// matchStep returns the TOTP step (previous/current/next within skew=1) whose
// generated code matches the submitted code via constant-time comparison, or -1
// when none match. This is the shared acceptance-set check used by Verify and
// DisableWithCode.
func matchStep(secret, code string) int64 {
	nowStep := time.Now().Unix() / totpPeriodSeconds
	for _, step := range []int64{nowStep - 1, nowStep, nowStep + 1} {
		cand, gerr := totp.GenerateCode(secret, time.Unix(step*totpPeriodSeconds, 0))
		if gerr == nil && subtle.ConstantTimeCompare([]byte(cand), []byte(code)) == 1 {
			return step
		}
	}
	return -1
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
// It uppercases, keeps only RFC 4648 base32 characters (A-Z, 2-7) to match the
// generated codes, and regroups exactly 16 characters as XXXX-XXXX-XXXX-XXXX
// (the stored format). Non-16-length input is returned cleaned-but-ungrouped,
// which simply will not match any stored hash.
func normalizeRecoveryCode(code string) string {
	var b strings.Builder
	for _, c := range strings.ToUpper(code) {
		if (c >= 'A' && c <= 'Z') || (c >= '2' && c <= '7') {
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
