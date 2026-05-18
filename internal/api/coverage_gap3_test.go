package api

import (
	"context"
	"encoding/json"
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
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// ---------------------------------------------------------------------------
// calculateStats Tests - Tokens Metric
// ---------------------------------------------------------------------------

// TestCalculateStats_TokensMetric tests calculateStats with metric=tokens
// to cover the token aggregation branches in by_model, by_provider, by_virtual_key queries.
func TestCalculateStats_TokensMetric(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Set up test data with token counts
	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-tokens-metric", "https://api.example.com/v1")
	// Insert request log with significant token counts
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 100, 200)

	ctx := context.Background()

	// Call calculateStats with metric=tokens
	stats, err := handler.calculateStats(ctx, 24*time.Hour, true, "tokens")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// With metric=tokens, ByModel and ByProvider should contain token counts
	if len(stats.ByModel) == 0 {
		t.Error("Expected ByModel to have entries with metric=tokens")
	}
	if len(stats.ByProvider) == 0 {
		t.Error("Expected ByProvider to have entries with metric=tokens")
	}

	// Check token totals
	if stats.TotalTokensPrompt != 100 {
		t.Errorf("Expected TotalTokensPrompt=100, got %d", stats.TotalTokensPrompt)
	}
	if stats.TotalTokensCompletion != 200 {
		t.Errorf("Expected TotalTokensCompletion=200, got %d", stats.TotalTokensCompletion)
	}
}

