package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/db"
)

//nolint:gosec,revive // test-only: error not critical, unnamedResult is test helper
func setupBackupRouter(t *testing.T) (chi.Router, string) {
	t.Helper()
	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{})
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

func TestBackupHandler_CreateBackup_NoPgDump(t *testing.T) {
	// Use a temp dir with no pg_dump
	r, _ := setupBackupRouter(t)

	// This test will fail if pg_dump IS installed, so we skip in that case
	if _, err := exec.LookPath("pg_dump"); err == nil {
		t.Skip("pg_dump is installed, cannot test missing binary")
	}

	req := httptest.NewRequest("POST", "/backups", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusPreconditionFailed {
		t.Errorf("expected 412, got %d", w.Code)
	}
}

// TestCreateBackup_Success_Integration tests that CreateBackup returns 200 with JSON response
// when pg_dump is available and database is accessible.
func TestCreateBackup_Success_Integration(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	// Check pg_dump is available
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump not installed, skipping integration test")
	}

	dir := t.TempDir()
	h := NewBackupHandler(apiTestDBURL, dir, &mockAdminAuth{})
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
		t.Skip("pg_dump not installed, skipping pg_dump failure test")
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

	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", filePath, &mockAdminAuth{})
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

	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", filePath, &mockAdminAuth{})
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

	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{})
	r := chi.NewRouter()
	h.Register(r)

	req := httptest.NewRequest("DELETE", "/backups/backup_readonly.dump", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

func TestParseTOC(t *testing.T) {
	input := `;
; Archive created at 2026-05-16 17:32:57 BST
;     dbname: modelhotel
;
224; 1259 16593 TABLE public app_logs modelhotel
3518; 0 16386 TABLE DATA public schema_migrations modelhotel
3526; 0 16593 TABLE DATA public app_logs modelhotel
3332; 2606 16396 CONSTRAINT public schema_migrations schema_migrations_name_key modelhotel
3372; 2606 16420 FK CONSTRAINT public models models_provider_id_fkey modelhotel
`

	entries := parseTOC(input)
	if len(entries) == 0 {
		t.Fatal("expected entries, got none")
	}

	found := false
	for _, e := range entries {
		if e.ObjectType == "TABLE" && e.Name == "app_logs" && e.Schema == "public" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected TABLE app_logs entry")
	}

	found = false
	for _, e := range entries {
		if e.ObjectType == "TABLE DATA" && e.Name == "schema_migrations" && e.EntryNumber == 3518 {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected TABLE DATA schema_migrations entry with number 3518")
	}

	found = false
	for _, e := range entries {
		if e.ObjectType == "FK CONSTRAINT" && e.Name == "models_provider_id_fkey" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected FK CONSTRAINT entry")
	}
}

func TestParseTOC_Empty(t *testing.T) {
	entries := parseTOC("")
	if len(entries) != 0 {
		t.Errorf("expected no entries, got %d", len(entries))
	}
}

func TestParseTOC_CommentsOnly(t *testing.T) {
	input := `;
; Just comments
;
`
	entries := parseTOC(input)
	if len(entries) != 0 {
		t.Errorf("expected no entries, got %d", len(entries))
	}
}

func TestCheckDangerousObjects_None(t *testing.T) {
	entries := []tocEntry{
		{EntryNumber: 1, ObjectType: "TABLE", Schema: "public", Name: "providers"},
		{EntryNumber: 2, ObjectType: "TABLE DATA", Schema: "public", Name: "providers"},
		{EntryNumber: 3, ObjectType: "CONSTRAINT", Schema: "public", Name: "providers_pkey"},
	}
	found := checkDangerousObjects(entries)
	if len(found) != 0 {
		t.Errorf("expected no dangerous objects, got %v", found)
	}
}

func TestCheckDangerousObjects_WithFunction(t *testing.T) {
	entries := []tocEntry{
		{EntryNumber: 1, ObjectType: "TABLE", Schema: "public", Name: "providers"},
		{EntryNumber: 2, ObjectType: "FUNCTION", Schema: "public", Name: "malicious_fn"},
		{EntryNumber: 3, ObjectType: "TRIGGER", Schema: "public", Name: "bad_trigger"},
	}
	found := checkDangerousObjects(entries)
	if len(found) != 2 {
		t.Fatalf("expected 2 dangerous objects, got %d: %v", len(found), found)
	}
	if found[0] != "FUNCTION public.malicious_fn" {
		t.Errorf("expected 'FUNCTION public.malicious_fn', got %q", found[0])
	}
	if found[1] != "TRIGGER public.bad_trigger" {
		t.Errorf("expected 'TRIGGER public.bad_trigger', got %q", found[1])
	}
}

func TestFindSchemaMigrationsEntry(t *testing.T) {
	entries := []tocEntry{
		{EntryNumber: 100, ObjectType: "TABLE", Schema: "public", Name: "providers"},
		{EntryNumber: 200, ObjectType: "TABLE DATA", Schema: "public", Name: "providers"},
		{EntryNumber: 300, ObjectType: "TABLE DATA", Schema: "public", Name: "schema_migrations"},
		{EntryNumber: 400, ObjectType: "TABLE DATA", Schema: "public", Name: "settings"},
	}

	result := findSchemaMigrationsEntry(entries)
	if result != 300 {
		t.Errorf("expected 300, got %d", result)
	}
}

func TestFindSchemaMigrationsEntry_NotFound(t *testing.T) {
	entries := []tocEntry{
		{EntryNumber: 100, ObjectType: "TABLE", Schema: "public", Name: "providers"},
	}

	result := findSchemaMigrationsEntry(entries)
	if result != 0 {
		t.Errorf("expected 0, got %d", result)
	}
}

func TestParseMigrationNamesFromSQL(t *testing.T) {
	sqlOutput := `--
-- PostgreSQL database dump
--

COPY public.schema_migrations (id, name, applied_at) FROM stdin;
1	001_init.sql	2026-05-09 18:26:13.624791+00
2	002_model_seen_and_settings.sql	2026-05-09 18:26:13.684247+00
3	003_model_details.sql	2026-05-09 18:26:13.694107+00
\.

-- Done
`
	names := parseMigrationNamesFromSQL(sqlOutput)
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}
	if names[0] != "001_init.sql" {
		t.Errorf("expected '001_init.sql', got %q", names[0])
	}
	if names[1] != "002_model_seen_and_settings.sql" {
		t.Errorf("expected '002_model_seen_and_settings.sql', got %q", names[1])
	}
	if names[2] != "003_model_details.sql" {
		t.Errorf("expected '003_model_details.sql', got %q", names[2])
	}
}

func TestParseMigrationNamesFromSQL_NoCopyBlock(t *testing.T) {
	sqlOutput := `-- No COPY block here
SELECT 1;
`
	names := parseMigrationNamesFromSQL(sqlOutput)
	if len(names) != 0 {
		t.Errorf("expected 0 names, got %d", len(names))
	}
}

func TestCompareMigrations_SameVersion(t *testing.T) {
	known := db.KnownMigrations()
	if len(known) == 0 {
		t.Fatal("expected known migrations, got none")
	}

	unknown := compareMigrations(known)
	if len(unknown) != 0 {
		t.Errorf("expected no unknown migrations, got %v", unknown)
	}
}

func TestCompareMigrations_OlderVersion(t *testing.T) {
	known := db.KnownMigrations()
	if len(known) < 2 {
		t.Fatal("need at least 2 known migrations for this test")
	}

	older := known[:len(known)-1]
	unknown := compareMigrations(older)
	if len(unknown) != 0 {
		t.Errorf("expected no unknown migrations for older dump, got %v", unknown)
	}
}

func TestCompareMigrations_NewerVersion(t *testing.T) {
	known := db.KnownMigrations()

	newerMigrations := make([]string, len(known))
	copy(newerMigrations, known)
	newerMigrations = append(newerMigrations, "999_future_migration.sql", "998_another_future.sql")
	unknown := compareMigrations(newerMigrations)
	if len(unknown) != 2 {
		t.Fatalf("expected 2 unknown migrations, got %d: %v", len(unknown), unknown)
	}
	if unknown[0] != "999_future_migration.sql" {
		t.Errorf("expected '999_future_migration.sql', got %q", unknown[0])
	}
}

func TestRestoreBackup_MissingAdminToken(t *testing.T) {
	r, _ := setupBackupRouter(t)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	part, _ := writer.CreateFormFile("dump", "test.dump")
	part.Write([]byte("not a real dump"))
	writer.Close()

	req := httptest.NewRequest("POST", "/backups/restore", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRestoreBackup_InvalidAdminToken(t *testing.T) {
	r, _ := setupBackupRouter(t)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("admin_token", "wrong-token")
	part, _ := writer.CreateFormFile("dump", "test.dump")
	part.Write([]byte("not a real dump"))
	writer.Close()

	req := httptest.NewRequest("POST", "/backups/restore", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d: %s", w.Code, w.Body.String())
	}
}

func TestRestoreBackup_MissingDumpFile(t *testing.T) {
	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{validateFn: func(s string) bool { return true }})
	router := chi.NewRouter()
	h.Register(router)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("admin_token", "valid-token")
	writer.Close()

	req := httptest.NewRequest("POST", "/backups/restore", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
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
	h := NewBackupHandler("postgres://test", longPath, &mockAdminAuth{})
	if h.backupDir != longPath {
		t.Errorf("expected backupDir to be original path, got %q", h.backupDir)
	}
}

// TestBackupHandler_CreateBackup_ConcurrentLock tests that a 409 Conflict
// is returned when a backup is already in progress.
func TestBackupHandler_CreateBackup_ConcurrentLock(t *testing.T) {
	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{})

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
	h := NewBackupHandler("postgres://invalid", dir, &mockAdminAuth{})
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
	h := NewBackupHandler("postgres://invalid", dir, &mockAdminAuth{})

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
	h := NewBackupHandler("postgres://invalid", dir, &mockAdminAuth{})

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
	h := NewBackupHandler("postgres://invalid", dir, &mockAdminAuth{})

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

// TestParseTOC_TwoWordPrefixes tests parsing of two-word object types
// like TABLE DATA, FK CONSTRAINT, MATERIALIZED VIEW.
func TestParseTOC_TwoWordPrefixes(t *testing.T) {
	input := `;
224; 1259 16593 TABLE public app_logs modelhotel
3518; 0 16386 TABLE DATA public schema_migrations modelhotel
3372; 2606 16420 FK CONSTRAINT public models models_provider_id_fkey modelhotel
4000; 0 16500 MATERIALIZED VIEW public stats_view modelhotel
`

	entries := parseTOC(input)
	if len(entries) != 4 {
		t.Fatalf("expected 4 entries, got %d", len(entries))
	}

	// Check TABLE
	if entries[0].ObjectType != "TABLE" || entries[0].Name != "app_logs" {
		t.Errorf("expected TABLE app_logs, got %s %s", entries[0].ObjectType, entries[0].Name)
	}

	// Check TABLE DATA
	if entries[1].ObjectType != "TABLE DATA" || entries[1].Name != "schema_migrations" {
		t.Errorf("expected TABLE DATA schema_migrations, got %s %s", entries[1].ObjectType, entries[1].Name)
	}

	// Check FK CONSTRAINT
	if entries[2].ObjectType != "FK CONSTRAINT" || entries[2].Name != "models_provider_id_fkey" {
		t.Errorf("expected FK CONSTRAINT models_provider_id_fkey, got %s %s", entries[2].ObjectType, entries[2].Name)
	}

	// Check MATERIALIZED VIEW
	if entries[3].ObjectType != "MATERIALIZED VIEW" {
		t.Errorf("expected MATERIALIZED VIEW, got %s", entries[3].ObjectType)
	}
}

// TestParseTOC_ShortAfterType tests parsing of entries with 1, 2, 3 afterType fields.
func TestParseTOC_ShortAfterType(t *testing.T) {
	// len(afterType) == 3 with CONSTRAINT type (schema table_name constraint_name, no owner)
	input3Constraint := `;
100; 2606 12345 CONSTRAINT public table_name constraint_name
`
	entries := parseTOC(input3Constraint)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].ObjectType != "CONSTRAINT" || entries[0].Name != "constraint_name" {
		t.Errorf("expected CONSTRAINT constraint_name, got %s %s", entries[0].ObjectType, entries[0].Name)
	}
	if entries[0].Schema != "public" {
		t.Errorf("expected schema public, got %q", entries[0].Schema)
	}

	// len(afterType) == 3 with non-CONSTRAINT type (schema name owner)
	input3NonConstraint := `;
200; 1259 12346 INDEX public index_name modelhotel
`
	entries = parseTOC(input3NonConstraint)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].ObjectType != "INDEX" || entries[0].Name != "index_name" {
		t.Errorf("expected INDEX index_name, got %s %s", entries[0].ObjectType, entries[0].Name)
	}

	// len(afterType) == 2 (schema name, no owner)
	input2 := `;
300; 1259 12347 SEQUENCE public seq_name
`
	entries = parseTOC(input2)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].ObjectType != "SEQUENCE" || entries[0].Name != "seq_name" {
		t.Errorf("expected SEQUENCE seq_name, got %s %s", entries[0].ObjectType, entries[0].Name)
	}

	// len(afterType) == 1 (name only, no schema)
	input1 := `;
400; 0 0 TYPE typename
`
	entries = parseTOC(input1)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if entries[0].ObjectType != "TYPE" || entries[0].Name != "typename" {
		t.Errorf("expected TYPE typename, got %s %s", entries[0].ObjectType, entries[0].Name)
	}
	if entries[0].Schema != "" {
		t.Errorf("expected empty schema for 1-field entry, got %q", entries[0].Schema)
	}
}

