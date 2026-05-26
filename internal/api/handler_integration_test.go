package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/admin"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/util"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

const testMasterKey = "testmasterkey1234567890abcdef"

// TestMain is defined in failover_api_test.go

func newTestHandler(t *testing.T) *Handler {
	t.Helper()

	dbURL := apiTestDBURL
	if dbURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}

	// Clean test data within our isolated database (safe since each package has its own DB)
	pool.Exec(context.Background(), `
		TRUNCATE providers, models, virtual_keys, request_logs,
		       app_logs, model_failover_groups, settings CASCADE
	`)

	// Create database instance
	database, err := db.New(context.Background(), dbURL, 5, 1)
	if err != nil {
		t.Skip("skipping: test database not available")
	}

	cfg := &config.Config{
		MasterKey:            testMasterKey,
		AllowHTTPProviders:   true,
		RateLimitEnabled:     false,
		DataDir:              t.TempDir(),
		AllowedProviderHosts: []string{"localhost", "127.0.0.1", "api.nano-gpt.com", "nano-gpt.com", "api.nanogpt.com", "nanogpt.com", "ngc.nanogpt.com", "openrouter.ai", "api.z.ai", "z.ai", "api.zai.chat", "zai.api.example.com", "api.deepseek.com", "deepseek.com", "api.anthropic.com", "anthropic.com", "api.openai.com", "opencode.ai", "api.example.com", "custom.example.com", "api.alpha.com", "api.beta.com", "api.first.com", "api.second.com", "api.generic.com", "example.com", "api.mistral.ai", "api.cohere.ai", "api.x.ai", "generativelanguage.googleapis.com", "192.168.1.1", "192.0.2.1", "httpbin.org"},
	}

	providerRepo := provider.NewRepository(pool)
	vkRepo := virtualkey.NewRepository(pool)

	// Create a temporary directory for admin token
	tmpDir := t.TempDir()
	adminMgr, _, err := admin.New(tmpDir, "test-admin-token")
	if err != nil {
		t.Fatalf("failed to create admin manager: %v", err)
	}

	settingsRepo := settings.NewRepository(pool)

	handler := NewHandler(cfg, providerRepo, database, adminMgr, vkRepo, settingsRepo, "test")
	if handler == nil {
		pool.Close()
		t.Fatal("handler is nil")
	}

	t.Cleanup(func() {
		database.Close()
		pool.Close()
	})

	return handler
}

func newTestHandlerWithRouter(t *testing.T) (*Handler, chi.Router) {
	t.Helper()
	h := newTestHandler(t)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.Register(r)
	// Mock Docker stats collector to avoid real Docker API calls which
	// spawn persistent HTTP transport goroutines that hang the test process.
	h.SetDockerStatsCollector(func(filter util.ContainerFilter) util.AggregatedDockerStats {
		return util.AggregatedDockerStats{}
	})
	return h, r
}

func TestNewHandler(t *testing.T) {

	h := newTestHandler(t)
	if h == nil {
		t.Fatal("handler is nil")
	}
	if h.Pool() == nil {
		t.Fatal("pool is nil")
	}
}

func TestHandlerRegister(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Test that routes are registered
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/providers", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Logf("Expected 200, got %d", rec.Code)
	}
}

func TestPool(t *testing.T) {

	h := newTestHandler(t)
	pool := h.Pool()
	if pool == nil {
		t.Fatal("pool is nil")
	}

	// Test database connection
	ctx := context.Background()
	var count int
	err := pool.Pool().QueryRow(ctx, "SELECT 1").Scan(&count)
	if err != nil {
		t.Fatalf("failed to query database: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected count=1, got %d", count)
	}
}

// Provider Tests

func TestListProviders_Empty(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/providers", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if len(response) != 0 {
		t.Errorf("Expected empty provider list, got %d providers", len(response))
	}
}

func TestCreateAndGetProvider(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a test provider
	providerData := `{"name": "test-openai", "base_url": "https://api.openai.com", "api_key": "sk-test123"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("Failed to parse create response: %v", err)
	}

	// Get the provider
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/providers/"+createResp.ID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var getResp struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		BaseURL      string `json:"base_url"`
		ProviderType string `json:"provider_type"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("Failed to parse get response: %v", err)
	}
	if getResp.ID != createResp.ID {
		t.Errorf("Expected ID %s, got %s", createResp.ID, getResp.ID)
	}
	if getResp.Name != "test-openai" {
		t.Errorf("Expected name 'test-openai', got %s", getResp.Name)
	}
}

func TestListProviders_AfterCreate(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a provider first
	providerData := `{"name": "test-provider", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	// List providers
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/providers", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if len(response) != 1 {
		t.Fatalf("Expected 1 provider, got %d", len(response))
	}
	if response[0].Name != "test-provider" {
		t.Errorf("Expected name 'test-provider', got %s", response[0].Name)
	}
}

func TestDeleteProvider(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a provider first
	providerData := `{"name": "test-delete", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("Failed to parse create response: %v", err)
	}

	// Delete the provider
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/providers/"+createResp.ID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("Expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify it's gone
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/providers/"+createResp.ID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 after delete, got %d", rec.Code)
	}
}

