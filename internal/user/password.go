package user

import (
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

// HashPassword derives an argon2id hash and returns it in PHC string format.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemoryKiB, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonTime, argonThreads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword checks password against an encoded argon2id hash in constant
// time. A malformed hash returns ErrHashFormat; a wrong password returns
// (false, nil).
func VerifyPassword(password, encoded string) (bool, error) {
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
	// Reject parameters far above the configured cost. VerifyPassword runs on the
	// unauthenticated login path, so an oversized m (only settable via DB
	// corruption or a foreign write) would make every verify allocate that much
	// memory and OOM the process. The 128 MiB ceiling sits well above the current
	// cost (19 MiB) and any realistic OWASP hardening, yet is far below the
	// gigabyte-scale allocation that would exhaust memory; raising the work
	// factors later stays within bounds.
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
	got := argon2.IDKey([]byte(password), salt, iters, mem, threads, uint32(len(want))) //nolint:gosec // length bounded to 512 above
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