// TestCalculateStats_TokensMetric_ByVirtualKey tests calculateStats with metric=tokens
// for the by_virtual_key query path.
func TestCalculateStats_TokensMetric_ByVirtualKey(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	ctx := context.Background()

	// Create a virtual key
	vkID := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO virtual_keys (id, name, key_hash, key_preview, created_at)
		VALUES ($1, 'test-vk-tokens', 'hash', 'sk-...ab', NOW())`,
		vkID)
	if err != nil {
		t.Fatalf("Failed to insert virtual key: %v", err)
	}

	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-vk-tokens", "https://api.example.com/v1")

	// Insert request log with virtual key and token counts
	insertRichTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 50, 75, requestLogOpts{
		VirtualKeyID: &vkID,
	})

	stats, err := handler.calculateStats(ctx, 24*time.Hour, true, "tokens")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// ByVirtualKey should contain the virtual key name with token count
	if stats.ByVirtualKey["test-vk-tokens"] != 125 {
		t.Errorf("Expected ByVirtualKey['test-vk-tokens']=125, got %d", stats.ByVirtualKey["test-vk-tokens"])
	}
}

// ---------------------------------------------------------------------------
// calculateStats Tests - Exclude Deleted False
// ---------------------------------------------------------------------------

// TestCalculateStats_ExcludeDeletedFalse tests calculateStats with excludeDeleted=false
// to cover the deleted virtual keys aggregate query path.
func TestCalculateStats_ExcludeDeletedFalse(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	ctx := context.Background()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-del-false", "https://api.example.com/v1")

	// Insert request log with a virtual_key_id that doesn't exist in virtual_keys table
	// This simulates a deleted virtual key
	deletedVKID := uuid.New()
	logID := uuid.New()
	insertRichTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20, requestLogOpts{
		VirtualKeyID: &deletedVKID,
	})

	stats, err := handler.calculateStats(ctx, 24*time.Hour, false, "requests")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// With excludeDeleted=false, deleted VK requests should appear in ByVirtualKey["Deleted"]
	if stats.ByVirtualKey["Deleted"] != 1 {
		t.Errorf("Expected ByVirtualKey['Deleted']=1, got %d", stats.ByVirtualKey["Deleted"])
	}
}

// TestCalculateStats_ExcludeDeletedFalse_Tokens tests calculateStats with excludeDeleted=false
// and metric=tokens to cover the deleted VK path with token aggregation.
func TestCalculateStats_ExcludeDeletedFalse_Tokens(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	ctx := context.Background()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-del-tok", "https://api.example.com/v1")

	// Insert request log with deleted VK and token counts
	deletedVKID := uuid.New()
	logID := uuid.New()
	insertRichTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 30, 40, requestLogOpts{
		VirtualKeyID: &deletedVKID,
	})

	stats, err := handler.calculateStats(ctx, 24*time.Hour, false, "tokens")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// Deleted VK should have token count (30+40=70)
	if stats.ByVirtualKey["Deleted"] != 70 {
		t.Errorf("Expected ByVirtualKey['Deleted']=70, got %d", stats.ByVirtualKey["Deleted"])
	}
}

// ---------------------------------------------------------------------------
// calculateStats Tests - 7d Period (Non-24h Secondary Query)
// ---------------------------------------------------------------------------

// TestCalculateStats_7dPeriod tests calculateStats with period=7d
// to cover the else branch for non-24h secondary queries.
func TestCalculateStats_7dPeriod(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	ctx := context.Background()

	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-7d-period", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	stats, err := handler.calculateStats(ctx, 7*24*time.Hour, true, "requests")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// With 7d period, TotalRequestsLast7d should be set
	if stats.TotalRequestsLast7d != 1 {
		t.Errorf("Expected TotalRequestsLast7d=1, got %d", stats.TotalRequestsLast7d)
	}

	// The else branch should have queried for 24h ago
	// TotalRequestsLast24h should also be set (from the secondary query)
	if stats.TotalRequestsLast24h != 1 {
		t.Errorf("Expected TotalRequestsLast24h=1, got %d", stats.TotalRequestsLast24h)
	}
}

// TestCalculateStats_1hPeriod tests calculateStats with period=1h
// to cover the else branch for non-7d initial period.
func TestCalculateStats_1hPeriod(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	ctx := context.Background()

	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-1h-period", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	stats, err := handler.calculateStats(ctx, 1*time.Hour, true, "requests")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// With 1h period, TotalRequestsLast24h is populated by the secondary query
	// (since period != 7d, the else branch queries 24h). Logs exist within 24h.
	// The else branch sets TotalRequestsLast24h = 0 initially
	// Then the secondary query for _24hAgo should populate it
	if stats.TotalRequestsLast24h < 1 {
		t.Errorf("Expected TotalRequestsLast24h>=1, got %d", stats.TotalRequestsLast24h)
	}
}

// ---------------------------------------------------------------------------
// calculateStats Tests - Chat/Arena Keys
// ---------------------------------------------------------------------------

// TestCalculateStats_ChatArenaKeys_Tokens tests calculateStats with chat/arena virtual_key_name
// and metric=tokens to cover the token aggregation path for chat/arena queries.
func TestCalculateStats_ChatArenaKeys_Tokens(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	ctx := context.Background()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-chat-tok", "https://api.example.com/v1")

	// Insert request logs with chat and arena virtual_key_name and token counts
	chatLogID := uuid.New()
	insertRichTestRequestLog(t, pool, chatLogID, providerID, "test-model", 200, 100, 100, 150, requestLogOpts{
		VirtualKeyName: "chat",
	})
	arenaLogID := uuid.New()
	insertRichTestRequestLog(t, pool, arenaLogID, providerID, "test-model", 200, 100, 80, 120, requestLogOpts{
		VirtualKeyName: "arena",
	})

	stats, err := handler.calculateStats(ctx, 24*time.Hour, true, "tokens")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// Chat should have token count (100+150=250)
	if stats.ByVirtualKey["chat"] != 250 {
		t.Errorf("Expected ByVirtualKey['chat']=250, got %d", stats.ByVirtualKey["chat"])
	}
	// Arena should have token count (80+120=200)
	if stats.ByVirtualKey["arena"] != 200 {
		t.Errorf("Expected ByVirtualKey['arena']=200, got %d", stats.ByVirtualKey["arena"])
	}
}

// ---------------------------------------------------------------------------
// calculateStats Tests - Query Error Paths
// ---------------------------------------------------------------------------

// TestCalculateStats_QueryError tests calculateStats with a closed pool
// to cover the error paths for various queries beyond the first one.
func TestCalculateStats_QueryError(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Close pool before calling calculateStats
	pool.Close()

	ctx := context.Background()
	_, err := handler.calculateStats(ctx, 24*time.Hour, true, "requests")
	if err == nil {
		t.Error("Expected error when pool is closed")
	}
}

// ---------------------------------------------------------------------------
// ListProviders Tests - Model and Token Counts
// ---------------------------------------------------------------------------

// TestListProviders_WithModelCounts tests ListProviders with providers that have models
// to cover the model count query and rows.Scan paths.
func TestListProviders_WithModelCounts(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, apiTestDBURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}
	defer pool.Close()

	// Clean test data
	pool.Exec(ctx, `TRUNCATE request_logs, models, providers CASCADE`)

	// Create admin manager
	tmpDir := t.TempDir()
	adminMgr, _, err := admin.New(tmpDir, "test-admin-token")
	if err != nil {
		t.Fatalf("failed to create admin manager: %v", err)
	}

	// Create handler
	cfg := &config.Config{
		MasterKey:          "testmasterkey1234567890abcdef",
		AllowHTTPProviders: true,
		DataDir:            tmpDir,
	}
	providerRepo := provider.NewRepository(pool)
	vkRepo := virtualkey.NewRepository(pool)
	settingsRepo := settings.NewRepository(pool)
	dbInst, err := db.New(ctx, apiTestDBURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create db instance: %v", err)
	}
	defer dbInst.Close()

	h := NewHandler(cfg, providerRepo, dbInst, adminMgr, vkRepo, settingsRepo, "test")
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.Register(r)

	// Create a provider
	createBody := `{"name":"test-provider-models","base_url":"https://api.example.com/v1","provider_type":"openai","api_key":"sk-testkey1234567890abcdef"}`
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create provider: expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	var created struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("failed to decode created provider: %v", err)
	}

	// Insert models for this provider
	modelID1 := uuid.New().String()
	modelID2 := uuid.New().String()
	_, err = pool.Exec(ctx, `
		INSERT INTO models (id, model_id, name, provider_id, enabled, created_at, last_seen_at)
		VALUES ($1, $2, $3, $4, true, NOW(), NOW()),
		       ($5, $6, $7, $4, true, NOW(), NOW())`,
		uuid.New(), modelID1, "model-1", created.ID,
		uuid.New(), modelID2, "model-2")
	if err != nil {
		t.Fatalf("Failed to insert models: %v", err)
	}

	// List providers
	req = httptest.NewRequest(http.MethodGet, "/providers", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list providers: expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var providers []provider.ProviderResponse
	if err := json.NewDecoder(w.Body).Decode(&providers); err != nil {
		t.Fatalf("failed to decode providers: %v", err)
	}

	// Find our test provider
	var found bool
	for _, p := range providers {
		if p.Name == "test-provider-models" {
			found = true
			if p.ModelCount != 2 {
				t.Errorf("Expected ModelCount=2, got %d", p.ModelCount)
			}
			break
		}
	}
	if !found {
		t.Error("Expected to find test-provider-models in list")
	}
}

// TestListProviders_WithTokenCounts tests ListProviders with request logs
// to cover the token count query and rows.Scan paths.
func TestListProviders_WithTokenCounts(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, apiTestDBURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}
	defer pool.Close()

	// Clean test data
	pool.Exec(ctx, `TRUNCATE request_logs, models, providers CASCADE`)

	// Create admin manager
	tmpDir := t.TempDir()
	adminMgr, _, err := admin.New(tmpDir, "test-admin-token")
	if err != nil {
		t.Fatalf("failed to create admin manager: %v", err)
	}

	// Create handler
	cfg := &config.Config{
		MasterKey:          "testmasterkey1234567890abcdef",
		AllowHTTPProviders: true,
		DataDir:            tmpDir,
	}
	providerRepo := provider.NewRepository(pool)
	vkRepo := virtualkey.NewRepository(pool)
	settingsRepo := settings.NewRepository(pool)
	dbInst, err := db.New(ctx, apiTestDBURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create db instance: %v", err)
	}
	defer dbInst.Close()

	h := NewHandler(cfg, providerRepo, dbInst, adminMgr, vkRepo, settingsRepo, "test")
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.Register(r)

	// Create a provider
	createBody := `{"name":"test-provider-tokens","base_url":"https://api.example.com/v1","provider_type":"openai","api_key":"sk-testkey1234567890abcdef"}`
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(createBody))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create provider: expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	var created struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&created); err != nil {
		t.Fatalf("failed to decode created provider: %v", err)
	}

	providerUUID, _ := uuid.Parse(created.ID)

	// Insert request logs with token counts for this provider
	logID := uuid.New()
	_, err = pool.Exec(ctx, `
		INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, 'test-model', 200, 100, 50, 75, NOW())`,
		logID, providerUUID)
	if err != nil {
		t.Fatalf("Failed to insert request log: %v", err)
	}

	// List providers
	req = httptest.NewRequest(http.MethodGet, "/providers", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list providers: expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var providers []provider.ProviderResponse
	if err := json.NewDecoder(w.Body).Decode(&providers); err != nil {
		t.Fatalf("failed to decode providers: %v", err)
	}

	// Find our test provider
	var found bool
	for _, p := range providers {
		if p.Name == "test-provider-tokens" {
			found = true
			if p.TotalTokens != 125 {
				t.Errorf("Expected TotalTokens=125, got %d", p.TotalTokens)
			}
			break
		}
	}
	if !found {
		t.Error("Expected to find test-provider-tokens in list")
	}
}

// TestListProviders_ModelCountQueryError tests ListProviders when the model count query fails
// (using closed pool) to cover the model count query error path.
func TestListProviders_ModelCountQueryError(t *testing.T) {
	// Skip this test as it requires internal access to db.DB fields
	// The error path is covered by TestListProviders_CancelledContext in admin_test.go
	t.Skip("requires internal db.DB manipulation - covered by TestListProviders_CancelledContext")
}

// ---------------------------------------------------------------------------
// ListModels Tests - Provider ID Filter and Error Paths
// ---------------------------------------------------------------------------

// TestListModels_ValidProviderIDFilter tests ListModels with valid UUID provider_id
// to cover the providerID filter path.
func TestListModels_ValidProviderIDFilter(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, apiTestDBURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}
	defer pool.Close()

	// Clean test data
	pool.Exec(ctx, `TRUNCATE models, providers CASCADE`)

	// Create admin manager
	tmpDir := t.TempDir()
	adminMgr, _, err := admin.New(tmpDir, "test-admin-token")
	if err != nil {
		t.Fatalf("failed to create admin manager: %v", err)
	}

	// Create handler
	cfg := &config.Config{
		MasterKey:          "testmasterkey1234567890abcdef",
		AllowHTTPProviders: true,
		DataDir:            tmpDir,
	}
	providerRepo := provider.NewRepository(pool)
	vkRepo := virtualkey.NewRepository(pool)
	settingsRepo := settings.NewRepository(pool)
	dbInst, err := db.New(ctx, apiTestDBURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create db instance: %v", err)
	}
	defer dbInst.Close()

	h := NewHandler(cfg, providerRepo, dbInst, adminMgr, vkRepo, settingsRepo, "test")
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.Register(r)

	// Create two providers
	createBody1 := `{"name":"provider-filter-1","base_url":"https://api.example.com/v1","provider_type":"openai","api_key":"sk-testkey1234567890abcdef"}`
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(createBody1))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create provider 1: expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	var created1 struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&created1); err != nil {
		t.Fatalf("failed to decode created provider: %v", err)
	}

	createBody2 := `{"name":"provider-filter-2","base_url":"https://api.example.com/v2","provider_type":"openai","api_key":"sk-testkey1234567890abcdef"}`
	req = httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(createBody2))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create provider 2: expected status %d, got %d: %s", http.StatusCreated, w.Code, w.Body.String())
	}

	var created2 struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(w.Body).Decode(&created2); err != nil {
		t.Fatalf("failed to decode created provider: %v", err)
	}

	providerUUID1, _ := uuid.Parse(created1.ID)
	providerUUID2, _ := uuid.Parse(created2.ID)

	// Insert models for each provider
	_, err = pool.Exec(ctx, `
		INSERT INTO models (id, model_id, name, provider_id, enabled, created_at, last_seen_at)
		VALUES ($1, 'model-1', 'Model 1', $2, true, NOW(), NOW()),
		       ($3, 'model-2', 'Model 2', $4, true, NOW(), NOW())`,
		uuid.New(), providerUUID1,
		uuid.New(), providerUUID2)
	if err != nil {
		t.Fatalf("Failed to insert models: %v", err)
	}

	// Request with provider_id filter for provider 1
	req = httptest.NewRequest(http.MethodGet, "/models?provider_id="+created1.ID, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("list models: expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}

	var models []ModelResponse
	if err := json.NewDecoder(w.Body).Decode(&models); err != nil {
		t.Fatalf("failed to decode models: %v", err)
	}

	// Should only return models for provider 1
	if len(models) != 1 {
		t.Errorf("Expected 1 model, got %d", len(models))
	}
	if len(models) > 0 && models[0].ProviderID != created1.ID {
		t.Errorf("Expected provider_id=%s, got %s", created1.ID, models[0].ProviderID)
	}
}

// TestListModels_RepoError tests ListModels when modelRepo.List returns an error
// (using closed pool) to cover the repository error path.
func TestListModels_RepoError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	ctx := context.Background()

	// Create a closed pool to trigger query errors
	closedPool, err := pgxpool.New(ctx, apiTestDBURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}
	closedPool.Close()

	// Create admin manager
	tmpDir := t.TempDir()
	adminMgr, _, err := admin.New(tmpDir, "test-admin-token")
	if err != nil {
		t.Fatalf("failed to create admin manager: %v", err)
	}

	// Create handler with closed pool
	cfg := &config.Config{
		MasterKey:          "testmasterkey1234567890abcdef",
		AllowHTTPProviders: true,
		DataDir:            tmpDir,
	}

	// Create db.DB with closed pool
	dbInst, err := db.New(ctx, apiTestDBURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create db instance: %v", err)
	}
	dbInst.Close()

	providerRepo := provider.NewRepository(closedPool)
	vkRepo := virtualkey.NewRepository(closedPool)
	settingsRepo := settings.NewRepository(closedPool)

	h := NewHandler(cfg, providerRepo, dbInst, adminMgr, vkRepo, settingsRepo, "test")
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.Register(r)

	// Request should fail with 500
	req := httptest.NewRequest(http.MethodGet, "/models", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d: %s", w.Code, w.Body.String())
	}
}