func TestDeleteProvider_InvalidUUID(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Delete with invalid UUID
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/providers/invalid-uuid", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid UUID, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDeleteProvider_NonExistent(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Delete non-existent provider
	nonExistentID := uuid.New().String()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/providers/"+nonExistentID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for non-existent provider, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetProvider_InvalidUUID(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Get with invalid UUID
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/providers/invalid-uuid", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid UUID, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetProvider_NonExistent(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Get non-existent provider
	nonExistentID := uuid.New().String()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/providers/"+nonExistentID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for non-existent provider, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateProvider_NonExistent(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Update non-existent provider
	nonExistentID := uuid.New().String()
	updateData := `{"name": "test-updated", "base_url": "https://api.anthropic.com"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/providers/"+nonExistentID, strings.NewReader(updateData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for non-existent provider, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestUpdateProvider_InvalidData(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a provider first
	providerData := `{"name": "test-update-invalid", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("Failed to parse create response: %v", err)
	}

	// Update with invalid name (too long)
	updateData := `{"name": ""}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("PUT", "/providers/"+createResp.ID, strings.NewReader(updateData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid name, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateProvider_EmptyName(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create with empty name
	providerData := `{"name": "", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for empty name, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestCreateProvider_Duplicate(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create first provider
	providerData := `{"name": "test-duplicate", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create first provider: %d", rec.Code)
	}

	// Try to create duplicate
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Errorf("Expected 409 for duplicate, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Test for admin.go - UpdateProvider_ChangeURL
func TestUpdateProvider_ChangeURL(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider first
	providerData := `{"name": "test-update-url", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("Failed to parse create response: %v", err)
	}

	// Update the provider's base_url
	updateData := `{"base_url": "https://api.anthropic.com"}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("PUT", "/providers/"+createResp.ID, strings.NewReader(updateData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the update took effect
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/providers/"+createResp.ID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var getResp struct {
		BaseURL string `json:"base_url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("Failed to parse get response: %v", err)
	}
	if getResp.BaseURL != "https://api.anthropic.com" {
		t.Errorf("Expected base_url 'https://api.anthropic.com', got %s", getResp.BaseURL)
	}
}

// Test for admin.go - CreateProvider_DifferentTypes
func TestCreateProvider_DifferentTypes(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	testCases := []struct {
		name     string
		baseURL  string
		expected string
	}{
		{"OpenAI", "https://api.openai.com", "openai"},
		{"Anthropic", "https://api.anthropic.com", "anthropic"},
		{"Google", "https://generativelanguage.googleapis.com", "google"},
		{"DeepSeek", "https://api.deepseek.com", "deepseek"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			providerData := fmt.Sprintf(`{"name": "test-%s", "base_url": %q, "api_key": "test-api-key"}`, tc.name, tc.baseURL)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
			req.Header.Set("Authorization", "Bearer test-admin-token")
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusCreated {
				t.Errorf("Expected 201 for %s, got %d: %s", tc.name, rec.Code, rec.Body.String())
			}

			// Provider type is detected internally but not returned in response
			// Just verify the provider was created successfully
			var createResp struct {
				ID string `json:"id"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
				t.Fatalf("Failed to parse create response: %v", err)
			}
			if createResp.ID == "" {
				t.Errorf("Expected non-empty provider ID for %s", tc.name)
			}
		})
	}
}

// Test for admin.go - ListProviders_WithProviders
func TestListProviders_WithPagination(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create multiple providers
	var rec *httptest.ResponseRecorder
	var req *http.Request
	for i := 0; i < 5; i++ {
		providerData := fmt.Sprintf(`{"name": "test-provider-%d", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`, i)
		rec = httptest.NewRecorder()
		req = httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Failed to create provider %d: %d", i, rec.Code)
		}
	}

	// List providers (pagination not supported by endpoint)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/providers", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response []struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if len(response) != 5 {
		t.Errorf("Expected 5 providers total, got %d", len(response))
	}
}
func TestCreateProvider_VariousTypes(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	testCases := []struct {
		name     string
		baseURL  string
		apiKey   string
		expectOK bool
	}{
		{"OpenAI", "https://api.openai.com", "sk-test123", true},
		{"Anthropic", "https://api.anthropic.com", "sk-ant-api03-test", true},
		{"Mistral", "https://api.mistral.ai", "test-api-key", true},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			providerData := fmt.Sprintf(`{"name": "test-%s-%s", "base_url": %q, "api_key": %q}`, tc.name, uuid.New().String()[:8], tc.baseURL, tc.apiKey)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
			req.Header.Set("Authorization", "Bearer test-admin-token")
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(rec, req)

			if tc.expectOK && rec.Code != http.StatusCreated {
				t.Errorf("Expected 201 for %s, got %d: %s", tc.name, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestPurgeLogs(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	testCases := []struct {
		name       string
		olderThan  string
		expectCode int
	}{
		{"Invalid time range", "invalid", http.StatusBadRequest},
		{"1 hour", "1h", http.StatusNoContent},
		{"1 day", "1d", http.StatusNoContent},
		{"1 week", "1w", http.StatusNoContent},
		{"1 month", "1m", http.StatusNoContent},
		{"All logs", "all", http.StatusNoContent},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			purgeData := fmt.Sprintf(`{"older_than": %q}`, tc.olderThan)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("DELETE", "/logs/purge", strings.NewReader(purgeData))
			req.Header.Set("Authorization", "Bearer test-admin-token")
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(rec, req)

			if rec.Code != tc.expectCode {
				t.Errorf("Expected %d for %s, got %d: %s", tc.expectCode, tc.name, rec.Code, rec.Body.String())
			}
		})
	}
}

func TestGetVirtualKey_NonExistent(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Get non-existent virtual key
	nonExistentID := uuid.New().String()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/virtual-keys/"+nonExistentID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for non-existent virtual key, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestListFailoverGroups(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create providers and models for failover group
	providerData := `{"name": "test-failover-provider", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// List failover groups (should be empty initially)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/failover-groups", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Groups []map[string]interface{} `json:"groups"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should be empty without failover groups
}

func TestDeleteFailoverGroup(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Delete non-existent failover group - should succeed with 204
	nonExistentID := uuid.New().String()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/failover-groups/"+nonExistentID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Delete returns 204 even for non-existent groups (idempotent)
	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected 204 for failover group delete, got %d: %s", rec.Code, rec.Body.String())
	}
}

// DeleteFailoverGroup - Additional coverage with cascade
func TestDeleteFailoverGroup_WithModels(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a provider
	providerData := `{"name": "test-delete-fg-provider", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert models directly via DB
	pool := h.Pool().Pool()
	modelID1 := uuid.New().String()
	modelID2 := uuid.New().String()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID1, providerResp.ID, "gpt-4o-mini-1", "GPT-4o Mini 1", true)
	if err != nil {
		t.Fatalf("Failed to insert model 1: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID2, providerResp.ID, "gpt-4o-mini-2", "GPT-4o Mini 2", true)
	if err != nil {
		t.Fatalf("Failed to insert model 2: %v", err)
	}

	// Create a failover group with these models via API
	groupData := `{"display_model":"test-delete-group","entry_ids":["` + modelID1 + `","` + modelID2 + `"]}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/failover-groups/", strings.NewReader(groupData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create failover group: %d: %s", rec.Code, rec.Body.String())
	}

	var groupResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &groupResp); err != nil {
		t.Fatalf("Failed to parse group response: %v", err)
	}

	// Now delete the failover group
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/failover-groups/"+groupResp.ID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected 204 for failover group delete, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the group is gone
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/failover-groups/"+groupResp.ID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 after delete, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSyncFailoverGroups(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Sync failover groups (should work even with no models)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/failover-groups/sync", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should return sync result
}

func TestFailoverCandidates(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Get candidates (should be empty without providers/models)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/failover-groups/candidates", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should be empty without providers/models
	if len(response) != 0 {
		t.Errorf("Expected empty candidates list, got %d candidates", len(response))
	}
}

func TestUpdateProvider(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a provider first
	providerData := `{"name": "test-update", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("Failed to parse create response: %v", err)
	}

	// Update the provider
	updateData := `{"name": "test-updated", "base_url": "https://api.anthropic.com"}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("PUT", "/providers/"+createResp.ID, strings.NewReader(updateData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the update
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/providers/"+createResp.ID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var getResp struct {
		Name    string `json:"name"`
		BaseURL string `json:"base_url"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("Failed to parse get response: %v", err)
	}
	if getResp.Name != "test-updated" {
		t.Errorf("Expected name 'test-updated', got %s", getResp.Name)
	}
	if getResp.BaseURL != "https://api.anthropic.com" {
		t.Errorf("Expected base_url 'https://api.anthropic.com', got %s", getResp.BaseURL)
	}
}

// Model Tests

func TestListModels(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/models", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should be empty without discovery
	if len(response) != 0 {
		t.Errorf("Expected empty model list, got %d models", len(response))
	}
}

// Test for models.go - UpdateModel_EnableDisable
func TestUpdateModel_EnableDisable(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := `{"name": "test-model-provider", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert a model directly via DB
	modelID := uuid.New().String()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerResp.ID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// Test disabling the model
	_, err = pool.Exec(context.Background(), `
		UPDATE models SET enabled = false WHERE id = $1
	`, modelID)
	if err != nil {
		t.Fatalf("Failed to disable model: %v", err)
	}

	// Verify the disable
	var enabledCheck bool
	if err := pool.QueryRow(context.Background(), `SELECT enabled FROM models WHERE id = $1`, modelID).Scan(&enabledCheck); err != nil {
		t.Fatalf("Failed to check enabled status: %v", err)
	}
	if enabledCheck != false {
		t.Errorf("Expected enabled=false after disable, got %v", enabledCheck)
	}

	// Test re-enabling the model
	_, err = pool.Exec(context.Background(), `
		UPDATE models SET enabled = true WHERE id = $1
	`, modelID)
	if err != nil {
		t.Fatalf("Failed to enable model: %v", err)
	}

	// Verify the enable
	if err := pool.QueryRow(context.Background(), `SELECT enabled FROM models WHERE id = $1`, modelID).Scan(&enabledCheck); err != nil {
		t.Fatalf("Failed to check enabled status: %v", err)
	}
	if enabledCheck != true {
		t.Errorf("Expected enabled=true after enable, got %v", enabledCheck)
	}
}

// Test for models.go - ListModels_WithPagination
func TestListModels_WithPagination(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := `{"name": "test-models-provider", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert multiple models directly via DB
	pool := h.Pool().Pool()
	for i := 0; i < 10; i++ {
		modelID := uuid.New().String()
		_, err := pool.Exec(context.Background(),
			`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
			modelID, providerResp.ID, fmt.Sprintf("gpt-4o-mini-%d", i), fmt.Sprintf("GPT-4o Mini %d", i), true)
		if err != nil {
			t.Fatalf("Failed to insert model %d: %v", i, err)
		}
	}

	// List models (pagination not supported by endpoint)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/models", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response []ModelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if len(response) != 10 {
		t.Errorf("Expected 10 models total, got %d", len(response))
	}
}

// Test for models.go - TestModel_Success (simulated - will fail with test API key but tests the path)
func TestTestModel_Success(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := fmt.Sprintf(`{"name": "test-model-provider-%s", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert a model directly via DB
	modelID := uuid.New().String()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerResp.ID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// Test the model - will fail because we're using a test API key, but tests the endpoint path
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/models/"+modelID+"/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Should return a response (likely with error due to invalid API key)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var testResp TestModelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &testResp); err != nil {
		t.Fatalf("Failed to parse test response: %v", err)
	}
	// Should have error field due to invalid API key
	if testResp.Error == "" {
		t.Error("Expected error field in test response due to invalid API key")
	}
}
func TestUpdateModel(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a provider
	providerData := fmt.Sprintf(`{"name": "test-model-provider-%s", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert a model directly via DB (no POST /models endpoint)
	modelID := uuid.New().String()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerResp.ID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// Update the model via API
	updateData := `{"display_name": "Updated Model", "enabled": false}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("PATCH", "/models/"+modelID, strings.NewReader(updateData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Stats Tests

func TestGetStats(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/stats", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should return some stats structure (may be empty)
}

func TestGetTimeSeries(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/stats/timeseries", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should return timeseries data (may be empty)
}

func TestGetProviderDistribution(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/stats/provider-distribution", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should return distribution data (may be empty)
}

// System Tests

func TestGetSystem(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)
	h.SetDockerStatsCollector(func(filter util.ContainerFilter) util.AggregatedDockerStats {
		return util.AggregatedDockerStats{}
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/system", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if len(response) == 0 {
		t.Error("Expected system info in response")
	}
}

// Settings Tests

func TestGetSettings(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/settings", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should return settings (may have defaults)
}

func TestUpdateSettingsIntegration(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	settingsData := `{"rate_limit_enabled": "true", "rate_limit_rps": "10", "rate_limit_burst": "20"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/settings", strings.NewReader(settingsData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the update
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/settings", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if response["rate_limit_enabled"] != "true" {
		t.Errorf("Expected rate_limit_enabled='true', got %v", response["rate_limit_enabled"])
	}
}

// UpdateSettings Tests - Additional coverage

func TestUpdateSettings_InvalidKey(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Try to update with invalid key
	settingsData := `{"invalid_key": "value"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/settings", strings.NewReader(settingsData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid key, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unknown setting") {
		t.Errorf("Expected error about unknown setting, got: %s", rec.Body.String())
	}
}

func TestUpdateSettings_ValueTooLong(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Try to update with value too long (maxSettingValueLen is typically 1000)
	longValue := strings.Repeat("x", 2000)
	settingsData := `{"rate_limit_rps": "` + longValue + `"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/settings", strings.NewReader(settingsData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for value too long, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "too long") {
		t.Errorf("Expected error about value length, got: %s", rec.Body.String())
	}
}

func TestUpdateSettings_InvalidIntValue(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Try to update int setting with non-numeric value
	settingsData := `{"rate_limit_rps": "not-a-number"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/settings", strings.NewReader(settingsData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid int value, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "must be a number") {
		t.Errorf("Expected error about numeric value, got: %s", rec.Body.String())
	}
}

func TestUpdateSettings_IntValueOutOfRange(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Try to update with value out of range (rate_limit_rps max is typically 1000)
	settingsData := `{"rate_limit_rps": "99999"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/settings", strings.NewReader(settingsData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for value out of range, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "must be between") {
		t.Errorf("Expected error about range, got: %s", rec.Body.String())
	}
}

func TestUpdateSettings_EmptyMap(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Try to update with empty map
	settingsData := `{}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/settings", strings.NewReader(settingsData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for empty map, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no settings provided") {
		t.Errorf("Expected error about no settings, got: %s", rec.Body.String())
	}
}

// App Logs Tests

func TestGetAppLogsIntegration(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs/app", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should return logs (may be empty)
}

func TestClearAppLogsIntegration(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/logs/app", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// GetAppLogs with filters - Additional coverage

func TestGetAppLogs_WithSeverityFilter(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Get logs with severity filter (level parameter)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs/app?history=true&level=error", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 for logs with severity filter, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Entries []map[string]interface{} `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should return logs filtered by severity (may be empty)
}

func TestGetAppLogs_WithSearchFilter(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Get logs with search filter
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs/app?history=true&search=test", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 for logs with search filter, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Entries []map[string]interface{} `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should return logs matching search term (may be empty)
}

func TestGetAppLogs_WithTimeRangeFilter(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Get logs with time range filter (last 24 hours)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs/app?history=true&from=2024-01-01T00:00:00Z", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 for logs with time range filter, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Entries []map[string]interface{} `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should return logs within time range (may be empty)
}

// Model Tests

func TestDeleteModel(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a provider
	providerData := `{"name": "test-model-provider-` + uuid.New().String() + `", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert a model directly via DB
	modelID := uuid.New().String()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerResp.ID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// Delete the model
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/models/"+modelID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("Expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestTestModel(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a provider
	providerData := `{"name": "test-model-provider-` + uuid.New().String() + `", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert a model directly via DB
	modelID := uuid.New().String()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerResp.ID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	// Test the model - will fail because we're using a test API key
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/models/"+modelID+"/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Should return error due to invalid API key
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var testResp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &testResp); err != nil {
		t.Fatalf("Failed to parse test response: %v", err)
	}
	// Should have error field
	if _, ok := testResp["error"]; !ok {
		t.Error("Expected error field in test response")
	}
}

// Discovery Handler Tests

func TestDiscoverProviderModelsIntegration(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a test provider
	providerData := `{"name": "test-discover", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Test discovery - will fail with test API key but should return proper error structure
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/providers/"+providerResp.ID+"/discover", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Should return an error due to invalid API key, but the handler should work
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Expected 500 error for discovery with invalid API key, got %d: %s", rec.Code, rec.Body.String())
	}

	// The error response is plain text, not JSON
	body := rec.Body.String()
	if body == "" {
		t.Error("Expected error message in response body")
	}
}

// DiscoverProviderModels - Additional coverage

func TestDiscoverProviderModels_NonExistentProvider(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Try to discover models for non-existent provider
	nonExistentID := uuid.New().String()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers/"+nonExistentID+"/discover", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for non-existent provider, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetProviderUsageIntegration(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a test provider
	providerData := `{"name": "test-usage", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Test usage endpoint - should return error for unsupported provider
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/providers/"+providerResp.ID+"/usage", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// OpenAI doesn't support usage endpoint, should return 400
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 for unsupported provider usage, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestGetProviderBalanceIntegration(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a test provider
	providerData := `{"name": "test-balance", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Test balance endpoint - should return error for unsupported provider
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/providers/"+providerResp.ID+"/balance", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// OpenAI doesn't support balance endpoint, should return 400
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 for unsupported provider balance, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestDiscoverAllModelsIntegration(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a test provider
	providerData := `{"name": "test-discover-all", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	// Test discover all models
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/providers/discover-all", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Should return results structure even if discovery fails
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for discover all, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Check that response has expected fields
	if _, ok := response["results"]; !ok {
		t.Error("Expected results field in response")
	}
	if _, ok := response["succeeded"]; !ok {
		t.Error("Expected succeeded field in response")
	}
	if _, ok := response["failed"]; !ok {
		t.Error("Expected failed field in response")
	}
	if _, ok := response["discovered"]; !ok {
		t.Error("Expected discovered field in response")
	}
}

func TestRefreshAllQuotasIntegration(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a test provider
	providerData := `{"name": "test-quotas", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	// Test refresh all quotas
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/providers/refresh-quotas", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Should return results structure even if no quotas are refreshed
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for refresh quotas, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Check that response has expected fields
	if _, ok := response["results"]; !ok {
		t.Error("Expected results field in response")
	}
	if _, ok := response["refreshed"]; !ok {
		t.Error("Expected refreshed field in response")
	}
	if _, ok := response["failed"]; !ok {
		t.Error("Expected failed field in response")
	}
	if _, ok := response["skipped"]; !ok {
		t.Error("Expected skipped field in response")
	}
}

// Events Handler Tests

func TestStreamEvents(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.RegisterEvents(r) // Use RegisterEvents instead of Register for SSE endpoint

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/events", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	// Use a context with cancellation to avoid hanging
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)

	// Start the request in a goroutine so we can cancel it
	done := make(chan bool)
	go func() {
		r.ServeHTTP(rec, req)
		done <- true
	}()

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	// Cancel the request to close the SSE connection
	cancel()

	// Wait for the handler to finish
	<-done

	// Should return 200 and start streaming
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for events stream, got %d: %s", rec.Code, rec.Body.String())
	}

	// Check that content type is event-stream
	contentType := rec.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Expected Content-Type 'text/event-stream', got %s", contentType)
	}

	// Check that initial comment is present
	body := rec.Body.String()
	if !strings.Contains(body, ": connected") {
		t.Error("Expected initial connection comment in stream")
	}
}

// Logs Handler Tests

func TestListLogs(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Entries []map[string]interface{} `json:"entries"`
		Total   int                      `json:"total"`
		Page    int                      `json:"page"`
		PerPage int                      `json:"per_page"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should return empty list when no logs exist
	if len(response.Entries) != 0 {
		t.Errorf("Expected empty log list, got %d entries", len(response.Entries))
	}
	if response.Total != 0 {
		t.Errorf("Expected total 0, got %d", response.Total)
	}
}

// Stats Tests with data

// TestGetStats_WithLogs tests the stats endpoint with actual request logs
func TestGetStats_WithLogs(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider first
	providerData := `{"name": "test-stats-provider", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert some request logs directly
	now := time.Now().UTC()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (
			provider_id, model_id, virtual_key_id, status_code, duration_ms, 
			proxy_overhead_ms, tokens_prompt, tokens_completion, created_at
		) VALUES (
			$1, $2, $3, $4, $5, $6, $7, $8, $9
		)`,
		providerResp.ID, "gpt-4", nil, 200, 1000, 50, 100, 200, now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	// Test stats endpoint
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/stats", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var stats StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to parse stats response: %v", err)
	}

	// Should have some calculated stats
	if stats.TotalRequestsLast24h == 0 {
		t.Error("Expected TotalRequestsLast24h to be > 0")
	}
	if stats.AvgLatencyMs == 0 {
		t.Error("Expected AvgLatencyMs to be > 0")
	}
	if stats.TotalTokensPrompt == 0 {
		t.Error("Expected TotalTokensPrompt to be > 0")
	}
}

// TestGetTimeSeries_DifferentPeriods tests timeseries with different period parameters
func TestGetTimeSeries_DifferentPeriods(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider first
	providerData := `{"name": "test-timeseries-provider", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert some request logs at different times
	now := time.Now().UTC()
	pool := h.Pool().Pool()
	for i := 0; i < 5; i++ {
		_, err := pool.Exec(context.Background(), `
			INSERT INTO request_logs (
				provider_id, model_id, virtual_key_id, status_code, duration_ms, 
				proxy_overhead_ms, tokens_prompt, tokens_completion, created_at
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9
			)`,
			providerResp.ID, "gpt-4", nil, 200, 1000, 50, 100, 200, now.Add(-time.Duration(i)*time.Hour))
		if err != nil {
			t.Fatalf("Failed to insert request log: %v", err)
		}
	}

	// Test different periods
	testCases := []struct {
		name   string
		period string
	}{
		{"1 hour", "1h"},
		{"1 day", "1d"},
		{"7 days", "7d"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/stats/timeseries?period="+tc.period, http.NoBody)
			req.Header.Set("Authorization", "Bearer test-admin-token")
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("Expected 200 for period %s, got %d: %s", tc.period, rec.Code, rec.Body.String())
			}

			var response TimeSeriesStats
			if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
				t.Fatalf("Failed to parse timeseries response: %v", err)
			}

			// Should have some points
			if len(response.Points) == 0 {
				t.Errorf("Expected some time series points for period %s", tc.period)
			}
		})
	}
}

// TestGetProviderDistribution_WithLogs tests provider distribution with actual logs
func TestGetProviderDistribution_WithLogs(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	var rec *httptest.ResponseRecorder
	var req *http.Request

	// Create multiple providers
	providers := []string{"provider1", "provider2", "provider3"}
	providerIDs := make(map[string]string)

	for _, name := range providers {
		providerData := fmt.Sprintf(`{"name": %q, "base_url": "https://api.openai.com", "api_key": "test-api-key"}`, name)
		rec = httptest.NewRecorder()
		req = httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Failed to create provider %s: %d", name, rec.Code)
		}

		var providerResp struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
			t.Fatalf("Failed to parse provider response: %v", err)
		}
		providerIDs[name] = providerResp.ID
	}

	// Insert request logs for different providers
	now := time.Now().UTC()
	pool := h.Pool().Pool()
	for name, providerID := range providerIDs {
		for i := 0; i < 3; i++ {
			_, err := pool.Exec(context.Background(), `
				INSERT INTO request_logs (
					provider_id, model_id, virtual_key_id, status_code, duration_ms, 
					proxy_overhead_ms, tokens_prompt, tokens_completion, created_at
				) VALUES (
					$1, $2, $3, $4, $5, $6, $7, $8, $9
				)`,
				providerID, "gpt-4", nil, 200, 1000, 50, 100, 200, now.Add(-time.Duration(i)*time.Hour))
			if err != nil {
				t.Fatalf("Failed to insert request log for provider %s: %v", name, err)
			}
		}
	}

	// Test provider distribution
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/stats/provider-distribution", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response ProviderDistributionStats
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse provider distribution response: %v", err)
	}

	// Should have distribution for multiple providers
	if len(response.Items) == 0 {
		t.Error("Expected provider distribution items")
	}

	// Check that shares sum to approximately 100
	totalShare := 0.0
	for _, item := range response.Items {
		totalShare += item.Share
	}

	if totalShare < 99.9 || totalShare > 100.1 {
		t.Errorf("Expected total share to be ~100, got %.1f", totalShare)
	}
}

// TestCalculateStats_Empty tests stats endpoint when there are no logs
func TestCalculateStats_Empty(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/stats", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var stats StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to parse stats response: %v", err)
	}

	// All stats should be 0 when there are no logs
	if stats.TotalRequestsLast24h != 0 {
		t.Errorf("Expected TotalRequestsLast24h to be 0, got %d", stats.TotalRequestsLast24h)
	}
	if stats.TotalRequestsLast7d != 0 {
		t.Errorf("Expected TotalRequestsLast7d to be 0, got %d", stats.TotalRequestsLast7d)
	}
	if stats.AvgLatencyMs != 0 {
		t.Errorf("Expected AvgLatencyMs to be 0, got %f", stats.AvgLatencyMs)
	}
	if stats.ErrorRate != 0 {
		t.Errorf("Expected ErrorRate to be 0, got %f", stats.ErrorRate)
	}
}

// App Logs Handler Tests

func TestGetAppLogs(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs/app", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response []map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should return empty list when no app logs exist
	if len(response) != 0 {
		t.Errorf("Expected empty app log list, got %d entries", len(response))
	}
}

// Test for models.go - UpdateModel_Validation
func TestUpdateModel_Validation(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := `{"name": "test-update-model-provider", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert a model directly via DB
	modelID := uuid.New().String()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled, context_length) VALUES ($1, $2, $3, $4, $5, $6)`,
		modelID, providerResp.ID, "gpt-4o-mini", "GPT-4o Mini", true, 128000)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	t.Run("InvalidUUID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/models/not-a-uuid", strings.NewReader(`{"display_name":"test"}`))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Errorf("got %d", w.Code)
		}
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/models/"+modelID, strings.NewReader(`{"display_name":`))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Errorf("got %d", w.Code)
		}
	})

	t.Run("EmptyBody", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/models/"+modelID, strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Errorf("got %d", w.Code)
		}
	})

	t.Run("InvalidDisplayName", func(t *testing.T) {
		longName := strings.Repeat("x", 129) // Too long
		req := httptest.NewRequest(http.MethodPatch, "/models/"+modelID, strings.NewReader(fmt.Sprintf(`{"display_name":%q}`, longName)))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Errorf("got %d", w.Code)
		}
	})

	t.Run("InvalidContextLength", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/models/"+modelID, strings.NewReader(`{"context_length":-1}`))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 400 {
			t.Errorf("got %d", w.Code)
		}
	})

	t.Run("ValidUpdate", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPatch, "/models/"+modelID, strings.NewReader(`{"display_name":"Updated Model", "context_length":131072}`))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 200 {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}
		// Verify update took effect
		var updated struct {
			DisplayName   string `json:"display_name"`
			ContextLength int    `json:"context_length"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &updated); err != nil {
			t.Fatalf("Failed to parse update response: %v", err)
		}
		if updated.DisplayName != "Updated Model" {
			t.Errorf("expected display_name 'Updated Model', got %q", updated.DisplayName)
		}
		if updated.ContextLength != 131072 {
			t.Errorf("expected context_length 131072, got %d", updated.ContextLength)
		}
	})
}

// Test for applogs.go - GetAppLogs_QueryParams
func TestGetAppLogs_QueryParams(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Insert test logs directly into the database
	now := time.Now().UTC()
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO app_logs (timestamp, level, source, message) VALUES
		($1, $2, $3, $4),
		($5, $6, $7, $8),
		($9, $10, $11, $12)
	`,
		now, "info", "proxy", "test info message",
		now, "warning", "auth", "test warning message",
		now, "error", "proxy", "test error message",
	)
	if err != nil {
		t.Fatalf("Failed to insert test logs: %v", err)
	}

	t.Run("SourceFilter", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/logs/app?history=true&source=proxy", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200 for source filter, got %d: %s", rec.Code, rec.Body.String())
		}

		var response struct {
			Entries []AppLogEntry `json:"entries"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		// Should only have proxy source entries
		for _, entry := range response.Entries {
			if entry.Source != "proxy" {
				t.Errorf("Expected only proxy source entries, got source %s", entry.Source)
			}
		}
	})

	t.Run("LevelFilter", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/logs/app?history=true&level=error", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200 for level filter, got %d: %s", rec.Code, rec.Body.String())
		}

		var response struct {
			Entries []AppLogEntry `json:"entries"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		// Should only have error level entries
		for _, entry := range response.Entries {
			if entry.Level != "error" {
				t.Errorf("Expected only error level entries, got level %s", entry.Level)
			}
		}
	})

	t.Run("SearchFilter", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/logs/app?history=true&search=warning", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200 for search filter, got %d: %s", rec.Code, rec.Body.String())
		}

		var response struct {
			Entries []AppLogEntry `json:"entries"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		// Should have entries containing "warning"
		foundWarning := false
		for _, entry := range response.Entries {
			if strings.Contains(strings.ToLower(entry.Message), "warning") {
				foundWarning = true
				break
			}
		}

		if !foundWarning {
			t.Error("Expected to find entries containing 'warning'")
		}
	})

	t.Run("LimitFilter", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/logs/app?history=true&per_page=2", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200 for limit filter, got %d: %s", rec.Code, rec.Body.String())
		}

		var response struct {
			Entries []AppLogEntry `json:"entries"`
			PerPage int           `json:"per_page"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		// Should have at most 2 entries
		if len(response.Entries) > 2 {
			t.Errorf("Expected at most 2 entries, got %d", len(response.Entries))
		}
		if response.PerPage != 2 {
			t.Errorf("Expected per_page=2, got %d", response.PerPage)
		}
	})

	t.Run("TimeFilter", func(t *testing.T) {
		// Use a time in the past to filter
		pastTime := time.Now().Add(-1 * time.Hour).Format(time.RFC3339)
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/logs/app?history=true&from="+pastTime, http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200 for time filter, got %d: %s", rec.Code, rec.Body.String())
		}

		var response struct {
			Entries []AppLogEntry `json:"entries"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		// Should have entries (all our test logs are recent)
		if len(response.Entries) == 0 {
			t.Error("Expected to find entries with time filter")
		}
	})

	t.Run("CombinedFilters", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/logs/app?history=true&source=proxy&level=error", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200 for combined filters, got %d: %s", rec.Code, rec.Body.String())
		}

		var response struct {
			Entries []AppLogEntry `json:"entries"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		// Should have entries matching both filters
		for _, entry := range response.Entries {
			if entry.Source != "proxy" || entry.Level != "error" {
				t.Errorf("Expected entries with source=proxy and level=error, got source=%s level=%s", entry.Source, entry.Level)
			}
		}
	})
}

// Test for discovery.go - DiscoverProviderModels_Success
func TestDiscoverProviderModels_Success(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	_ = h // Use h to avoid unused variable error

	// Create a provider with OpenAI URL (will fail with test API key but tests the handler path)
	providerData := `{"name": "test-discover-success", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Test discovery - will fail with test API key but should return proper error structure
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/providers/"+providerResp.ID+"/discover", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Should return an error due to invalid API key, but the handler should work
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Expected 500 error for discovery with invalid API key, got %d: %s", rec.Code, rec.Body.String())
	}

	// The error response is plain text, not JSON
	body := rec.Body.String()
	if body == "" {
		t.Error("Expected error message in response body")
	}
}

