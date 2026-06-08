package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
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

func TestCreateBackup_NoPgDump(t *testing.T) {
	h := newTestHandler(t)

	// Create a backup handler with a test directory
	backupDir := filepath.Join(h.cfg.DataDir, "backups")
	bh := NewBackupHandler(h.cfg.DatabaseURL, backupDir, h.adminMgr, nil)

	// Register the backup handler on a separate router
	backupRouter := chi.NewRouter()
	bh.Register(backupRouter)

	// This test will only pass if pg_dump is NOT installed
	if _, err := exec.LookPath("pg_dump"); err == nil {
		t.Skip("pg_dump is installed, cannot test missing binary path")
	}

	req := httptest.NewRequest("POST", "/backups", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	backupRouter.ServeHTTP(w, req)

	// Should get 412 Precondition Failed
	if w.Code != http.StatusPreconditionFailed {
		t.Errorf("expected 412 Precondition Failed, got %d: %s", w.Code, w.Body.String())
	}
}

// TestDiscoverProviderModels_DisabledProviderExplicit tests the disabled provider path

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
