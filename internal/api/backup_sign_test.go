package api

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupSignatureKey_DerivesAndIsolates(t *testing.T) {
	a, err := backupSignatureKey("master-key-one")
	if err != nil {
		t.Fatalf("backupSignatureKey: %v", err)
	}
	if len(a) != 32 {
		t.Errorf("key length = %d, want 32", len(a))
	}
	again, err := backupSignatureKey("master-key-one")
	if err != nil {
		t.Fatalf("backupSignatureKey (repeat): %v", err)
	}
	if !bytes.Equal(a, again) {
		t.Error("derivation is not deterministic; existing signatures would stop verifying across restarts")
	}
	b, err := backupSignatureKey("master-key-two")
	if err != nil {
		t.Fatalf("backupSignatureKey (other master key): %v", err)
	}
	if bytes.Equal(a, b) {
		t.Error("different master keys derived the same signing key")
	}
	// The signing key must not be the master key itself: a leaked signing key
	// must not hand over the key that decrypts provider credentials.
	if string(a) == "master-key-one" {
		t.Error("signing key is the raw master key")
	}
	if _, err := backupSignatureKey(""); err == nil {
		t.Error("empty master key should not yield a signing key")
	}
}

func TestSignAndVerifyBackup_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backup_test.dump")
	if err := os.WriteFile(path, []byte("dump-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := signBackupFile(path, "master"); err != nil {
		t.Fatalf("signBackupFile: %v", err)
	}
	if _, err := os.Stat(path + backupSignatureExt); err != nil {
		t.Fatalf("sidecar not written: %v", err)
	}

	status, err := verifyBackupFile(path, "master")
	if err != nil {
		t.Fatalf("verifyBackupFile: %v", err)
	}
	if status != backupSigValid {
		t.Errorf("status = %v, want valid", status)
	}
}

func TestVerifyBackupFile_DetectsTamperingAndWrongKey(t *testing.T) {
	dir := t.TempDir()

	tampered := filepath.Join(dir, "tampered.dump")
	if err := os.WriteFile(tampered, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := signBackupFile(tampered, "master"); err != nil {
		t.Fatal(err)
	}
	// Rewrite the dump after signing, exactly what a planted-dump attack does.
	if err := os.WriteFile(tampered, []byte("injected-admin-row"), 0o600); err != nil {
		t.Fatal(err)
	}
	status, err := verifyBackupFile(tampered, "master")
	if err != nil {
		t.Fatalf("verifyBackupFile: %v", err)
	}
	if status != backupSigInvalid {
		t.Errorf("tampered dump status = %v, want invalid", status)
	}

	intact := filepath.Join(dir, "intact.dump")
	if err := os.WriteFile(intact, []byte("original"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := signBackupFile(intact, "master"); err != nil {
		t.Fatal(err)
	}
	status, err = verifyBackupFile(intact, "different-master")
	if err != nil {
		t.Fatalf("verifyBackupFile: %v", err)
	}
	if status != backupSigInvalid {
		t.Errorf("foreign-key signature status = %v, want invalid", status)
	}
}

func TestVerifyBackupFile_UnsignedIsReportedNotFailed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "legacy.dump")
	if err := os.WriteFile(path, []byte("predates signing"), 0o600); err != nil {
		t.Fatal(err)
	}

	status, err := verifyBackupFile(path, "master")
	if err != nil {
		t.Fatalf("verifyBackupFile on unsigned dump: %v", err)
	}
	if status != backupSigMissing {
		t.Errorf("status = %v, want missing (legacy backups must stay restorable)", status)
	}
}

func TestVerifyBackupFile_NoMasterKeyReportsUnavailable(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.dump")
	if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	status, err := verifyBackupFile(path, "")
	if err != nil {
		t.Fatalf("verifyBackupFile without master key: %v", err)
	}
	if status != backupSigUnavailable {
		t.Errorf("status = %v, want unavailable", status)
	}
}

func TestVerifyBackupFile_CorruptSidecarIsInvalid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "corrupt.dump")
	if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+backupSignatureExt, []byte("not-hex-at-all"), 0o600); err != nil {
		t.Fatal(err)
	}

	status, err := verifyBackupFile(path, "master")
	if err != nil {
		t.Fatalf("verifyBackupFile: %v", err)
	}
	if status != backupSigInvalid {
		t.Errorf("status = %v, want invalid for an unparseable sidecar", status)
	}
}