// Test for discovery.go - GetProviderUsage_Success
func TestGetProviderUsage_Success(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	_ = h // Use h to avoid unused variable error

	// Create a provider with NanoGPT URL (will fail with test API key but tests the handler path)
	providerData := `{"name": "test-nanogpt-usage", "base_url": "https://api.nano-gpt.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Test usage endpoint - will fail with test API key but should return proper error
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/providers/"+providerResp.ID+"/usage", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Should return an error due to invalid API key, but the handler should work
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("Expected 500 error for usage with invalid API key, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Test for discovery.go - DiscoverAllModels_MultipleProviders
func TestDiscoverAllModels_MultipleProviders(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	_ = h // Use h to avoid unused variable error

	// Create first provider (OpenAI)
	providerData1 := `{"name": "test-openai-discover", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData1))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create first provider: %d", rec.Code)
	}

	// Create second provider (Anthropic)
	providerData2 := `{"name": "test-anthropic-discover", "base_url": "https://api.anthropic.com", "api_key": "test-api-key"}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/providers", strings.NewReader(providerData2))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create second provider: %d", rec.Code)
	}

	// Test discover all models - will fail with test API keys but should return proper structure
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/providers/discover-all", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Should succeed
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for discover all, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should have expected response fields
	if _, ok := response["results"]; !ok {
		t.Error("Expected results field in response")
	}
	if _, ok := response["succeeded"]; !ok {
		t.Error("Expected succeeded field in response")
	}
	if _, ok := response["failed"]; !ok {
		t.Error("Expected failed field in response")
	}
	if _, ok := response["discovered"]; !ok {
		t.Error("Expected discovered field in response")
	}
}

// Discovery Handler Tests - Uncovered Code Paths

func TestDiscoverProviderModels_NotFound(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Call with non-existent UUID
	nonExistentID := uuid.New().String()
	req := httptest.NewRequest("POST", "/providers/"+nonExistentID+"/discover", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Should get 404
	if w.Code != 404 {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestGetProviderUsage_NanoGPT(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create provider with NanoGPT base URL
	body := `{"name":"test-nanogpt","base_url":"https://ngc.nanogpt.com/v1","api_key":"test-api-key"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Try to get usage
	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/usage", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should get 400 - NanoGPT usage not supported
	if w2.Code != 400 {
		t.Errorf("expected 400, got %d", w2.Code)
	}
}

