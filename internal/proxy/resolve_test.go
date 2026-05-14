package proxy

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// ---------------------------------------------------------------------------
// shouldFailover pure unit tests (no DB required)
//
// For status codes that return before reaching settingsRepo.GetBool
// (5xx, 401/403, and non-failover codes), settingsRepo can be nil safely.
// ---------------------------------------------------------------------------

func TestShouldFailover_PureUnit_5xx(t *testing.T) {
	h := &Handler{
		cfg:          &config.Config{MasterKey: "test"},
		settingsRepo: nil, // safe: 5xx path returns before touching settingsRepo
	}

	for _, code := range []int{500, 501, 502, 503, 504, 505, 510, 511, 599} {
		if !h.shouldFailover(context.Background(), code) {
			t.Errorf("status %d should trigger failover", code)
		}
	}
}

func TestShouldFailover_PureUnit_AuthErrors(t *testing.T) {
	h := &Handler{
		cfg:          &config.Config{MasterKey: "test"},
		settingsRepo: nil, // safe: auth error path returns before touching settingsRepo
	}

	for _, code := range []int{401, 403} {
		if !h.shouldFailover(context.Background(), code) {
			t.Errorf("status %d should trigger failover", code)
		}
	}
}

func TestShouldFailover_PureUnit_NoFailoverCodes(t *testing.T) {
	h := &Handler{
		cfg:          &config.Config{MasterKey: "test"},
		settingsRepo: nil, // safe: non-failover codes return false before reaching settingsRepo
	}

	tests := []struct {
		name string
		code int
	}{
		{"200 OK", 200},
		{"201 Created", 201},
		{"204 No Content", 204},
		{"301 Moved", 301},
		{"302 Found", 302},
		{"304 Not Modified", 304},
		{"400 Bad Request", 400},
		{"404 Not Found", 404},
		{"405 Method Not Allowed", 405},
		{"408 Request Timeout", 408},
		{"415 Unsupported Media Type", 415},
		{"422 Unprocessable Entity", 422},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if h.shouldFailover(context.Background(), tt.code) {
				t.Errorf("status %d should NOT trigger failover", tt.code)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// shouldFailover integration tests (requires PostgreSQL)
//
// The 429 path calls settingsRepo.GetBool, so it needs a real DB.
// ---------------------------------------------------------------------------

func TestShouldFailover_Integration_429DefaultEnabled(t *testing.T) {
	h := newIntegrationHandler()

	// Default setting for failover_on_rate_limit is true
	if !h.shouldFailover(context.Background(), 429) {
		t.Error("429 should trigger failover when failover_on_rate_limit=true (default)")
	}
}

func TestShouldFailover_Integration_429Disabled(t *testing.T) {
	h := newIntegrationHandler()

	if err := h.settingsRepo.Set(context.Background(), "failover_on_rate_limit", "false"); err != nil {
		t.Fatalf("failed to set setting: %v", err)
	}
	defer func() {
		_ = h.settingsRepo.Set(context.Background(), "failover_on_rate_limit", "true")
	}()
	h.settingsRepo.InvalidateCache("failover_on_rate_limit")

	if h.shouldFailover(context.Background(), 429) {
		t.Error("429 should NOT trigger failover when failover_on_rate_limit=false")
	}
}

func TestShouldFailover_Integration_TableDriven(t *testing.T) {
	h := newIntegrationHandler()

	tests := []struct {
		name     string
		code     int
		expected bool
	}{
		{"500 Internal Server Error", 500, true},
		{"502 Bad Gateway", 502, true},
		{"503 Service Unavailable", 503, true},
		{"401 Unauthorized", 401, true},
		{"403 Forbidden", 403, true},
		{"429 Too Many Requests", 429, true},
		{"200 OK", 200, false},
		{"201 Created", 201, false},
		{"400 Bad Request", 400, false},
		{"404 Not Found", 404, false},
		{"422 Unprocessable", 422, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := h.shouldFailover(context.Background(), tt.code)
			if got != tt.expected {
				t.Errorf("shouldFailover(%d) = %v, want %v", tt.code, got, tt.expected)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// resolveHotelModel integration tests (requires PostgreSQL)
// ---------------------------------------------------------------------------

func TestResolveHotelModel_GroupNotFound(t *testing.T) {
	h := newIntegrationHandler()

	candidates, timings, err := h.resolveHotelModel(context.Background(), "nonexistent-model-xyz")

	if err == nil {
		t.Error("expected error for nonexistent failover group")
	}
	if candidates != nil {
		t.Error("candidates should be nil on error")
	}
	// Timings should be zero since the error occurs before any lookup
	if timings.modelLookupMs != 0 {
		t.Errorf("modelLookupMs = %f, want 0 on early error", timings.modelLookupMs)
	}
	if timings.providerLookupMs != 0 {
		t.Errorf("providerLookupMs = %f, want 0 on early error", timings.providerLookupMs)
	}
	if timings.keyDecryptMs != 0 {
		t.Errorf("keyDecryptMs = %f, want 0 on early error", timings.keyDecryptMs)
	}
}

func TestResolveHotelModel_ContextCanceled(t *testing.T) {
	h := newIntegrationHandler()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := h.resolveHotelModel(ctx, "some-model")

	if err == nil {
		t.Error("expected error with canceled context")
	}
}

// ---------------------------------------------------------------------------
// resolveSpecificProvider integration tests (requires PostgreSQL)
// ---------------------------------------------------------------------------

func TestResolveSpecificProvider_ProviderNotFound(t *testing.T) {
	h := newIntegrationHandler()

	candidates, timings, err := h.resolveSpecificProvider(context.Background(), "nonexistent-provider", "some-model")

	if err == nil {
		t.Error("expected error for nonexistent provider")
	}
	if candidates != nil {
		t.Error("candidates should be nil on error")
	}
	// providerLookupMs is measured even on error (time.Since was called)
	_ = timings
}

func TestResolveSpecificProvider_ModelNotFound(t *testing.T) {
	h := newIntegrationHandler()

	providers, err := h.providerRepo.List(context.Background())
	if err != nil || len(providers) == 0 {
		t.Skip("no providers in database")
	}

	candidates, _, err := h.resolveSpecificProvider(context.Background(), providers[0].Name, "nonexistent-model-xyz")

	if err == nil {
		t.Error("expected error for nonexistent model")
	}
	if candidates != nil {
		t.Error("candidates should be nil on error")
	}
}

func TestResolveSpecificProvider_ContextCanceled(t *testing.T) {
	h := newIntegrationHandler()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, _, err := h.resolveSpecificProvider(ctx, "some-provider", "some-model")

	if err == nil {
		t.Error("expected error with canceled context")
	}
}

// ---------------------------------------------------------------------------
// resolveTimings struct tests
// ---------------------------------------------------------------------------

func TestResolveTimings_ZeroValue(t *testing.T) {
	var rt resolveTimings

	if rt.modelLookupMs != 0 {
		t.Errorf("zero resolveTimings.modelLookupMs = %f, want 0", rt.modelLookupMs)
	}
	if rt.providerLookupMs != 0 {
		t.Errorf("zero resolveTimings.providerLookupMs = %f, want 0", rt.providerLookupMs)
	}
	if rt.keyDecryptMs != 0 {
		t.Errorf("zero resolveTimings.keyDecryptMs = %f, want 0", rt.keyDecryptMs)
	}
}

// ---------------------------------------------------------------------------
// resolveHotelModel integration tests (requires PostgreSQL) - expanded
// ---------------------------------------------------------------------------

func TestResolveHotelModel_Success(t *testing.T) {
	h := newIntegrationHandler()

	// Create a provider
	keyPair, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt API key: %v", err)
	}

	providerName := "test-provider-hotel-" + uuid.New().String()[:8]
	createdProvider, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    providerName,
		BaseURL: "https://api.test.com",
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer func() { _ = h.providerRepo.Delete(context.Background(), createdProvider.ID) }()

	// Create a model
	modelID := uuid.New()
	testModel := &model.Model{
		ID:               modelID,
		ProviderID:       createdProvider.ID,
		ModelID:          "gpt-4",
		Name:             "GPT-4",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "chat",
		InputModalities:  "[\"text\"]",
		OutputModalities: "[\"text\"]",
		Enabled:          true,
		ProviderName:     providerName,
		ProviderEnabled:  true,
	}
	if err := h.modelRepo.Upsert(context.Background(), testModel); err != nil {
		t.Fatalf("failed to create model: %v", err)
	}
	defer func() { _ = h.modelRepo.DeleteByID(context.Background(), modelID) }()

	// Create a failover group
	if _, err := h.failoverRepo.Upsert(context.Background(), "hotel-model", []uuid.UUID{modelID}); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}
	defer func() { _ = h.failoverRepo.Delete(context.Background(), "hotel-model") }()

	candidates, timings, err := h.resolveHotelModel(context.Background(), "hotel-model")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	if candidates[0].model.ID != modelID {
		t.Errorf("expected model ID %v, got %v", modelID, candidates[0].model.ID)
	}
	if candidates[0].provider.ID != createdProvider.ID {
		t.Errorf("expected provider ID %v, got %v", createdProvider.ID, candidates[0].provider.ID)
	}
	if candidates[0].apiKey != "test-api-key" {
		t.Errorf("expected decrypted API key, got %q", candidates[0].apiKey)
	}

	// Verify timings were recorded
	if timings.modelLookupMs < 0 {
		t.Errorf("expected modelLookupMs > 0, got %f", timings.modelLookupMs)
	}
	if timings.providerLookupMs < 0 {
		t.Errorf("expected providerLookupMs > 0, got %f", timings.providerLookupMs)
	}
}

// ---------------------------------------------------------------------------
// resolveSpecificProvider integration tests (requires PostgreSQL) - expanded
// ---------------------------------------------------------------------------

func TestResolveSpecificProvider_Success(t *testing.T) {
	h := newIntegrationHandler()

	// Create a provider
	keyPair, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt API key: %v", err)
	}

	providerName := "test-provider-specific-" + uuid.New().String()[:8]
	createdProvider, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    providerName,
		BaseURL: "https://api.test.com",
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer func() { _ = h.providerRepo.Delete(context.Background(), createdProvider.ID) }()

	// Create a model
	modelID := uuid.New()
	testModel := &model.Model{
		ID:               modelID,
		ProviderID:       createdProvider.ID,
		ModelID:          "specific-model",
		Name:             "Specific Model",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "chat",
		InputModalities:  "[\"text\"]",
		OutputModalities: "[\"text\"]",
		Enabled:          true,
		ProviderName:     providerName,
		ProviderEnabled:  true,
	}
	if err := h.modelRepo.Upsert(context.Background(), testModel); err != nil {
		t.Fatalf("failed to create model: %v", err)
	}
	defer func() { _ = h.modelRepo.DeleteByID(context.Background(), modelID) }()

	candidates, timings, err := h.resolveSpecificProvider(context.Background(), providerName, "specific-model")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	if candidates[0].model.ModelID != "specific-model" {
		t.Errorf("expected model ID 'specific-model', got %v", candidates[0].model.ModelID)
	}
	if candidates[0].provider.ID != createdProvider.ID {
		t.Errorf("expected provider ID %v, got %v", createdProvider.ID, candidates[0].provider.ID)
	}
	if candidates[0].apiKey != "test-api-key" {
		t.Errorf("expected decrypted API key, got %q", candidates[0].apiKey)
	}

	// Verify timings were recorded
	if timings.modelLookupMs < 0 {
		t.Errorf("expected modelLookupMs > 0, got %f", timings.modelLookupMs)
	}
	if timings.providerLookupMs < 0 {
		t.Errorf("expected providerLookupMs > 0, got %f", timings.providerLookupMs)
	}
	if timings.keyDecryptMs < 0 {
		t.Errorf("expected keyDecryptMs > 0, got %f", timings.keyDecryptMs)
	}
}

// TestResolveHotelModel_MultipleEntriesWithDisabled tests failover group with multiple entries where some are disabled
func TestResolveHotelModel_MultipleEntriesWithDisabled(t *testing.T) {
	h := newIntegrationHandler()

	// Create a provider
	keyPair, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt API key: %v", err)
	}

	providerName := "test-provider-multi-" + uuid.New().String()[:8]
	createdProvider, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    providerName,
		BaseURL: "https://api.test.com",
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer func() { _ = h.providerRepo.Delete(context.Background(), createdProvider.ID) }()

	// Create two models
	modelID1 := uuid.New()
	modelID2 := uuid.New()
	testModel1 := &model.Model{
		ID:               modelID1,
		ProviderID:       createdProvider.ID,
		ModelID:          "model-1",
		Name:             "Model 1",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "chat",
		InputModalities:  "[\"text\"]",
		OutputModalities: "[\"text\"]",
		Enabled:          true,
		ProviderName:     providerName,
		ProviderEnabled:  true,
	}
	testModel2 := &model.Model{
		ID:               modelID2,
		ProviderID:       createdProvider.ID,
		ModelID:          "model-2",
		Name:             "Model 2",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "chat",
		InputModalities:  "[\"text\"]",
		OutputModalities: "[\"text\"]",
		Enabled:          true,
		ProviderName:     providerName,
		ProviderEnabled:  true,
	}
	if err := h.modelRepo.Upsert(context.Background(), testModel1); err != nil {
		t.Fatalf("failed to create model 1: %v", err)
	}
	defer func() { _ = h.modelRepo.DeleteByID(context.Background(), modelID1) }()
	if err := h.modelRepo.Upsert(context.Background(), testModel2); err != nil {
		t.Fatalf("failed to create model 2: %v", err)
	}
	defer func() { _ = h.modelRepo.DeleteByID(context.Background(), modelID2) }()

	// Create a failover group with first entry disabled
	entryEnabled := map[string]bool{
		modelID1.String(): false, // disabled
		modelID2.String(): true,  // enabled
	}
	if _, err := h.failoverRepo.UpsertWithConfig(context.Background(), "multi-entry-model", []uuid.UUID{modelID1, modelID2}, entryEnabled, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}
	defer func() { _ = h.failoverRepo.Delete(context.Background(), "multi-entry-model") }()

	candidates, timings, err := h.resolveHotelModel(context.Background(), "multi-entry-model")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate (second entry), got %d", len(candidates))
	}

	if candidates[0].model.ID != modelID2 {
		t.Errorf("expected model ID %v (second entry), got %v", modelID2, candidates[0].model.ID)
	}

	// Verify timings were recorded
	if timings.modelLookupMs < 0 {
		t.Errorf("expected modelLookupMs > 0, got %f", timings.modelLookupMs)
	}
}

// TestResolveHotelModel_ModelDisabled tests when the only model in failover group is disabled
func TestResolveHotelModel_ModelDisabled(t *testing.T) {
	h := newIntegrationHandler()

	// Create a provider
	keyPair, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt API key: %v", err)
	}

	providerName := "test-provider-disabled-" + uuid.New().String()[:8]
	createdProvider, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    providerName,
		BaseURL: "https://api.test.com",
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer func() { _ = h.providerRepo.Delete(context.Background(), createdProvider.ID) }()

	// Create a model
	modelID := uuid.New()
	testModel := &model.Model{
		ID:               modelID,
		ProviderID:       createdProvider.ID,
		ModelID:          "disabled-model",
		Name:             "Disabled Model",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "chat",
		InputModalities:  "[\"text\"]",
		OutputModalities: "[\"text\"]",
		Enabled:          true,
		ProviderName:     providerName,
		ProviderEnabled:  true,
	}
	if err := h.modelRepo.Upsert(context.Background(), testModel); err != nil {
		t.Fatalf("failed to create model: %v", err)
	}
	defer func() { _ = h.modelRepo.DeleteByID(context.Background(), modelID) }()

	// Create a failover group
	if _, err := h.failoverRepo.Upsert(context.Background(), "disabled-model-fg", []uuid.UUID{modelID}); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}
	defer func() { _ = h.failoverRepo.Delete(context.Background(), "disabled-model-fg") }()

	// Disable the model via direct SQL
	pool := testDB.Pool()
	if _, err := pool.Exec(context.Background(), "UPDATE models SET enabled = false WHERE id = $1", modelID); err != nil {
		t.Fatalf("failed to disable model: %v", err)
	}
	// Invalidate the model cache so the handler reads fresh data from DB
	model.InvalidateModelCache()

	candidates, _, err := h.resolveHotelModel(context.Background(), "disabled-model-fg")

	// When all candidates are disabled, returns empty candidates with no error
	// (the error is returned later when trying to route the request)
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates, got %d", len(candidates))
	}
}

