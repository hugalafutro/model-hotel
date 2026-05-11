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
		INSERT INTO request_logs (provider_id, model_id, request_hash, status_code, tokens_prompt, tokens_completion, streaming, state, created_at)
		VALUES
			($1, 'hotel/gpt-4o', 'test-gtc-hash-1', 200, 100, 50, false, 'success', now()),
			($1, 'hotel/gpt-4o', 'test-gtc-hash-2', 200, 200, 100, false, 'success', now()),
			($1, 'hotel/claude-3', 'test-gtc-hash-3', 200, 150, 75, false, 'success', now()),
			($1, 'openai/gpt-4', 'test-gtc-hash-4', 200, 500, 250, false, 'success', now()),
			($1, 'hotel/gpt-4o', 'test-gtc-hash-5', 200, 0, 0, false, 'success', now())
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
	req = httptest.NewRequest(http.MethodGet, "/providers/"+createResp.ID+"/usage", http.NoBody)
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

// ---------------------------------------------------------------------------
// Admin Handler Tests - ListProviders
// ---------------------------------------------------------------------------

// TestListProviders_Integration tests the ListProviders handler with an empty database.
func TestListProviders_Integration(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/providers", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var response []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(response) != 0 {
		t.Errorf("expected empty provider list, got %d providers", len(response))
	}
}

// TestListProviders_WithProviders tests listing providers when database has entries.
func TestListProviders_WithProviders(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create two providers
	provider1 := `{"name": "test-list-1", "base_url": "https://api.openai.com", "api_key": "sk-test1"}`
	provider2 := `{"name": "test-list-2", "base_url": "https://api.anthropic.com", "api_key": "sk-ant-test"}`

	for _, body := range []string{provider1, provider2} {
		req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")

		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("failed to create provider: %d: %s", w.Code, w.Body.String())
		}
	}

	// List all providers
	req := httptest.NewRequest(http.MethodGet, "/providers", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var response []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(response) != 2 {
		t.Errorf("expected 2 providers, got %d", len(response))
	}
}

// ---------------------------------------------------------------------------
// Admin Handler Tests - CreateProvider
// ---------------------------------------------------------------------------

// TestCreateProvider_Integration_Success tests creating a provider with valid data.
func TestCreateProvider_Integration_Success(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	body := `{"name": "test-create-success", "base_url": "https://api.openai.com", "api_key": "sk-test123"}`
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected status 201 Created, got %d: %s", w.Code, w.Body.String())
	}

	var response struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		BaseURL string `json:"base_url"`
	}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response.Name != "test-create-success" {
		t.Errorf("expected name 'test-create-success', got %s", response.Name)
	}
	if response.BaseURL != "https://api.openai.com" {
		t.Errorf("expected base_url 'https://api.openai.com', got %s", response.BaseURL)
	}
	if response.ID == "" {
		t.Error("expected non-empty ID")
	}
}

// ---------------------------------------------------------------------------
// Admin Handler Tests - UpdateProvider
// ---------------------------------------------------------------------------