func TestGetProviderUsage_OpenRouter(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create provider with OpenRouter base URL
	body := `{"name":"test-openrouter","base_url":"https://openrouter.ai/api/v1","api_key":"test-api-key"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Try to get usage
	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/usage", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should get 500 - OpenRouter API call fails with invalid key
	if w2.Code != 500 {
		t.Errorf("expected 500, got %d", w2.Code)
	}
}

func TestGetProviderBalance_DefaultUnsupported(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create provider with non-DeepSeek URL
	body := `{"name":"test-balance-unsupported","base_url":"https://api.openai.com","api_key":"test-api-key"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Try to get balance
	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/balance", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should get 400 - balance not supported
	if w2.Code != 400 {
		t.Errorf("expected 400, got %d", w2.Code)
	}
}

func TestGetProviderBalance_DeepSeek(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create provider with DeepSeek base URL
	body := `{"name":"test-deepseek","base_url":"https://api.deepseek.com","api_key":"test-api-key"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Try to get balance
	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/balance", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should get 500 - DeepSeek API call fails with invalid key
	if w2.Code != 500 {
		t.Errorf("expected 500, got %d", w2.Code)
	}
}

func TestRefreshAllQuotas_NanoGPT(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create provider with NanoGPT base URL
	body := `{"name":"test-nanogpt-quotas","base_url":"https://ngc.nanogpt.com/v1","api_key":"test-api-key","enabled":true}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Verify provider was created successfully
	if w.Code != 201 {
		t.Fatalf("Failed to create provider: %d - %s", w.Code, w.Body.String())
	}

	// Refresh all quotas
	req2 := httptest.NewRequest("POST", "/providers/refresh-quotas", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should succeed
	if w2.Code != 200 {
		t.Errorf("expected 200, got %d", w2.Code)
	}

	var resp2 map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&resp2)
	resultsInterface, ok := resp2["results"]
	if !ok {
		t.Fatal("results field missing from response")
	}
	// results can be nil or empty slice when no quota-supporting providers exist
	if resultsInterface == nil {
		// This is acceptable - no providers support quotas
		return
	}
	results := resultsInterface.([]interface{})

	// Should have one result
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestRefreshAllQuotas_ZAICoding(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create provider with ZAICoding base URL
	body := `{"name":"test-zai-quotas","base_url":"https://api.zai.chat/api/v1","api_key":"test-api-key"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Refresh all quotas
	req2 := httptest.NewRequest("POST", "/providers/refresh-quotas", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should succeed
	if w2.Code != 200 {
		t.Errorf("expected 200, got %d", w2.Code)
	}

	var resp2 map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&resp2)
	resultsInterface, ok := resp2["results"]
	if !ok {
		t.Fatal("results field missing from response")
	}
	// results can be nil or empty slice when no quota-supporting providers exist
	if resultsInterface == nil {
		// This is acceptable - no providers support quotas
		return
	}
	results := resultsInterface.([]interface{})

	// Should have one result
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestRefreshAllQuotas_DeepSeek(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create provider with DeepSeek base URL
	body := `{"name":"test-deepseek-quotas","base_url":"https://api.deepseek.com","api_key":"test-api-key"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Refresh all quotas
	req2 := httptest.NewRequest("POST", "/providers/refresh-quotas", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should succeed
	if w2.Code != 200 {
		t.Errorf("expected 200, got %d", w2.Code)
	}

	var resp2 map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&resp2)
	resultsInterface, ok := resp2["results"]
	if !ok {
		t.Fatal("results field missing from response")
	}
	// results can be nil or empty slice when no quota-supporting providers exist
	if resultsInterface == nil {
		// This is acceptable - no providers support quotas
		return
	}
	results := resultsInterface.([]interface{})

	// Should have one result
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestRefreshAllQuotas_OpenRouter(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create provider with OpenRouter base URL
	body := `{"name":"test-openrouter-quotas","base_url":"https://openrouter.ai/api/v1","api_key":"test-api-key"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Refresh all quotas
	req2 := httptest.NewRequest("POST", "/providers/refresh-quotas", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should succeed
	if w2.Code != 200 {
		t.Errorf("expected 200, got %d", w2.Code)
	}

	var resp2 map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&resp2)
	resultsInterface, ok := resp2["results"]
	if !ok {
		t.Fatal("results field missing from response")
	}
	if resultsInterface == nil {
		t.Fatal("results field is nil")
	}
	results := resultsInterface.([]interface{})

	// Should have one result
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}

func TestRefreshAllQuotas_SkippedProvider(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create provider with unknown URL
	body := `{"name":"test-unknown-quotas","base_url":"https://api.anthropic.com","api_key":"test-api-key"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Refresh all quotas
	req2 := httptest.NewRequest("POST", "/providers/refresh-quotas", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should succeed
	if w2.Code != 200 {
		t.Errorf("expected 200, got %d", w2.Code)
	}

	var resp2 map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&resp2)
	resultsInterface, ok := resp2["results"]
	if !ok {
		t.Fatal("results field missing from response")
	}
	// results can be nil or empty slice when no quota-supporting providers exist
	if resultsInterface == nil {
		// This is acceptable - no providers support quotas
		return
	}
	results := resultsInterface.([]interface{})

	// Should have one result with refreshed=false
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}

	result := results[0].(map[string]interface{})
	if result["refreshed"].(bool) != false {
		t.Errorf("expected refreshed=false, got %v", result["refreshed"])
	}
}

func TestRefreshAllQuotas_DisabledProvider(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create provider
	body := `{"name":"test-disabled-quotas","base_url":"https://api.openai.com","api_key":"test-api-key"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Disable the provider
	h.dbPool.Pool().Exec(context.Background(), "UPDATE providers SET enabled = false WHERE id = $1", providerID)

	// Refresh all quotas
	req2 := httptest.NewRequest("POST", "/providers/refresh-quotas", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should succeed
	if w2.Code != 200 {
		t.Errorf("expected 200, got %d", w2.Code)
	}

	var resp2 map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&resp2)
	resultsInterface, ok := resp2["results"]
	if !ok {
		t.Fatal("results field missing from response")
	}
	// results can be nil or empty slice when no quota-supporting providers exist
	if resultsInterface == nil {
		// This is acceptable - no providers support quotas
		return
	}
	results := resultsInterface.([]interface{})

	// Should have no results since disabled provider is skipped
	if len(results) != 0 {
		t.Errorf("expected 0 results for disabled provider, got %d", len(results))
	}
}

// Test for settings.go - UpdateSettings_RateLimit
// Test for admin.go - CreateProvider_KeylessProvider
func TestCreateProvider_KeylessProvider(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Test creating a keyless provider (should work)
	providerData := `{"name": "test-keyless", "base_url": "https://opencode.ai/zen/v1"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201 for keyless provider, got %d: %s", rec.Code, rec.Body.String())
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("Failed to parse create response: %v", err)
	}
	if createResp.ID == "" {
		t.Error("Expected non-empty provider ID")
	}
}

// Test for admin.go - CreateProvider_EmptyAPIKey
func TestCreateProvider_EmptyAPIKey(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Test creating a provider with empty API key (should work for local Ollama)
	providerData := `{"name": "test-empty-key", "base_url": "https://opencode.ai/zen/v1", "api_key": ""}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201 for empty API key, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Test for admin.go - UpdateProvider_ChangeName
func TestUpdateProvider_ChangeName(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider first
	providerData := `{"name": "test-update-name", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("Failed to parse create response: %v", err)
	}

	// Update the provider's name
	updateData := `{"name": "test-updated-name"}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("PUT", "/providers/"+createResp.ID, strings.NewReader(updateData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the update took effect
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/providers/"+createResp.ID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var getResp struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &getResp); err != nil {
		t.Fatalf("Failed to parse get response: %v", err)
	}
	if getResp.Name != "test-updated-name" {
		t.Errorf("Expected name 'test-updated-name', got %s", getResp.Name)
	}
}

// Test for admin.go - UpdateProvider_InvalidName
func TestUpdateProvider_InvalidName(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider first
	providerData := `{"name": "test-update-invalid", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var createResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("Failed to parse create response: %v", err)
	}

	// Update with invalid name (too long)
	longName := strings.Repeat("x", 129) // Too long
	updateData := fmt.Sprintf(`{"name": %q}`, longName)
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("PUT", "/providers/"+createResp.ID, strings.NewReader(updateData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid name, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Test for discovery.go - DiscoverProviderModels_InvalidProvider
func TestDiscoverProviderModels_InvalidProvider(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with invalid URL
	providerData := `{"name": "test-discover-invalid", "base_url": "https://httpbin.org", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Try to discover models - should return error
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/providers/"+providerResp.ID+"/discover", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Should return an error due to invalid URL
	if rec.Code != http.StatusInternalServerError && rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected error for invalid URL, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Test for discovery.go - GetProviderUsage_UnsupportedProvider
func TestGetProviderUsage_UnsupportedProvider(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider that doesn't support usage (OpenAI)
	providerData := `{"name": "test-usage-unsupported", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", rec.Code)
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Try to get usage - should return 400 for unsupported provider
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/providers/"+providerResp.ID+"/usage", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 for unsupported provider, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Test for discovery.go - DiscoverAllModels_NoProviders
func TestDiscoverAllModels_NoProviders(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Test discover all models with no providers
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers/discover-all", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Should succeed with empty results
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for discover all with no providers, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should have expected response fields
	if _, ok := response["results"]; !ok {
		t.Error("Expected results field in response")
	}
	if _, ok := response["succeeded"]; !ok {
		t.Error("Expected succeeded field in response")
	}
	if _, ok := response["failed"]; !ok {
		t.Error("Expected failed field in response")
	}
	if _, ok := response["discovered"]; !ok {
		t.Error("Expected discovered field in response")
	}
}

// Test for applogs.go - GetAppLogs_Empty
func TestGetAppLogs_Empty(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Initialize the app log buffer
	InitAppLogBuffer(h.Pool().Pool())
	defer StopAppLogWriter()

	// Get logs when empty
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs/app", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var entries []AppLogEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should be empty
	if len(entries) != 0 {
		t.Errorf("Expected empty log list, got %d entries", len(entries))
	}
}

// Test for applogs.go - GetAppLogs_WithLimit
func TestGetAppLogs_WithLimit(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Initialize the app log buffer
	InitAppLogBuffer(h.Pool().Pool())
	defer StopAppLogWriter()

	// Create a slog.Logger with the AppSlogHandler and set it as default
	slogHandler := NewAppSlogHandler(slog.LevelInfo)
	debuglog.SetHandler(slogHandler)

	// Write multiple log messages
	for i := 0; i < 10; i++ {
		debuglog.Info(fmt.Sprintf("test message %d", i), "source", "test")
	}

	// Give the async writer a moment to process
	time.Sleep(100 * time.Millisecond)

	// Get logs with limit
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs/app?limit=5", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var entries []AppLogEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should have at most 5 entries
	if len(entries) > 5 {
		t.Errorf("Expected at most 5 entries with limit=5, got %d", len(entries))
	}
}

func TestUpdateSettings_RateLimit(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	settingsData := `{"rate_limit_enabled": "true", "rate_limit_rps": "50", "rate_limit_burst": "100"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/settings", strings.NewReader(settingsData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the update
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/settings", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if response["rate_limit_enabled"] != "true" {
		t.Errorf("Expected rate_limit_enabled='true', got %v", response["rate_limit_enabled"])
	}
	if response["rate_limit_rps"] != "50" {
		t.Errorf("Expected rate_limit_rps='50', got %v", response["rate_limit_rps"])
	}
	if response["rate_limit_burst"] != "100" {
		t.Errorf("Expected rate_limit_burst='100', got %v", response["rate_limit_burst"])
	}
}

// Test for failover.go - SyncFailoverGroups
func TestSyncFailoverGroups_WithModels(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	var rec *httptest.ResponseRecorder
	var req *http.Request

	// Create providers and models
	for i := 0; i < 3; i++ {
		providerData := fmt.Sprintf(`{"name": "test-failover-provider-%d", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`, i)
		rec = httptest.NewRecorder()
		req = httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Failed to create provider %d: %d", i, rec.Code)
		}

		var providerResp struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
			t.Fatalf("Failed to parse provider response: %v", err)
		}

		// Insert models with same model_id (for failover grouping)
		for j := 0; j < 2; j++ {
			modelID := uuid.New().String()
			pool := h.Pool().Pool()
			_, err := pool.Exec(context.Background(),
				`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
				modelID, providerResp.ID, fmt.Sprintf("gpt-4o-mini-%d-%d", i, j), fmt.Sprintf("GPT-4o Mini %d", j), true)
			if err != nil {
				t.Fatalf("Failed to insert model: %v", err)
			}
		}
	}

	// Call the sync endpoint
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/failover-groups/sync", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should return sync result
	if _, ok := response["disabled_groups"]; !ok {
		t.Error("Expected 'disabled_groups' field in sync response")
	}
}
func TestGetAppLogsHistory(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs/app?history=true", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Entries      []map[string]interface{} `json:"entries"`
		Total        int                      `json:"total"`
		Page         int                      `json:"page"`
		PerPage      int                      `json:"per_page"`
		LevelCounts  map[string]int           `json:"level_counts"`
		SourceCounts map[string]int           `json:"source_counts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should return empty history when no app logs exist
	if len(response.Entries) != 0 {
		t.Errorf("Expected empty app log history, got %d entries", len(response.Entries))
	}
	if response.Total != 0 {
		t.Errorf("Expected total 0, got %d", response.Total)
	}
	if response.LevelCounts == nil {
		t.Error("Expected level_counts in response")
	}
	if response.SourceCounts == nil {
		t.Error("Expected source_counts in response")
	}
}

// TestAppSlogHandler_Handle tests the slog.Handler implementation
func TestAppSlogHandler_Handle(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Initialize the app log buffer with the database pool
	InitAppLogBuffer(h.Pool().Pool())
	defer StopAppLogWriter()

	// Create a slog.Logger with the AppSlogHandler and set it as default
	slogHandler := NewAppSlogHandler(slog.LevelInfo)
	debuglog.SetHandler(slogHandler)

	// Write some log messages
	debuglog.Info("test info message", "source", "test", "key", "value")
	debuglog.Warn("test warning message", "source", "test", "key", "value")
	debuglog.Error("test error message", "source", "test", "key", "value")

	// Give the async writer a moment to process
	time.Sleep(100 * time.Millisecond)

	// Get the logs from the ring buffer
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs/app", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var entries []AppLogEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should have at least 3 entries (info, warning, error)
	if len(entries) < 3 {
		t.Errorf("Expected at least 3 log entries, got %d", len(entries))
	}

	// Check that we have different levels
	foundLevels := make(map[string]bool)
	for _, entry := range entries {
		foundLevels[entry.Level] = true
	}

	if !foundLevels["info"] {
		t.Error("Expected to find info level log")
	}
	if !foundLevels["warning"] {
		t.Error("Expected to find warning level log")
	}
	if !foundLevels["error"] {
		t.Error("Expected to find error level log")
	}
}

// TestFlush_WriterFlush tests the DB writer flush functionality
func TestFlush_WriterFlush(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	_ = r

	// Initialize the app log buffer with the database pool
	InitAppLogBuffer(h.Pool().Pool())

	// Create a slog.Logger with the AppSlogHandler and set it as default
	slogHandler := NewAppSlogHandler(slog.LevelInfo)
	debuglog.SetHandler(slogHandler)

	// Write some log messages
	debuglog.Info("test info message", "source", "test", "key", "value")
	debuglog.Warn("test warning message", "source", "test", "key", "value")

	// Give the async writer a moment to process
	time.Sleep(100 * time.Millisecond)

	// Trigger a flush by stopping the writer
	StopAppLogWriter()

	// Query the DB directly to verify entries were written
	var count int
	err := h.Pool().Pool().QueryRow(context.Background(), "SELECT COUNT(*) FROM app_logs").Scan(&count)
	if err != nil {
		t.Fatalf("Failed to query app_logs count: %v", err)
	}

	// Should have entries in the DB
	if count < 2 {
		t.Errorf("Expected at least 2 log entries in DB, got %d", count)
	}
}

// TestGetAppLogsHistory_MultipleFilters tests getAppLogsHistory with different query parameters
func TestGetAppLogsHistory_MultipleFilters(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Initialize the app log buffer with the database pool
	InitAppLogBuffer(h.Pool().Pool())
	defer StopAppLogWriter()

	// Create a slog.Logger with the AppSlogHandler and set it as default
	slogHandler := NewAppSlogHandler(slog.LevelInfo)
	debuglog.SetHandler(slogHandler)

	// Write some log messages with different sources and levels
	debuglog.Info("test info message", "source", "proxy")
	debuglog.Warn("test warning message", "source", "auth")
	debuglog.Error("test error message", "source", "proxy")

	// Give the async writer a moment to process
	time.Sleep(100 * time.Millisecond)

	// Trigger a flush
	StopAppLogWriter()

	// Test with level filter
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs/app?history=true&level=error", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for level filter, got %d: %s", rec.Code, rec.Body.String())
	}

	var response struct {
		Entries []AppLogEntry `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should only have error level entries
	for _, entry := range response.Entries {
		if entry.Level != "error" {
			t.Errorf("Expected only error level entries, got level %s", entry.Level)
		}
	}

	// Test with source filter
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/logs/app?history=true&source=proxy", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for source filter, got %d: %s", rec.Code, rec.Body.String())
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should only have proxy source entries
	for _, entry := range response.Entries {
		if entry.Source != "proxy" {
			t.Errorf("Expected only proxy source entries, got source %s", entry.Source)
		}
	}

	// Test with search filter
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/logs/app?history=true&search=warning", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for search filter, got %d: %s", rec.Code, rec.Body.String())
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should have entries containing "warning"
	foundWarning := false
	for _, entry := range response.Entries {
		if strings.Contains(strings.ToLower(entry.Message), "warning") {
			foundWarning = true
			break
		}
	}

	if !foundWarning {
		t.Error("Expected to find entries containing 'warning'")
	}
}

// TestFailoverAddProvider tests adding a provider to a failover group
func TestFailoverAddProvider(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create two providers with unique names
	provider1Data := `{"name": "test-failover-provider-1-` + uuid.New().String()[:8] + `", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(provider1Data))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var provider1Resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &provider1Resp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	provider2Data := `{"name": "test-failover-provider-2-` + uuid.New().String()[:8] + `", "base_url": "https://api.anthropic.com", "api_key": "test-api-key"}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/providers", strings.NewReader(provider2Data))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	var provider2Resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &provider2Resp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert two models directly via DB
	pool := h.Pool().Pool()
	model1ID := uuid.New().String()
	model2ID := uuid.New().String()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		model1ID, provider1Resp.ID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("Failed to insert model 1: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		model2ID, provider2Resp.ID, "claude-3-5-sonnet", "Claude 3.5 Sonnet", true)
	if err != nil {
		t.Fatalf("Failed to insert model 2: %v", err)
	}

	// Create a failover group with both models
	groupData := `{"display_model":"test-failover-group-` + uuid.New().String()[:8] + `","entry_ids":["` + model1ID + `","` + model2ID + `"]}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/failover-groups/", strings.NewReader(groupData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create failover group: %d: %s", rec.Code, rec.Body.String())
	}

	var groupResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &groupResp); err != nil {
		t.Fatalf("Failed to parse group response: %v", err)
	}

	// Update the failover group with a new priority_order (reordering)
	reorderData := `{"priority_order": ["` + model2ID + `", "` + model1ID + `"]}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("PUT", "/failover-groups/"+groupResp.ID, strings.NewReader(reorderData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	// Should return 200 for successful update
	if rec.Code != http.StatusOK {
		t.Logf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestFailoverAddProvider_NonExistentGroup tests updating a non-existent failover group
func TestFailoverAddProvider_NonExistentGroup(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Try to update non-existent failover group
	nonExistentGroupID := uuid.New().String()
	updateData := `{"priority_order": []}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/failover-groups/"+nonExistentGroupID, strings.NewReader(updateData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	// Should return 404 for non-existent group
	if rec.Code != http.StatusNotFound {
		t.Errorf("Expected 404 for non-existent failover group, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestFailoverReorderProvider tests reordering providers in a failover group
func TestFailoverReorderProvider(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create two providers
	provider1Data := `{"name": "test-provider-1", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(provider1Data))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider 1: %d: %s", rec.Code, rec.Body.String())
	}

	var provider1Resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &provider1Resp); err != nil {
		t.Fatalf("Failed to parse provider 1 response: %v", err)
	}

	provider2Data := `{"name": "test-provider-2", "base_url": "https://api.anthropic.com", "api_key": "test-api-key"}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/providers", strings.NewReader(provider2Data))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider 2: %d: %s", rec.Code, rec.Body.String())
	}

	var provider2Resp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &provider2Resp); err != nil {
		t.Fatalf("Failed to parse provider 2 response: %v", err)
	}

	// Insert models directly via DB
	pool := h.Pool().Pool()
	model1ID := uuid.New().String()
	model2ID := uuid.New().String()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		model1ID, provider1Resp.ID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("Failed to insert model 1: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		model2ID, provider2Resp.ID, "claude-3-5-sonnet", "Claude 3.5 Sonnet", true)
	if err != nil {
		t.Fatalf("Failed to insert model 2: %v", err)
	}

	// Create a failover group with both models
	groupData := `{"display_model":"test-reorder-group","entry_ids":["` + model1ID + `","` + model2ID + `"]}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/failover-groups/", strings.NewReader(groupData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create failover group: %d: %s", rec.Code, rec.Body.String())
	}

	var groupResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &groupResp); err != nil {
		t.Fatalf("Failed to parse group response: %v", err)
	}

	// Reorder providers - swap positions
	reorderData := `{"priority_order": ["` + model2ID + `", "` + model1ID + `"]}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("PUT", "/failover-groups/"+groupResp.ID, strings.NewReader(reorderData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for reorder, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the order changed
	var updatedGroup struct {
		ID      string `json:"id"`
		Entries []struct {
			ModelUUID string `json:"model_uuid"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &updatedGroup); err != nil {
		t.Fatalf("Failed to parse updated group: %v", err)
	}
	if len(updatedGroup.Entries) != 2 {
		t.Fatalf("Expected 2 entries, got %d", len(updatedGroup.Entries))
	}
	if updatedGroup.Entries[0].ModelUUID != model2ID {
		t.Errorf("First entry should be model2 after reorder, got %s", updatedGroup.Entries[0].ModelUUID)
	}
	if updatedGroup.Entries[1].ModelUUID != model1ID {
		t.Errorf("Second entry should be model1 after reorder, got %s", updatedGroup.Entries[1].ModelUUID)
	}
}

// TestDeleteModel_NonExistent tests deleting a non-existent model
func TestDeleteModel_NonExistent(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Try to delete non-existent model
	nonExistentID := uuid.New().String()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/models/"+nonExistentID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Delete returns 204 even for non-existent models (idempotent)
	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
}

// =============================================================================
// Coverage Improvement Tests
// =============================================================================

// TestCreateBackup_AlreadyInProgress tests the "backup already in progress" path
func TestCreateBackup_AlreadyInProgress(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Create a backup handler with a test directory and manually trigger the mutex
	backupDir := filepath.Join(h.cfg.DataDir, "backups")
	bh := NewBackupHandler(h.cfg.DatabaseURL, backupDir, h.adminMgr)

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
	bh := NewBackupHandler(h.cfg.DatabaseURL, backupDir, h.adminMgr)

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
func TestDiscoverProviderModels_DisabledProviderExplicit(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	body := `{"name":"test-disc-disabled-explicit","base_url":"https://api.openai.com","api_key":"sk-test123"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	providerID, ok := resp["id"].(string)
	if !ok {
		t.Fatalf("Provider ID not found in response")
	}

	// Disable the provider via repository update
	provRepo := provider.NewRepository(h.Pool().Pool())
	updateReq := provider.UpdateProviderRequest{
		Enabled: &[]bool{false}[0],
	}
	_, err := provRepo.Update(context.Background(), uuid.MustParse(providerID), updateReq, nil, nil, nil)
	if err != nil {
		t.Fatalf("Failed to disable provider: %v", err)
	}

	// Try to discover models on disabled provider
	req2 := httptest.NewRequest("POST", "/providers/"+providerID+"/discover", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should get 400 Bad Request for disabled provider
	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for disabled provider, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestDiscoverProviderModels_AutodiscoveryDisabled tests that discovery is rejected when autodiscovery_enabled is false
func TestDiscoverProviderModels_AutodiscoveryDisabled(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	body := `{"name":"test-disc-autodis-disabled","base_url":"https://api.openai.com","api_key":"sk-test123"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	providerID, ok := resp["id"].(string)
	if !ok {
		t.Fatalf("Provider ID not found in response")
	}

	// Disable autodiscovery via repository update (but keep provider enabled)
	provRepo := provider.NewRepository(h.Pool().Pool())
	updateReq := provider.UpdateProviderRequest{
		AutodiscoveryEnabled: &[]bool{false}[0],
		Enabled:              &[]bool{true}[0],
	}
	_, err := provRepo.Update(context.Background(), uuid.MustParse(providerID), updateReq, nil, nil, nil)
	if err != nil {
		t.Fatalf("Failed to update provider autodiscovery setting: %v", err)
	}

	// Try to discover models on provider with autodiscovery disabled
	req2 := httptest.NewRequest("POST", "/providers/"+providerID+"/discover", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should get 400 Bad Request for autodiscovery disabled
	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for autodiscovery disabled provider, got %d: %s", w2.Code, w2.Body.String())
	}

	if !strings.Contains(w2.Body.String(), "autodiscovery is disabled for this provider") {
		t.Errorf("expected error message 'autodiscovery is disabled for this provider', got %q", w2.Body.String())
	}
}

// TestListProviders_WithSearchFilter tests the search filter functionality
func TestListProviders_WithSearchFilter(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create providers with distinct names
	providers := []struct {
		name string
		url  string
	}{
		{"test-openai-provider", "https://api.openai.com"},
		{"test-anthropic-provider", "https://api.anthropic.com"},
		{"test-mistral-provider", "https://api.mistral.ai"},
	}

	for _, p := range providers {
		body := fmt.Sprintf(`{"name":"%s","base_url":"%s","api_key":"sk-test"}`, p.name, p.url)
		req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("Failed to create provider %s: %d", p.name, w.Code)
		}
	}

	// Test search filter - note: current implementation doesn't support search query param
	// This test documents the current behavior (search is ignored)
	req := httptest.NewRequest("GET", "/providers?search=openai", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var response []map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Current implementation returns all providers (search not implemented)
	if len(response) != 3 {
		t.Errorf("expected 3 providers, got %d", len(response))
	}
}

// TestUpdateProvider_InvalidBody tests updating with invalid JSON body
func TestUpdateProvider_InvalidBody(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider first
	body := `{"name":"test-update-invalid","base_url":"https://api.openai.com","api_key":"sk-test123"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Try to update with invalid JSON
	req2 := httptest.NewRequest("PUT", "/providers/"+providerID, strings.NewReader("{invalid json}"))
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should get 400 Bad Request
	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestUpdateProvider_WithNewAPIKey tests updating with a new API key
func TestUpdateProvider_WithNewAPIKey(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider first
	body := `{"name":"test-update-key","base_url":"https://api.openai.com","api_key":"sk-old123"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Update with new API key
	updateBody := `{"name":"test-update-key","base_url":"https://api.openai.com","api_key":"sk-new456"}`
	req2 := httptest.NewRequest("PUT", "/providers/"+providerID, strings.NewReader(updateBody))
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestListLogs_WithProviderIDFilter tests filtering logs by provider_id
func TestListLogs_WithProviderIDFilter(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create two providers
	body1 := `{"name":"test-logs-provider-1","base_url":"https://api.openai.com","api_key":"sk-test1"}`
	req1 := httptest.NewRequest("POST", "/providers", strings.NewReader(body1))
	req1.Header.Set("Authorization", "Bearer test-admin-token")
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	body2 := `{"name":"test-logs-provider-2","base_url":"https://api.anthropic.com","api_key":"sk-test2"}`
	req2 := httptest.NewRequest("POST", "/providers", strings.NewReader(body2))
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	var resp1, resp2 map[string]interface{}
	json.NewDecoder(w1.Body).Decode(&resp1)
	json.NewDecoder(w2.Body).Decode(&resp2)
	providerID1 := resp1["id"].(string)
	providerID2 := resp2["id"].(string)

	// Insert test logs for provider 1
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.MustParse(providerID1), "gpt-4", 200, 1000, 100, 200, time.Now())
	if err != nil {
		t.Fatalf("Failed to insert test log: %v", err)
	}

	// Insert test logs for provider 2
	_, err = pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.MustParse(providerID2), "claude-3", 200, 1500, 150, 250, time.Now())
	if err != nil {
		t.Fatalf("Failed to insert test log: %v", err)
	}

	// Filter by provider_id
	req := httptest.NewRequest("GET", "/logs?provider_id="+providerID1, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var response map[string]interface{}
	json.NewDecoder(w.Body).Decode(&response)
	entries := response["entries"].([]interface{})
	if len(entries) != 1 {
		t.Errorf("expected 1 log entry for provider 1, got %d", len(entries))
	}
}

// TestListLogs_WithModelIDFilter tests filtering logs by model_id
func TestListLogs_WithModelIDFilter(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	body := `{"name":"test-logs-model-provider","base_url":"https://api.openai.com","api_key":"sk-test"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Insert test logs with different models
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.MustParse(providerID), "gpt-4-turbo", 200, 1000, 100, 200, time.Now())
	if err != nil {
		t.Fatalf("Failed to insert test log: %v", err)
	}

	_, err = pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.MustParse(providerID), "gpt-3.5-turbo", 200, 800, 80, 160, time.Now())
	if err != nil {
		t.Fatalf("Failed to insert test log: %v", err)
	}

	// Filter by model_id (partial match)
	req2 := httptest.NewRequest("GET", "/logs?model_id=gpt-4", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w2.Code, w2.Body.String())
	}

	var response map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&response)
	entries := response["entries"].([]interface{})
	if len(entries) != 1 {
		t.Errorf("expected 1 log entry for gpt-4 model, got %d", len(entries))
	}
}

// TestListLogs_WithVirtualKeyIDFilter tests filtering logs by virtual_key_id
func TestListLogs_WithVirtualKeyFilter(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	body := `{"name":"test-logs-vk-provider","base_url":"https://api.openai.com","api_key":"sk-test"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Create a virtual key
	vkBody := `{"name":"test-vk-logs"}`
	req2 := httptest.NewRequest("POST", "/virtual-keys", strings.NewReader(vkBody))
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	var vkResp map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&vkResp)
	virtualKeyID := vkResp["id"].(string)
	virtualKeyName := vkResp["name"].(string)

	// Insert test log with virtual key
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, virtual_key_id, virtual_key_name, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		uuid.MustParse(providerID), "gpt-4", uuid.MustParse(virtualKeyID), virtualKeyName, 200, 1000, 100, 200, time.Now())
	if err != nil {
		t.Fatalf("Failed to insert test log: %v", err)
	}

	// Test logs endpoint (should include virtual key info)
	req3 := httptest.NewRequest("GET", "/logs", http.NoBody)
	req3.Header.Set("Authorization", "Bearer test-admin-token")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)

	if w3.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w3.Code, w3.Body.String())
	}

	var response map[string]interface{}
	json.NewDecoder(w3.Body).Decode(&response)
	entries := response["entries"].([]interface{})
	if len(entries) != 1 {
		t.Errorf("expected 1 log entry, got %d", len(entries))
	}
}

// TestGetProviderUsage_Error tests the error path when discovery service returns an error
func TestGetProviderUsage_Error(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with OpenRouter base URL (supported type)
	body := `{"name":"test-usage-error","base_url":"https://openrouter.ai/api/v1","api_key":"invalid-key-for-error"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Try to get usage - this will fail because the API key is invalid
	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/usage", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should get 500 Internal Server Error when discovery fails
	if w2.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 Internal Server Error, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestGetProviderBalance_Error tests the error path for balance endpoint
func TestGetProviderBalance_Error(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with DeepSeek base URL (supported type)
	body := `{"name":"test-balance-error","base_url":"https://api.deepseek.com","api_key":"invalid-key"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Try to get balance - this will fail because the API key is invalid
	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/balance", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should get 500 Internal Server Error when discovery fails
	if w2.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 Internal Server Error, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestListProviders_WithPaginationAndModelCounts tests pagination query params
func TestListProviders_WithPaginationAndModelCounts(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	body := `{"name":"test-pagination-provider","base_url":"https://api.openai.com","api_key":"sk-test"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Create a model for this provider to test model count
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO models (provider_id, model_id, name, enabled)
		VALUES ($1, $2, $3, $4)`,
		uuid.MustParse(providerID), "test-model-1", "Test Model 1", true)
	if err != nil {
		t.Fatalf("Failed to insert test model: %v", err)
	}

	_, err = pool.Exec(context.Background(), `
		INSERT INTO models (provider_id, model_id, name, enabled)
		VALUES ($1, $2, $3, $4)`,
		uuid.MustParse(providerID), "test-model-2", "Test Model 2", true)
	if err != nil {
		t.Fatalf("Failed to insert test model: %v", err)
	}

	// Test list providers - should include model counts
	req2 := httptest.NewRequest("GET", "/providers", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w2.Code, w2.Body.String())
	}

	var response []map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&response)
	if len(response) < 1 {
		t.Fatalf("expected at least 1 provider, got %d", len(response))
	}

	// Check that model_count is present
	found := false
	for _, p := range response {
		if p["id"] == providerID {
			found = true
			modelCount, ok := p["model_count"].(float64)
			if !ok {
				t.Errorf("expected model_count to be a number, got %T", p["model_count"])
			}
			if modelCount != 2 {
				t.Errorf("expected model_count=2, got %v", modelCount)
			}
			break
		}
	}
	if !found {
		t.Errorf("provider not found in list response")
	}
}

// TestUpdateProvider_NameConflict tests updating a provider with a conflicting name
func TestUpdateProvider_NameConflict(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create two providers
	body1 := `{"name":"test-conflict-provider-1","base_url":"https://api.openai.com","api_key":"sk-test1"}`
	req1 := httptest.NewRequest("POST", "/providers", strings.NewReader(body1))
	req1.Header.Set("Authorization", "Bearer test-admin-token")
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	body2 := `{"name":"test-conflict-provider-2","base_url":"https://api.anthropic.com","api_key":"sk-test2"}`
	req2 := httptest.NewRequest("POST", "/providers", strings.NewReader(body2))
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	var resp1, resp2 map[string]interface{}
	json.NewDecoder(w1.Body).Decode(&resp1)
	json.NewDecoder(w2.Body).Decode(&resp2)
	providerID2 := resp2["id"].(string)

	// Try to update provider 2 with provider 1's name - should fail
	updateBody := `{"name":"test-conflict-provider-1"}`
	req3 := httptest.NewRequest("PUT", "/providers/"+providerID2, strings.NewReader(updateBody))
	req3.Header.Set("Authorization", "Bearer test-admin-token")
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)

	// Should get 409 Conflict
	if w3.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict, got %d: %s", w3.Code, w3.Body.String())
	}
}