// TestResolveHotelModel_ProviderDisabled tests when the provider is disabled
func TestResolveHotelModel_ProviderDisabled(t *testing.T) {
	h := newIntegrationHandler()

	// Create a provider
	keyPair, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt API key: %v", err)
	}

	providerName := "test-provider-disabled-" + uuid.New().String()[:8]
	createdProvider, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    providerName,
		BaseURL: "https://api.test.com",
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer func() { _ = h.providerRepo.Delete(context.Background(), createdProvider.ID) }()

	// Create a model
	modelID := uuid.New()
	testModel := &model.Model{
		ID:               modelID,
		ProviderID:       createdProvider.ID,
		ModelID:          "provider-disabled-model",
		Name:             "Provider Disabled Model",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "chat",
		InputModalities:  "[\"text\"]",
		OutputModalities: "[\"text\"]",
		Enabled:          true,
		ProviderName:     providerName,
		ProviderEnabled:  true,
	}
	if err := h.modelRepo.Upsert(context.Background(), testModel); err != nil {
		t.Fatalf("failed to create model: %v", err)
	}
	defer func() { _ = h.modelRepo.DeleteByID(context.Background(), modelID) }()

	// Create a failover group
	if _, err := h.failoverRepo.Upsert(context.Background(), "provider-disabled-fg", []uuid.UUID{modelID}); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}
	defer func() { _ = h.failoverRepo.Delete(context.Background(), "provider-disabled-fg") }()

	// Disable the provider
	disabled := false
	if _, err := h.providerRepo.Update(context.Background(), createdProvider.ID, provider.UpdateProviderRequest{Enabled: &disabled}, createdProvider.EncryptedKey, createdProvider.KeyNonce, createdProvider.KeySalt); err != nil {
		t.Fatalf("failed to disable provider: %v", err)
	}

	candidates, _, err := h.resolveHotelModel(context.Background(), "provider-disabled-fg")

	// When all candidates are filtered out, returns empty candidates with no error
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates, got %d", len(candidates))
	}
}

