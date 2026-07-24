package user

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters follow the OWASP password-storage cheat-sheet baseline
// (19 MiB, 2 iterations, 1 lane). The encoded hash is self-describing, so these
// can be raised later; existing hashes keep verifying with their stored params.
const (
	argonMemoryKiB = 19 * 1024
	argonTime      = 2
	argonThreads   = 1
	argonSaltLen   = 16
	argonKeyLen    = 32
)

// ErrHashFormat reports a stored hash that is not a well-formed argon2id
// encoded string; it means DB corruption or a foreign write, never bad input.
var ErrHashFormat = errors.New("malformed password hash")

// argon2MaxConcurrent bounds how many Argon2id derivations run at once. Argon2
// is deliberately memory-hard, and password verification runs on the
// unauthenticated login path where the per-IP and per-account throttles admit a
// burst before the first failure is recorded. Without this cap a burst of
// logins multiplies the per-hash memory cost (up to the 128 MiB ceiling in
// VerifyPassword) into gigabytes of simultaneous allocation and can OOM the
// process. The semaphore caps aggregate in-flight Argon2 memory at
// argon2MaxConcurrent times the per-hash cost instead.
const argon2MaxConcurrent = 4

var argon2Sem = make(chan struct{}, argon2MaxConcurrent)

// argon2IDKey runs argon2.IDKey under the concurrency semaphore. The acquire
// honours ctx: a caller whose request is canceled or times out while queued
// abandons the slot instead of running Argon2 work for a client that has gone
// away, so a saturated queue can't accumulate doomed work and starve live
// logins. Each derivation is itself CPU-bound and bounded-time.
func argon2IDKey(ctx context.Context, password, salt []byte, time, memory uint32, threads uint8, keyLen uint32) ([]byte, error) {
	select {
	case argon2Sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-argon2Sem }()
	return argon2.IDKey(password, salt, time, memory, threads, keyLen), nil
}

// HashPassword derives an argon2id hash and returns it in PHC string format.
// It returns ctx.Err() if ctx is canceled while queued for the Argon2 slot.
func HashPassword(ctx context.Context, password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := argon2IDKey(ctx, []byte(password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword checks password against an encoded argon2id hash in constant
// time. A malformed hash returns ErrHashFormat; a context canceled while queued
// for the Argon2 slot returns ctx.Err(); a wrong password returns (false, nil).
func VerifyPassword(ctx context.Context, password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, ErrHashFormat
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, ErrHashFormat
	}
	var mem, iters uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &mem, &iters, &threads); err != nil {
		return false, ErrHashFormat
	}
	if mem == 0 || iters == 0 || threads == 0 {
		return false, ErrHashFormat
	}
	// Reject parameters far above the configured cost — the per-hash companion to
	// the argon2MaxConcurrent semaphore. Bounding a single derivation to 128 MiB
	// keeps aggregate in-flight Argon2 memory at argon2MaxConcurrent * 128 MiB even
	// against a hostile stored hash (only settable via DB corruption or a foreign
	// write). 128 MiB sits well above the current cost (19 MiB) and any realistic
	// OWASP hardening, so raising the work factors later stays within bounds.
	if mem > 128*1024 || iters > 30 || threads > 64 {
		return false, ErrHashFormat
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrHashFormat
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(want) == 0 || len(want) > 512 {
		return false, ErrHashFormat
	}
	got, err := argon2IDKey(ctx, []byte(password), salt, iters, mem, threads, uint32(len(want))) //nolint:gosec // length bounded to 512 above
	if err != nil {
		return false, err
	}
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
