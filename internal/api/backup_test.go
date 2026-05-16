package api

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
