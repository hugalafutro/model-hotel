package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestCreateBackup_AlreadyInProgress(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a backup handler with a test directory and manually trigger the mutex
	backupDir := filepath.Join(h.cfg.DataDir, "backups")
	bh := NewBackupHandler(h.cfg.DatabaseURL, backupDir, h.adminMgr, nil)

	// Manually lock the mutex to simulate an in-progress backup
	bh.backupMu.Lock()
	defer bh.backupMu.Unlock()

	// Register the backup handler on a separate router to test it directly
	backupRouter := chi.NewRouter()
	bh.Register(backupRouter)

	req := httptest.NewRequest("POST", "/backups", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	backupRouter.ServeHTTP(w, req)

	// Should get 409 Conflict
	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict, got %d: %s", w.Code, w.Body.String())
	}
}

// TestCreateBackup_NoPgDump tests the pg_dump not found path

// The pg_dump-missing 412 path is covered deterministically by
// TestCreateBackup_NoPgDump_ManipulatedPATH; this environment-conditional
// duplicate that skipped when pg_dump was installed was removed (no skips).

func TestListBackups_EmptyDirectory_Integration(t *testing.T) {
	h, router := newTestHandlerWithRouter(t)
	_ = h

	req := httptest.NewRequest("GET", "/backups", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var backups []interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &backups); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
}