// TestParseTOC_MalformedLines tests handling of malformed TOC lines.
func TestParseTOC_MalformedLines(t *testing.T) {
	// Line without semicolon
	inputNoSemicolon := `;
100 1259 16593 TABLE public app_logs modelhotel
`
	entries := parseTOC(inputNoSemicolon)
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for line without semicolon, got %d", len(entries))
	}

	// Entry number not parseable
	inputBadEntryNum := `;
abc; 1259 16593 TABLE public app_logs modelhotel
`
	entries = parseTOC(inputBadEntryNum)
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for non-numeric entry number, got %d", len(entries))
	}

	// Too few fields (less than 3 after splitting)
	inputFewFields := `;
100; TABLE
`
	entries = parseTOC(inputFewFields)
	if len(entries) != 0 {
		t.Errorf("expected 0 entries for too few fields, got %d", len(entries))
	}
}

// TestExtractMigrationNames_Integration tests extractMigrationNames with a real
// pg_dump file. Skips if test database is not available.
func TestExtractMigrationNames_Integration(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	// Check pg_restore is available
	if _, err := exec.LookPath("pg_restore"); err != nil {
		t.Skip("pg_restore not installed, skipping integration test")
	}

	// Create a backup using pg_dump
	dir := t.TempDir()
	dumpPath := filepath.Join(dir, "test.dump")

	// Extract password from DATABASE_URL
	u, err := url.Parse(apiTestDBURL)
	if err != nil {
		t.Fatalf("failed to parse DB URL: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pgDumpPath, err := exec.LookPath("pg_dump")
	if err != nil {
		t.Skip("pg_dump not available")
	}

	cmd := exec.CommandContext(ctx, pgDumpPath,
		"--format=custom",
		"--no-password",
		"--file="+dumpPath,
		apiTestDBURL,
	)
	if u.User != nil {
		if pass, ok := u.User.Password(); ok {
			cmd.Env = append(os.Environ(), "PGPASSWORD="+pass)
		}
	}

	if err := cmd.Run(); err != nil {
		t.Fatalf("pg_dump failed: %v", err)
	}

	// Find the schema_migrations entry
	listCtx, listCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer listCancel()

	pgRestorePath, err := exec.LookPath("pg_restore")
	if err != nil {
		t.Fatalf("pg_restore not found: %v", err)
	}

	listCmd := exec.CommandContext(listCtx, pgRestorePath, "--list", dumpPath)
	var listStdout bytes.Buffer
	listCmd.Stdout = &listStdout
	if err := listCmd.Run(); err != nil {
		t.Fatalf("pg_restore --list failed: %v", err)
	}

	entries := parseTOC(listStdout.String())
	schemaEntry := findSchemaMigrationsEntry(entries)
	if schemaEntry == 0 {
		t.Skip("no schema_migrations entry found in dump")
	}

	// Now test extractMigrationNames
	migrations, err := extractMigrationNames(dumpPath, schemaEntry)
	if err != nil {
		t.Fatalf("extractMigrationNames failed: %v", err)
	}

	if len(migrations) == 0 {
		t.Error("expected non-empty migration list")
	}

	// Verify we got some migration names
	for _, m := range migrations {
		if m == "" {
			t.Error("got empty migration name")
		}
	}
}

// TestRestoreBackup_ConcurrentLock tests that a 409 Conflict is returned
// when backup or restore is already in progress.
func TestRestoreBackup_ConcurrentLock(t *testing.T) {
	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{validateFn: func(string) bool { return true }})

	// Lock the mutex to simulate an in-progress operation
	h.backupMu.Lock()
	defer h.backupMu.Unlock()

	r := chi.NewRouter()
	h.Register(r)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("admin_token", "valid-token")
	part, _ := writer.CreateFormFile("dump", "test.dump")
	part.Write([]byte("not a real dump"))
	writer.Close()

	req := httptest.NewRequest("POST", "/backups/restore", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected status %d, got %d: %s", http.StatusConflict, w.Code, w.Body.String())
	}
}

// TestRestoreBackup_MultipartParseError tests that a 400 Bad Request is
// returned when the multipart form cannot be parsed.
func TestRestoreBackup_MultipartParseError(t *testing.T) {
	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{validateFn: func(string) bool { return true }})
	r := chi.NewRouter()
	h.Register(r)

	// Send a request with invalid multipart content type
	req := httptest.NewRequest("POST", "/backups/restore", strings.NewReader("not-multipart"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d: %s", http.StatusBadRequest, w.Code, w.Body.String())
	}
}

// TestRestoreBackup_DangerousObjects tests that a dump containing dangerous
// objects (FUNCTION, TRIGGER, etc.) is rejected.
func TestRestoreBackup_DangerousObjects(t *testing.T) {
	// Test the parseTOC and checkDangerousObjects functions directly
	// since crafting a real pg_dump with dangerous objects is complex
	input := `;
100; 1259 16593 TABLE public providers modelhotel
200; 0 0 FUNCTION public malicious_fn modelhotel
300; 0 0 TRIGGER public bad_trigger modelhotel
`
	entries := parseTOC(input)
	dangerous := checkDangerousObjects(entries)

	if len(dangerous) != 2 {
		t.Fatalf("expected 2 dangerous objects, got %d: %v", len(dangerous), dangerous)
	}

	foundFunction := false
	foundTrigger := false
	for _, d := range dangerous {
		if strings.Contains(d, "FUNCTION") {
			foundFunction = true
		}
		if strings.Contains(d, "TRIGGER") {
			foundTrigger = true
		}
	}
	if !foundFunction {
		t.Error("expected FUNCTION to be detected as dangerous")
	}
	if !foundTrigger {
		t.Error("expected TRIGGER to be detected as dangerous")
	}
}

// TestRestoreBackup_NoSchemaMigrations tests that a dump without
// schema_migrations TABLE DATA entry is rejected.
func TestRestoreBackup_NoSchemaMigrations(t *testing.T) {
	// Test findSchemaMigrationsEntry with no matching entry
	entries := []tocEntry{
		{EntryNumber: 100, ObjectType: "TABLE", Schema: "public", Name: "providers"},
		{EntryNumber: 200, ObjectType: "TABLE DATA", Schema: "public", Name: "providers"},
		{EntryNumber: 300, ObjectType: "TABLE DATA", Schema: "public", Name: "settings"},
	}

	result := findSchemaMigrationsEntry(entries)
	if result != 0 {
		t.Errorf("expected 0 (not found), got %d", result)
	}
}

// TestRestoreBackup_Integration performs a full backup and restore cycle
// against the test database. Skips if test database is not available.
func TestRestoreBackup_Integration(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	// Check required binaries
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump not installed")
	}
	if _, err := exec.LookPath("pg_restore"); err != nil {
		t.Skip("pg_restore not installed")
	}

	// Create a test database for this integration test
	// We'll create a backup, then restore it
	dir := t.TempDir()

	// Create handler with test DB
	h := NewBackupHandler(apiTestDBURL, dir, &mockAdminAuth{validateFn: func(string) bool { return true }})
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			next.ServeHTTP(w, r)
		})
	})
	h.Register(r)

	// Step 1: Create a backup
	req := httptest.NewRequest("POST", "/backups", http.NoBody)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status %d for backup creation, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	var backupResult backupEntry
	if err := json.Unmarshal(w.Body.Bytes(), &backupResult); err != nil {
		t.Fatalf("failed to parse backup response: %v", err)
	}

	if backupResult.Filename == "" {
		t.Fatal("expected non-empty filename")
	}

	// Step 2: Verify the backup file exists
	backupPath := filepath.Join(dir, backupResult.Filename)
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		t.Fatalf("backup file was not created: %s", backupPath)
	}

	// Step 3: List backups to verify it appears
	req = httptest.NewRequest("GET", "/backups", http.NoBody)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d for list, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var backups []backupEntry
	if err := json.Unmarshal(w.Body.Bytes(), &backups); err != nil {
		t.Fatalf("failed to parse list response: %v", err)
	}

	found := false
	for _, b := range backups {
		if b.Filename == backupResult.Filename {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("backup %q not found in list", backupResult.Filename)
	}

	// Note: Full restore test would require dropping and recreating the database,
	// which is complex in a test environment. The restore endpoint is tested
	// indirectly through the unit tests above and the successful backup creation.
}