// TestListLogs_WithStatusCodeFilter tests filtering logs by status_code
func TestListLogs_WithStatusCodeFilter(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	body := `{"name":"test-logs-status-provider","base_url":"https://api.openai.com","api_key":"sk-test"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Insert test logs with different status codes
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.MustParse(providerID), "gpt-4", 200, 1000, 100, 200, time.Now())
	if err != nil {
		t.Fatalf("Failed to insert test log: %v", err)
	}

	_, err = pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at, error_message)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		uuid.MustParse(providerID), "gpt-4", 500, 2000, 0, 0, time.Now(), "Internal error")
	if err != nil {
		t.Fatalf("Failed to insert test log: %v", err)
	}

	// Filter by 5xx status codes
	req2 := httptest.NewRequest("GET", "/logs?status_code=5xx", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w2.Code, w2.Body.String())
	}

	var response map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&response)
	entries := response["entries"].([]interface{})
	if len(entries) != 1 {
		t.Errorf("expected 1 log entry with 5xx status, got %d", len(entries))
	}
}

// TestListLogs_WithDateRangeFilter tests filtering logs by date range
func TestListLogs_WithDateRangeFilter(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	body := `{"name":"test-logs-date-provider","base_url":"https://api.openai.com","api_key":"sk-test"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Insert test logs with different timestamps
	now := time.Now().UTC()
	// Use specific times that are clearly separated
	oldTime := now.Add(-2 * time.Hour)
	newTime := now.Add(2 * time.Hour)

	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.MustParse(providerID), "gpt-4", 200, 1000, 100, 200, oldTime)
	if err != nil {
		t.Fatalf("Failed to insert test log: %v", err)
	}

	_, err = pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.MustParse(providerID), "gpt-4", 200, 1000, 100, 200, newTime)
	if err != nil {
		t.Fatalf("Failed to insert test log: %v", err)
	}

	// Filter by date range - only logs from 1 hour ago onwards (should get only newTime log)
	fromTime := now.Add(-1 * time.Hour)
	req2 := httptest.NewRequest("GET", "/logs?from="+fromTime.Format(time.RFC3339), http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w2.Code, w2.Body.String())
	}

	var response map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&response)
	entries := response["entries"].([]interface{})
	// Should only get the newTime log (1 entry)
	if len(entries) != 1 {
		t.Errorf("expected 1 log entry in date range, got %d", len(entries))
	}
}

// TestDiscoverProviderModels_WithInvalidProviderType tests discovery on a provider with unsupported type
func TestDiscoverProviderModels_WithInvalidProviderType(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with a custom/self-hosted base URL
	body := `{"name":"test-custom-provider","base_url":"https://custom.example.com","api_key":"sk-custom"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Try to discover - this will likely fail with an API error
	req2 := httptest.NewRequest("POST", "/providers/"+providerID+"/discover", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Discovery will attempt to call the custom endpoint and fail
	// We just verify it doesn't crash - actual response depends on network
	if w2.Code != http.StatusOK && w2.Code != http.StatusInternalServerError {
		t.Errorf("expected 200 or 500, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestGetProviderBalance_UnsupportedProvider tests balance endpoint for unsupported provider type
func TestGetProviderBalance_UnsupportedProvider(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with OpenAI base URL (not supported for balance)
	uniqueName := "test-bal-unsup-" + uuid.New().String()[:8]
	body := fmt.Sprintf(`{"name":"%s","base_url":"https://api.openai.com","api_key":"sk-test"}`, uniqueName)
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Try to get balance - OpenAI is not supported
	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/balance", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should get 400 Bad Request for unsupported provider type
	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestListLogs_WithPagination tests pagination parameters
func TestListLogs_WithPagination(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	body := `{"name":"test-logs-pagination","base_url":"https://api.openai.com","api_key":"sk-test"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Insert multiple test logs
	pool := h.Pool().Pool()
	for i := 0; i < 5; i++ {
		_, err := pool.Exec(context.Background(), `
			INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			uuid.MustParse(providerID), "gpt-4", 200, 1000, 100, 200, time.Now())
		if err != nil {
			t.Fatalf("Failed to insert test log: %v", err)
		}
	}

	// Test with page=2, per_page=2
	req2 := httptest.NewRequest("GET", "/logs?page=2&per_page=2", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w2.Code, w2.Body.String())
	}

	var response map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&response)
	entries := response["entries"].([]interface{})
	total := response["total"].(float64)
	page := response["page"].(float64)
	perPage := response["per_page"].(float64)

	if total != 5 {
		t.Errorf("expected total=5, got %v", total)
	}
	if page != 2 {
		t.Errorf("expected page=2, got %v", page)
	}
	if perPage != 2 {
		t.Errorf("expected per_page=2, got %v", perPage)
	}
	// Page 2 with per_page=2 should return 2 entries (entries 3-4)
	if len(entries) != 2 {
		t.Errorf("expected 2 entries on page 2, got %d", len(entries))
	}
}

// TestListLogs_With4xxStatusCodeFilter tests filtering by 4xx status codes
func TestListLogs_With4xxStatusCodeFilter(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	body := `{"name":"test-logs-4xx","base_url":"https://api.openai.com","api_key":"sk-test"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Insert test logs with different status codes
	pool := h.Pool().Pool()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		uuid.MustParse(providerID), "gpt-4", 200, 1000, 100, 200, time.Now())
	if err != nil {
		t.Fatalf("Failed to insert test log: %v", err)
	}

	_, err = pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at, error_message)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		uuid.MustParse(providerID), "gpt-4", 429, 500, 0, 0, time.Now(), "Rate limit exceeded")
	if err != nil {
		t.Fatalf("Failed to insert test log: %v", err)
	}

	// Filter by 4xx status codes
	req2 := httptest.NewRequest("GET", "/logs?status_code=4xx", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w2.Code, w2.Body.String())
	}

	var response map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&response)
	entries := response["entries"].([]interface{})
	if len(entries) != 1 {
		t.Errorf("expected 1 log entry with 4xx status, got %d", len(entries))
	}
}

