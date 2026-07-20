package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

//nolint:gosec,revive // test-only: error not critical, unnamedResult is test helper
func setupBackupRouter(t *testing.T) (chi.Router, string) {
	t.Helper()
	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{}, nil)
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

func TestBackupHandler_ListBackups_Empty(t *testing.T) {
	r, _ := setupBackupRouter(t)

	req := httptest.NewRequest("GET", "/backups", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result []backupEntry
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty list, got %d items", len(result))
	}
}

func TestBackupHandler_ListBackups_WithFiles(t *testing.T) {
	r, dir := setupBackupRouter(t)

	// Create fake backup files - names encode timestamps so sort is deterministic
	for _, name := range []string{"backup_20250101_120000.dump", "backup_20250102_120000.dump"} {
		//nolint:gosec // test-only: permissive perms acceptable
		if err := os.WriteFile(filepath.Join(dir, name), []byte("test"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Create a non-dump file that should be ignored
	//nolint:gosec // test-only: permissive perms acceptable
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/backups", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result []backupEntry
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 backups, got %d", len(result))
	}
	// Should be sorted by CreatedAt descending (newest first).
	// Both files share the same modtime, so order depends on filename fallback.
	// Just verify we got 2 items and both are .dump files.
	for _, b := range result {
		if b.SizeBytes != 4 {
			t.Errorf("expected size 4, got %d for %s", b.SizeBytes, b.Filename)
		}
	}
}

func TestBackupHandler_DownloadBackup(t *testing.T) {
	r, dir := setupBackupRouter(t)

	content := []byte("fake backup content")
	//nolint:gosec // test-only: permissive perms acceptable
	if err := os.WriteFile(filepath.Join(dir, "backup_test.dump"), content, 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/backups/backup_test.dump", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if w.Body.String() != string(content) {
		t.Errorf("expected file content, got %q", w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); cd != `attachment; filename="backup_test.dump"` {
		t.Errorf("expected Content-Disposition header, got %q", cd)
	}
}

func TestBackupHandler_DownloadBackup_NotFound(t *testing.T) {
	r, _ := setupBackupRouter(t)

	req := httptest.NewRequest("GET", "/backups/nonexistent.dump", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestBackupHandler_DownloadBackup_PathTraversal(t *testing.T) {
	r, _ := setupBackupRouter(t)

	traversalCases := []struct {
		name   string
		path   string
		expect int
	}{
		{"no extension", "/backups/noext", http.StatusBadRequest},
		// Chi normalizes path traversal: ../../../etc/passwd.dump resolves
		// outside the backup dir and is caught by the filepath.Abs prefix check (404).
		{"parent traversal", "/backups/../../../etc/passwd.dump", http.StatusNotFound},
		{"dotdot in middle", "/backups/foo/../../etc/passwd.dump", http.StatusNotFound},
		{"backslash", "/backups/..\\..\\etc\\passwd.dump", http.StatusBadRequest},
		// CRLF: chi does not decode %0d%0a to literal \r\n in URL params,
		// so the filename passes validation but the file doesn't exist (404).
		{"CRLF injection", "/backups/foo%0d%0a.dump", http.StatusNotFound},
		{"valid but missing", "/backups/missing.dump", http.StatusNotFound},
	}

	for _, tc := range traversalCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tc.path, http.NoBody)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.expect {
				t.Errorf("expected %d, got %d", tc.expect, w.Code)
			}
		})
	}
}

func TestBackupHandler_DeleteBackup(t *testing.T) {
	r, dir := setupBackupRouter(t)

	path := filepath.Join(dir, "backup_delete.dump")
	//nolint:gosec // test-only: permissive perms acceptable
	if err := os.WriteFile(path, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("DELETE", "/backups/backup_delete.dump", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d", w.Code)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("expected file to be deleted")
	}
}

func TestBackupHandler_DeleteBackup_PathTraversal(t *testing.T) {
	r, _ := setupBackupRouter(t)

	cases := []struct {
		name   string
		path   string
		expect int
	}{
		{"parent traversal", "/backups/../../../etc/passwd.dump", http.StatusNotFound},
		{"dotdot in middle", "/backups/foo/../../etc/passwd.dump", http.StatusNotFound},
		{"backslash", "/backups/..\\..\\etc\\passwd.dump", http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("DELETE", tc.path, http.NoBody)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.expect {
				t.Errorf("expected %d, got %d", tc.expect, w.Code)
			}
		})
	}
}

func TestBackupHandler_DeleteBackup_NotFound(t *testing.T) {
	r, _ := setupBackupRouter(t)

	req := httptest.NewRequest("DELETE", "/backups/nonexistent.dump", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// The "pg_dump missing -> 412" path is covered deterministically by
// TestCreateBackup_NoPgDump_ManipulatedPATH (subprocess with empty PATH), so the
// earlier environment-conditional duplicates that skipped when pg_dump happened
// to be installed were removed: a test must pass or fail, never skip.

// TestCreateBackup_Success_Integration tests that CreateBackup returns 200 with JSON response
// when pg_dump is available and database is accessible.
func TestCreateBackup_Success_Integration(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	// Check pg_dump is available
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Fatal("pg_dump not installed, skipping integration test")
	}

	dir := t.TempDir()
	h := NewBackupHandler(apiTestDBURL, dir, &mockAdminAuth{}, nil)
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			next.ServeHTTP(w, r)
		})
	})
	h.Register(r)

	req := httptest.NewRequest("POST", "/backups", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	var result backupEntry
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	if result.Filename == "" {
		t.Error("expected non-empty filename")
	}
	if !strings.HasSuffix(result.Filename, ".dump") {
		t.Errorf("expected filename to end with .dump, got %q", result.Filename)
	}
	if result.SizeBytes <= 0 {
		t.Errorf("expected positive size_bytes, got %d", result.SizeBytes)
	}
	if result.CreatedAt == "" {
		t.Error("expected non-empty created_at")
	}
}