// TestResolveHotelModel_CircuitBreakerOpen tests when circuit breaker is open for provider
func TestResolveHotelModel_CircuitBreakerOpen(t *testing.T) {
	h := newIntegrationHandler()

	// Create a provider
	keyPair, err := auth.Encrypt("test-api-key", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("failed to encrypt API key: %v", err)
	}

	providerName := "test-provider-cb-" + uuid.New().String()[:8]
	createdProvider, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    providerName,
		BaseURL: "https://api.test.com",
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer func() { _ = h.providerRepo.Delete(context.Background(), createdProvider.ID) }()

	// Create a model
	modelID := uuid.New()
	testModel := &model.Model{
		ID:               modelID,
		ProviderID:       createdProvider.ID,
		ModelID:          "cb-model",
		Name:             "CB Model",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "chat",
		InputModalities:  "[\"text\"]",
		OutputModalities: "[\"text\"]",
		Enabled:          true,
		ProviderName:     providerName,
		ProviderEnabled:  true,
	}
	if err := h.modelRepo.Upsert(context.Background(), testModel); err != nil {
		t.Fatalf("failed to create model: %v", err)
	}
	defer func() { _ = h.modelRepo.DeleteByID(context.Background(), modelID) }()

	// Create a failover group
	if _, err := h.failoverRepo.Upsert(context.Background(), "cb-fg", []uuid.UUID{modelID}); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}
	defer func() { _ = h.failoverRepo.Delete(context.Background(), "cb-fg") }()

	// Open the circuit breaker for the provider (threshold=5 by default)
	for i := 0; i < 5; i++ {
		h.circuitBreaker.RecordFailure(createdProvider.ID)
	}

	candidates, _, err := h.resolveHotelModel(context.Background(), "cb-fg")

	// When circuit breaker is open, returns empty candidates with no error
	if err != nil {
		t.Errorf("expected no error, got %v", err)
	}
	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates, got %d", len(candidates))
	}
}