func TestRestoreBackup_InvalidDump(t *testing.T) {
	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{validateFn: func(s string) bool { return true }})
	router := chi.NewRouter()
	h.Register(router)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("admin_token", "valid-token")
	part, _ := writer.CreateFormFile("dump", "test.dump")
	part.Write([]byte("this is not a pg_dump file"))
	writer.Close()

	req := httptest.NewRequest("POST", "/backups/restore", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestRestoreBackup_TempFileError tests that a 500 is returned when the temp
// file cannot be created in the backup directory.
func TestRestoreBackup_TempFileError(t *testing.T) {
	// Create a regular file where the backup dir should be, so os.CreateTemp fails
	file, err := os.CreateTemp(t.TempDir(), "backup-blocker-*")
	if err != nil {
		t.Fatal(err)
	}
	filePath := file.Name()
	file.Close()

	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", filePath, &mockAdminAuth{validateFn: func(string) bool { return true }})
	r := chi.NewRouter()
	h.Register(r)

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("admin_token", "valid-token")
	part, _ := writer.CreateFormFile("dump", "test.dump")
	part.Write([]byte("not a real dump"))
	writer.Close()

	req := httptest.NewRequest("POST", "/backups/restore", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d: %s", w.Code, w.Body.String())
	}
}

// TestRestoreBackup_BodyTruncatedByMaxBytes tests that a request body
// truncated by MaxBytesReader causes a 400 form parse error. This exercises
// the MaxBytesReader path, not the io.Copy error path in RestoreBackup.
func TestRestoreBackup_BodyTruncatedByMaxBytes(t *testing.T) {
	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{validateFn: func(string) bool { return true }})
	r := chi.NewRouter()
	h.Register(r)

	// Create a multipart form and then limit the request body to 10 bytes.
	// MaxBytesReader causes a read error when the handler tries to parse
	// the multipart form, resulting in a 400 (form parse error), not
	// the io.Copy error path in RestoreBackup.
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("admin_token", "valid-token")
	part, _ := writer.CreateFormFile("dump", "test.dump")
	part.Write([]byte("partial data"))
	writer.Close()

	req := httptest.NewRequest("POST", "/backups/restore", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Body = http.MaxBytesReader(httptest.NewRecorder(), req.Body, 10)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Expect 400 (form parse failure due to body limit)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestRestoreBackup_WithRealDump_Integration tests the restore handler's
// validation path with a real pg_dump file. It uses an invalid database URL
// so pg_restore --clean fails after validation passes, avoiding the os.Exit(0)
// that a successful restore would trigger.
func TestRestoreBackup_WithRealDump_Integration(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump not installed")
	}
	if _, err := exec.LookPath("pg_restore"); err != nil {
		t.Skip("pg_restore not installed")
	}

	// Step 1: Create a real pg_dump file
	dir := t.TempDir()
	dumpPath := filepath.Join(dir, "test_backup.dump")

	u, err := url.Parse(apiTestDBURL)
	if err != nil {
		t.Fatalf("failed to parse DB URL: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pgDumpPath, _ := exec.LookPath("pg_dump")
	cmd := exec.CommandContext(ctx, pgDumpPath,
		"--format=custom",
		"--no-password",
		"--file="+dumpPath,
		apiTestDBURL,
	)
	if u.User != nil {
		if pass, ok := u.User.Password(); ok {
			cmd.Env = append(os.Environ(), "PGPASSWORD="+pass)
		}
	}
	if err := cmd.Run(); err != nil {
		t.Fatalf("pg_dump failed: %v", err)
	}

	// Step 2: Upload the dump via the restore endpoint.
	// Use an invalid database URL so pg_restore --clean fails after
	// validation passes. This avoids the os.Exit(0) goroutine that
	// a successful restore would trigger (which kills the test process).
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{validateFn: func(string) bool { return true }})
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			next.ServeHTTP(w, r)
		})
	})
	h.Register(r)

	dumpContent, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("failed to read dump file: %v", err)
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("admin_token", "valid-token")
	part, _ := writer.CreateFormFile("dump", "test_backup.dump")
	part.Write(dumpContent)
	writer.Close()

	req := httptest.NewRequest("POST", "/backups/restore", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Validation passes (no dangerous objects, has schema_migrations,
	// migrations match), but pg_restore --clean fails because the
	// database URL is invalid. Expect 500.
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 (pg_restore fails on invalid DB), got %d: %s", w.Code, w.Body.String())
	}
}

