package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// setupSignedBackupRouter is setupBackupRouter with a signing key wired, so the
// signature paths are exercised the way a configured deployment runs them.
//
//nolint:revive // unnamedResult: test helper, mirrors setupBackupRouter
func setupSignedBackupRouter(t *testing.T, masterKey string) (chi.Router, string) {
	t.Helper()
	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{}, nil)
	h.SetSigningKey(masterKey)
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			next.ServeHTTP(w, r)
		})
	})
	h.Register(r)
	return r, dir
}

func writeSignedBackup(t *testing.T, dir, name, content, masterKey string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := signBackupFile(path, masterKey); err != nil {
		t.Fatalf("signBackupFile: %v", err)
	}
	return path
}

// A dump rewritten after signing is the planted-backup attack. It must not be
// handed to the operator, because the next thing that happens to a downloaded
// backup is a restore.
func TestDownloadBackup_RefusesTamperedDump(t *testing.T) {
	r, dir := setupSignedBackupRouter(t, "master")
	path := writeSignedBackup(t, dir, "backup_test.dump", "original contents", "master")

	if err := os.WriteFile(path, []byte("contents with an injected admin row"), 0o600); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/backups/backup_test.dump", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409: tampered dump was served (%s)", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "integrity") {
		t.Errorf("response should explain the integrity failure, got %q", body)
	}
}

func TestDownloadBackup_ServesIntactSignedDump(t *testing.T) {
	r, dir := setupSignedBackupRouter(t, "master")
	writeSignedBackup(t, dir, "backup_test.dump", "original contents", "master")

	req := httptest.NewRequest("GET", "/backups/backup_test.dump", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	if w.Body.String() != "original contents" {
		t.Errorf("body = %q, want the dump contents", w.Body.String())
	}
}

// Backups written before signing existed, and dumps this instance never signed,
// have no sidecar. Refusing them would make legitimate old backups
// unrestorable, so they are served and reported as unsigned instead.
func TestDownloadBackup_ServesUnsignedLegacyDump(t *testing.T) {
	r, dir := setupSignedBackupRouter(t, "master")
	if err := os.WriteFile(filepath.Join(dir, "backup_legacy.dump"), []byte("predates signing"), 0o600); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/backups/backup_legacy.dump", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: unsigned legacy backups must stay downloadable (%s)", w.Code, w.Body.String())
	}
}

// With no MASTER_KEY there is nothing to verify against, and backups must keep
// working exactly as they did before signing existed.
func TestDownloadBackup_NoMasterKeyStillServes(t *testing.T) {
	r, dir := setupSignedBackupRouter(t, "")
	if err := os.WriteFile(filepath.Join(dir, "backup_test.dump"), []byte("bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/backups/backup_test.dump", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
}

func TestListBackups_ReportsSignedState(t *testing.T) {
	r, dir := setupSignedBackupRouter(t, "master")
	writeSignedBackup(t, dir, "backup_signed.dump", "signed contents", "master")
	if err := os.WriteFile(filepath.Join(dir, "backup_unsigned.dump"), []byte("no sidecar"), 0o600); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/backups", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var entries []backupEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 (sidecars must not be listed as backups)", len(entries))
	}
	for _, e := range entries {
		want := e.Filename == "backup_signed.dump"
		if e.Signed != want {
			t.Errorf("%s: signed = %v, want %v", e.Filename, e.Signed, want)
		}
	}
}

func TestDeleteBackup_RemovesSignatureSidecar(t *testing.T) {
	r, dir := setupSignedBackupRouter(t, "master")
	path := writeSignedBackup(t, dir, "backup_test.dump", "contents", "master")

	req := httptest.NewRequest("DELETE", "/backups/backup_test.dump", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (%s)", w.Code, w.Body.String())
	}
	if _, err := os.Stat(path + backupSignatureExt); !os.IsNotExist(err) {
		t.Error("sidecar outlived its dump")
	}
}

// restoreRequestWithSignature builds the multipart form the restore handler
// reads, carrying only the signature field the verifier looks at.
func restoreRequestWithSignature(t *testing.T, signature string) *http.Request {
	t.Helper()
	body := &bytes.Buffer{}
	mw := multipart.NewWriter(body)
	if err := mw.WriteField("signature", signature); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("POST", "/backups/restore", body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	return req
}

func TestVerifyUploadedDump_RejectsSignatureMismatch(t *testing.T) {
	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{}, nil)
	h.SetSigningKey("master")

	// A signature for different bytes than the ones uploaded.
	other := filepath.Join(dir, "other.dump")
	if err := os.WriteFile(other, []byte("the bytes that were signed"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := signBackupFile(other, "master"); err != nil {
		t.Fatal(err)
	}
	sig, err := os.ReadFile(other + backupSignatureExt)
	if err != nil {
		t.Fatal(err)
	}

	uploaded := filepath.Join(dir, "uploaded.dump")
	if err := os.WriteFile(uploaded, []byte("different bytes entirely"), 0o600); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	if h.verifyUploadedDump(w, restoreRequestWithSignature(t, string(sig)), uploaded) {
		t.Error("restore proceeded with a dump that does not match its signature")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestVerifyUploadedDump_AcceptsMatchingSignature(t *testing.T) {
	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{}, nil)
	h.SetSigningKey("master")

	uploaded := filepath.Join(dir, "uploaded.dump")
	if err := os.WriteFile(uploaded, []byte("genuine dump bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := signBackupFile(uploaded, "master"); err != nil {
		t.Fatal(err)
	}
	sig, err := os.ReadFile(uploaded + backupSignatureExt)
	if err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	if !h.verifyUploadedDump(w, restoreRequestWithSignature(t, string(sig)), uploaded) {
		t.Errorf("restore rejected a correctly signed dump: %s", w.Body.String())
	}
}

// No signature supplied is the legacy and foreign-dump case: it proceeds, since
// blocking it would make old backups unrestorable, but never silently.
func TestVerifyUploadedDump_ProceedsWithoutSignature(t *testing.T) {
	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{}, nil)
	h.SetSigningKey("master")

	uploaded := filepath.Join(dir, "uploaded.dump")
	if err := os.WriteFile(uploaded, []byte("unsigned dump"), 0o600); err != nil {
		t.Fatal(err)
	}

	w := httptest.NewRecorder()
	if !h.verifyUploadedDump(w, restoreRequestWithSignature(t, ""), uploaded) {
		t.Errorf("restore blocked an unsigned dump: %s", w.Body.String())
	}
}