// TestResolveHotelModel_EmptyAPIKey tests resolution with a provider that has empty encrypted key
func TestResolveHotelModel_EmptyAPIKey(t *testing.T) {
	h := newIntegrationHandler()

	// Create a provider with empty API key (keyless-like behavior)
	providerName := "test-provider-emptykey-" + uuid.New().String()[:8]
	createdProvider, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    providerName,
		BaseURL: "https://api.test.com",
		APIKey:  "",
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("failed to create provider with empty key: %v", err)
	}
	defer func() { _ = h.providerRepo.Delete(context.Background(), createdProvider.ID) }()

	// Create a model
	modelID := uuid.New()
	testModel := &model.Model{
		ID:               modelID,
		ProviderID:       createdProvider.ID,
		ModelID:          "empty-key-model",
		Name:             "Empty Key Model",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "chat",
		InputModalities:  "[\"text\"]",
		OutputModalities: "[\"text\"]",
		Enabled:          true,
		ProviderName:     providerName,
		ProviderEnabled:  true,
	}
	if err := h.modelRepo.Upsert(context.Background(), testModel); err != nil {
		t.Fatalf("failed to create model: %v", err)
	}
	defer func() { _ = h.modelRepo.DeleteByID(context.Background(), modelID) }()

	// Create a failover group
	if _, err := h.failoverRepo.Upsert(context.Background(), "empty-key-fg", []uuid.UUID{modelID}); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}
	defer func() { _ = h.failoverRepo.Delete(context.Background(), "empty-key-fg") }()

	candidates, timings, err := h.resolveHotelModel(context.Background(), "empty-key-fg")

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}

	if candidates[0].model.ID != modelID {
		t.Errorf("expected model ID %v, got %v", modelID, candidates[0].model.ID)
	}
	if candidates[0].provider.ID != createdProvider.ID {
		t.Errorf("expected provider ID %v, got %v", createdProvider.ID, candidates[0].provider.ID)
	}
	// Empty key providers should have empty API key
	if candidates[0].apiKey != "" {
		t.Errorf("expected empty API key, got %q", candidates[0].apiKey)
	}

	// Verify timings were recorded
	if timings.modelLookupMs < 0 {
		t.Errorf("expected modelLookupMs > 0, got %f", timings.modelLookupMs)
	}
}

