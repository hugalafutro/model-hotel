package api

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"strings"
)

// backupSignatureExt is appended to a dump's own name to form its signature
// sidecar. The signature lives beside the dump rather than inside it so the
// dump stays a valid pg_restore input, and the suffix deliberately does not end
// in ".dump" so sidecars are never listed, downloaded, or offered for restore.
const backupSignatureExt = ".sig"

// backupSignatureInfo separates the backup-signing key from every other key
// derived from MASTER_KEY. A leaked signing key must not be usable to decrypt
// provider credentials, so it is an HKDF expansion of the master key under this
// label rather than the master key itself.
const backupSignatureInfo = "model-hotel/backup-hmac/v1"

// backupSigStatus is the outcome of checking a dump against its sidecar.
type backupSigStatus int

const (
	// backupSigValid means a sidecar exists and matches the dump's bytes.
	backupSigValid backupSigStatus = iota
	// backupSigInvalid means a sidecar exists but does not match: the dump was
	// modified after signing, signed under a different MASTER_KEY, or the
	// sidecar itself is unreadable. Always treated as fatal by callers.
	backupSigInvalid
	// backupSigMissing means no sidecar exists. Backups predating signing and
	// dumps from other instances land here and stay usable; the state is
	// surfaced to the operator rather than blocking, because refusing them
	// would make legitimate old backups unrestorable.
	backupSigMissing
	// backupSigUnavailable means no MASTER_KEY is configured, so nothing can be
	// signed or verified at all.
	backupSigUnavailable
)

// errNoMasterKey is returned when signing is attempted without a master key.
var errNoMasterKey = errors.New("no master key configured; cannot sign backups")

// backupSignatureKey derives the HMAC key used for backup signatures from the
// master key. Deterministic across restarts, or previously written signatures
// would stop verifying.
func backupSignatureKey(masterKey string) ([]byte, error) {
	if masterKey == "" {
		return nil, errNoMasterKey
	}
	// No salt: the derivation must reproduce exactly from MASTER_KEY alone, and
	// the label already separates this key from other uses of the master key.
	return hkdf.Key(sha256.New, []byte(masterKey), nil, backupSignatureInfo, 32)
}

// hmacBackupFile streams the file through HMAC-SHA256 so a multi-gigabyte dump
// is never held in memory.
func hmacBackupFile(path string, key []byte) ([]byte, error) {
	//nolint:gosec // G304: path is built by the caller from the validated backup dir
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	mac := hmac.New(sha256.New, key)
	if _, err := io.Copy(mac, f); err != nil {
		return nil, err
	}
	return mac.Sum(nil), nil
}

// signBackupFile writes the signature sidecar for a finished dump.
func signBackupFile(path, masterKey string) error {
	key, err := backupSignatureKey(masterKey)
	if err != nil {
		return err
	}
	sum, err := hmacBackupFile(path, key)
	if err != nil {
		return err
	}
	return os.WriteFile(path+backupSignatureExt, []byte(hex.EncodeToString(sum)), 0o600)
}

// readSignatureSidecar reads a dump's sidecar. found=false means there is no
// sidecar at all, which is a legitimate state (unsigned backup) rather than a
// failure; err is reserved for a sidecar that exists but cannot be read.
func readSignatureSidecar(path string) (contents []byte, found bool, err error) {
	data, err := os.ReadFile(path + backupSignatureExt) //nolint:gosec // G304: sidecar of a caller-validated path
	switch {
	case os.IsNotExist(err):
		return nil, false, nil
	case err != nil:
		return nil, false, err
	default:
		return data, true, nil
	}
}

// decodeSignature parses a stored or supplied signature. ok=false means the
// value is not a signature at all, which counts as a failed check rather than
// an I/O problem.
func decodeSignature(s string) (sig []byte, ok bool) {
	raw, err := hex.DecodeString(strings.TrimSpace(s))
	if err != nil {
		return nil, false
	}
	return raw, true
}

// checkSignature compares a dump against an expected signature. The error
// return is only for failing to read the dump: a mismatch is a status, so
// callers can tell "cannot check" apart from "checked and wrong".
func checkSignature(path string, want []byte, masterKey string) (backupSigStatus, error) {
	key, err := backupSignatureKey(masterKey)
	if err != nil {
		return backupSigUnavailable, err
	}
	got, err := hmacBackupFile(path, key)
	if err != nil {
		return backupSigInvalid, err
	}
	if !hmac.Equal(got, want) {
		return backupSigInvalid, nil
	}
	return backupSigValid, nil
}

// verifyBackupFile checks a dump in the backup directory against its sidecar.
func verifyBackupFile(path, masterKey string) (backupSigStatus, error) {
	if masterKey == "" {
		return backupSigUnavailable, nil
	}

	stored, found, err := readSignatureSidecar(path)
	if err != nil {
		// A sidecar that exists but cannot be read leaves the dump unprovable.
		return backupSigInvalid, err
	}
	if !found {
		return backupSigMissing, nil
	}

	want, ok := decodeSignature(string(stored))
	if !ok {
		return backupSigInvalid, nil
	}
	return checkSignature(path, want, masterKey)
}

// verifyBackupBytesAgainstSignature checks an uploaded dump against a signature
// supplied alongside it, for the restore path where the dump did not come from
// this server's backup directory.
func verifyBackupBytesAgainstSignature(path, signature, masterKey string) (backupSigStatus, error) {
	if strings.TrimSpace(signature) == "" {
		return backupSigMissing, nil
	}
	if masterKey == "" {
		return backupSigUnavailable, nil
	}
	want, ok := decodeSignature(signature)
	if !ok {
		return backupSigInvalid, nil
	}
	return checkSignature(path, want, masterKey)
}

// removeBackupWithSignature deletes a dump and its sidecar. The dump's removal
// error is what callers act on; a missing sidecar is normal (unsigned backups)
// and never an error, but a sidecar left behind would outlive its dump forever.
func removeBackupWithSignature(path string) error {
	err := os.Remove(path)
	if sigErr := os.Remove(path + backupSignatureExt); sigErr != nil && !os.IsNotExist(sigErr) {
		return sigErr
	}
	return err
}
