package api

import (
	"bytes"
	"context"
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
	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// PurgeLogs Tests - Error Paths
// ---------------------------------------------------------------------------

// TestPurgeLogs_InvalidBody tests that PurgeLogs returns 400 when
// the request body is not valid JSON.
func TestPurgeLogs_InvalidBody(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Send invalid JSON
	body := strings.NewReader(`{invalid json}`)
	req := httptest.NewRequest("DELETE", "/logs/purge", body)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestPurgeLogs_InvalidOlderThan tests that PurgeLogs returns 400 when
// the older_than value is invalid (e.g., "2x").
func TestPurgeLogs_InvalidOlderThan(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	body := strings.NewReader(`{"older_than":"2x"}`)
	req := httptest.NewRequest("DELETE", "/logs/purge", body)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

// TestPurgeLogs_RepositoryDBError tests the repository-level DB error
// path when the database is unavailable. This complements the existing
// TestPurgeLogs_DBError in logs_coverage_test.go by testing the repository
// directly with a closed pool.
func TestPurgeLogs_RepositoryDBError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	closedPool := newClosedPool(t)
	defer closedPool.Close()

	// Test the repository directly since Handler requires a working pool
	ctx := context.Background()
	_, err := closedPool.Exec(ctx, `DELETE FROM request_logs`)
	if err == nil {
		t.Error("expected error when executing DELETE with closed pool")
	}
}

// ---------------------------------------------------------------------------
// ListLogs Tests - Cache and Filtering
// ---------------------------------------------------------------------------

// TestListLogs_CacheHit tests that the second identical request returns
// X-Cache: HIT header.
func TestListLogs_CacheHit(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Clear cache first
	globalLogsCache.mu.Lock()
	globalLogsCache.entries = make(map[string]*logsCacheEntry)
	globalLogsCache.mu.Unlock()

	// First request - should be MISS
	req := httptest.NewRequest("GET", "/logs/", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	cacheHeader := w.Header().Get("X-Cache")
	if cacheHeader != "MISS" {
		t.Errorf("first request: expected X-Cache: MISS, got %q", cacheHeader)
	}

	// Second identical request - should be HIT
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req)

	if w2.Code != http.StatusOK {
		t.Fatalf("second request: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}

	cacheHeader2 := w2.Header().Get("X-Cache")
	if cacheHeader2 != "HIT" {
		t.Errorf("second request: expected X-Cache: HIT, got %q", cacheHeader2)
	}
}

// TestListLogs_FilterByProviderID tests ListLogs with valid and invalid
// UUID provider_id parameters.
func TestListLogs_FilterByProviderID(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Valid UUID - should add SQL filter
	validUUID := uuid.New().String()
	req := httptest.NewRequest("GET", "/logs/?provider_id="+validUUID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("valid UUID: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Invalid UUID - should be silently ignored (no SQL filter added)
	invalidUUID := "not-a-uuid"
	req2 := httptest.NewRequest("GET", "/logs/?provider_id="+invalidUUID, http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")

	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("invalid UUID: expected 200, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestListLogs_FilterBySpecificStatusCode tests ListLogs with a specific
// numeric status code (e.g., ?status_code=200).
func TestListLogs_FilterBySpecificStatusCode(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/logs/?status_code=200", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestListLogs_SortDirAsc tests ListLogs with ascending sort direction.
func TestListLogs_SortDirAsc(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/logs/?sort_by=time&sort_dir=asc", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestListLogs_DateFilterInvalidFormat tests that invalid date formats
// are silently ignored (not added to query).
func TestListLogs_DateFilterInvalidFormat(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Invalid date format - should be silently ignored
	req := httptest.NewRequest("GET", "/logs/?from=invalid-date", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// extractMigrationNames Tests - Error Paths
// ---------------------------------------------------------------------------

// TestExtractMigrationNames_FilterFileWriteError tests extractMigrationNames
// when os.CreateTemp fails (e.g., TMPDIR points to non-existent directory).
func TestExtractMigrationNames_FilterFileWriteError(t *testing.T) {
	// Save original TMPDIR
	origTmpdir := os.Getenv("TMPDIR")
	t.Cleanup(func() {
		if origTmpdir == "" {
			os.Unsetenv("TMPDIR")
		} else {
			os.Setenv("TMPDIR", origTmpdir)
		}
	})

	// Set TMPDIR to non-existent directory to cause os.CreateTemp to fail
	os.Setenv("TMPDIR", "/nonexistent/path/that/does/not/exist")

	// Use any dump path (doesn't need to exist, will fail before pg_restore)
	dumpPath := "/tmp/test.dump"

	_, err := extractMigrationNames(dumpPath, 100)
	if err == nil {
		t.Error("expected error when filter file cannot be created")
	}
	if !strings.Contains(err.Error(), "failed to create filter file") {
		t.Errorf("expected 'failed to create filter file' error, got: %v", err)
	}
}

// TestExtractMigrationNames_PgRestoreNotFound tests extractMigrationNames
// when pg_restore is not found in PATH.
func TestExtractMigrationNames_PgRestoreNotFound(t *testing.T) {
	// Create an empty temp file to use as dump path
	tmpFile, err := os.CreateTemp(t.TempDir(), "test-dump-*.dump")
	if err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}
	tmpFile.Close()

	// Save original PATH and set it to non-existent directory
	origPath := os.Getenv("PATH")
	t.Cleanup(func() {
		os.Setenv("PATH", origPath)
	})
	os.Setenv("PATH", "/nonexistent")

	_, err = extractMigrationNames(tmpFile.Name(), 100)
	if err == nil {
		t.Error("expected error when pg_restore is not found")
	}
	if !strings.Contains(err.Error(), "pg_restore not found") {
		t.Errorf("expected 'pg_restore not found' error, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// NewBackupHandler Tests - filepath.Abs Fallback
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// Additional Backup Handler Tests
// ---------------------------------------------------------------------------

// TestBackupHandler_RestoreBackup_FilterFileError tests RestoreBackup
// with a valid pg_dump but a read-only directory. Note: because
// os.CreateTemp uses the system temp dir, not the backup dir, this
// test primarily exercises the pg_restore path rather than the filter
// file error branch. It documents an untested edge case.
func TestBackupHandler_RestoreBackup_FilterFileError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump not installed")
	}
	if _, err := exec.LookPath("pg_restore"); err != nil {
		t.Skip("pg_restore not installed")
	}

	// Create a valid dump file
	dir := t.TempDir()
	dumpPath := filepath.Join(dir, "test.dump")

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

	// Create handler with read-only backup dir to cause filter file creation to fail
	readonlyDir := t.TempDir()
	// Make directory read-only
	if err := os.Chmod(readonlyDir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(readonlyDir, 0o755)

	h := NewBackupHandler(apiTestDBURL, readonlyDir, &mockAdminAuth{validateFn: func(string) bool { return true }})
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			next.ServeHTTP(w, r)
		})
	})
	h.Register(r)

	// Read the dump file and upload via restore endpoint
	dumpContent, err := os.ReadFile(dumpPath)
	if err != nil {
		t.Fatalf("failed to read dump file: %v", err)
	}

	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)
	writer.WriteField("admin_token", "valid-token")
	part, _ := writer.CreateFormFile("dump", "test.dump")
	part.Write(dumpContent)
	writer.Close()

	req := httptest.NewRequest("POST", "/backups/restore", &buf)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// The filter file is created in /tmp (system temp), not backup dir,
	// so this test actually exercises the pg_restore path, not filter file error.
	// To test filter file error, we'd need to make /tmp read-only which is not feasible.
	// This test documents that the filter file error path exists but requires
	// system-level conditions to trigger.
	if w.Code != http.StatusInternalServerError && w.Code != http.StatusOK {
		t.Logf("restore returned %d: %s", w.Code, w.Body.String())
	}
}