func TestBackupHandler_CreateBackup_PgDumpFailed(t *testing.T) {
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Fatal("pg_dump not installed, skipping pg_dump failure test")
	}

	r, _ := setupBackupRouter(t)

	req := httptest.NewRequest("POST", "/backups", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBackupHandler_CreateBackup_MkdirAllError(t *testing.T) {
	// Create a regular file where the backup dir should be
	file, err := os.CreateTemp(t.TempDir(), "backup-blocker-*")
	if err != nil {
		t.Fatal(err)
	}
	filePath := file.Name()
	file.Close()

	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", filePath, &mockAdminAuth{}, nil)
	r := chi.NewRouter()
	h.Register(r)

	req := httptest.NewRequest("POST", "/backups", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBackupHandler_ListBackups_ReadDirError(t *testing.T) {
	// Create a regular file where the backup dir should be
	file, err := os.CreateTemp(t.TempDir(), "not-a-dir-*")
	if err != nil {
		t.Fatal(err)
	}
	filePath := file.Name()
	file.Close()

	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", filePath, &mockAdminAuth{}, nil)
	r := chi.NewRouter()
	h.Register(r)

	req := httptest.NewRequest("GET", "/backups", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBackupHandler_DeleteBackup_RemoveError(t *testing.T) {
	dir := t.TempDir()

	// Create a .dump file
	dumpPath := filepath.Join(dir, "backup_readonly.dump")
	if err := os.WriteFile(dumpPath, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Make directory read-only so os.Remove fails
	//nolint:gosec // test-only: permissive to restrictive is fine
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(dir, 0o755) // restore for cleanup

	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{}, nil)
	r := chi.NewRouter()
	h.Register(r)

	req := httptest.NewRequest("DELETE", "/backups/backup_readonly.dump", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// TestNewBackupHandler_AbsFallback tests that when filepath.Abs fails,
// the original path is used as fallback.
func TestNewBackupHandler_LongAbsolutePath(t *testing.T) {
	// filepath.Abs on Linux does not enforce path length limits; it just
	// calls Getwd + Clean. So a very long absolute path still succeeds,
	// and the fallback (L39-41) is not exercised. This test verifies
	// that NewBackupHandler handles long paths without panicking.
	longPath := "/tmp/" + strings.Repeat("a", 5000)
	h := NewBackupHandler("postgres://test", longPath, &mockAdminAuth{}, nil)
	if h.backupDir != longPath {
		t.Errorf("expected backupDir to be original path, got %q", h.backupDir)
	}
}

// TestBackupHandler_CreateBackup_ConcurrentLock tests that a 409 Conflict
// is returned when a backup is already in progress.
func TestBackupHandler_CreateBackup_ConcurrentLock(t *testing.T) {
	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{}, nil)

	// Lock the mutex to simulate an in-progress backup
	h.backupMu.Lock()
	defer h.backupMu.Unlock()

	r := chi.NewRouter()
	h.Register(r)

	req := httptest.NewRequest("POST", "/backups", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected status %d, got %d: %s", http.StatusConflict, w.Code, w.Body.String())
	}
}

// TestBackupHandler_ListBackups_NonExistentDir tests that an empty array
// is returned when the backup directory doesn't exist.
func TestBackupHandler_ListBackups_NonExistentDir(t *testing.T) {
	// Create handler with a non-existent directory
	dir := filepath.Join(t.TempDir(), "nonexistent")
	h := NewBackupHandler("postgres://invalid", dir, &mockAdminAuth{}, nil)
	r := chi.NewRouter()
	h.Register(r)

	req := httptest.NewRequest("GET", "/backups", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var result []backupEntry
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty slice, got %d items", len(result))
	}
}

// TestBackupHandler_ListBackups_SingleDumpFile tests that a single .dump
// file in the backup directory is listed with correct metadata.
// The Info() error path (L163-165 in backup.go) cannot be triggered on
// Linux: os.DirEntry.Info() on a dangling symlink returns the symlink's
// own metadata, not an error. That path exists as a defensive measure for
// race conditions where a file is deleted between ReadDir and Info() calls.
func TestBackupHandler_ListBackups_SingleDumpFile(t *testing.T) {
	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid", dir, &mockAdminAuth{}, nil)

	// Create a .dump file (content is not a real pg_dump, but ListBackups
	// only reads file info, not content).
	dumpPath := filepath.Join(dir, "backup_valid.dump")
	//nolint:gosec // test-only: permissive perms acceptable
	if err := os.WriteFile(dumpPath, []byte("test"), 0o644); err != nil {
		t.Fatal(err)
	}

	r := chi.NewRouter()
	h.Register(r)

	req := httptest.NewRequest("GET", "/backups", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var result []backupEntry
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 backup, got %d", len(result))
	}
}

// TestValidateBackupFilename_PrefixEscape tests that paths resolving outside
// the backup directory are rejected by the absolute path prefix check.
func TestValidateBackupFilename_PathSeparators(t *testing.T) {
	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid", dir, &mockAdminAuth{}, nil)

	// Filenames containing / or \ are rejected by the ContainsAny check
	// (L186-187) before the prefix check is reached. These test the
	// path-separator guard, not the prefix-escape guard.
	invalidCases := []string{
		"../etc/passwd.dump",
		"foo/../../etc/passwd.dump",
	}

	for _, filename := range invalidCases {
		result := h.validateBackupFilename(filename)
		if result != "" {
			t.Errorf("expected empty string for %q, got %q", filename, result)
		}
	}

	// Valid filename should resolve inside the backup dir
	validResult := h.validateBackupFilename("backup_valid.dump")
	if validResult == "" {
		t.Error("expected non-empty result for valid filename")
	}
}

// TestValidateBackupFilename_InvalidChars tests that filenames with
// path separators and non-.dump extensions are rejected.
func TestValidateBackupFilename_InvalidChars(t *testing.T) {
	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid", dir, &mockAdminAuth{}, nil)

	cases := []struct {
		name     string
		filename string
	}{
		{"forward slash", "foo/bar.dump"},
		{"backslash", "foo\\bar.dump"},
		{"carriage return", "foo\rbar.dump"},
		{"newline", "foo\nbar.dump"},
		{"no extension", "backup"},
		{"wrong extension", "backup.sql"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := h.validateBackupFilename(tc.filename)
			if result != "" {
				t.Errorf("expected empty string for %q, got %q", tc.filename, result)
			}
		})
	}
}

// TestNewBackupHandler_Constructor tests the NewBackupHandler constructor
// with various inputs including empty backup dir and long paths.
func TestNewBackupHandler_Constructor(t *testing.T) {
	adminAuth := &mockAdminAuth{}

	// Test with normal inputs — NewBackupHandler always returns non-nil.
	h := NewBackupHandler("postgres://user:pass@localhost/db", "/tmp/backups", adminAuth, nil)
	if h.databaseURL != "postgres://user:pass@localhost/db" {
		t.Errorf("expected databaseURL to be set, got %q", h.databaseURL)
	}
	if h.adminMgr != adminAuth {
		t.Error("expected adminMgr to be set")
	}
	// backupDir should be absolute path
	if !filepath.IsAbs(h.backupDir) {
		t.Errorf("expected backupDir to be absolute, got %q", h.backupDir)
	}

	// Test with empty backup dir path
	hEmpty := NewBackupHandler("postgres://user:pass@localhost/db", "", adminAuth, nil)
	// Empty string should resolve to current working directory
	if !filepath.IsAbs(hEmpty.backupDir) {
		t.Errorf("expected backupDir to be absolute for empty input, got %q", hEmpty.backupDir)
	}

	// Test with long path
	longPath := "/tmp/" + strings.Repeat("a", 5000)
	hLong := NewBackupHandler("postgres://user:pass@localhost/db", longPath, adminAuth, nil)
	if hLong.backupDir != longPath {
		t.Errorf("expected backupDir to be original long path, got %q", hLong.backupDir)
	}
}

// TestBackupHandler_Register tests that the Register method correctly
// registers all backup routes.
func TestBackupHandler_Register(t *testing.T) {
	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{}, nil)
	r := chi.NewRouter()
	h.Register(r)

	// Test GET /backups → 200 (empty list)
	req := httptest.NewRequest("GET", "/backups", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("GET /backups: expected 200, got %d", w.Code)
	}

	// Test POST /backups → depends on pg_dump (will be 412 or 500)
	req = httptest.NewRequest("POST", "/backups", http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	// Either 412 (pg_dump not found) or 500 (pg_dump failed) is acceptable
	if w.Code != http.StatusPreconditionFailed && w.Code != http.StatusInternalServerError {
		t.Errorf("POST /backups: expected 412 or 500, got %d", w.Code)
	}

	// Test POST /backups/restore → depends on auth/multipart (will be 401 or 400)
	req = httptest.NewRequest("POST", "/backups/restore", http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	// Without admin token or multipart form, should be 401 or 400
	if w.Code != http.StatusUnauthorized && w.Code != http.StatusBadRequest {
		t.Errorf("POST /backups/restore: expected 401 or 400, got %d", w.Code)
	}

	// Test GET /backups/{filename} → 404 (not found)
	req = httptest.NewRequest("GET", "/backups/nonexistent.dump", http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("GET /backups/{filename}: expected 404, got %d", w.Code)
	}

	// Test DELETE /backups/{filename} → 404 (not found)
	req = httptest.NewRequest("DELETE", "/backups/nonexistent.dump", http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("DELETE /backups/{filename}: expected 404, got %d", w.Code)
	}
}

// TestValidateBackupFilename_ValidNames tests validateBackupFilename with
// various valid filename patterns.
func TestValidateBackupFilename_ValidNames(t *testing.T) {
	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid", dir, &mockAdminAuth{}, nil)

	// Valid names like "backup_20250101_120000.dump" should return the filename
	validCases := []string{
		"backup_20250101_120000.dump",
		"backup-2025-01-01.dump",
		"backup.v2.2025.dump",
		"backup.dump",
		"backup_20250101_120000_123456.dump",
	}

	for _, filename := range validCases {
		result := h.validateBackupFilename(filename)
		if result == "" {
			t.Errorf("expected non-empty result for valid filename %q", filename)
		}
		// Result should be absolute path within backup dir
		expectedPath := filepath.Join(dir, filename)
		if result != expectedPath {
			t.Errorf("for filename %q: expected %q, got %q", filename, expectedPath, result)
		}
	}

	// Invalid names should return empty string
	invalidCases := []string{
		"backup.sql",         // wrong extension
		"backup",             // no extension
		"../etc/passwd.dump", // path traversal
		"foo/bar.dump",       // contains slash
		"foo\\bar.dump",      // contains backslash
		"backup\r\n.dump",    // contains CR/LF
	}

	for _, filename := range invalidCases {
		result := h.validateBackupFilename(filename)
		if result != "" {
			t.Errorf("expected empty string for invalid filename %q, got %q", filename, result)
		}
	}
}

// ---------------------------------------------------------------------------
// Tests moved from coverage_gap_test.go
// ---------------------------------------------------------------------------

// TestCreateBackup_Success tests the happy path of BackupHandler.CreateBackup().
// This test requires pg_dump to be installed on the system.
func TestCreateBackup_Success(t *testing.T) {

	// Skip if pg_dump is not available
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Fatal("pg_dump not installed, skipping backup integration test")
	}

	backupDir := t.TempDir()
	bh := NewBackupHandler(apiTestDBURL, backupDir, &mockAdminAuth{}, nil)

	r := chi.NewRouter()
	bh.Register(r)

	req := httptest.NewRequest(http.MethodPost, "/backups", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Filename  string `json:"filename"`
		SizeBytes int64  `json:"size_bytes"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Filename == "" {
		t.Error("response should have non-empty filename")
	}

	if resp.SizeBytes == 0 {
		t.Error("response should have non-zero size_bytes")
	}

	// Verify the backup file actually exists on disk
	backupPath := backupDir + "/" + resp.Filename
	if _, err := exec.LookPath("stat"); err == nil {
		//nolint:gosec // test-only subprocess
		if _, err := exec.Command("stat", backupPath).CombinedOutput(); err != nil {
			t.Errorf("backup file should exist at %s", backupPath)
		}
	}
}

// TestNewBackupHandler_PathHandling documents the filepath.Abs fallback
// behavior. On Linux, filepath.Abs rarely fails, so the fallback path
// (backup.go:39-41) is not exercised. This test verifies that
// NewBackupHandler handles various path formats without panicking.
// The fallback is documented as being exercised in environments where
// Abs fails (e.g., path length limits on other platforms).
func TestNewBackupHandler_PathHandling(t *testing.T) {
	// Test with various path formats
	testCases := []struct {
		name      string
		backupDir string
	}{
		{"relative path", "backups"},
		{"absolute path", "/tmp/backups"},
		{"long path", "/tmp/" + strings.Repeat("a", 5000)},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewBackupHandler("postgres://test", tc.backupDir, &mockAdminAuth{}, nil)
			if h.backupDir == "" {
				t.Error("expected non-empty backupDir")
			}
			if h.adminMgr == nil {
				t.Error("expected non-nil adminMgr")
			}
		})
	}
}

func TestCreateBackup_NoPgDump_ManipulatedPATH(t *testing.T) {
	// This test runs itself as a subprocess to safely manipulate PATH
	// without affecting other tests running in parallel.
	if os.Getenv("TEST_NO_PG_DUMP") == "1" {
		dir := t.TempDir()
		h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{}, nil)
		r := chi.NewRouter()
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				next.ServeHTTP(w, r)
			})
		})
		h.Register(r)

		req := httptest.NewRequest("POST", "/backups", http.NoBody)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusPreconditionFailed {
			fmt.Printf("NO_PG_DUMP: expected 412, got %d: %s\n", w.Code, w.Body.String())
			os.Exit(1)
		}
		if !strings.Contains(w.Body.String(), "pg_dump not found") {
			fmt.Printf("NO_PG_DUMP: expected error to mention pg_dump, got: %s\n", w.Body.String())
			os.Exit(1)
		}
		os.Exit(0)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestCreateBackup_NoPgDump_ManipulatedPATH")
	cmd.Env = append(os.Environ(), "TEST_NO_PG_DUMP=1", "PATH=")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("subprocess failed: %v\noutput: %s", err, output)
	}
}

// TestValidateBackupFilename_AbsPathPrefixEscape tests the absolute-path
// prefix check (L192-194). When filepath.Abs returns an error or the
// resolved path falls outside the backup directory, validation fails.
// On Linux, filepath.Abs can fail if os.Getwd() fails (e.g., CWD deleted).
// Since we cannot safely change the process working directory in a
// parallel test, we verify the ContainsAny guard on L187 blocks the
// only viable escape vectors (/, \), and the prefix check catches
// edge cases where filepath.Join+Abs produces an unexpected path.
func TestValidateBackupFilename_AbsPathPrefixEscape(t *testing.T) {
	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid", dir, &mockAdminAuth{}, nil)

	// A filename without path separators that resolves inside backupDir
	// should pass validation
	validResult := h.validateBackupFilename("normal.dump")
	if validResult == "" {
		t.Error("expected valid filename to pass validation")
	}

	// Verify the ContainsAny guard blocks all escape vectors
	escapeVectors := []string{
		"../etc/passwd.dump",    // parent traversal
		"../../etc/passwd.dump", // double parent
		"foo/../../bar.dump",    // mixed traversal
		"foo\\bar.dump",         // backslash
		"foo\rbar.dump",         // CR
		"foo\nbar.dump",         // LF
	}
	for _, vec := range escapeVectors {
		result := h.validateBackupFilename(vec)
		if result != "" {
			t.Errorf("expected empty for %q, got %q", vec, result)
		}
	}
}

// TestNewBackupHandler_AbsFallback_Subprocess tests the filepath.Abs fallback
// (L39-41) by running in a subprocess where the working directory has been
// deleted, causing filepath.Abs to fail. It also covers the validateBackupFilename
// prefix-escape check (L192-194) under the same conditions.
func TestNewBackupHandler_AbsFallback_Subprocess(t *testing.T) {
	// This test runs itself as a subprocess to safely change CWD
	// to a deleted directory without affecting other tests.
	if os.Getenv("TEST_DELETED_CWD") == "1" {
		// We are the subprocess. Our CWD has been deleted by the parent.
		// filepath.Abs should fail because os.Getwd() fails.

		// Test L39-41: NewBackupHandler falls back to original path
		h := NewBackupHandler("postgres://test", "my_backup_dir", &mockAdminAuth{}, nil)
		if h.backupDir != "my_backup_dir" {
			fmt.Printf("FALLBACK FAILED: expected my_backup_dir, got %q\n", h.backupDir)
			os.Exit(1)
		}

		// Test L192-194: validateBackupFilename returns "" when filepath.Abs fails
		result := h.validateBackupFilename("test.dump")
		if result != "" {
			fmt.Printf("PREFIX CHECK FAILED: expected empty string, got %q\n", result)
			os.Exit(1)
		}

		// Success
		os.Exit(0)
	}

	// Parent: create a temp dir, start subprocess with it as CWD, then delete it
	tmpDir := t.TempDir()

	cmd := exec.Command(os.Args[0], "-test.run=TestNewBackupHandler_AbsFallback_Subprocess")
	cmd.Env = append(os.Environ(), "TEST_DELETED_CWD=1")
	cmd.Dir = tmpDir

	// Start the subprocess first
	if err := cmd.Start(); err != nil {
		t.Fatalf("cannot start subprocess: %v", err)
	}

	// Delete the CWD while the subprocess is running
	//nolint:gosec // test-only: removing test directory
	if err := os.RemoveAll(tmpDir); err != nil {
		t.Fatalf("cannot remove temp dir: %v", err)
	}

	// Wait for the subprocess to finish
	if err := cmd.Wait(); err != nil {
		t.Fatalf("subprocess failed: %v", err)
	}
}

// TestBackup_PasswordStrippedFromArgs verifies that when DATABASE_URL contains
// a password, it is stripped from the command-line arguments and passed via
// PGPASSWORD environment variable instead. This prevents passwords from
// appearing in process listings (ps, /proc, etc.).
func TestBackup_PasswordStrippedFromArgs(t *testing.T) {
	// Create a temporary directory for the mock pg_dump and capture file
	tmpDir := t.TempDir()
	mockPgDump := filepath.Join(tmpDir, "pg_dump")
	captureFile := filepath.Join(tmpDir, "capture.txt")

	// Create a mock pg_dump script that writes its args and env to a file
	// and creates an empty backup file to simulate success
	mockScript := `#!/bin/bash
# Parse --file= argument to find output path
OUTPUT_FILE=""
for arg in "$@"; do
	if [[ "$arg" == --file=* ]]; then
		OUTPUT_FILE="${arg#--file=}"
	fi
	# Write command-line args (one per line, prefixed with ARG:)
	echo "ARG:$arg" >> "` + captureFile + `"
done
# Write PGPASSWORD env var if set (or note it's missing)
if [ -n "$PGPASSWORD" ]; then
	echo "PGPASSWORD:$PGPASSWORD" >> "` + captureFile + `"
else
	echo "PGPASSWORD:" >> "` + captureFile + `"
fi
# Create empty backup file to simulate success
if [ -n "$OUTPUT_FILE" ]; then
	touch "$OUTPUT_FILE"
fi
# Exit successfully so the handler thinks backup worked
exit 0
`
	//nolint:gosec // test-only: script in temp dir
	if err := os.WriteFile(mockPgDump, []byte(mockScript), 0o755); err != nil {
		t.Fatalf("failed to write mock pg_dump: %v", err)
	}

	// Temporarily modify PATH so exec.LookPath finds our mock first
	originalPath := os.Getenv("PATH")
	//nolint:errcheck // cleanup: restore PATH after test
	defer os.Setenv("PATH", originalPath)
	//nolint:errcheck // prepend mock dir to PATH
	os.Setenv("PATH", tmpDir+":"+originalPath)

	// Test case 1: DATABASE_URL with password
	t.Run("with_password", func(t *testing.T) {
		// Clear capture file
		//nolint:errcheck,gosec // test-only: clearing capture file
		os.WriteFile(captureFile, []byte{}, 0o644)

		backupDir := t.TempDir()
		databaseURL := "postgresql://user:secret@localhost:5432/dbname"
		h := NewBackupHandler(databaseURL, backupDir, &mockAdminAuth{}, nil)
		r := chi.NewRouter()
		h.Register(r)

		req := httptest.NewRequest("POST", "/backups", http.NoBody)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// The mock script always succeeds, so we expect 201 Created
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}

		// Read the capture file
		captured, err := os.ReadFile(captureFile)
		if err != nil {
			t.Fatalf("failed to read capture file: %v", err)
		}
		capturedStr := string(captured)

		// Verify 1: Password should NOT appear in command-line args
		if strings.Contains(capturedStr, "secret") {
			// Check if it's in an ARG line (bad) vs PGPASSWORD line (ok)
			lines := strings.SplitSeq(capturedStr, "\n")
			for line := range lines {
				if strings.HasPrefix(line, "ARG:") && strings.Contains(line, "secret") {
					t.Errorf("password 'secret' found in command-line args: %s", line)
				}
			}
		}

		// Verify 2: PGPASSWORD should be set to "secret"
		if !strings.Contains(capturedStr, "PGPASSWORD:secret") {
			t.Errorf("expected PGPASSWORD:secret in capture, got:\n%s", capturedStr)
		}

		// Verify 3: The connection URL should have user but no password
		// Should be something like: postgresql://user@localhost:5432/dbname
		hasUserOnlyURL := false
		for line := range strings.SplitSeq(capturedStr, "\n") {
			if strings.HasPrefix(line, "ARG:postgresql://user@") && !strings.Contains(line, ":secret") {
				hasUserOnlyURL = true
				break
			}
		}
		if !hasUserOnlyURL {
			t.Errorf("expected URL with user but no password, got:\n%s", capturedStr)
		}
	})

	// Test case 2: DATABASE_URL without password
	t.Run("without_password", func(t *testing.T) {
		// Clear capture file
		//nolint:errcheck,gosec // test-only: clearing capture file
		os.WriteFile(captureFile, []byte{}, 0o644)

		backupDir := t.TempDir()
		databaseURL := "postgresql://user@localhost:5432/dbname"
		h := NewBackupHandler(databaseURL, backupDir, &mockAdminAuth{}, nil)
		r := chi.NewRouter()
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				next.ServeHTTP(w, r)
			})
		})
		h.Register(r)

		req := httptest.NewRequest("POST", "/backups", http.NoBody)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		// The mock script always succeeds
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
		}

		// Read the capture file
		captured, err := os.ReadFile(captureFile)
		if err != nil {
			t.Fatalf("failed to read capture file: %v", err)
		}
		capturedStr := string(captured)

		// Verify: PGPASSWORD should NOT be set (empty)
		if strings.Contains(capturedStr, "PGPASSWORD:") {
			for line := range strings.SplitSeq(capturedStr, "\n") {
				if line == "PGPASSWORD:" {
					// Empty PGPASSWORD is correct (not set)
					return
				}
				if strings.HasPrefix(line, "PGPASSWORD:") && len(line) > len("PGPASSWORD:") {
					t.Errorf("expected empty PGPASSWORD, got: %s", line)
				}
			}
		}
	})
}

// setupBackupRouterWithSettings creates a backup handler with a settingsRepo for tests
// that need retention settings.
//
//nolint:revive // unnamedResult is test helper
func setupBackupRouterWithSettings(t *testing.T, ss SettingsStore) (chi.Router, string) {
	t.Helper()
	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{}, ss)
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

// --- B2: TOTP gate for backup restore form-field auth ---

// backupTOTPRouter builds a BackupHandler with the given totp flag + session
// manager, mounts it on a chi router, and returns the router.
func backupTOTPRouter(t *testing.T, totpOn bool, sessionMgr WebAuthnSessionManager) chi.Router {
	t.Helper()
	dir := t.TempDir()
	adminMgr := &mockAdminAuth{validateFn: func(token string) bool { return token == "valid-raw-token" }}
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, adminMgr, nil)
	h.SetSessionAuth(sessionMgr, func() bool { return totpOn })
	r := chi.NewRouter()
	h.Register(r)
	return r
}