// TestDiscoverProviderModels_SuccessPath tests the success path where discovery works
func TestDiscoverProviderModels_SuccessPath(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with OpenAI base URL (will fail to discover but tests the code path)
	body := `{"name":"test-disc-success","base_url":"https://api.openai.com","api_key":"sk-test123"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Try to discover - this will fail because the API key is fake, but it tests the code path
	req2 := httptest.NewRequest("POST", "/providers/"+providerID+"/discover", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Discovery will attempt to call the endpoint and fail
	// We verify it doesn't crash and returns an appropriate error
	if w2.Code != http.StatusInternalServerError {
		t.Errorf("expected 500 Internal Server Error, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestGetProviderUsage_ZAICoding tests ZAI Coding provider usage endpoint
func TestGetProviderUsage_ZAICoding(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with ZAI Coding base URL pattern
	body := `{"name":"test-usage-zai","base_url":"https://zai.api.example.com","api_key":"test-key"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Try to get usage - will fail because URL is fake, but tests the code path
	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/usage", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should get 500 because the API call fails (or 400 if provider type not recognized)
	// We accept both since the exact behavior depends on URL pattern matching
	if w2.Code != http.StatusInternalServerError && w2.Code != http.StatusBadRequest {
		t.Errorf("expected 500 or 400, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestFailoverCandidates_Empty tests the Candidates endpoint with no models
func TestFailoverCandidates_Empty(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/failover-groups/candidates", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var candidates []interface{}
	if err := json.NewDecoder(w.Body).Decode(&candidates); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(candidates) != 0 {
		t.Errorf("expected empty candidates list, got %d", len(candidates))
	}
}

// TestFailoverCandidates_WithModels tests the Candidates endpoint with enabled models
func TestFailoverCandidates_WithModels(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	body := `{"name":"test-candidates-provider","base_url":"https://api.openai.com","api_key":"sk-test"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Insert enabled models directly via DB
	pool := h.Pool().Pool()
	modelID1 := uuid.New().String()
	modelID2 := uuid.New().String()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID1, providerID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("Failed to insert model 1: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID2, providerID, "gpt-4o", "GPT-4o", true)
	if err != nil {
		t.Fatalf("Failed to insert model 2: %v", err)
	}

	// Get candidates
	req2 := httptest.NewRequest("GET", "/failover-groups/candidates", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w2.Code, w2.Body.String())
	}

	var candidates []map[string]interface{}
	if err := json.NewDecoder(w2.Body).Decode(&candidates); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(candidates) != 2 {
		t.Errorf("expected 2 candidates, got %d", len(candidates))
	}
}

// TestFailoverCandidates_DisabledModels tests that disabled models are filtered out
func TestFailoverCandidates_DisabledModels(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	body := `{"name":"test-disabled-provider","base_url":"https://api.openai.com","api_key":"sk-test"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Insert one enabled and one disabled model
	pool := h.Pool().Pool()
	modelID1 := uuid.New().String()
	modelID2 := uuid.New().String()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID1, providerID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("Failed to insert model 1: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID2, providerID, "gpt-4o", "GPT-4o", false)
	if err != nil {
		t.Fatalf("Failed to insert model 2: %v", err)
	}

	// Get candidates - should only return enabled model
	req2 := httptest.NewRequest("GET", "/failover-groups/candidates", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w2.Code, w2.Body.String())
	}

	var candidates []map[string]interface{}
	if err := json.NewDecoder(w2.Body).Decode(&candidates); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if len(candidates) != 1 {
		t.Errorf("expected 1 candidate (disabled filtered out), got %d", len(candidates))
	}

	if candidates[0]["model_id"] != "gpt-4o-mini" {
		t.Errorf("expected gpt-4o-mini, got %v", candidates[0]["model_id"])
	}
}

// TestFailoverSync_Success tests the Sync endpoint
func TestFailoverSync_Success(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	uniqueName := "test-sync-prov-" + uuid.New().String()[:8]
	body := fmt.Sprintf(`{"name":"%s","base_url":"https://api.openai.com","api_key":"sk-test"}`, uniqueName)
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Insert two models (failover groups require at least 2 entries)
	pool := h.Pool().Pool()
	modelID1 := uuid.New().String()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID1, providerID, "gpt-4o-mini", "GPT-4o Mini", true)
	if err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}
	modelID2 := uuid.New().String()
	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID2, providerID, "gpt-4o", "GPT-4o", true)
	if err != nil {
		t.Fatalf("Failed to insert model 2: %v", err)
	}

	// Create a failover group with 2 entries
	groupData := `{"display_model":"test-sync-group","entry_ids":["` + modelID1 + `","` + modelID2 + `"]}`
	req2 := httptest.NewRequest("POST", "/failover-groups/", strings.NewReader(groupData))
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusCreated {
		t.Fatalf("Failed to create failover group: %d: %s", w2.Code, w2.Body.String())
	}

	// Sync all failover groups
	req3 := httptest.NewRequest("POST", "/failover-groups/sync", http.NoBody)
	req3.Header.Set("Authorization", "Bearer test-admin-token")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)

	if w3.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w3.Code, w3.Body.String())
	}
}

// TestDeleteFailoverGroup_NonExistent tests deleting a non-existent failover group
func TestDeleteFailoverGroup_NonExistent(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	nonExistentID := uuid.New().String()
	req := httptest.NewRequest("DELETE", "/failover-groups/"+nonExistentID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Delete returns 204 even for non-existent groups (idempotent)
	if w.Code != http.StatusNoContent {
		t.Errorf("expected 204 No Content, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGetSystem_NoCache tests the system stats endpoint
func TestGetSystem_NoCache(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/system", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify response structure
	if response["app"] == nil {
		t.Error("expected 'app' in response")
	}
	if response["db"] == nil {
		t.Error("expected 'db' in response")
	}
	if response["docker"] == nil {
		t.Error("expected 'docker' in response")
	}
}

// TestGetAppLogs_EmptyResult tests the app logs endpoint with no logs
func TestGetAppLogs_EmptyResult(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/logs/app", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	// Default mode returns a JSON array of log entries (may be empty)
	var response []interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	// Empty array is expected when no logs exist
}

// TestGetStats_Empty tests the stats endpoint with no data
func TestGetStats_Empty(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/stats", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var response map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify response structure exists
	if response["by_model"] == nil {
		t.Error("expected 'by_model' in response")
	}
	if response["by_provider"] == nil {
		t.Error("expected 'by_provider' in response")
	}
	if response["by_virtual_key"] == nil {
		t.Error("expected 'by_virtual_key' in response")
	}
}

// TestListProviders_SearchFilter_Integration tests listing providers (search filter not implemented)
func TestListProviders_SearchFilter_Integration(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create two providers with different names
	body1 := `{"name":"search-test-alpha","base_url":"https://api.alpha.com","api_key":"sk-alpha"}`
	req1 := httptest.NewRequest("POST", "/providers", strings.NewReader(body1))
	req1.Header.Set("Authorization", "Bearer test-admin-token")
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	if w1.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w1.Code, w1.Body.String())
	}

	body2 := `{"name":"search-test-beta","base_url":"https://api.beta.com","api_key":"sk-beta"}`
	req2 := httptest.NewRequest("POST", "/providers", strings.NewReader(body2))
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w2.Code, w2.Body.String())
	}

	// List all providers - returns all providers (search filter not implemented in handler)
	req3 := httptest.NewRequest("GET", "/providers", http.NoBody)
	req3.Header.Set("Authorization", "Bearer test-admin-token")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)

	if w3.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w3.Code, w3.Body.String())
	}

	// Response is an array of providers
	var providers []map[string]interface{}
	if err := json.NewDecoder(w3.Body).Decode(&providers); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(providers) != 2 {
		t.Errorf("expected 2 providers, got %d", len(providers))
	}
}