func TestRemoveBackupWithSignature_RemovesBoth(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gone.dump")
	if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := signBackupFile(path, "master"); err != nil {
		t.Fatal(err)
	}

	if err := removeBackupWithSignature(path); err != nil {
		t.Fatalf("removeBackupWithSignature: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("dump still present")
	}
	if _, err := os.Stat(path + backupSignatureExt); !os.IsNotExist(err) {
		t.Error("sidecar left behind; signatures would accumulate forever")
	}
}

func TestRemoveBackupWithSignature_MissingSidecarIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "unsigned.dump")
	if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := removeBackupWithSignature(path); err != nil {
		t.Fatalf("removeBackupWithSignature on unsigned dump: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("dump still present")
	}
}

func TestSignBackupFile_FailsWithoutMasterKeyOrFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.dump")
	if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := signBackupFile(path, ""); err == nil {
		t.Error("signing without a master key should fail rather than write an unkeyed signature")
	}
	if err := signBackupFile(filepath.Join(dir, "missing.dump"), "master"); err == nil {
		t.Error("signing a nonexistent dump should fail")
	}
}

// A sidecar that exists but cannot be read leaves the dump unprovable, so the
// check must report a hard failure rather than quietly passing. A directory in
// the sidecar's place reproduces an unreadable sidecar portably.
func TestVerifyBackupFile_UnreadableSidecarFailsClosed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "blocked.dump")
	if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path+backupSignatureExt, 0o750); err != nil {
		t.Fatal(err)
	}

	status, err := verifyBackupFile(path, "master")
	if err == nil {
		t.Error("unreadable sidecar should surface an error, not pass silently")
	}
	if status != backupSigInvalid {
		t.Errorf("status = %v, want invalid", status)
	}
}

func TestVerifyBackupFile_MissingDumpWithSidecarIsAnError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vanished.dump")
	// A sidecar with no dump beside it: the file was removed after signing.
	if err := os.WriteFile(path+backupSignatureExt, []byte("00ff"), 0o600); err != nil {
		t.Fatal(err)
	}

	status, err := verifyBackupFile(path, "master")
	if err == nil {
		t.Error("hashing a nonexistent dump should error")
	}
	if status != backupSigInvalid {
		t.Errorf("status = %v, want invalid", status)
	}
}

func TestVerifyUploadedDumpSignature_UnavailableAndMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.dump")
	if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	status, err := verifyUploadedDumpSignature(path, "x.dump", "deadbeef", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != backupSigUnavailable {
		t.Errorf("no master key: status = %v, want unavailable", status)
	}

	status, err = verifyUploadedDumpSignature(path, "x.dump", "not-hex", "master")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != backupSigInvalid {
		t.Errorf("malformed signature: status = %v, want invalid", status)
	}
}

// The dump's own removal error is what callers act on, but a sidecar that
// cannot be removed must not be reported as a clean delete: it would outlive
// the dump and later be matched against an unrelated file of the same name.
func TestRemoveBackupWithSignature_SidecarRemovalFailureSurfaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.dump")
	if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A non-empty directory in the sidecar's place cannot be removed.
	sidecar := path + backupSignatureExt
	if err := os.Mkdir(sidecar, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sidecar, "child"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := removeBackupWithSignature(path); err == nil {
		t.Error("a sidecar that could not be removed should be reported")
	}
}

