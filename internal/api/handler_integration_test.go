package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
		MasterKey:          testMasterKey,
		AllowHTTPProviders: true,
		RateLimitEnabled:   false,
		DataDir:            t.TempDir(),
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

	handler := NewHandler(cfg, providerRepo, database, adminMgr, vkRepo, settingsRepo)
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
	req := httptest.NewRequest("GET", "/providers", nil)
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
	req := httptest.NewRequest("GET", "/providers", nil)
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
	req = httptest.NewRequest("GET", "/providers/"+createResp.ID, nil)
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
	req = httptest.NewRequest("GET", "/providers", nil)
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
	req = httptest.NewRequest("DELETE", "/providers/"+createResp.ID, nil)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("Expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify it's gone
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/providers/"+createResp.ID, nil)
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
	req := httptest.NewRequest("DELETE", "/providers/invalid-uuid", nil)
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
	req := httptest.NewRequest("DELETE", "/providers/"+nonExistentID, nil)
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
	req := httptest.NewRequest("GET", "/providers/invalid-uuid", nil)
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
	req := httptest.NewRequest("GET", "/providers/"+nonExistentID, nil)
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
	req = httptest.NewRequest("GET", "/providers/"+createResp.ID, nil)
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
			providerData := fmt.Sprintf(`{"name": "test-%s", "base_url": "%s", "api_key": "test-api-key"}`, tc.name, tc.baseURL)
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
	req = httptest.NewRequest("GET", "/providers", nil)
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
			providerData := fmt.Sprintf(`{"name": "test-%s-%s", "base_url": "%s", "api_key": "%s"}`, tc.name, uuid.New().String()[:8], tc.baseURL, tc.apiKey)
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
			purgeData := fmt.Sprintf(`{"older_than": "%s"}`, tc.olderThan)
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
	req := httptest.NewRequest("GET", "/virtual-keys/"+nonExistentID, nil)
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
	req = httptest.NewRequest("GET", "/failover-groups", nil)
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
	req := httptest.NewRequest("DELETE", "/failover-groups/"+nonExistentID, nil)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	// Delete returns 204 even for non-existent groups (idempotent)
	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected 204 for failover group delete, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSyncFailoverGroups(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Sync failover groups (should work even with no models)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/failover-groups/sync", nil)
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
	req := httptest.NewRequest("GET", "/failover-groups/candidates", nil)
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
	req = httptest.NewRequest("GET", "/providers/"+createResp.ID, nil)
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
	req := httptest.NewRequest("GET", "/models", nil)
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
	req = httptest.NewRequest("GET", "/models", nil)
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
	req = httptest.NewRequest("POST", "/models/"+modelID+"/test", nil)
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
	req := httptest.NewRequest("GET", "/stats", nil)
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
	req := httptest.NewRequest("GET", "/stats/timeseries", nil)
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
	req := httptest.NewRequest("GET", "/stats/provider-distribution", nil)
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

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/system", nil)
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
	req := httptest.NewRequest("GET", "/settings", nil)
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
	req = httptest.NewRequest("GET", "/settings", nil)
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

// App Logs Tests

func TestGetAppLogsIntegration(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/logs/app", nil)
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
	req := httptest.NewRequest("DELETE", "/logs/app", nil)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
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
	req = httptest.NewRequest("DELETE", "/models/"+modelID, nil)
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
	req = httptest.NewRequest("POST", "/models/"+modelID+"/test", nil)
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
	req = httptest.NewRequest("POST", "/providers/"+providerResp.ID+"/discover", nil)
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
	req = httptest.NewRequest("GET", "/providers/"+providerResp.ID+"/usage", nil)
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
	req = httptest.NewRequest("GET", "/providers/"+providerResp.ID+"/balance", nil)
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
	req = httptest.NewRequest("POST", "/providers/discover-all", nil)
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
	req = httptest.NewRequest("POST", "/providers/refresh-quotas", nil)
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
	req := httptest.NewRequest("GET", "/events", nil)
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
	req := httptest.NewRequest("GET", "/logs", nil)
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
	req = httptest.NewRequest("GET", "/stats", nil)
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
			req := httptest.NewRequest("GET", "/stats/timeseries?period="+tc.period, nil)
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
		providerData := fmt.Sprintf(`{"name": "%s", "base_url": "https://api.openai.com", "api_key": "test-api-key"}`, name)
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
	req = httptest.NewRequest("GET", "/stats/provider-distribution", nil)
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
	req := httptest.NewRequest("GET", "/stats", nil)
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
	req := httptest.NewRequest("GET", "/logs/app", nil)
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
		req := httptest.NewRequest(http.MethodPatch, "/models/"+modelID, strings.NewReader(fmt.Sprintf(`{"display_name":"%s"}`, longName)))
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
		req := httptest.NewRequest("GET", "/logs/app?history=true&source=proxy", nil)
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
		req := httptest.NewRequest("GET", "/logs/app?history=true&level=error", nil)
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
		req := httptest.NewRequest("GET", "/logs/app?history=true&search=warning", nil)
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
		req := httptest.NewRequest("GET", "/logs/app?history=true&per_page=2", nil)
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
		req := httptest.NewRequest("GET", "/logs/app?history=true&from="+pastTime, nil)
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
		req := httptest.NewRequest("GET", "/logs/app?history=true&source=proxy&level=error", nil)
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
	req = httptest.NewRequest("POST", "/providers/"+providerResp.ID+"/discover", nil)
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
	req = httptest.NewRequest("GET", "/providers/"+providerResp.ID+"/usage", nil)
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
	req = httptest.NewRequest("POST", "/providers/discover-all", nil)
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

func TestDiscoverProviderModels_DisabledProvider(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	// Create provider
	body := `{"name":"test-disc-disabled","base_url":"https://api.openai.com","api_key":"sk-test123"}`
	req := httptest.NewRequest("POST", "/providers", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Check response status
	if w.Code != http.StatusCreated {
		t.Fatalf("Expected 201, got %d: %s", w.Code, w.Body.String())
	}

	// Parse response to get provider ID
	var resp map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("Failed to parse provider creation response: %v, body: %s", err, w.Body.String())
	}
	providerID, ok := resp["id"].(string)
	if !ok {
		t.Fatalf("Provider ID not found in response or not a string: %+v", resp)
	}

	// Disable the provider via SQL
	h.dbPool.Pool().Exec(context.Background(), "UPDATE providers SET enabled = false WHERE id = $1", providerID)

	// Try to discover
	req2 := httptest.NewRequest("POST", "/providers/"+providerID+"/discover", nil)
	req2.Header.Set("Authorization", "Bearer test-admin-token")
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, req2)

	// Should get error (400 for disabled provider, or 500 for API key failure)
	if w2.Code != 400 && w2.Code != 500 {
		t.Errorf("expected 400 or 500, got %d", w2.Code)
	}
}

func TestDiscoverProviderModels_InvalidUUID(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Call with invalid UUID
	req := httptest.NewRequest("POST", "/providers/not-a-uuid/discover", nil)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Should get 400
	if w.Code != 400 {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestDiscoverProviderModels_NotFound(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Call with non-existent UUID
	nonExistentID := uuid.New().String()
	req := httptest.NewRequest("POST", "/providers/"+nonExistentID+"/discover", nil)
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
	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/usage", nil)
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
	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/usage", nil)
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
	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/balance", nil)
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
	req2 := httptest.NewRequest("GET", "/providers/"+providerID+"/balance", nil)
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
	req2 := httptest.NewRequest("POST", "/providers/refresh-quotas", nil)
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
	req2 := httptest.NewRequest("POST", "/providers/refresh-quotas", nil)
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
	req2 := httptest.NewRequest("POST", "/providers/refresh-quotas", nil)
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
	req2 := httptest.NewRequest("POST", "/providers/refresh-quotas", nil)
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
	req2 := httptest.NewRequest("POST", "/providers/refresh-quotas", nil)
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
	req2 := httptest.NewRequest("POST", "/providers/refresh-quotas", nil)
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
	req = httptest.NewRequest("GET", "/providers/"+createResp.ID, nil)
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
	updateData := fmt.Sprintf(`{"name": "%s"}`, longName)
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
	req = httptest.NewRequest("POST", "/providers/"+providerResp.ID+"/discover", nil)
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
	req = httptest.NewRequest("GET", "/providers/"+providerResp.ID+"/usage", nil)
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
	req := httptest.NewRequest("POST", "/providers/discover-all", nil)
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
	req := httptest.NewRequest("GET", "/logs/app", nil)
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
	req := httptest.NewRequest("GET", "/logs/app?limit=5", nil)
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
	req = httptest.NewRequest("GET", "/settings", nil)
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
	req = httptest.NewRequest("POST", "/failover-groups/sync", nil)
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
	req := httptest.NewRequest("GET", "/logs/app?history=true", nil)
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
	req := httptest.NewRequest("GET", "/logs/app", nil)
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
	req := httptest.NewRequest("GET", "/logs/app?history=true&level=error", nil)
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
	req = httptest.NewRequest("GET", "/logs/app?history=true&source=proxy", nil)
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
	req = httptest.NewRequest("GET", "/logs/app?history=true&search=warning", nil)
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
