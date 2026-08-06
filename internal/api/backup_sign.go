package api

import (
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
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

// hmacBackupReader streams contents through HMAC-SHA256, binding the backup's
// filename into the MAC ahead of the bytes. Signing contents alone would let a
// (dump, sidecar) pair be renamed together and still verify, so an attacker who
// can write to the backup directory could drop an older genuine backup in
// today's place: it would verify clean and restore stale state, reinstating
// revoked keys and deleted accounts without forging anything. The name is
// NUL-terminated, which is unambiguous because a POSIX filename cannot contain
// a NUL byte, so no (name, contents) pair can be confused for another.
//
// Streaming rather than buffering keeps a multi-gigabyte dump out of memory.
func hmacBackupReader(r io.Reader, name string, key []byte) ([]byte, error) {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(name))
	mac.Write([]byte{0})
	if _, err := io.Copy(mac, r); err != nil {
		return nil, err
	}
	return mac.Sum(nil), nil
}

// hmacBackupFileAs signs a file's contents under a caller-supplied name. The
// name is separate from the path because the restore path hashes a temp file
// under the name the upload declared, which is the name that was signed.
func hmacBackupFileAs(path, name string, key []byte) ([]byte, error) {
	//nolint:gosec // G304: path is built by the caller from the validated backup dir, or is a temp file this handler just wrote
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	return hmacBackupReader(f, name, key)
}

// hmacBackupFile signs a dump in place, binding its own base name.
func hmacBackupFile(path string, key []byte) ([]byte, error) {
	return hmacBackupFileAs(path, filepath.Base(path), key)
}

// hmacBackupHandle signs the bytes behind an already-open handle and rewinds it
// for the caller. Verifying by path while serving from a handle opened earlier
// is a race an attacker with write access to the backup directory wins by
// renaming the genuine dump over the tampered one after the open: the check
// would hash the genuine inode and the transfer would send the tampered one.
func hmacBackupHandle(f *os.File, name string, key []byte) ([]byte, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	sum, err := hmacBackupReader(f, name, key)
	if err != nil {
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	return sum, nil
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

// compareSignature compares a computed MAC against the expected one.
func compareSignature(got, want []byte) backupSigStatus {
	if !hmac.Equal(got, want) {
		return backupSigInvalid
	}
	return backupSigValid
}

// sidecarExpectation reads and decodes a dump's sidecar, reporting what the
// caller should do before any hashing happens.
func sidecarExpectation(path, masterKey string) (want []byte, status backupSigStatus, err error) {
	if masterKey == "" {
		return nil, backupSigUnavailable, nil
	}
	stored, found, err := readSignatureSidecar(path)
	if err != nil {
		// A sidecar that exists but cannot be read leaves the dump unprovable.
		return nil, backupSigInvalid, err
	}
	if !found {
		return nil, backupSigMissing, nil
	}
	decoded, ok := decodeSignature(string(stored))
	if !ok {
		return nil, backupSigInvalid, nil
	}
	return decoded, backupSigValid, nil
}

// verifyBackupFile checks a dump in the backup directory against its sidecar,
// reading the dump by path. Callers that already hold the file open should use
// verifyBackupHandle instead so the bytes checked are the bytes they will use.
func verifyBackupFile(path, masterKey string) (backupSigStatus, error) {
	want, status, err := sidecarExpectation(path, masterKey)
	if err != nil || status != backupSigValid {
		return status, err
	}
	key, err := backupSignatureKey(masterKey)
	if err != nil {
		return backupSigUnavailable, err
	}
	got, err := hmacBackupFile(path, key)
	if err != nil {
		return backupSigInvalid, err
	}
	return compareSignature(got, want), nil
}

// verifyBackupHandle checks the bytes behind an open handle against the sidecar
// for name, and leaves the handle rewound. This is what the download path uses:
// checking the same inode it is about to serve closes the window where the file
// at the path is swapped between the open and the check.
func verifyBackupHandle(f *os.File, path, masterKey string) (backupSigStatus, error) {
	want, status, err := sidecarExpectation(path, masterKey)
	if err != nil || status != backupSigValid {
		return status, err
	}
	key, err := backupSignatureKey(masterKey)
	if err != nil {
		return backupSigUnavailable, err
	}
	got, err := hmacBackupHandle(f, filepath.Base(path), key)
	if err != nil {
		return backupSigInvalid, err
	}
	return compareSignature(got, want), nil
}

// verifyUploadedDumpSignature checks an uploaded dump against a signature
// supplied alongside it, for the restore path where the dump did not come from
// this server's backup directory. The name is the one the upload declared,
// because the signature binds it: a dump renamed on its way through a browser
// will not verify, and the caller can retry without a signature (recorded as an
// unverified restore) rather than being stuck.
func verifyUploadedDumpSignature(path, name, signature, masterKey string) (backupSigStatus, error) {
	if strings.TrimSpace(signature) == "" {
		return backupSigMissing, nil
	}
	if masterKey == "" {
		return backupSigUnavailable, nil
	}
	// The name binding is only unambiguous while no name contains a NUL, and
	// this one is a multipart parameter the uploader chose rather than a name
	// read from the filesystem. Enforce the precondition here instead of
	// assuming it: a name carrying a NUL could otherwise shift the boundary
	// between name and contents and make a different pair hash identically.
	if strings.ContainsRune(name, 0) {
		return backupSigInvalid, nil
	}
	want, ok := decodeSignature(signature)
	if !ok {
		return backupSigInvalid, nil
	}
	key, err := backupSignatureKey(masterKey)
	if err != nil {
		return backupSigUnavailable, err
	}
	got, err := hmacBackupFileAs(path, name, key)
	if err != nil {
		return backupSigInvalid, err
	}
	return compareSignature(got, want), nil
}

// removeBackupWithSignature deletes a dump and its sidecar. A missing sidecar
// is normal (unsigned backups) and never an error, but one left behind would
// outlive its dump and later be checked against an unrelated file of the same
// name. The dump's own error takes precedence so callers keep classifying a
// missing dump as a 404; a sidecar that could not be removed is reported only
// when the dump itself came away cleanly.
func removeBackupWithSignature(path string) error {
	err := os.Remove(path)
	sigErr := os.Remove(path + backupSignatureExt)
	if err != nil {
		return err
	}
	if sigErr != nil && !os.IsNotExist(sigErr) {
		return sigErr
	}
	return nil
}