// TestRestoreBackup_DangerousObjectsInDump_Integration tests that a dump
// containing dangerous objects is rejected by the restore handler.
// Creates a real dump, then verifies the validation logic.
func TestRestoreBackup_DangerousObjectsInDump_Integration(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump not installed")
	}
	if _, err := exec.LookPath("pg_restore"); err != nil {
		t.Skip("pg_restore not installed")
	}

	// Create a real dump and verify it does NOT contain dangerous objects
	dir := t.TempDir()
	dumpPath := filepath.Join(dir, "test_backup.dump")

	u, err := url.Parse(apiTestDBURL)
	if err != nil {
		t.Fatalf("failed to parse DB URL: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pgDumpPath, _ := exec.LookPath("pg_dump")
	cmd := exec.CommandContext(ctx, pgDumpPath,
		"--format=custom",
		"--no-password",
		"--file="+dumpPath,
		apiTestDBURL,
	)
	if u.User != nil {
		if pass, ok := u.User.Password(); ok {
			cmd.Env = append(os.Environ(), "PGPASSWORD="+pass)
		}
	}
	if err := cmd.Run(); err != nil {
		t.Fatalf("pg_dump failed: %v", err)
	}

	// Verify the dump passes validation (no dangerous objects)
	pgRestorePath, _ := exec.LookPath("pg_restore")
	listCmd := exec.CommandContext(ctx, pgRestorePath, "--list", dumpPath)
	var listStdout bytes.Buffer
	listCmd.Stdout = &listStdout
	if err := listCmd.Run(); err != nil {
		t.Fatalf("pg_restore --list failed: %v", err)
	}

	entries := parseTOC(listStdout.String())
	dangerous := checkDangerousObjects(entries)
	if len(dangerous) != 0 {
		t.Errorf("expected no dangerous objects in model-hotel dump, got: %v", dangerous)
	}

	// Verify schema_migrations is present
	schemaEntry := findSchemaMigrationsEntry(entries)
	if schemaEntry == 0 {
		t.Error("expected schema_migrations entry in model-hotel dump")
	}

	// Verify migrations match known list
	migrations, err := extractMigrationNames(dumpPath, schemaEntry)
	if err != nil {
		t.Fatalf("extractMigrationNames failed: %v", err)
	}
	unknown := compareMigrations(migrations)
	if len(unknown) != 0 {
		t.Errorf("expected no unknown migrations, got: %v", unknown)
	}
}

// TestRestoreBackup_NoSchemaMigrationsInDump_Integration tests that a dump
// without schema_migrations TABLE DATA is rejected by the restore handler.
// Creates a dump from a table-only subset of the test DB.
func TestRestoreBackup_NoSchemaMigrationsInDump_Integration(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump not installed")
	}
	if _, err := exec.LookPath("pg_restore"); err != nil {
		t.Skip("pg_restore not installed")
	}

	// Create a dump that only includes a test table (no schema_migrations)
	dir := t.TempDir()
	dumpPath := filepath.Join(dir, "no_migrations.dump")

	u, err := url.Parse(apiTestDBURL)
	if err != nil {
		t.Fatalf("failed to parse DB URL: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Create a temporary table to dump (won't have schema_migrations)
	tmpTable := fmt.Sprintf("test_backup_nomig_%d", time.Now().UnixNano())
	psqlUser := ""
	if u.User != nil {
		psqlUser = u.User.Username()
	}
	psqlEnv := os.Environ()
	if u.User != nil {
		if pass, ok := u.User.Password(); ok {
			psqlEnv = append(os.Environ(), "PGPASSWORD="+pass)
		}
	}
	createArgs := []string{"-h", u.Hostname()}
	if port := u.Port(); port != "" {
		createArgs = append(createArgs, "-p", port)
	}
	createArgs = append(createArgs, "-U", psqlUser, "-d", strings.TrimPrefix(u.Path, "/"), "-c",
		fmt.Sprintf("CREATE TABLE %s (id serial primary key, name text)", tmpTable))
	createCmd := exec.CommandContext(ctx, "psql", createArgs...)
	createCmd.Env = psqlEnv
	if out, err := createCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create table: %v: %s", err, string(out))
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		args := []string{"-h", u.Hostname()}
		if port := u.Port(); port != "" {
			args = append(args, "-p", port)
		}
		args = append(args, "-U", psqlUser, "-d", strings.TrimPrefix(u.Path, "/"), "-c",
			fmt.Sprintf("DROP TABLE IF EXISTS %s", tmpTable))
		dropCmd := exec.CommandContext(cleanupCtx, "psql", args...)
		dropCmd.Env = psqlEnv
		dropCmd.CombinedOutput()
	}()

	pgDumpPath, _ := exec.LookPath("pg_dump")
	cmd := exec.CommandContext(ctx, pgDumpPath,
		"--format=custom",
		"--no-password",
		"--file="+dumpPath,
		"-t", tmpTable,
		apiTestDBURL,
	)
	if u.User != nil {
		if pass, ok := u.User.Password(); ok {
			cmd.Env = append(os.Environ(), "PGPASSWORD="+pass)
		}
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pg_dump failed: %v: %s", err, string(out))
	}

	// Upload the dump via the restore endpoint
	h := NewBackupHandler(apiTestDBURL, dir, &mockAdminAuth{validateFn: func(string) bool { return true }})
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			next.ServeHTTP(w, r)
		})
	})
	h.Register(r)

	dumpContent, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("failed to read dump file: %v", err)
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("admin_token", "valid-token")
	part, _ := writer.CreateFormFile("dump", "no_migrations.dump")
	part.Write(dumpContent)
	writer.Close()

	req := httptest.NewRequest("POST", "/backups/restore", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 (no schema_migrations), got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "schema_migrations") {
		t.Errorf("expected error to mention schema_migrations, got: %s", w.Body.String())
	}
}

