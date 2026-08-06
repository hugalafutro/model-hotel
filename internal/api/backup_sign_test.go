package api

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

// TestSignBackupFile_SidecarIsNotListedAsABackup guards the listing filter: a
// sidecar sitting next to its dump must never show up as a restorable backup.
func TestSignBackupFile_SidecarIsNotListedAsABackup(t *testing.T) {
	name := "backup_20260806_010203_0001_manual.dump" + backupSignatureExt
	if strings.HasSuffix(name, ".dump") {
		t.Errorf("sidecar name %q ends in .dump and would be listed and offered for restore", name)
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

func TestCheckSignature_ReportsUnavailableWithoutKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.dump")
	if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	status, err := checkSignature(path, []byte("whatever"), "")
	if err == nil {
		t.Error("checkSignature without a master key should error")
	}
	if status != backupSigUnavailable {
		t.Errorf("status = %v, want unavailable", status)
	}
}

func TestCheckSignature_MissingDumpIsAnError(t *testing.T) {
	status, err := checkSignature(filepath.Join(t.TempDir(), "missing.dump"), []byte("sig"), "master")
	if err == nil {
		t.Error("hashing a nonexistent dump should error")
	}
	if status != backupSigInvalid {
		t.Errorf("status = %v, want invalid", status)
	}
}

func TestVerifyBackupBytesAgainstSignature_UnavailableAndMalformed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.dump")
	if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	status, err := verifyBackupBytesAgainstSignature(path, "deadbeef", "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if status != backupSigUnavailable {
		t.Errorf("no master key: status = %v, want unavailable", status)
	}

	status, err = verifyBackupBytesAgainstSignature(path, "not-hex", "master")
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