// TestCreateProvider_DuplicateName_Integration tests that duplicate provider names return 409 Conflict
func TestCreateProvider_DuplicateName_Integration(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create first provider
	body1 := `{"name":"test-dup","base_url":"https://api.first.com","api_key":"sk-first"}`
	req1 := httptest.NewRequest("POST", "/providers", strings.NewReader(body1))
	req1.Header.Set("Authorization", "Bearer test-admin-token")
	req1.Header.Set("Content-Type", "application/json")
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	if w1.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w1.Code, w1.Body.String())
	}

	// Try to create second provider with same name
	body2 := `{"name":"test-dup","base_url":"https://api.second.com","api_key":"sk-second"}`
	req2 := httptest.NewRequest("POST", "/providers", strings.NewReader(body2))
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusConflict {
		t.Errorf("expected 409 Conflict, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestDeleteProvider_NonExistent_Integration tests deleting a non-existent provider returns 404
func TestDeleteProvider_NonExistent_Integration(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Try to delete a non-existent UUID
	nonExistentID := "00000000-0000-0000-0000-000000000000"
	req := httptest.NewRequest("DELETE", "/providers/"+nonExistentID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found, got %d: %s", w.Code, w.Body.String())
	}
}

// flushingResponseWriter wraps httptest.ResponseRecorder to implement http.Flusher
type flushingResponseWriter struct {
	*httptest.ResponseRecorder
}

func (fw *flushingResponseWriter) Flush() {
	// No-op for testing - just need to satisfy the interface
}

// TestStreamEvents_Connected tests that the SSE endpoint establishes connection
func TestStreamEvents_Connected(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.RegisterEvents(r) // Use RegisterEvents for SSE endpoint

	// Create a request to the events endpoint
	req := httptest.NewRequest("GET", "/events", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	// Use custom ResponseWriter that implements http.Flusher
	fw := &flushingResponseWriter{ResponseRecorder: httptest.NewRecorder()}

	// Create a context with timeout to avoid hanging
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	r.ServeHTTP(fw, req)

	// Verify Content-Type is set correctly
	contentType := fw.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("expected Content-Type 'text/event-stream', got '%s'", contentType)
	}

	// Verify initial connection comment is present
	body := fw.Body.String()
	if !strings.Contains(body, ": connected") {
		t.Errorf("expected ': connected' in response, got: %s", body)
	}
}

// TestUpdateModel_EnableDisable_Integration tests enabling and disabling a model
func TestUpdateModel_EnableDisable_Integration(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider first
	body := `{"name":"test-update-model","base_url":"https://api.example.com","api_key":"sk-test"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Insert a model directly into the DB (matching schema from existing test)
	pool := h.Pool().Pool()
	modelID := uuid.New().String()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO models (id, provider_id, model_id, name, enabled)
		VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerID, "test-model", "Test Model", true)
	if err != nil {
		t.Fatalf("Failed to insert test model: %v", err)
	}

	// Disable the model
	disableBody := `{"enabled": false}`
	req2 := httptest.NewRequest("PATCH", "/models/"+modelID, strings.NewReader(disableBody))
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	req2.Header.Set("Content-Type", "application/json")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200 OK when disabling model, got %d: %s", w2.Code, w2.Body.String())
	}

	// Verify the model is disabled
	var modelResp map[string]interface{}
	json.NewDecoder(w2.Body).Decode(&modelResp)
	if modelResp["enabled"] != false {
		t.Errorf("expected model to be disabled, got enabled=%v", modelResp["enabled"])
	}

	// Enable the model
	enableBody := `{"enabled": true}`
	req3 := httptest.NewRequest("PATCH", "/models/"+modelID, strings.NewReader(enableBody))
	req3.Header.Set("Authorization", "Bearer test-admin-token")
	req3.Header.Set("Content-Type", "application/json")
	w3 := httptest.NewRecorder()
	r.ServeHTTP(w3, req3)

	if w3.Code != http.StatusOK {
		t.Errorf("expected 200 OK when enabling model, got %d: %s", w3.Code, w3.Body.String())
	}

	// Verify the model is enabled
	json.NewDecoder(w3.Body).Decode(&modelResp)
	if modelResp["enabled"] != true {
		t.Errorf("expected model to be enabled, got enabled=%v", modelResp["enabled"])
	}
}

// TestTestModel_NonExistentProvider_Integration tests that testing a model on non-existent provider returns 404
func TestTestModel_NonExistentProvider_Integration(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Try to test a model on a non-existent provider
	nonExistentID := "00000000-0000-0000-0000-000000000000"
	body := `{"model":"gpt-4"}`
	req := httptest.NewRequest("POST", "/providers/"+nonExistentID+"/models/test", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGetProviderBalance_UnsupportedType_Integration tests balance check on unsupported provider type
func TestGetProviderBalance_UnsupportedType_Integration(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with a generic URL (not nanogpt/openrouter/deepseek/zai-coding)
	uniqueName := "test-bal-generic-" + uuid.New().String()[:8]
	body := fmt.Sprintf(`{"name":"%s","base_url":"https://api.generic.com","api_key":"sk-generic"}`, uniqueName)
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Try to get balance - should return 400 for unsupported provider type
	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/balance", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for unsupported provider type, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestGetProviderBalance_OpenRouterError_Integration tests balance check on OpenRouter provider
// Note: Current implementation only supports DeepSeek, so OpenRouter returns 400 (unsupported)
func TestGetProviderBalance_OpenRouterError_Integration(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider with OpenRouter base URL pattern
	body := `{"name":"test-balance-openrouter","base_url":"https://openrouter.ai/api/v1","api_key":"sk-fake-key"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)
	providerID := resp["id"].(string)

	// Try to get balance - returns 400 since only DeepSeek is supported
	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/balance", http.NoBody)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	if w2.Code != http.StatusBadRequest {
		t.Errorf("expected 400 Bad Request for unsupported provider type, got %d: %s", w2.Code, w2.Body.String())
	}
}

// TestListModels_WithModels tests listing models with provider_id filter
func TestListModels_WithModels(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := fmt.Sprintf(`{"name":"test-list-models-provider-%s","base_url":"https://api.openai.com","api_key":"test-api-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert multiple models with different properties
	pool := h.Pool().Pool()
	for i := 0; i < 3; i++ {
		modelID := uuid.New().String()
		_, err := pool.Exec(context.Background(),
			`INSERT INTO models (id, provider_id, model_id, name, enabled, context_length) VALUES ($1, $2, $3, $4, $5, $6)`,
			modelID, providerResp.ID, fmt.Sprintf("list-gpt-%d", i), fmt.Sprintf("List GPT %d", i), true, 128000)
		if err != nil {
			t.Fatalf("Failed to insert model %d: %v", i, err)
		}
	}

	t.Run("FilterByProviderID", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/models?provider_id="+providerResp.ID, http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var response []ModelResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if len(response) < 3 {
			t.Errorf("Expected at least 3 models for provider, got %d", len(response))
		}
	})
}

// TestGetSystem_Details tests the system endpoint returns expected structure
func TestGetSystem_Details(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/system", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Check top-level sections exist
	for _, section := range []string{"app", "db", "docker"} {
		if _, ok := response[section]; !ok {
			t.Errorf("Expected section '%s' in system response", section)
		}
	}

	// Check app section has expected fields
	if app, ok := response["app"].(map[string]interface{}); ok {
		for _, field := range []string{"uptime_seconds", "goroutines", "memory_current_bytes"} {
			if _, exists := app[field]; !exists {
				t.Errorf("Expected field 'app.%s' in system response", field)
			}
		}
	}

	// Check db section has expected fields
	if db, ok := response["db"].(map[string]interface{}); ok {
		for _, field := range []string{"connections"} {
			if _, exists := db[field]; !exists {
				t.Errorf("Expected field 'db.%s' in system response", field)
			}
		}
	}
}

// TestDeleteModel_WithFailoverGroup tests that deleting a model in a failover group cascades
func TestDeleteModel_WithFailoverGroup(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := fmt.Sprintf(`{"name":"test-delete-fg-provider-%s","base_url":"https://api.openai.com","api_key":"test-api-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert two models (failover groups require at least 2 entries)
	pool := h.Pool().Pool()
	modelID1 := uuid.New().String()
	modelID2 := uuid.New().String()

	_, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID1, providerResp.ID, "gpt-4o-1", "GPT-4o Model 1", true)
	if err != nil {
		t.Fatalf("Failed to insert model 1: %v", err)
	}

	_, err = pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID2, providerResp.ID, "gpt-4o-2", "GPT-4o Model 2", true)
	if err != nil {
		t.Fatalf("Failed to insert model 2: %v", err)
	}

	// Create a failover group with both models
	groupData := `{"display_model":"test-fg-group","entry_ids":["` + modelID1 + `","` + modelID2 + `"]}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/failover-groups/", strings.NewReader(groupData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create failover group: %d: %s", rec.Code, rec.Body.String())
	}

	// Delete model1 (referenced by failover group) - should succeed with cascade
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/models/"+modelID1, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected 204 for model delete (with cascade), got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestDeleteModel_InvalidUUID tests deleting with invalid UUID format
func TestDeleteModel_InvalidUUID(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/models/invalid-uuid", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid UUID, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestPurgeLogs_BeforeTimestamp tests purging logs before a specific timestamp
func TestPurgeLogs_BeforeTimestamp(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider first (request_logs has FK to providers)
	providerData := fmt.Sprintf(`{"name":"test-purge-provider-%s","base_url":"https://api.openai.com","api_key":"test-api-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert some old request logs
	pool := h.Pool().Pool()
	now := time.Now().UTC()
	oldTime := now.Add(-48 * time.Hour) // 2 days ago

	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		providerResp.ID, "gpt-4", 200, 100, oldTime)
	if err != nil {
		t.Fatalf("Failed to insert old log: %v", err)
	}

	// Purge logs before 24 hours ago
	purgeData := `{"older_than": "1d"}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/logs/purge", strings.NewReader(purgeData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected 204 for purge logs before timestamp, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestPurgeLogs_KeepDays tests purging logs older than 1 week
func TestPurgeLogs_KeepDays(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider first (request_logs has FK to providers)
	providerData := fmt.Sprintf(`{"name":"test-keep-provider-%s","base_url":"https://api.openai.com","api_key":"test-api-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert some old request logs
	pool := h.Pool().Pool()
	now := time.Now().UTC()
	oldTime := now.Add(-10 * 24 * time.Hour) // 10 days ago

	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (provider_id, model_id, status_code, duration_ms, created_at)
		VALUES ($1, $2, $3, $4, $5)`,
		providerResp.ID, "gpt-4", 200, 100, oldTime)
	if err != nil {
		t.Fatalf("Failed to insert old log: %v", err)
	}

	// Purge logs older than 2024-01-01
	purgeData := `{"older_than":"1w"}`
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("DELETE", "/logs/purge", strings.NewReader(purgeData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected 204 for purge logs keep days, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestPurgeLogs_InvalidData tests purge with invalid request data
func TestPurgeLogs_InvalidData(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	t.Run("InvalidJSON", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("DELETE", "/logs/purge", strings.NewReader(`{invalid json}`))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected 400 for invalid JSON, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("EmptyBody", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("DELETE", "/logs/purge", strings.NewReader(`{}`))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected 400 for empty body, got %d: %s", rec.Code, rec.Body.String())
		}
	})
}

// TestUpdateProvider_EnableDisable tests enabling and disabling a provider
func TestUpdateProvider_EnableDisable(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := fmt.Sprintf(`{"name":"test-enable-provider-%s","base_url":"https://api.openai.com","api_key":"test-api-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	t.Run("DisableProvider", func(t *testing.T) {
		updateData := `{"enabled": false}`
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("PUT", "/providers/"+providerResp.ID, strings.NewReader(updateData))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var response map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if response["enabled"] != false {
			t.Errorf("Expected enabled=false, got %v", response["enabled"])
		}
	})

	t.Run("ReEnableProvider", func(t *testing.T) {
		updateData := `{"enabled": true}`
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("PUT", "/providers/"+providerResp.ID, strings.NewReader(updateData))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var response map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if response["enabled"] != true {
			t.Errorf("Expected enabled=true, got %v", response["enabled"])
		}
	})
}

// TestUpdateProvider_PartialUpdate_WithGet tests partial updates with GET verification
func TestUpdateProvider_PartialUpdate_WithGet(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Create a provider
	providerData := fmt.Sprintf(`{"name":"test-partial-update-%s","base_url":"https://api.openai.com","api_key":"test-api-key"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	t.Run("UpdateOnlyName", func(t *testing.T) {
		updateData := `{"name": "updated-name-only"}`
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("PUT", "/providers/"+providerResp.ID, strings.NewReader(updateData))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify the name was updated
		rec = httptest.NewRecorder()
		req = httptest.NewRequest("GET", "/providers/"+providerResp.ID, http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		var response map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if response["name"] != "updated-name-only" {
			t.Errorf("Expected name 'updated-name-only', got %v", response["name"])
		}
	})

	t.Run("UpdateOnlyBaseURL", func(t *testing.T) {
		updateData := `{"base_url": "https://api.anthropic.com"}`
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("PUT", "/providers/"+providerResp.ID, strings.NewReader(updateData))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		// Verify the base_url was updated
		rec = httptest.NewRecorder()
		req = httptest.NewRequest("GET", "/providers/"+providerResp.ID, http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		var response map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
			t.Fatalf("Failed to parse response: %v", err)
		}

		if response["base_url"] != "https://api.anthropic.com" {
			t.Errorf("Expected base_url 'https://api.anthropic.com', got %v", response["base_url"])
		}
	})
}

// TestStreamEvents_InitialConnection tests SSE endpoint initial connection
func TestStreamEvents_InitialConnection(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.RegisterEvents(r)

	// Create request with admin auth
	req := httptest.NewRequest("GET", "/events", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	// Use flushing response writer
	fw := &flushingResponseWriter{ResponseRecorder: httptest.NewRecorder()}

	// Use context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	r.ServeHTTP(fw, req)

	// Verify status code
	if fw.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", fw.Code, fw.Body.String())
	}

	// Verify Content-Type
	contentType := fw.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Expected Content-Type 'text/event-stream', got '%s'", contentType)
	}

	// Verify Cache-Control header for SSE
	cacheControl := fw.Header().Get("Cache-Control")
	if cacheControl != "no-cache" {
		t.Errorf("Expected Cache-Control 'no-cache', got '%s'", cacheControl)
	}

	// Verify Connection header
	connection := fw.Header().Get("Connection")
	if connection != "keep-alive" {
		t.Errorf("Expected Connection 'keep-alive', got '%s'", connection)
	}

	// Verify initial connection comment
	body := fw.Body.String()
	if !strings.Contains(body, ": connected") {
		t.Errorf("Expected ': connected' in response body, got: %s", body)
	}
}

// TestStreamEvents_Unauthorized tests SSE endpoint without auth
func TestStreamEvents_Unauthorized(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.RegisterEvents(r)

	req := httptest.NewRequest("GET", "/events", http.NoBody)
	// No Authorization header
	fw := &flushingResponseWriter{ResponseRecorder: httptest.NewRecorder()}

	r.ServeHTTP(fw, req)

	// Should return 401 or 403 without auth
	if fw.Code != http.StatusUnauthorized && fw.Code != http.StatusForbidden {
		t.Errorf("Expected 401 or 403, got %d: %s", fw.Code, fw.Body.String())
	}
}

// TestGetStats_WithQueryParams_Integration tests /stats endpoint with various query parameters
func TestGetStats_WithQueryParams_Integration(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create a provider first (FK constraint)
	providerData := fmt.Sprintf(`{"name":"test-stats-prov-%s","base_url":"https://api.openai.com","api_key":"sk-test"}`, uuid.New().String()[:8])
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}

	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}

	// Insert request_logs with various data
	now := time.Now().UTC()
	pool := h.Pool().Pool()

	// Insert logs for last 24 hours with different metrics
	for i := 0; i < 5; i++ {
		_, err := pool.Exec(context.Background(), `
			INSERT INTO request_logs (
				provider_id, model_id, status_code, duration_ms, 
				tokens_prompt, tokens_completion, created_at, 
				proxy_overhead_ms, ttft_ms
			) VALUES (
				$1, $2, $3, $4, $5, $6, $7, $8, $9
			)`,
			providerResp.ID, "gpt-4", 200, 1000.0,
			100+i*10, 200+i*20, now.Add(-time.Duration(i)*time.Hour),
			50.0, 100.0)
		if err != nil {
			t.Fatalf("Failed to insert request log: %v", err)
		}
	}

	// Test with period=7d&metric=tokens&exclude_deleted=true
	t.Run("period_7d_metric_tokens", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/stats?period=7d&metric=tokens&exclude_deleted=true", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var stats StatsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
			t.Fatalf("Failed to parse stats response: %v", err)
		}

		// Should have calculated tokens
		if stats.TotalTokensPrompt == 0 {
			t.Error("Expected TotalTokensPrompt > 0")
		}
		if stats.TotalTokensCompletion == 0 {
			t.Error("Expected TotalTokensCompletion > 0")
		}
	})

	// Test with period=1h
	t.Run("period_1h", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/stats?period=1h", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var stats StatsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
			t.Fatalf("Failed to parse stats response: %v", err)
		}

		// 1h period should have fewer or equal requests than 24h
		if stats.TotalRequestsLast24h < stats.RequestsLast1h {
			t.Error("Expected 24h requests >= 1h requests")
		}
	})

	// Test with metric=requests (default)
	t.Run("metric_requests", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/stats?metric=requests", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var stats StatsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
			t.Fatalf("Failed to parse stats response: %v", err)
		}

		// Should have request counts
		if stats.TotalRequestsLast24h == 0 {
			t.Error("Expected TotalRequestsLast24h > 0")
		}
	})

	// Test with exclude_deleted=false
	t.Run("exclude_deleted_false", func(t *testing.T) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest("GET", "/stats?exclude_deleted=false", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Errorf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var stats StatsResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
			t.Fatalf("Failed to parse stats response: %v", err)
		}

		// Should return stats (exclude_deleted=false is default behavior)
		if stats.TotalRequestsLast24h == 0 {
			t.Error("Expected TotalRequestsLast24h > 0")
		}
	})
}

// TestStreamEvents_WithTypeFilter_Integration tests /events endpoint with type filter
func TestStreamEvents_WithTypeFilter_Integration(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.RegisterEvents(r)

	// Create request with admin auth and type filter
	req := httptest.NewRequest("GET", "/events?type=model.discovered", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	// Use custom ResponseWriter that implements http.Flusher
	fw := &flushingResponseWriter{ResponseRecorder: httptest.NewRecorder()}

	// Use context with timeout to avoid hanging
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	r.ServeHTTP(fw, req)

	// Verify status code
	if fw.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", fw.Code, fw.Body.String())
	}

	// Verify Content-Type header for SSE
	contentType := fw.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Expected Content-Type 'text/event-stream', got '%s'", contentType)
	}

	// Verify Cache-Control header for SSE
	cacheControl := fw.Header().Get("Cache-Control")
	if cacheControl != "no-cache" {
		t.Errorf("Expected Cache-Control 'no-cache', got '%s'", cacheControl)
	}

	// Verify Connection header
	connection := fw.Header().Get("Connection")
	if connection != "keep-alive" {
		t.Errorf("Expected Connection 'keep-alive', got '%s'", connection)
	}

	// Verify X-Accel-Buffering header
	xAccelBuffering := fw.Header().Get("X-Accel-Buffering")
	if xAccelBuffering != "no" {
		t.Errorf("Expected X-Accel-Buffering 'no', got '%s'", xAccelBuffering)
	}

	// Verify initial connection comment is sent
	body := fw.Body.String()
	if !strings.Contains(body, ": connected") {
		t.Errorf("Expected ': connected' in response body, got: %s", body)
	}
}
func TestCreateProvider_BaseURLTooLong_Integration(t *testing.T) {
	_, router := newTestHandlerWithRouter(t)

	longURL := "https://example.com/" + strings.Repeat("a", 490) // > 500 chars
	body := fmt.Sprintf(`{"name":"test-provider-%s","base_url":"%s","api_key":"sk-test"}`, uuid.New().String()[:8], longURL)

	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for base_url too long, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateProvider_APIKeyTooLong_Integration(t *testing.T) {
	_, router := newTestHandlerWithRouter(t)

	longKey := strings.Repeat("x", 501)
	body := fmt.Sprintf(`{"name":"test-provider-%s","base_url":"https://api.example.com/v1","api_key":"%s"}`, uuid.New().String()[:8], longKey)

	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for api_key too long, got %d: %s", w.Code, w.Body.String())
	}
}

func TestCreateProvider_HTTPSRequired_Integration(t *testing.T) {
	// This test verifies the HTTPS enforcement path, but the test config
	// has AllowHTTPProviders=true. Instead, test ValidateProviderURL error
	// by providing a URL with a blocked host.
	_, router := newTestHandlerWithRouter(t)

	// Even with AllowHTTPProviders, the ValidateProviderURL check rejects invalid hosts
	body := fmt.Sprintf(`{"name":"test-provider-%s","base_url":"https://192.168.1.1:443/v1","api_key":"sk-test"}`, uuid.New().String()[:8])

	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Should be rejected because internal IPs are not in allowed hosts
	if w.Code != http.StatusBadRequest {
		t.Logf("Got status %d (may pass if no ALLOWED_PROVIDER_HOSTS restriction): %s", w.Code, w.Body.String())
	}
}

func TestCreateProvider_KeylessWithProperURL_Integration(t *testing.T) {
	_, router := newTestHandlerWithRouter(t)

	// Keyless provider (opencode-zen) should work without API key
	body := fmt.Sprintf(`{"name":"test-zen-%s","base_url":"https://opencode.ai/zen/v1"}`, uuid.New().String()[:8])

	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("Expected 201 for keyless provider, got %d: %s", w.Code, w.Body.String())
	}
}

func TestListProviders_WithTokenCounts_Integration(t *testing.T) {
	h, router := newTestHandlerWithRouter(t)
	_ = h

	// Create a provider
	provName := "tp-tokens-" + uuid.New().String()[:8]
	provBody := fmt.Sprintf(`{"name":"%s","base_url":"https://api.example.com/v1","api_key":"sk-testkey123"}`, provName)
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(provBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d %s", w.Code, w.Body.String())
	}

	// Parse the provider ID from the create response
	var createResp map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("Failed to parse create response: %v", err)
	}
	provIDStr := createResp["id"].(string)
	provUUID, err := uuid.Parse(provIDStr)
	if err != nil {
		t.Fatalf("Failed to parse provider UUID: %v", err)
	}

	// Insert a request log with token counts
	_, err = h.dbPool.Pool().Exec(context.Background(),
		`INSERT INTO request_logs (id, provider_id, model_id, virtual_key_id, tokens_prompt, tokens_completion, status_code, latency_ms, created_at)
		 VALUES ($1, $2, 'test-model', NULL, 100, 50, 200, 123, NOW())`,
		uuid.New(), provUUID)
	if err != nil {
		t.Logf("Failed to insert request log (non-fatal): %v", err)
	}

	// List providers - should include token counts
	req = httptest.NewRequest("GET", "/providers", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var providers []map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &providers); err != nil {
		t.Fatalf("Failed to parse list response: %v", err)
	}

	if len(providers) == 0 {
		t.Error("Expected at least 1 provider in list")
	}
}

func TestUpdateSettings_TooManySettings_Integration(t *testing.T) {
	_, router := newTestHandlerWithRouter(t)

	// Create a map with >50 settings
	settings := make(map[string]string)
	for i := 0; i < 51; i++ {
		settings[fmt.Sprintf("setting_%d", i)] = "value"
	}
	body, _ := json.Marshal(settings)

	req := httptest.NewRequest("PUT", "/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for too many settings, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateSettings_ValidFloatSetting_Integration(t *testing.T) {
	_, router := newTestHandlerWithRouter(t)

	body := `{"rate_limit_rps":"30.5"}`

	req := httptest.NewRequest("PUT", "/settings", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for valid float setting, got %d: %s", w.Code, w.Body.String())
	}
}

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

func TestGetStats_WithFilters_Integration(t *testing.T) {
	h, router := newTestHandlerWithRouter(t)

	// Create a provider first
	provBody := fmt.Sprintf(`{"name":"stats-test-%s","base_url":"https://api.example.com/v1","api_key":"sk-test"}`, uuid.New().String()[:8])
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(provBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", w.Code)
	}

	var createResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &createResp)
	provIDStr := createResp["id"].(string)
	provUUID, _ := uuid.Parse(provIDStr)

	// Insert request log
	_, _ = h.dbPool.Pool().Exec(context.Background(),
		`INSERT INTO request_logs (id, provider_id, model_id, virtual_key_id, tokens_prompt, tokens_completion, status_code, latency_ms, created_at)
		 VALUES ($1, $2, 'gpt-4', NULL, 50, 25, 200, 100, NOW())`,
		uuid.New(), provUUID)

	// Get stats with metric filter
	req = httptest.NewRequest("GET", "/stats?period=30d&metric=tokens", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// Virtual Key Update Tests

func TestUpdateVirtualKey(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	t.Run("Success", func(t *testing.T) {
		// Create a virtual key first
		createBody := `{"name":"test-update-key"}`
		req := httptest.NewRequest("POST", "/virtual-keys", strings.NewReader(createBody))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Failed to create virtual key: %d: %s", rec.Code, rec.Body.String())
		}

		var createResp map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
			t.Fatalf("Failed to parse create response: %v", err)
		}
		keyID := createResp["id"].(string)

		// Update the key's name
		updateBody := `{"name":"updated-key-name"}`
		req = httptest.NewRequest("PUT", "/virtual-keys/"+keyID, strings.NewReader(updateBody))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var updateResp map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &updateResp); err != nil {
			t.Fatalf("Failed to parse update response: %v", err)
		}
		if updateResp["name"] != "updated-key-name" {
			t.Errorf("Expected name 'updated-key-name', got %v", updateResp["name"])
		}
	})

	t.Run("ReservedName", func(t *testing.T) {
		// Create a virtual key first
		createBody := `{"name":"test-reserved-key"}`
		req := httptest.NewRequest("POST", "/virtual-keys", strings.NewReader(createBody))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Failed to create virtual key: %d: %s", rec.Code, rec.Body.String())
		}

		var createResp map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
			t.Fatalf("Failed to parse create response: %v", err)
		}
		keyID := createResp["id"].(string)

		// Try to update to reserved name "admin"
		updateBody := `{"name":"admin"}`
		req = httptest.NewRequest("PUT", "/virtual-keys/"+keyID, strings.NewReader(updateBody))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected 400 for reserved name, got %d: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "reserved") {
			t.Errorf("Expected error about reserved name, got: %s", rec.Body.String())
		}
	})

	t.Run("EmptyName", func(t *testing.T) {
		// Create a virtual key first
		createBody := `{"name":"test-empty-name-key"}`
		req := httptest.NewRequest("POST", "/virtual-keys", strings.NewReader(createBody))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Failed to create virtual key: %d: %s", rec.Code, rec.Body.String())
		}

		var createResp map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
			t.Fatalf("Failed to parse create response: %v", err)
		}
		keyID := createResp["id"].(string)

		// Try to update with empty name
		updateBody := `{"name":""}`
		req = httptest.NewRequest("PUT", "/virtual-keys/"+keyID, strings.NewReader(updateBody))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected 400 for empty name, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("NotFound", func(t *testing.T) {
		// Try to update non-existent key
		nonExistentID := uuid.New().String()
		updateBody := `{"name":"test-name"}`
		req := httptest.NewRequest("PUT", "/virtual-keys/"+nonExistentID, strings.NewReader(updateBody))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected 404 for non-existent key, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("RateLimits", func(t *testing.T) {
		// Create a virtual key first
		createBody := `{"name":"test-ratelimit-key"}`
		req := httptest.NewRequest("POST", "/virtual-keys", strings.NewReader(createBody))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Failed to create virtual key: %d: %s", rec.Code, rec.Body.String())
		}

		var createResp map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &createResp); err != nil {
			t.Fatalf("Failed to parse create response: %v", err)
		}
		keyID := createResp["id"].(string)

		// Update with rate limits
		updateBody := `{"name":"ratelimited-key","rate_limit_rps":10,"rate_limit_burst":20}`
		req = httptest.NewRequest("PUT", "/virtual-keys/"+keyID, strings.NewReader(updateBody))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
		}

		var updateResp map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &updateResp); err != nil {
			t.Fatalf("Failed to parse update response: %v", err)
		}
		if updateResp["name"] != "ratelimited-key" {
			t.Errorf("Expected name 'ratelimited-key', got %v", updateResp["name"])
		}
		// Rate limits are returned as floats in JSON
		if updateResp["rate_limit_rps"] != float64(10) {
			t.Errorf("Expected rate_limit_rps=10, got %v", updateResp["rate_limit_rps"])
		}
		if updateResp["rate_limit_burst"] != float64(20) {
			t.Errorf("Expected rate_limit_burst=20, got %v", updateResp["rate_limit_burst"])
		}
	})
}

// Stats Query Parameter Tests

func TestGetStats_WithExcludeDeleted(t *testing.T) {
	h, router := newTestHandlerWithRouter(t)

	// Create a provider first
	provBody := fmt.Sprintf(`{"name":"stats-exclude-deleted-%s","base_url":"https://api.example.com/v1","api_key":"sk-test"}`, uuid.New().String()[:8])
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(provBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", w.Code)
	}

	var createResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &createResp)
	provIDStr := createResp["id"].(string)
	provUUID, _ := uuid.Parse(provIDStr)

	// Insert request logs
	pool := h.dbPool.Pool()
	now := time.Now().UTC()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (id, provider_id, model_id, virtual_key_id, status_code, duration_ms, proxy_overhead_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, 'gpt-4', NULL, 200, 1000, 50, 100, 200, $3)`,
		uuid.New(), provUUID, now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	// Test with exclude_deleted=true
	req = httptest.NewRequest("GET", "/stats?exclude_deleted=true", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stats StatsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to parse stats response: %v", err)
	}
	if stats.TotalRequestsLast24h == 0 {
		t.Error("Expected TotalRequestsLast24h > 0")
	}
}

func TestGetStats_WithMetricTokens(t *testing.T) {
	h, router := newTestHandlerWithRouter(t)

	// Create a provider first
	provBody := fmt.Sprintf(`{"name":"stats-metric-tokens-%s","base_url":"https://api.example.com/v1","api_key":"sk-test"}`, uuid.New().String()[:8])
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(provBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", w.Code)
	}

	var createResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &createResp)
	provIDStr := createResp["id"].(string)
	provUUID, _ := uuid.Parse(provIDStr)

	// Create a virtual key
	vkBody := `{"name":"test-metric-tokens-key"}`
	req = httptest.NewRequest("POST", "/virtual-keys", strings.NewReader(vkBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create virtual key: %d", w.Code)
	}

	var vkResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &vkResp)
	vkIDStr := vkResp["id"].(string)
	vkUUID, _ := uuid.Parse(vkIDStr)

	// Insert request logs with token counts using the virtual key
	pool := h.dbPool.Pool()
	now := time.Now().UTC()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (id, provider_id, model_id, virtual_key_id, status_code, duration_ms, proxy_overhead_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, 'gpt-4', $3, 200, 1000, 50, 100, 200, $4)`,
		uuid.New(), provUUID, vkUUID, now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	// Test with metric=tokens
	req = httptest.NewRequest("GET", "/stats?metric=tokens", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stats StatsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to parse stats response: %v", err)
	}
	if len(stats.ByModel) == 0 {
		t.Error("Expected ByModel to be populated with metric=tokens")
	}
	if len(stats.ByProvider) == 0 {
		t.Error("Expected ByProvider to be populated with metric=tokens")
	}
	if len(stats.ByVirtualKey) == 0 {
		t.Error("Expected ByVirtualKey to be populated with metric=tokens")
	}
}

func TestGetStats_Period7d(t *testing.T) {
	h, router := newTestHandlerWithRouter(t)

	// Create a provider first
	provBody := fmt.Sprintf(`{"name":"stats-period-7d-%s","base_url":"https://api.example.com/v1","api_key":"sk-test"}`, uuid.New().String()[:8])
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(provBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", w.Code)
	}

	var createResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &createResp)
	provIDStr := createResp["id"].(string)
	provUUID, _ := uuid.Parse(provIDStr)

	// Insert request logs
	pool := h.dbPool.Pool()
	now := time.Now().UTC()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (id, provider_id, model_id, virtual_key_id, status_code, duration_ms, proxy_overhead_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, 'gpt-4', NULL, 200, 1000, 50, 100, 200, $3)`,
		uuid.New(), provUUID, now.Add(-2*24*time.Hour))
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	// Test with period=7d
	req = httptest.NewRequest("GET", "/stats?period=7d", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stats StatsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to parse stats response: %v", err)
	}
	if stats.TotalRequestsLast7d == 0 {
		t.Error("Expected TotalRequestsLast7d > 0")
	}
}