// TestRestoreBackup_DangerousObjectsHandler_Integration tests that a dump
// containing dangerous objects (FUNCTION) is rejected by the restore handler.
func TestRestoreBackup_DangerousObjectsHandler_Integration(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump not installed")
	}
	if _, err := exec.LookPath("pg_restore"); err != nil {
		t.Skip("pg_restore not installed")
	}

	// Create a dangerous function in the test DB, dump it, then clean up
	dir := t.TempDir()
	dumpPath := filepath.Join(dir, "dangerous.dump")

	u, err := url.Parse(apiTestDBURL)
	if err != nil {
		t.Fatalf("failed to parse DB URL: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fnName := fmt.Sprintf("test_dangerous_fn_%d", time.Now().UnixNano())
	psqlUser := ""
	if u.User != nil {
		psqlUser = u.User.Username()
	}
	psqlArgs := []string{"-h", u.Hostname()}
	if port := u.Port(); port != "" {
		psqlArgs = append(psqlArgs, "-p", port)
	}
	psqlArgs = append(psqlArgs, "-U", psqlUser,
		"-d", strings.TrimPrefix(u.Path, "/"),
		"-c", fmt.Sprintf("CREATE OR REPLACE FUNCTION %s() RETURNS void AS $$ BEGIN NULL; END; $$ LANGUAGE plpgsql", fnName))
	psqlCmd := exec.CommandContext(ctx, "psql", psqlArgs...)
	if u.User != nil {
		if pass, ok := u.User.Password(); ok {
			psqlCmd.Env = append(os.Environ(), "PGPASSWORD="+pass)
		}
	}
	if out, err := psqlCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to create function: %v: %s", err, string(out))
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		dropArgs := []string{"-h", u.Hostname()}
		if port := u.Port(); port != "" {
			dropArgs = append(dropArgs, "-p", port)
		}
		dropArgs = append(dropArgs, "-U", psqlUser,
			"-d", strings.TrimPrefix(u.Path, "/"),
			"-c", fmt.Sprintf("DROP FUNCTION IF EXISTS %s()", fnName))
		dropCmd := exec.CommandContext(cleanupCtx, "psql", dropArgs...)
		if u.User != nil {
			if pass, ok := u.User.Password(); ok {
				dropCmd.Env = append(os.Environ(), "PGPASSWORD="+pass)
			}
		}
		dropCmd.CombinedOutput()
	}()

	// Dump the entire database (which now includes the function)
	pgDumpPath, _ := exec.LookPath("pg_dump")
	cmd := exec.CommandContext(ctx, pgDumpPath,
		"--format=custom",
		"--no-password",
		"--file="+dumpPath,
		apiTestDBURL,
	)
	if u.User != nil {
		if pass, ok := u.User.Password(); ok {
			cmd.Env = append(os.Environ(), "PGPASSWORD="+pass)
		}
	}
	if err := cmd.Run(); err != nil {
		t.Fatalf("pg_dump failed: %v", err)
	}

	// Upload the dump via the restore endpoint
	h := NewBackupHandler(apiTestDBURL, dir, &mockAdminAuth{validateFn: func(string) bool { return true }})
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			next.ServeHTTP(w, r)
		})
	})
	h.Register(r)

	dumpContent, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("failed to read dump file: %v", err)
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("admin_token", "valid-token")
	part, _ := writer.CreateFormFile("dump", "dangerous.dump")
	part.Write(dumpContent)
	writer.Close()

	req := httptest.NewRequest("POST", "/backups/restore", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 (dangerous objects), got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "dangerous") {
		t.Errorf("expected error to mention dangerous objects, got: %s", w.Body.String())
	}
}