// A (dump, sidecar) pair that verifies under any name lets an attacker with
// write access to the backup directory drop an older genuine backup in today's
// place: it would verify clean and restore stale state, reinstating revoked
// keys and deleted accounts without forging anything. The signature therefore
// covers the filename.
func TestSignature_IsBoundToTheFilename(t *testing.T) {
	dir := t.TempDir()
	original := filepath.Join(dir, "backup_old.dump")
	if err := os.WriteFile(original, []byte("last month's database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := signBackupFile(original, "master"); err != nil {
		t.Fatal(err)
	}

	// Move the genuine dump and its genuine sidecar into today's name.
	renamed := filepath.Join(dir, "backup_today.dump")
	if err := os.Rename(original, renamed); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(original+backupSignatureExt, renamed+backupSignatureExt); err != nil {
		t.Fatal(err)
	}

	status, err := verifyBackupFile(renamed, "master")
	if err != nil {
		t.Fatalf("verifyBackupFile: %v", err)
	}
	if status != backupSigInvalid {
		t.Errorf("status = %v, want invalid: a whole signed pair was replayed under another name", status)
	}
}

// The download path checks the handle it already holds. Swapping the file at
// the path after it is opened must not change the verdict for the bytes being
// served, which is the rename race an attacker with directory write access
// would otherwise win.
//
// The swap-in file is signed under the SAME base name as the target, so a
// path-based check would recompute a genuinely valid signature and pass. That
// is what makes this test fail if the handle-based check regresses: the name
// binding alone cannot catch this one.
func TestVerifyBackupHandle_ChecksTheOpenInodeNotThePath(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backup_test.dump")
	if err := os.WriteFile(path, []byte("tampered contents"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A genuine dump carrying the same base name, signed in a separate
	// directory so both can exist at once.
	sub := t.TempDir()
	genuine := filepath.Join(sub, "backup_test.dump")
	if err := os.WriteFile(genuine, []byte("genuine contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := signBackupFile(genuine, "master"); err != nil {
		t.Fatal(err)
	}
	sig, err := os.ReadFile(genuine + backupSignatureExt)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+backupSignatureExt, sig, 0o600); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	// The attacker swaps the genuine file over the path after the open. A
	// path-based check now hashes genuine contents under the same name the
	// sidecar was signed with and returns valid; the handle still refers to the
	// tampered inode that would actually be served.
	if err := os.Rename(genuine, path); err != nil {
		t.Fatal(err)
	}

	status, err := verifyBackupHandle(f, path, "master")
	if err != nil {
		t.Fatalf("verifyBackupHandle: %v", err)
	}
	if status != backupSigInvalid {
		t.Errorf("status = %v, want invalid: verified a different inode than the one open for serving", status)
	}
}

// The handle must be rewound after hashing or the download would serve nothing.
func TestVerifyBackupHandle_LeavesTheHandleAtTheStart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "backup_test.dump")
	if err := os.WriteFile(path, []byte("dump contents"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := signBackupFile(path, "master"); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	status, err := verifyBackupHandle(f, path, "master")
	if err != nil || status != backupSigValid {
		t.Fatalf("verify = %v, %v; want valid", status, err)
	}
	rest, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	if string(rest) != "dump contents" {
		t.Errorf("handle left at offset %d; download would serve %q", len(rest), rest)
	}
}

// The uploaded name is a multipart parameter the uploader chose, not a name
// read from the filesystem, so the NUL-free precondition the name binding rests
// on has to be enforced rather than assumed.
func TestVerifyUploadedDumpSignature_RejectsNULInTheDeclaredName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "uploaded.dump")
	if err := os.WriteFile(path, []byte("contents"), 0o600); err != nil {
		t.Fatal(err)
	}

	status, err := verifyUploadedDumpSignature(path, "backup\x00extra.dump", "00ff", "master")
	if err != nil {
		t.Fatalf("verifyUploadedDumpSignature: %v", err)
	}
	if status != backupSigInvalid {
		t.Errorf("status = %v, want invalid for a name containing NUL", status)
	}
}
