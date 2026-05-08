package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// TestGetTokenCounts tests FailoverHandler.getTokenCounts() which queries
// request_logs for models with hotel/ prefix and sums tokens in the last 30 days.
func TestGetTokenCounts(t *testing.T) {
	h := newIntegrationFailoverHandler()
	if h == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	pool := h.dbPool

	providerID := "00000000-0000-0000-0000-000000000001"

	// Clean slate: remove stale data from previous runs
	_, _ = pool.Exec(ctx, `DELETE FROM request_logs WHERE request_hash LIKE 'test-gtc-%'`)
	_, _ = pool.Exec(ctx, `DELETE FROM providers WHERE id = $1`, providerID)

	// Insert test provider (FK dependency for request_logs)
	_, err := pool.Exec(ctx, `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_salt, masked_key, created_at, updated_at)
		VALUES ($1, 'test-provider', 'https://api.example.com', '', '', 'sk-***', now(), now())
	`, providerID)
	if err != nil {
		t.Fatalf("failed to insert test provider: %v", err)
	}

	// Insert test data into request_logs:
	// - hotel/gpt-4o: (100+50) + (200+100) + (0+0) = 450 total tokens
	// - hotel/claude-3: (150+75) = 225 total tokens
	// - openai/gpt-4: should NOT appear (not hotel/ prefix)
	_, err = pool.Exec(ctx, `
		INSERT INTO request_logs (provider_id, model_id, request_id, request_hash, status_code, tokens_prompt, tokens_completion, streaming, state, created_at)
		VALUES
			($1, 'hotel/gpt-4o', 'test-gtc-1', 'test-gtc-hash-1', 200, 100, 50, false, 'success', now()),
			($1, 'hotel/gpt-4o', 'test-gtc-2', 'test-gtc-hash-2', 200, 200, 100, false, 'success', now()),
			($1, 'hotel/claude-3', 'test-gtc-3', 'test-gtc-hash-3', 200, 150, 75, false, 'success', now()),
			($1, 'openai/gpt-4', 'test-gtc-4', 'test-gtc-hash-4', 200, 500, 250, false, 'success', now()),
			($1, 'hotel/gpt-4o', 'test-gtc-5', 'test-gtc-hash-5', 200, 0, 0, false, 'success', now())
	`, providerID)
	if err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	// Clean up after test
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM request_logs WHERE request_hash LIKE 'test-gtc-%'`)
		_, _ = pool.Exec(ctx, `DELETE FROM providers WHERE id = $1`, providerID)
	})

	// Verify token counts are correct for hotel/ models
	counts, err := h.getTokenCounts(ctx)
	if err != nil {
		t.Fatalf("getTokenCounts failed: %v", err)
	}

	if counts["hotel/gpt-4o"] != 450 {
		t.Errorf("hotel/gpt-4o count = %d, want 450", counts["hotel/gpt-4o"])
	}

	if counts["hotel/claude-3"] != 225 {
		t.Errorf("hotel/claude-3 count = %d, want 225", counts["hotel/claude-3"])
	}

	// openai/gpt-4 should NOT be in the map (not hotel/ prefix)
	if _, exists := counts["openai/gpt-4"]; exists {
		t.Error("openai/gpt-4 should not be in counts (not hotel/ prefix)")
	}

	// Empty case: delete hotel/ rows, should return empty map
	_, err = pool.Exec(ctx, `DELETE FROM request_logs WHERE request_hash LIKE 'test-gtc-%'`)
	if err != nil {
		t.Fatalf("failed to delete test rows: %v", err)
	}

	counts, err = h.getTokenCounts(ctx)
	if err != nil {
		t.Fatalf("getTokenCounts failed on empty case: %v", err)
	}

	if len(counts) != 0 {
		t.Errorf("expected empty map when no hotel/ rows exist, got %d entries", len(counts))
	}
}

// TestCreateBackup_Success tests the happy path of BackupHandler.CreateBackup().
// This test requires pg_dump to be installed on the system.
func TestCreateBackup_Success(t *testing.T) {

	// Skip if pg_dump is not available
	if _, err := exec.LookPath("pg_dump"); err != nil {
		t.Skip("pg_dump not installed, skipping backup integration test")
	}

	if apiTestDBURL == "" {
		t.Skip("database not available")
	}

	backupDir := t.TempDir()
	bh := NewBackupHandler(apiTestDBURL, backupDir)

	r := chi.NewRouter()
	bh.Register(r)

	req := httptest.NewRequest(http.MethodPost, "/backups", nil)
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
		if _, err := exec.Command("stat", backupPath).CombinedOutput(); err != nil {
			t.Errorf("backup file should exist at %s", backupPath)
		}
	}
}

// TestGetProviderUsage_UnsupportedType tests the default branch of
// Handler.GetProviderUsage() which returns 400 for unsupported provider types.
func TestGetProviderUsage_UnsupportedType(t *testing.T) {

	_, r := newTestHandlerWithRouter(t)

	// Create a provider with an unsupported base URL
	unknownURL := "https://api.unknown-provider.example.com/v1"
	createBody := map[string]interface{}{
		"name":     "unsupported-provider-" + t.Name(),
		"base_url": unknownURL,
		"api_key":  "test-key",
	}
	bodyBytes, err := json.Marshal(createBody)
	if err != nil {
		t.Fatalf("failed to marshal request body: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/providers", bytes.NewReader(bodyBytes))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("failed to create provider: %d: %s", w.Code, w.Body.String())
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&createResp); err != nil {
		t.Fatalf("failed to decode create response: %v", err)
	}

	// Now request usage for this provider
	req = httptest.NewRequest(http.MethodGet, "/providers/"+createResp.ID+"/usage", nil)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 Bad Request, got %d: %s", w.Code, w.Body.String())
	}

	if !strings.Contains(w.Body.String(), "usage information not supported") {
		t.Errorf("expected error about unsupported provider type, got: %s", w.Body.String())
	}
}