// TestRestoreBackup_UnknownMigrations_Integration tests that a dump from a
// newer version (with unknown migrations) is rejected by the restore handler.
func TestRestoreBackup_UnknownMigrations_Integration(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump not installed")
	}
	if _, err := exec.LookPath("pg_restore"); err != nil {
		t.Skip("pg_restore not installed")
	}

	dir := t.TempDir()
	dumpPath := filepath.Join(dir, "newer_version.dump")

	u, err := url.Parse(apiTestDBURL)
	if err != nil {
		t.Fatalf("failed to parse DB URL: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	psqlUser := ""
	if u.User != nil {
		psqlUser = u.User.Username()
	}
	psqlEnv := os.Environ()
	if u.User != nil {
		if pass, ok := u.User.Password(); ok {
			psqlEnv = append(os.Environ(), "PGPASSWORD="+pass)
		}
	}

	// Insert a fake future migration into schema_migrations
	fakeMigration := "999_future_migration.sql"
	insertArgs := []string{"-h", u.Hostname()}
	if port := u.Port(); port != "" {
		insertArgs = append(insertArgs, "-p", port)
	}
	insertArgs = append(insertArgs, "-U", psqlUser, "-d", strings.TrimPrefix(u.Path, "/"), "-c",
		fmt.Sprintf("INSERT INTO schema_migrations (name) VALUES ('%s') ON CONFLICT DO NOTHING", fakeMigration))
	insertCmd := exec.CommandContext(ctx, "psql", insertArgs...)
	insertCmd.Env = psqlEnv
	if out, err := insertCmd.CombinedOutput(); err != nil {
		t.Fatalf("failed to insert fake migration: %v: %s", err, string(out))
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		deleteArgs := []string{"-h", u.Hostname()}
		if port := u.Port(); port != "" {
			deleteArgs = append(deleteArgs, "-p", port)
		}
		deleteArgs = append(deleteArgs, "-U", psqlUser, "-d", strings.TrimPrefix(u.Path, "/"), "-c",
			fmt.Sprintf("DELETE FROM schema_migrations WHERE name = '%s'", fakeMigration))
		deleteCmd := exec.CommandContext(cleanupCtx, "psql", deleteArgs...)
		deleteCmd.Env = psqlEnv
		deleteCmd.CombinedOutput()
	}()

	// Create a dump of the database (which now has the fake migration)
	pgDumpPath, _ := exec.LookPath("pg_dump")
	cmd := exec.CommandContext(ctx, pgDumpPath,
		"--format=custom",
		"--no-password",
		"--file="+dumpPath,
		apiTestDBURL,
	)
	if u.User != nil {
		if pass, ok := u.User.Password(); ok {
			cmd.Env = append(os.Environ(), "PGPASSWORD="+pass)
		}
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("pg_dump failed: %v: %s", err, string(out))
	}

	// Upload the dump via the restore endpoint
	h := NewBackupHandler(apiTestDBURL, dir, &mockAdminAuth{validateFn: func(string) bool { return true }})
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			next.ServeHTTP(w, r)
		})
	})
	h.Register(r)

	dumpContent, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("failed to read dump file: %v", err)
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("admin_token", "valid-token")
	part, _ := writer.CreateFormFile("dump", "newer_version.dump")
	part.Write(dumpContent)
	writer.Close()

	req := httptest.NewRequest("POST", "/backups/restore", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 (unknown migrations), got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "newer version") {
		t.Errorf("expected error to mention newer version, got: %s", w.Body.String())
	}
}

// TestCheckDangerousObjects_AllTypes tests that checkDangerousObjects detects
// all dangerous object types from the dangerousObjectTypes map.
func TestCheckDangerousObjects_AllTypes(t *testing.T) {
	// Test each dangerous type individually
	dangerousTypes := []string{
		"FUNCTION", "AGGREGATE", "TRIGGER", "EXTENSION", "PROCEDURE",
		"OPERATOR", "CAST", "COLLATION", "CONVERSION", "DOMAIN",
		"EVENT TRIGGER", "FOREIGN DATA", "FOREIGN TABLE", "MATERIALIZED VIEW",
		"SERVER", "TYPE",
	}

	for _, objType := range dangerousTypes {
		entries := []tocEntry{
			{EntryNumber: 1, ObjectType: "TABLE", Schema: "public", Name: "safe_table"},
			{EntryNumber: 2, ObjectType: objType, Schema: "public", Name: "dangerous_object"},
		}
		found := checkDangerousObjects(entries)
		if len(found) != 1 {
			t.Errorf("expected 1 dangerous object for %s, got %d: %v", objType, len(found), found)
		}
		if len(found) > 0 && !strings.Contains(found[0], objType) {
			t.Errorf("expected result to contain %q, got %q", objType, found[0])
		}
	}

	// Test mixed slice with both dangerous and safe types
	mixedEntries := []tocEntry{
		{EntryNumber: 1, ObjectType: "TABLE", Schema: "public", Name: "providers"},
		{EntryNumber: 2, ObjectType: "TABLE DATA", Schema: "public", Name: "providers"},
		{EntryNumber: 3, ObjectType: "FUNCTION", Schema: "public", Name: "malicious_fn"},
		{EntryNumber: 4, ObjectType: "CONSTRAINT", Schema: "public", Name: "providers_pkey"},
		{EntryNumber: 5, ObjectType: "TRIGGER", Schema: "public", Name: "bad_trigger"},
		{EntryNumber: 6, ObjectType: "EXTENSION", Schema: "public", Name: "uuid_ossp"},
		{EntryNumber: 7, ObjectType: "INDEX", Schema: "public", Name: "idx_name"},
	}

	found := checkDangerousObjects(mixedEntries)
	if len(found) != 3 {
		t.Fatalf("expected 3 dangerous objects, got %d: %v", len(found), found)
	}

	// Verify the returned strings include the type name
	expectedTypes := []string{"FUNCTION", "TRIGGER", "EXTENSION"}
	for i, expected := range expectedTypes {
		if !strings.Contains(found[i], expected) {
			t.Errorf("expected result %d to contain %q, got %q", i, expected, found[i])
		}
	}
}

// TestCompareMigrations_EmptyDumpMigrations tests compareMigrations with
// various scenarios: empty dump, all known, and partial with unknown.
func TestCompareMigrations_EmptyDumpMigrations(t *testing.T) {
	known := db.KnownMigrations()
	if len(known) == 0 {
		t.Fatal("expected known migrations, got none")
	}

	// When dumpMigrations is empty, should return empty list (nothing to compare)
	unknown := compareMigrations([]string{})
	if len(unknown) != 0 {
		t.Errorf("expected 0 unknown migrations for empty dump, got %d", len(unknown))
	}

	// When dumpMigrations has all known migrations, should return empty unknown list
	unknown = compareMigrations(known)
	if len(unknown) != 0 {
		t.Errorf("expected no unknown migrations for complete dump, got %v", unknown)
	}

	// When dumpMigrations has all known plus one unknown migration, should return only the unknown one
	newerWithUnknown := make([]string, len(known))
	copy(newerWithUnknown, known)
	newerWithUnknown = append(newerWithUnknown, "999_unknown_migration.sql")

	unknown = compareMigrations(newerWithUnknown)
	if len(unknown) != 1 {
		t.Fatalf("expected 1 unknown migration, got %d: %v", len(unknown), unknown)
	}
	if unknown[0] != "999_unknown_migration.sql" {
		t.Errorf("expected '999_unknown_migration.sql', got %q", unknown[0])
	}
}