// TestUpdateProvider_Integration_Success tests updating a provider's fields.
func TestUpdateProvider_Integration_Success(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create provider first
	createBody := `{"name": "test-update-original", "base_url": "https://api.openai.com", "api_key": "sk-test"}`
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(createBody))
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

	// Update the provider
	updateBody := `{"name": "test-update-new", "base_url": "https://api.anthropic.com"}`
	req = httptest.NewRequest(http.MethodPut, "/providers/"+createResp.ID, strings.NewReader(updateBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var updateResp struct {
		ID      string `json:"id"`
		Name    string `json:"name"`
		BaseURL string `json:"base_url"`
	}
	if err := json.NewDecoder(w.Body).Decode(&updateResp); err != nil {
		t.Fatalf("failed to decode update response: %v", err)
	}

	if updateResp.Name != "test-update-new" {
		t.Errorf("expected name 'test-update-new', got %s", updateResp.Name)
	}
	if updateResp.BaseURL != "https://api.anthropic.com" {
		t.Errorf("expected base_url 'https://api.anthropic.com', got %s", updateResp.BaseURL)
	}
}

// TestUpdateProvider_NotFound tests updating a non-existent provider.
func TestUpdateProvider_NotFound(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	unknownID := "00000000-0000-0000-0000-000000000000"
	body := `{"name": "test-update-notfound"}`
	req := httptest.NewRequest(http.MethodPut, "/providers/"+unknownID, strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected status 404 Not Found, got %d: %s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Admin Handler Tests - DeleteProvider
// ---------------------------------------------------------------------------

// TestDeleteProvider_Integration_Success tests deleting an existing provider.
func TestDeleteProvider_Integration_Success(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create provider first
	createBody := `{"name": "test-delete-success", "base_url": "https://api.openai.com", "api_key": "sk-test"}`
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(createBody))
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

	// Delete the provider
	req = httptest.NewRequest(http.MethodDelete, "/providers/"+createResp.ID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected status 204 No Content, got %d: %s", w.Code, w.Body.String())
	}

	// Verify it's gone
	req = httptest.NewRequest(http.MethodGet, "/providers/"+createResp.ID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected status 404 Not Found after delete, got %d", w.Code)
	}
}

// ---------------------------------------------------------------------------
// Failover Handler Tests - Sync
// ---------------------------------------------------------------------------

// TestFailoverSync_Integration tests the Sync endpoint.
func TestFailoverSync_Integration(t *testing.T) {
	h := newIntegrationFailoverHandler()
	if h == nil {
		t.Skip("database not available")
	}

	req, w := newChiRequest(http.MethodPost, "/failover-groups/sync", nil)

	h.Sync(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify response has expected structure (DisabledGroups, SyncErrors)
	if _, ok := response["disabled_groups"]; !ok {
		t.Error("expected 'disabled_groups' field in sync response")
	}
}

// ---------------------------------------------------------------------------
// Settings Handler Tests - UpdateSettings
// ---------------------------------------------------------------------------

// TestUpdateSettings_Integration_MultipleKeys tests updating multiple settings at once.
func TestUpdateSettings_Integration_MultipleKeys(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	body := `{"rate_limit_enabled": "true", "rate_limit_rps": "50", "toast_duration": "3000"}`
	req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var response map[string]string
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["rate_limit_enabled"] != "true" {
		t.Errorf("expected rate_limit_enabled='true', got %s", response["rate_limit_enabled"])
	}
	if response["rate_limit_rps"] != "50" {
		t.Errorf("expected rate_limit_rps='50', got %s", response["rate_limit_rps"])
	}
	if response["toast_duration"] != "3000" {
		t.Errorf("expected toast_duration='3000', got %s", response["toast_duration"])
	}
}

// TestUpdateSettings_FloatValue tests updating a float-type setting.
func TestUpdateSettings_FloatValue(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	body := `{"rate_limit_rps": "25.5"}`
	req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var response map[string]string
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["rate_limit_rps"] != "25.5" {
		t.Errorf("expected rate_limit_rps='25.5', got %s", response["rate_limit_rps"])
	}
}

// TestUpdateSettings_OutOfRangeInt tests updating with an integer value out of range.
func TestUpdateSettings_OutOfRangeInt(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// toast_duration max is 15000
	body := `{"toast_duration": "99999"}`
	req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 Bad Request, got %d: %s", w.Code, w.Body.String())
	}

	if !strings.Contains(w.Body.String(), "must be between") {
		t.Errorf("expected error about range, got: %s", w.Body.String())
	}
}

// TestUpdateSettings_OutOfRangeFloat tests updating with a float value out of range.
func TestUpdateSettings_OutOfRangeFloat(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// rate_limit_rps max is 10000
	body := `{"rate_limit_rps": "99999"}`
	req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 Bad Request, got %d: %s", w.Code, w.Body.String())
	}

	if !strings.Contains(w.Body.String(), "must be between") {
		t.Errorf("expected error about range, got: %s", w.Body.String())
	}
}

// TestUpdateSettings_TooManyKeys tests the limit on number of settings in one request.
func TestUpdateSettings_TooManyKeys(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Build a request with more than 50 unique keys
	// The >50 check happens before key validation, so keys don't need to be valid
	body := `{`
	for i := 0; i < 55; i++ {
		if i > 0 {
			body += `,`
		}
		body += `"setting_key_` + string(rune('a'+(i/26))) + string(rune('a'+(i%26))) + `":"value"`
	}
	body += `}`

	req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 Bad Request, got %d: %s", w.Code, w.Body.String())
	}

	if !strings.Contains(w.Body.String(), "too many settings") {
		t.Errorf("expected error about too many settings, got: %s", w.Body.String())
	}
}