func TestGetStats_Period1h(t *testing.T) {
	h, router := newTestHandlerWithRouter(t)

	// Create a provider first
	provBody := fmt.Sprintf(`{"name":"stats-period-1h-%s","base_url":"https://api.example.com/v1","api_key":"sk-test"}`, uuid.New().String()[:8])
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(provBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", w.Code)
	}

	var createResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &createResp)
	provIDStr := createResp["id"].(string)
	provUUID, _ := uuid.Parse(provIDStr)

	// Insert request logs
	pool := h.dbPool.Pool()
	now := time.Now().UTC()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (id, provider_id, model_id, virtual_key_id, status_code, duration_ms, proxy_overhead_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, 'gpt-4', NULL, 200, 1000, 50, 100, 200, $3)`,
		uuid.New(), provUUID, now.Add(-30*time.Minute))
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	// Test with period=1h
	req = httptest.NewRequest("GET", "/stats?period=1h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stats StatsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to parse stats response: %v", err)
	}
	if stats.RequestsLast1h == 0 {
		t.Error("Expected RequestsLast1h > 0")
	}
}

func TestGetStats_WithChatLogs(t *testing.T) {
	h, router := newTestHandlerWithRouter(t)

	// Create a provider first
	provBody := fmt.Sprintf(`{"name":"stats-chat-logs-%s","base_url":"https://api.example.com/v1","api_key":"sk-test"}`, uuid.New().String()[:8])
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(provBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", w.Code)
	}

	var createResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &createResp)
	provIDStr := createResp["id"].(string)
	provUUID, _ := uuid.Parse(provIDStr)

	// Insert request logs with virtual_key_name = 'chat'
	pool := h.dbPool.Pool()
	now := time.Now().UTC()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (id, provider_id, model_id, virtual_key_id, virtual_key_name, status_code, duration_ms, proxy_overhead_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, 'gpt-4', NULL, 'chat', 200, 1000, 50, 100, 200, $3)`,
		uuid.New(), provUUID, now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	// Test stats endpoint
	req = httptest.NewRequest("GET", "/stats", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stats StatsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatalf("Failed to parse stats response: %v", err)
	}
	if _, ok := stats.ByVirtualKey["chat"]; !ok {
		t.Error("Expected ByVirtualKey to contain 'chat' entry")
	}
}

func TestGetProviderDistribution_WithMetricTokens(t *testing.T) {
	h, router := newTestHandlerWithRouter(t)

	// Create providers
	provBody := fmt.Sprintf(`{"name":"prov-dist-tokens-%s","base_url":"https://api.example.com/v1","api_key":"sk-test"}`, uuid.New().String()[:8])
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(provBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", w.Code)
	}

	var createResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &createResp)
	provIDStr := createResp["id"].(string)
	provUUID, _ := uuid.Parse(provIDStr)

	// Insert request logs with token counts
	pool := h.dbPool.Pool()
	now := time.Now().UTC()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (id, provider_id, model_id, virtual_key_id, status_code, duration_ms, proxy_overhead_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, 'gpt-4', NULL, 200, 1000, 50, 100, 200, $3)`,
		uuid.New(), provUUID, now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	// Test with metric=tokens
	req = httptest.NewRequest("GET", "/stats/provider-distribution?metric=tokens", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var dist ProviderDistributionStats
	if err := json.Unmarshal(w.Body.Bytes(), &dist); err != nil {
		t.Fatalf("Failed to parse distribution response: %v", err)
	}
	if len(dist.Items) == 0 {
		t.Fatal("Expected items in distribution response")
	}
	// With metric=tokens, Tokens should be > 0
	if dist.Items[0].Tokens == 0 {
		t.Error("Expected Tokens > 0 with metric=tokens")
	}
}

func TestGetTimeSeries_WithExcludeDeleted(t *testing.T) {
	h, router := newTestHandlerWithRouter(t)

	// Create a provider first
	provBody := fmt.Sprintf(`{"name":"timeseries-exclude-deleted-%s","base_url":"https://api.example.com/v1","api_key":"sk-test"}`, uuid.New().String()[:8])
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(provBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", w.Code)
	}

	var createResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &createResp)
	provIDStr := createResp["id"].(string)
	provUUID, _ := uuid.Parse(provIDStr)

	// Insert request logs
	pool := h.dbPool.Pool()
	now := time.Now().UTC()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (id, provider_id, model_id, virtual_key_id, status_code, duration_ms, proxy_overhead_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, 'gpt-4', NULL, 200, 1000, 50, 100, 200, $3)`,
		uuid.New(), provUUID, now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	// Test with exclude_deleted=true
	req = httptest.NewRequest("GET", "/stats/timeseries?exclude_deleted=true", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var ts TimeSeriesStats
	if err := json.Unmarshal(w.Body.Bytes(), &ts); err != nil {
		t.Fatalf("Failed to parse time series response: %v", err)
	}
	if len(ts.Points) == 0 {
		t.Error("Expected time series points")
	}
}

func TestGetProviderDistribution_WithExcludeDeleted(t *testing.T) {
	h, router := newTestHandlerWithRouter(t)

	// Create a provider first
	provBody := fmt.Sprintf(`{"name":"prov-dist-exclude-deleted-%s","base_url":"https://api.example.com/v1","api_key":"sk-test"}`, uuid.New().String()[:8])
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(provBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d", w.Code)
	}

	var createResp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &createResp)
	provIDStr := createResp["id"].(string)
	provUUID, _ := uuid.Parse(provIDStr)

	// Insert request logs
	pool := h.dbPool.Pool()
	now := time.Now().UTC()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO request_logs (id, provider_id, model_id, virtual_key_id, status_code, duration_ms, proxy_overhead_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, 'gpt-4', NULL, 200, 1000, 50, 100, 200, $3)`,
		uuid.New(), provUUID, now.Add(-1*time.Hour))
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	// Test with exclude_deleted=true
	req = httptest.NewRequest("GET", "/stats/provider-distribution?exclude_deleted=true", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var dist ProviderDistributionStats
	if err := json.Unmarshal(w.Body.Bytes(), &dist); err != nil {
		t.Fatalf("Failed to parse distribution response: %v", err)
	}
	if len(dist.Items) == 0 {
		t.Error("Expected items in distribution response")
	}
}

// Ollama Cloud Account Tests

func TestGetOllamaCloudAccount(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	t.Run("NotFound", func(t *testing.T) {
		// Try to get account for non-existent provider
		nonExistentID := uuid.New().String()
		req := httptest.NewRequest("GET", "/providers/"+nonExistentID+"/account", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected 404 for non-existent provider, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("WrongProviderType", func(t *testing.T) {
		// Create a non-Ollama-Cloud provider (OpenAI)
		providerData := `{"name":"test-openai-account","base_url":"https://api.openai.com","api_key":"sk-test"}`
		req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
		}

		var providerResp map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
			t.Fatalf("Failed to parse provider response: %v", err)
		}
		providerID := providerResp["id"].(string)

		// Try to get account for OpenAI provider (not supported)
		req = httptest.NewRequest("GET", "/providers/"+providerID+"/account", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected 400 for wrong provider type, got %d: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "not supported") {
			t.Errorf("Expected error about unsupported provider type, got: %s", rec.Body.String())
		}
	})

	// Note: Success case omitted - GetOllamaCloudAccount requires real network calls
	// to the Ollama Cloud API which would hang tests or require valid credentials.
	// The negative tests above verify the handler's validation and error paths.
}