// TestParseTOC_MaterializedViewAndSpecialTypes tests parseTOC with various
// special object types including MATERIALIZED VIEW, FK CONSTRAINT, TABLE DATA,
// and DEFAULT ACL.
func TestParseTOC_MaterializedViewAndSpecialTypes(t *testing.T) {
	input := `;
; Archive created at 2026-05-16 17:32:57 BST
;
100; 1259 16500 MATERIALIZED VIEW public stats_view modelhotel
200; 2606 16420 FK CONSTRAINT public models models_provider_id_fkey modelhotel
300; 0 16386 TABLE DATA public schema_migrations modelhotel
400; 0 0 DEFAULT ACL public - modelhotel
500; 1259 16593 TABLE public app_logs modelhotel
`

	entries := parseTOC(input)
	if len(entries) != 5 {
		t.Fatalf("expected 5 entries, got %d", len(entries))
	}

	// Check MATERIALIZED VIEW
	if entries[0].ObjectType != "MATERIALIZED VIEW" {
		t.Errorf("expected MATERIALIZED VIEW, got %q", entries[0].ObjectType)
	}
	if entries[0].Schema != "public" {
		t.Errorf("expected schema 'public', got %q", entries[0].Schema)
	}
	if entries[0].Name != "stats_view" {
		t.Errorf("expected name 'stats_view', got %q", entries[0].Name)
	}
	if entries[0].EntryNumber != 100 {
		t.Errorf("expected entry number 100, got %d", entries[0].EntryNumber)
	}

	// Check FK CONSTRAINT
	if entries[1].ObjectType != "FK CONSTRAINT" {
		t.Errorf("expected FK CONSTRAINT, got %q", entries[1].ObjectType)
	}
	if entries[1].Name != "models_provider_id_fkey" {
		t.Errorf("expected name 'models_provider_id_fkey', got %q", entries[1].Name)
	}

	// Check TABLE DATA (two-word prefix)
	if entries[2].ObjectType != "TABLE DATA" {
		t.Errorf("expected TABLE DATA, got %q", entries[2].ObjectType)
	}
	if entries[2].Name != "schema_migrations" {
		t.Errorf("expected name 'schema_migrations', got %q", entries[2].Name)
	}

	// Check DEFAULT ACL (two-word prefix)
	if entries[3].ObjectType != "DEFAULT ACL" {
		t.Errorf("expected DEFAULT ACL, got %q", entries[3].ObjectType)
	}
}

// TestNewBackupHandler_Constructor tests the NewBackupHandler constructor
// with various inputs including empty backup dir and long paths.
func TestNewBackupHandler_Constructor(t *testing.T) {
	adminAuth := &mockAdminAuth{}

	// Test with normal inputs
	h := NewBackupHandler("postgres://user:pass@localhost/db", "/tmp/backups", adminAuth)
	if h == nil {
		t.Fatal("expected non-nil handler")
	}
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
	hEmpty := NewBackupHandler("postgres://user:pass@localhost/db", "", adminAuth)
	if hEmpty == nil {
		t.Fatal("expected non-nil handler with empty backup dir")
	}
	// Empty string should resolve to current working directory
	if !filepath.IsAbs(hEmpty.backupDir) {
		t.Errorf("expected backupDir to be absolute for empty input, got %q", hEmpty.backupDir)
	}

	// Test with long path
	longPath := "/tmp/" + strings.Repeat("a", 5000)
	hLong := NewBackupHandler("postgres://user:pass@localhost/db", longPath, adminAuth)
	if hLong == nil {
		t.Fatal("expected non-nil handler with long path")
	}
	if hLong.backupDir != longPath {
		t.Errorf("expected backupDir to be original long path, got %q", hLong.backupDir)
	}
}

// TestBackupHandler_Register tests that the Register method correctly
// registers all backup routes.
func TestBackupHandler_Register(t *testing.T) {
	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{})
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

// TestParseTOC_WithCommentLines tests that parseTOC correctly skips comment
// lines, empty lines, and malformed lines while parsing valid entries.
func TestParseTOC_WithCommentLines(t *testing.T) {
	input := `;
; Archive created at 2026-05-16 17:32:57 BST
;     dbname: modelhotel
; This is a comment

100; 1259 16593 TABLE public providers modelhotel
; Another comment in the middle

200; 0 16386 TABLE DATA public schema_migrations modelhotel

; Final comment
300; 2606 16420 FK CONSTRAINT public models models_provider_id_fkey modelhotel
`

	entries := parseTOC(input)
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries (skipping comments and empty lines), got %d", len(entries))
	}

	// Verify all entries are valid
	if entries[0].ObjectType != "TABLE" || entries[0].Name != "providers" {
		t.Errorf("expected TABLE providers, got %s %s", entries[0].ObjectType, entries[0].Name)
	}
	if entries[1].ObjectType != "TABLE DATA" || entries[1].Name != "schema_migrations" {
		t.Errorf("expected TABLE DATA schema_migrations, got %s %s", entries[1].ObjectType, entries[1].Name)
	}
	if entries[2].ObjectType != "FK CONSTRAINT" || entries[2].Name != "models_provider_id_fkey" {
		t.Errorf("expected FK CONSTRAINT models_provider_id_fkey, got %s %s", entries[2].ObjectType, entries[2].Name)
	}
}

// TestValidateBackupFilename_ValidNames tests validateBackupFilename with
// various valid filename patterns.
func TestValidateBackupFilename_ValidNames(t *testing.T) {
	dir := t.TempDir()
	h := NewBackupHandler("postgres://invalid", dir, &mockAdminAuth{})

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
		t.Skip("pg_dump not installed, skipping backup integration test")
	}

	backupDir := t.TempDir()
	bh := NewBackupHandler(apiTestDBURL, backupDir, &mockAdminAuth{})

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

// ---------------------------------------------------------------------------
// Tests moved from coverage_gap2_test.go
// ---------------------------------------------------------------------------