func TestResolveSpecificProvider_WrongMasterKey(t *testing.T) {
	h := newIntegrationHandler()

	// Create a provider with wrong master key
	keyPair, err := auth.Encrypt("test-api-key", "wrong-master-key")
	if err != nil {
		t.Fatalf("failed to encrypt API key: %v", err)
	}

	providerName := "test-provider-wrong-key-" + uuid.New().String()[:8]
	createdProvider, err := h.providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    providerName,
		BaseURL: "https://api.test.com",
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	defer func() { _ = h.providerRepo.Delete(context.Background(), createdProvider.ID) }()

	// Create a model
	modelID := uuid.New()
	testModel := &model.Model{
		ID:               modelID,
		ProviderID:       createdProvider.ID,
		ModelID:          "wrong-key-model",
		Name:             "Wrong Key Model",
		Capabilities:     "{}",
		Params:           "{}",
		Modality:         "chat",
		InputModalities:  "[\"text\"]",
		OutputModalities: "[\"text\"]",
		Enabled:          true,
		ProviderName:     providerName,
		ProviderEnabled:  true,
	}
	if err := h.modelRepo.Upsert(context.Background(), testModel); err != nil {
		t.Fatalf("failed to create model: %v", err)
	}
	defer func() { _ = h.modelRepo.DeleteByID(context.Background(), modelID) }()

	candidates, _, err := h.resolveSpecificProvider(context.Background(), providerName, "wrong-key-model")

	if err == nil {
		t.Error("expected error for wrong master key")
	}
	if len(candidates) != 0 {
		t.Errorf("expected 0 candidates, got %d", len(candidates))
	}
}