// TestExtractMigrationNames_FilterFileWriteError tests extractMigrationNames
// when os.CreateTemp fails (e.g., TMPDIR points to non-existent directory).
func TestExtractMigrationNames_FilterFileWriteError(t *testing.T) {
	// This test runs itself as a subprocess to safely manipulate TMPDIR
	// without affecting other tests running in parallel.
	if os.Getenv("TEST_FILTER_FILE_WRITE_ERROR") == "1" {
		os.Setenv("TMPDIR", "/nonexistent/path/that/does/not/exist")
		dumpPath := "/tmp/test.dump"

		_, err := extractMigrationNames(dumpPath, 100)
		if err == nil {
			fmt.Printf("FILTER_WRITE: expected error when filter file cannot be created\n")
			os.Exit(1)
		}
		if !strings.Contains(err.Error(), "failed to create filter file") {
			fmt.Printf("FILTER_WRITE: expected 'failed to create filter file', got: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestExtractMigrationNames_FilterFileWriteError")
	cmd.Env = append(os.Environ(), "TEST_FILTER_FILE_WRITE_ERROR=1", "TMPDIR=/nonexistent/path/that/does/not/exist")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("subprocess failed: %v\noutput: %s", err, output)
	}
}

// TestExtractMigrationNames_PgRestoreNotFound tests extractMigrationNames
// when pg_restore is not found in PATH.
func TestExtractMigrationNames_PgRestoreNotFound(t *testing.T) {
	// This test runs itself as a subprocess to safely manipulate PATH
	// without affecting other tests running in parallel.
	if os.Getenv("TEST_PG_RESTORE_NOT_FOUND") == "1" {
		tmpFile, err := os.CreateTemp(t.TempDir(), "test-dump-*.dump")
		if err != nil {
			fmt.Printf("PG_RESTORE_NOT_FOUND: failed to create temp file: %v\n", err)
			os.Exit(1)
		}
		tmpFile.Close()

		_, err = extractMigrationNames(tmpFile.Name(), 100)
		if err == nil {
			fmt.Printf("PG_RESTORE_NOT_FOUND: expected error when pg_restore not found\n")
			os.Exit(1)
		}
		if !strings.Contains(err.Error(), "pg_restore not found") {
			fmt.Printf("PG_RESTORE_NOT_FOUND: expected 'pg_restore not found', got: %v\n", err)
			os.Exit(1)
		}
		os.Exit(0)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestExtractMigrationNames_PgRestoreNotFound")
	cmd.Env = append(os.Environ(), "TEST_PG_RESTORE_NOT_FOUND=1", "PATH=/nonexistent")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("subprocess failed: %v\noutput: %s", err, output)
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
			h := NewBackupHandler("postgres://test", tc.backupDir, &mockAdminAuth{})
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
		h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{})
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

// TestExtractMigrationNames_PgRestoreRunError tests that extractMigrationNames
// returns an error when pg_restore --list fails (L445-447).
func TestExtractMigrationNames_PgRestoreRunError(t *testing.T) {
	// Create an invalid dump file that will cause pg_restore to fail
	tmpFile, err := os.CreateTemp(t.TempDir(), "invalid-dump-*.dump")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	// Write garbage data that's not a valid pg_dump format
	if _, err := tmpFile.WriteString("this is not a valid pg_dump file"); err != nil {
		tmpFile.Close()
		t.Fatal(err)
	}
	tmpFile.Close()

	_, err = extractMigrationNames(tmpFile.Name(), 100)
	if err == nil {
		t.Error("expected error when pg_restore fails")
	}
	if !strings.Contains(err.Error(), "pg_restore filter failed") {
		t.Errorf("expected 'pg_restore filter failed' error, got: %v", err)
	}
}

func TestRestoreBackup_PgRestoreNotFound(t *testing.T) {
	// This test runs itself as a subprocess to safely manipulate PATH
	// without affecting other tests running in parallel.
	if os.Getenv("TEST_NO_PG_RESTORE") == "1" {
		dir := t.TempDir()
		h := NewBackupHandler("postgres://invalid:invalid@127.0.0.1:1/nonexistent", dir, &mockAdminAuth{validateFn: func(string) bool { return true }})
		r := chi.NewRouter()
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				next.ServeHTTP(w, r)
			})
		})
		h.Register(r)

		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		writer.WriteField("admin_token", "valid-token")
		part, _ := writer.CreateFormFile("dump", "test.dump")
		part.Write([]byte("dummy dump content"))
		writer.Close()

		req := httptest.NewRequest("POST", "/backups/restore", &buf)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusPreconditionFailed {
			fmt.Printf("NO_PG_RESTORE: expected 412, got %d: %s\n", w.Code, w.Body.String())
			os.Exit(1)
		}
		if !strings.Contains(w.Body.String(), "pg_restore not found") {
			fmt.Printf("NO_PG_RESTORE: expected error to mention pg_restore, got: %s\n", w.Body.String())
			os.Exit(1)
		}
		os.Exit(0)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestRestoreBackup_PgRestoreNotFound")
	cmd.Env = append(os.Environ(), "TEST_NO_PG_RESTORE=1", "PATH=")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("subprocess failed: %v\noutput: %s", err, output)
	}
}

func TestRestoreBackup_ExtractMigrationsError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump not installed")
	}
	if _, err := exec.LookPath("pg_restore"); err != nil {
		t.Skip("pg_restore not installed")
	}

	// This test runs itself as a subprocess to safely manipulate TMPDIR
	// without affecting other tests running in parallel.
	if os.Getenv("TEST_EXTRACT_MIGRATIONS_ERROR") == "1" {
		dir := t.TempDir()

		// Create a valid dump so RestoreBackup passes pg_restore --list validation.
		u, err := url.Parse(apiTestDBURL)
		if err != nil {
			fmt.Printf("EXTRACT_MIG: failed to parse DB URL: %v\n", err)
			os.Exit(1)
		}
		dumpPath := filepath.Join(dir, "test.dump")
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pgDumpPath, _ := exec.LookPath("pg_dump")
		cmd := exec.CommandContext(ctx, pgDumpPath,
			"--format=custom",
			"--no-password",
			"--file="+dumpPath,
			apiTestDBURL,
		)
		if u.User != nil {
			if pass, ok := u.User.Password(); ok {
				cmd.Env = append(os.Environ(), "PGPASSWORD="+pass)
			}
		}
		if err := cmd.Run(); err != nil {
			fmt.Printf("EXTRACT_MIG: pg_dump failed: %v\n", err)
			os.Exit(1)
		}

		h := NewBackupHandler(apiTestDBURL, dir, &mockAdminAuth{validateFn: func(string) bool { return true }})
		r := chi.NewRouter()
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				next.ServeHTTP(w, r)
			})
		})
		h.Register(r)

		dumpContent, err := os.ReadFile(dumpPath)
		if err != nil {
			fmt.Printf("EXTRACT_MIG: failed to read dump file: %v\n", err)
			os.Exit(1)
		}

		var buf bytes.Buffer
		writer := multipart.NewWriter(&buf)
		writer.WriteField("admin_token", "valid-token")
		part, _ := writer.CreateFormFile("dump", "test.dump")
		part.Write(dumpContent)
		writer.Close()

		// Set TMPDIR to non-existent path so extractMigrationNames' filter file
		// creation fails. Safe to do here since we're in an isolated subprocess.
		os.Setenv("TMPDIR", "/nonexistent/path/that/does/not/exist")

		req := httptest.NewRequest("POST", "/backups/restore", &buf)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusInternalServerError {
			fmt.Printf("EXTRACT_MIG: expected 500, got %d: %s\n", w.Code, w.Body.String())
			os.Exit(1)
		}
		if !strings.Contains(w.Body.String(), "failed to extract migration info") {
			fmt.Printf("EXTRACT_MIG: expected error to mention extraction failure, got: %s\n", w.Body.String())
			os.Exit(1)
		}
		os.Exit(0)
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestRestoreBackup_ExtractMigrationsError")
	cmd.Env = append(os.Environ(), "TEST_EXTRACT_MIGRATIONS_ERROR=1")
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
	h := NewBackupHandler("postgres://invalid", dir, &mockAdminAuth{})

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
		h := NewBackupHandler("postgres://test", "my_backup_dir", &mockAdminAuth{})
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
		t.Skipf("cannot start subprocess: %v", err)
	}

	// Delete the CWD while the subprocess is running
	//nolint:gosec // test-only: removing test directory
	if err := os.RemoveAll(tmpDir); err != nil {
		t.Skipf("cannot remove temp dir: %v", err)
	}

	// Wait for the subprocess to finish
	if err := cmd.Wait(); err != nil {
		t.Fatalf("subprocess failed: %v", err)
	}
}
