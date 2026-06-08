package provider

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/db"
)

// ---------------------------------------------------------------------------
// TestMain — integration test DB setup
// ---------------------------------------------------------------------------

var testDB *db.DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	testDBURL, setupErr := db.SetupTestDB("provider")
	if setupErr != nil {
		log.Printf("failed to setup test DB: %v", setupErr)
		os.Exit(1)
	}
	defer db.CleanupTestDB("provider")

	var err error
	testDB, err = db.New(ctx, testDBURL, 25, 5)
	if err != nil {
		log.Printf("failed to initialize test DB: %v", err)
		os.Exit(1) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
	}
	defer testDB.Close()

	os.Exit(m.Run()) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
}

func newTestRepo(t *testing.T) *Repository {
	t.Helper()

	return NewRepository(testDB.Pool())
}

// ---------------------------------------------------------------------------
// NewRepository
// ---------------------------------------------------------------------------

func TestNewRepository_NilPool(t *testing.T) {
	repo := NewRepository(nil)
	if repo == nil {
		t.Error("NewRepository(nil) should return non-nil")
	}
}

// ---------------------------------------------------------------------------
// Provider JSON round-trip
// ---------------------------------------------------------------------------

func TestProvider_JSONRoundTrip(t *testing.T) {
	id := uuid.New()
	now := time.Now().Truncate(time.Millisecond).UTC()
	masked := "sk...xz"

	p := Provider{
		ID:           id,
		Name:         "test-provider",
		BaseURL:      "https://api.example.com/v1",
		EncryptedKey: []byte("enc"),
		KeyNonce:     []byte("nonce"),
		KeySalt:      []byte("salt"),
		MaskedKey:    &masked,
		Enabled:      true,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal Provider: %v", err)
	}

	var decoded Provider
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal Provider: %v", err)
	}

	if decoded.ID != p.ID {
		t.Errorf("ID = %v, want %v", decoded.ID, p.ID)
	}
	if decoded.Name != p.Name {
		t.Errorf("Name = %q, want %q", decoded.Name, p.Name)
	}
	if decoded.BaseURL != p.BaseURL {
		t.Errorf("BaseURL = %q, want %q", decoded.BaseURL, p.BaseURL)
	}
	if decoded.Enabled != p.Enabled {
		t.Errorf("Enabled = %v, want %v", decoded.Enabled, p.Enabled)
	}
	// EncryptedKey, KeyNonce, KeySalt have `json:"-"` so they should be zero
	if len(decoded.EncryptedKey) != 0 {
		t.Error("EncryptedKey should be empty (json:\"-\")")
	}
	if decoded.MaskedKey == nil || *decoded.MaskedKey != masked {
		t.Errorf("MaskedKey = %v, want %q", decoded.MaskedKey, masked)
	}
}

func TestProvider_JSONNilFields(t *testing.T) {
	p := Provider{
		ID:   uuid.New(),
		Name: "minimal",
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded Provider
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.MaskedKey != nil {
		t.Errorf("MaskedKey should be nil, got %v", decoded.MaskedKey)
	}
	if decoded.LastDiscoveredAt != nil {
		t.Error("LastDiscoveredAt should be nil")
	}
	if decoded.LastUsedAt != nil {
		t.Error("LastUsedAt should be nil")
	}
}

// ---------------------------------------------------------------------------
// CreateProviderRequest JSON
// ---------------------------------------------------------------------------

func TestCreateProviderRequest_JSON(t *testing.T) {
	raw := `{"name":"openai","base_url":"https://api.openai.com/v1","api_key":"sk-test-123"}`

	var req CreateProviderRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if req.Name != "openai" {
		t.Errorf("Name = %q, want %q", req.Name, "openai")
	}
	if req.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("BaseURL = %q, want %q", req.BaseURL, "https://api.openai.com/v1")
	}
	if req.APIKey != "sk-test-123" {
		t.Errorf("APIKey = %q, want %q", req.APIKey, "sk-test-123")
	}
}

func TestCreateProviderRequest_JSONEmptyKey(t *testing.T) {
	raw := `{"name":"keyless","base_url":"https://opencode.ai/zen"}`
	var req CreateProviderRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.APIKey != "" {
		t.Errorf("APIKey should be empty, got %q", req.APIKey)
	}
}

// ---------------------------------------------------------------------------
// UpdateProviderRequest JSON
// ---------------------------------------------------------------------------

func TestUpdateProviderRequest_JSONPartial(t *testing.T) {
	raw := `{"enabled":false}`
	var req UpdateProviderRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if req.Name != nil {
		t.Errorf("Name should be nil, got %v", req.Name)
	}
	if req.BaseURL != nil {
		t.Errorf("BaseURL should be nil, got %v", req.BaseURL)
	}
	if req.APIKey != nil {
		t.Errorf("APIKey should be nil, got %v", req.APIKey)
	}
	if req.Enabled == nil || *req.Enabled != false {
		t.Errorf("Enabled = %v, want false", req.Enabled)
	}
}

func TestUpdateProviderRequest_JSONFull(t *testing.T) {
	raw := `{"name":"new-name","base_url":"https://new.url","api_key":"new-key","enabled":true}`
	var req UpdateProviderRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if req.Name == nil || *req.Name != "new-name" {
		t.Errorf("Name = %v, want %q", req.Name, "new-name")
	}
	if req.BaseURL == nil || *req.BaseURL != "https://new.url" {
		t.Errorf("BaseURL = %v, want %q", req.BaseURL, "https://new.url")
	}
	if req.APIKey == nil || *req.APIKey != "new-key" {
		t.Errorf("APIKey = %v, want %q", req.APIKey, "new-key")
	}
	if req.Enabled == nil || *req.Enabled != true {
		t.Errorf("Enabled = %v, want true", req.Enabled)
	}
}

func TestUpdateProviderRequest_JSONEmpty(t *testing.T) {
	raw := `{}`
	var req UpdateProviderRequest
	if err := json.Unmarshal([]byte(raw), &req); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if req.Name != nil || req.BaseURL != nil || req.APIKey != nil || req.Enabled != nil {
		t.Error("all fields should be nil for empty JSON object")
	}
}

// ---------------------------------------------------------------------------
// ProviderResponse JSON
// ---------------------------------------------------------------------------

func TestProviderResponse_JSON(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond).UTC()
	resp := ProviderResponse{
		ID:          uuid.New(),
		Name:        "test",
		BaseURL:     "https://api.example.com",
		MaskedKey:   "sk...ab",
		Enabled:     true,
		CreatedAt:   now,
		UpdatedAt:   now,
		ModelCount:  5,
		TotalTokens: 1000,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal to map: %v", err)
	}

	if decoded["masked_key"] != "sk...ab" {
		t.Errorf("masked_key = %v, want %q", decoded["masked_key"], "sk...ab")
	}
	if decoded["model_count"] != float64(5) {
		t.Errorf("model_count = %v, want 5", decoded["model_count"])
	}
	if decoded["total_tokens"] != float64(1000) {
		t.Errorf("total_tokens = %v, want 1000", decoded["total_tokens"])
	}
}

func TestProviderResponse_JSONNilTimestamps(t *testing.T) {
	resp := ProviderResponse{
		ID:        uuid.New(),
		Name:      "minimal",
		MaskedKey: "N/A",
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded map[string]interface{}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if _, ok := decoded["last_discovered_at"]; !ok {
		t.Error("last_discovered_at key should be present even if nil")
	}
	if _, ok := decoded["last_used_at"]; !ok {
		t.Error("last_used_at key should be present even if nil")
	}
}

// ---------------------------------------------------------------------------
// GetOpenAIModels — catalog validation
// ---------------------------------------------------------------------------

func TestGetOpenAIModels_NonEmpty(t *testing.T) {
	catalog := GetOpenAIModels()
	if len(catalog) == 0 {
		t.Error("GetOpenAIModels should return non-empty catalog")
	}
}

func TestGetOpenAIModels_AllFieldsValid(t *testing.T) {
	catalog := GetOpenAIModels()
	for i, spec := range catalog {
		if spec.ModelID == "" {
			t.Errorf("catalog[%d]: ModelID is empty", i)
		}
		if spec.DisplayName == "" {
			t.Errorf("catalog[%d] (%s): DisplayName is empty", i, spec.ModelID)
		}
		if spec.ContextLength <= 0 {
			t.Errorf("catalog[%d] (%s): ContextLength = %d, want > 0", i, spec.ModelID, spec.ContextLength)
		}
		if spec.MaxOutputTokens <= 0 {
			t.Errorf("catalog[%d] (%s): MaxOutputTokens = %d, want > 0", i, spec.ModelID, spec.MaxOutputTokens)
		}
		if spec.InputPricePerMillion < 0 {
			t.Errorf("catalog[%d] (%s): InputPricePerMillion = %f, want >= 0", i, spec.ModelID, spec.InputPricePerMillion)
		}
		if spec.OutputPricePerMillion < 0 {
			t.Errorf("catalog[%d] (%s): OutputPricePerMillion = %f, want >= 0", i, spec.ModelID, spec.OutputPricePerMillion)
		}
		if spec.InputPricePerMillionCacheHit < 0 {
			t.Errorf("catalog[%d] (%s): InputPricePerMillionCacheHit = %f, want >= 0", i, spec.ModelID, spec.InputPricePerMillionCacheHit)
		}
		if spec.Modality == "" {
			t.Errorf("catalog[%d] (%s): Modality is empty", i, spec.ModelID)
		}
	}
}

func TestGetOpenAIModels_NoDuplicateModelIDs(t *testing.T) {
	catalog := GetOpenAIModels()
	seen := make(map[string]bool)
	for _, spec := range catalog {
		if seen[spec.ModelID] {
			t.Errorf("duplicate ModelID in OpenAI catalog: %s", spec.ModelID)
		}
		seen[spec.ModelID] = true
	}
}

// ---------------------------------------------------------------------------
// LookupOpenAICatalog
// ---------------------------------------------------------------------------

func TestLookupOpenAICatalog_NotFound(t *testing.T) {
	catalog := GetOpenAIModels()
	result := LookupOpenAICatalog(catalog, "nonexistent-model-xyz")
	if result != nil {
		t.Errorf("expected nil for unknown model, got %+v", result)
	}
}

func TestLookupOpenAICatalog_Found(t *testing.T) {
	catalog := GetOpenAIModels()
	if len(catalog) == 0 {
		t.Skip("catalog is empty")
	}
	first := catalog[0]
	result := LookupOpenAICatalog(catalog, first.ModelID)
	if result == nil {
		t.Fatalf("expected non-nil for %q", first.ModelID)
		return
	}
	if result.ModelID != first.ModelID {
		t.Errorf("ModelID = %q, want %q", result.ModelID, first.ModelID)
	}
	if result.DisplayName != first.DisplayName {
		t.Errorf("DisplayName = %q, want %q", result.DisplayName, first.DisplayName)
	}
}

func TestLookupOpenAICatalog_NilSlice(t *testing.T) {
	result := LookupOpenAICatalog(nil, "gpt-5.5")
	if result != nil {
		t.Error("expected nil when catalog is nil")
	}
}

func TestLookupOpenAICatalog_EmptySlice(t *testing.T) {
	result := LookupOpenAICatalog([]OpenAIModelSpec{}, "gpt-5.5")
	if result != nil {
		t.Error("expected nil for empty catalog")
	}
}

// ---------------------------------------------------------------------------
// GetAnthropicPricing — catalog validation
// ---------------------------------------------------------------------------

func TestGetAnthropicPricing_NonEmpty(t *testing.T) {
	catalog := GetAnthropicPricing()
	if len(catalog) == 0 {
		t.Error("GetAnthropicPricing should return non-empty catalog")
	}
}

func TestGetAnthropicPricing_AllFieldsValid(t *testing.T) {
	catalog := GetAnthropicPricing()
	for i, spec := range catalog {
		if spec.ModelID == "" {
			t.Errorf("pricing[%d]: ModelID is empty", i)
		}
		if spec.InputPricePerMillion < 0 {
			t.Errorf("pricing[%d] (%s): InputPricePerMillion = %f, want >= 0", i, spec.ModelID, spec.InputPricePerMillion)
		}
		if spec.OutputPricePerMillion < 0 {
			t.Errorf("pricing[%d] (%s): OutputPricePerMillion = %f, want >= 0", i, spec.ModelID, spec.OutputPricePerMillion)
		}
		if spec.InputPricePerMillionCacheHit < 0 {
			t.Errorf("pricing[%d] (%s): InputPricePerMillionCacheHit = %f, want >= 0", i, spec.ModelID, spec.InputPricePerMillionCacheHit)
		}
	}
}

func TestGetAnthropicPricing_NoDuplicateModelIDs(t *testing.T) {
	catalog := GetAnthropicPricing()
	seen := make(map[string]bool)
	for _, spec := range catalog {
		if seen[spec.ModelID] {
			t.Errorf("duplicate ModelID in Anthropic pricing: %s", spec.ModelID)
		}
		seen[spec.ModelID] = true
	}
}

// ---------------------------------------------------------------------------
// NewDiscoveryService
// ---------------------------------------------------------------------------

func TestNewDiscoveryService_NonNil(t *testing.T) {
	svc := NewDiscoveryService(nil, nil)
	if svc == nil {
		t.Fatal("NewDiscoveryService should return non-nil")
		return
	}
	if svc.httpClient == nil {
		t.Error("httpClient should be non-nil")
	}
}

// ---------------------------------------------------------------------------
// OpenAIModelSpec — struct JSON round-trip
// ---------------------------------------------------------------------------

func TestOpenAIModelSpec_JSONRoundTrip(t *testing.T) {
	spec := OpenAIModelSpec{
		ModelID:                      "test-model",
		DisplayName:                  "Test Model",
		Description:                  "A test model",
		ContextLength:                128000,
		MaxOutputTokens:              8192,
		Modality:                     "text",
		InputModalities:              `["text"]`,
		OutputModalities:             `["text"]`,
		Streaming:                    true,
		Reasoning:                    false,
		ToolCalling:                  true,
		StructuredOutput:             true,
		Vision:                       false,
		InputPricePerMillion:         1.5,
		InputPricePerMillionCacheHit: 0.15,
		OutputPricePerMillion:        6.0,
	}

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded OpenAIModelSpec
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ModelID != spec.ModelID {
		t.Errorf("ModelID = %q, want %q", decoded.ModelID, spec.ModelID)
	}
	if decoded.ContextLength != spec.ContextLength {
		t.Errorf("ContextLength = %d, want %d", decoded.ContextLength, spec.ContextLength)
	}
	if decoded.InputPricePerMillion != spec.InputPricePerMillion {
		t.Errorf("InputPricePerMillion = %f, want %f", decoded.InputPricePerMillion, spec.InputPricePerMillion)
	}
}

// ---------------------------------------------------------------------------
// AnthropicPricingSpec — struct JSON round-trip
// ---------------------------------------------------------------------------

func TestAnthropicPricingSpec_JSONRoundTrip(t *testing.T) {
	spec := AnthropicPricingSpec{
		ModelID:                      "claude-test",
		InputPricePerMillion:         3.0,
		InputPricePerMillionCacheHit: 0.3,
		OutputPricePerMillion:        15.0,
	}

	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded AnthropicPricingSpec
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.ModelID != spec.ModelID {
		t.Errorf("ModelID = %q, want %q", decoded.ModelID, spec.ModelID)
	}
	if decoded.InputPricePerMillion != spec.InputPricePerMillion {
		t.Errorf("InputPricePerMillion = %f, want %f", decoded.InputPricePerMillion, spec.InputPricePerMillion)
	}
	if decoded.OutputPricePerMillion != spec.OutputPricePerMillion {
		t.Errorf("OutputPricePerMillion = %f, want %f", decoded.OutputPricePerMillion, spec.OutputPricePerMillion)
	}
}

// ===========================================================================
// Integration tests (require PostgreSQL)
// ===========================================================================

// uniqueName generates a unique provider name for isolation.
func uniqueName(t *testing.T) string {
	t.Helper()
	return "test-prov-" + uuid.New().String()[:8]
}

func TestRepository_CreateAndGet(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	name := uniqueName(t)
	req := CreateProviderRequest{
		Name:    name,
		BaseURL: "https://api.example.com/v1",
		APIKey:  "sk-test-key-12345",
	}

	p, err := repo.Create(ctx, req, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if p.ID == uuid.Nil {
		t.Error("ID should not be zero")
	}
	if p.Name != name {
		t.Errorf("Name = %q, want %q", p.Name, name)
	}
	if p.BaseURL != req.BaseURL {
		t.Errorf("BaseURL = %q, want %q", p.BaseURL, req.BaseURL)
	}
	if !p.Enabled {
		t.Error("Enabled should be true by default")
	}
	if p.MaskedKey == nil || *p.MaskedKey == "" {
		t.Error("MaskedKey should be set")
	}
	if p.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}

	// Get by ID
	found, err := repo.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if found.ID != p.ID {
		t.Errorf("Get returned wrong ID: %v, want %v", found.ID, p.ID)
	}
	if found.Name != p.Name {
		t.Errorf("Get returned wrong Name: %q, want %q", found.Name, p.Name)
	}
}

func TestRepository_Get_NotFound(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	_, err := repo.Get(ctx, uuid.New())
	if err == nil {
		t.Error("expected error for nonexistent ID")
	}
}

func TestRepository_List(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	name := uniqueName(t)
	_, err := repo.Create(ctx, CreateProviderRequest{
		Name:    name,
		BaseURL: "https://list-test.example.com",
		APIKey:  "sk-list-test",
	}, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	providers, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	found := false
	for _, p := range providers {
		if p.Name == name {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("List did not include provider %q", name)
	}
}

func TestRepository_GetByIDs(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	p1, err := repo.Create(ctx, CreateProviderRequest{
		Name: uniqueName(t), BaseURL: "https://a.example.com", APIKey: "sk-a",
	}, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err != nil {
		t.Fatalf("Create p1: %v", err)
	}

	p2, err := repo.Create(ctx, CreateProviderRequest{
		Name: uniqueName(t), BaseURL: "https://b.example.com", APIKey: "sk-b",
	}, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err != nil {
		t.Fatalf("Create p2: %v", err)
	}

	result, err := repo.GetByIDs(ctx, []uuid.UUID{p1.ID, p2.ID})
	if err != nil {
		t.Fatalf("GetByIDs: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	if _, ok := result[p1.ID]; !ok {
		t.Error("p1 not found in result")
	}
	if _, ok := result[p2.ID]; !ok {
		t.Error("p2 not found in result")
	}
}

func TestRepository_GetByIDs_EmptySlice(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	result, err := repo.GetByIDs(ctx, []uuid.UUID{})
	if err != nil {
		t.Fatalf("GetByIDs with empty slice: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 results for empty slice, got %d", len(result))
	}
}

func TestRepository_GetByIDs_NotFound(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	result, err := repo.GetByIDs(ctx, []uuid.UUID{uuid.New()})
	if err != nil {
		t.Fatalf("GetByIDs: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 results for nonexistent ID, got %d", len(result))
	}
}

func TestRepository_GetByName(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	name := uniqueName(t)
	created, err := repo.Create(ctx, CreateProviderRequest{
		Name: name, BaseURL: "https://name-test.example.com", APIKey: "sk-name",
	}, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	found, err := repo.GetByName(ctx, name)
	if err != nil {
		t.Fatalf("GetByName: %v", err)
	}
	if found.ID != created.ID {
		t.Errorf("GetByName ID = %v, want %v", found.ID, created.ID)
	}
}

func TestRepository_GetByName_NotFound(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	_, err := repo.GetByName(ctx, "nonexistent-provider-"+uuid.New().String())
	if err == nil {
		t.Error("expected error for nonexistent name")
	}
}

func TestRepository_GetByName_NormalizedName(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create provider with hyphenated name
	name := "test-provider-with-hyphens"
	created, err := repo.Create(ctx, CreateProviderRequest{
		Name: name, BaseURL: "https://hyphen-test.example.com", APIKey: "sk-hyphen",
	}, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Try to get with space-separated name (should normalize to hyphens)
	spaceName := "test provider with hyphens"
	found, err := repo.GetByName(ctx, spaceName)
	if err != nil {
		t.Fatalf("GetByName with normalized name: %v", err)
	}
	if found.ID != created.ID {
		t.Errorf("GetByName normalized ID = %v, want %v", found.ID, created.ID)
	}
}

func TestRepository_GetByIDs_WithCache(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create a provider
	p1, err := repo.Create(ctx, CreateProviderRequest{
		Name: uniqueName(t), BaseURL: "https://cache-test.example.com", APIKey: "sk-cache",
	}, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// First call populates cache
	_, err = repo.Get(ctx, p1.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// Second call with GetByIDs should use cache
	result, err := repo.GetByIDs(ctx, []uuid.UUID{p1.ID})
	if err != nil {
		t.Fatalf("GetByIDs: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 result, got %d", len(result))
	}
	if _, ok := result[p1.ID]; !ok {
		t.Error("cached provider not found in result")
	}
}

func TestRepository_GetByIDs_MixedCachedAndUncached(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create two providers
	p1, err := repo.Create(ctx, CreateProviderRequest{
		Name: uniqueName(t), BaseURL: "https://mixed1.example.com", APIKey: "sk-mixed1",
	}, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err != nil {
		t.Fatalf("Create p1: %v", err)
	}

	p2, err := repo.Create(ctx, CreateProviderRequest{
		Name: uniqueName(t), BaseURL: "https://mixed2.example.com", APIKey: "sk-mixed2",
	}, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err != nil {
		t.Fatalf("Create p2: %v", err)
	}

	// Cache only p1
	_, err = repo.Get(ctx, p1.ID)
	if err != nil {
		t.Fatalf("Get p1: %v", err)
	}

	// GetByIDs should return both (one from cache, one from DB)
	result, err := repo.GetByIDs(ctx, []uuid.UUID{p1.ID, p2.ID})
	if err != nil {
		t.Fatalf("GetByIDs: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 results, got %d", len(result))
	}
}

func TestRepository_GetByIDs_SomeNotFound(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create one provider
	p1, err := repo.Create(ctx, CreateProviderRequest{
		Name: uniqueName(t), BaseURL: "https://somefound.example.com", APIKey: "sk-found",
	}, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Request existing + non-existing ID
	nonExistentID := uuid.New()
	result, err := repo.GetByIDs(ctx, []uuid.UUID{p1.ID, nonExistentID})
	if err != nil {
		t.Fatalf("GetByIDs: %v", err)
	}
	// Should only return the existing one
	if len(result) != 1 {
		t.Errorf("expected 1 result (only existing), got %d", len(result))
	}
	if _, ok := result[p1.ID]; !ok {
		t.Error("existing provider not found in result")
	}
	if _, ok := result[nonExistentID]; ok {
		t.Error("non-existent provider should not be in result")
	}
}

func TestRepository_Update(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	p, err := repo.Create(ctx, CreateProviderRequest{
		Name: uniqueName(t), BaseURL: "https://old.example.com", APIKey: "sk-old",
	}, []byte("enc-old"), []byte("nonce-old"), []byte("salt-old"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	newName := "updated-" + uuid.New().String()[:8]
	newBase := "https://new.example.com"
	newKey := "sk-new-key"
	enabled := false

	updated, err := repo.Update(ctx, p.ID, UpdateProviderRequest{
		Name:    &newName,
		BaseURL: &newBase,
		APIKey:  &newKey,
		Enabled: &enabled,
	}, []byte("enc-new"), []byte("nonce-new"), []byte("salt-new"))
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	if updated.Name != newName {
		t.Errorf("Name = %q, want %q", updated.Name, newName)
	}
	if updated.BaseURL != newBase {
		t.Errorf("BaseURL = %q, want %q", updated.BaseURL, newBase)
	}
	if updated.Enabled != false {
		t.Error("Enabled should be false after update")
	}
}

func TestRepository_Update_NotFound(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	_, err := repo.Update(ctx, uuid.New(), UpdateProviderRequest{}, nil, nil, nil)
	if err == nil {
		t.Error("expected error for updating nonexistent provider")
	}
}

func TestRepository_Delete(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	p, err := repo.Create(ctx, CreateProviderRequest{
		Name: uniqueName(t), BaseURL: "https://delete-test.example.com", APIKey: "sk-del",
	}, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if err := repo.Delete(ctx, p.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	_, err = repo.Get(ctx, p.ID)
	if err == nil {
		t.Error("expected error getting deleted provider")
	}
}

func TestRepository_Delete_NotFound(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	err := repo.Delete(ctx, uuid.New())
	if err == nil {
		t.Error("expected error deleting nonexistent provider")
	}
}

func TestRepository_TouchLastUsed(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	p, err := repo.Create(ctx, CreateProviderRequest{
		Name: uniqueName(t), BaseURL: "https://touch-test.example.com", APIKey: "sk-touch",
	}, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	beforeTouch := time.Now()
	if err := repo.TouchLastUsed(ctx, p.ID); err != nil {
		t.Fatalf("TouchLastUsed: %v", err)
	}

	// Cache was invalidated, so this should hit DB
	found, err := repo.Get(ctx, p.ID)
	if err != nil {
		t.Fatalf("Get after touch: %v", err)
	}
	if found.LastUsedAt == nil {
		t.Fatal("LastUsedAt should be set after TouchLastUsed")
	}
	if found.LastUsedAt.Before(beforeTouch.Add(-1 * time.Second)) {
		t.Errorf("LastUsedAt = %v, should be around %v", found.LastUsedAt, beforeTouch)
	}
}

func TestRepository_Create_CancelledContext(t *testing.T) {
	repo := newTestRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel before query executes

	_, err := repo.Create(ctx, CreateProviderRequest{
		Name: uniqueName(t), BaseURL: "https://cancel-test.example.com", APIKey: "sk-cancel",
	}, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err == nil {
		t.Error("expected error from Create with cancelled context")
	}
}

func TestRepository_List_CancelledContext(t *testing.T) {
	repo := newTestRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := repo.List(ctx)
	if err == nil {
		t.Error("expected error from List with cancelled context")
	}
}

func TestRepository_GetByIDs_CancelledContext(t *testing.T) {
	repo := newTestRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Use a random UUID that won't be in cache, forcing a DB query
	ids := []uuid.UUID{uuid.New()}
	_, err := repo.GetByIDs(ctx, ids)
	if err == nil {
		t.Error("expected error from GetByIDs with cancelled context")
	}
}

func TestRepository_Delete_CancelledContext(t *testing.T) {
	repo := newTestRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := repo.Delete(ctx, uuid.New())
	if err == nil {
		t.Error("expected error from Delete with cancelled context")
	}
}

func TestRepository_TouchLastUsed_CancelledContext(t *testing.T) {
	repo := newTestRepo(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := repo.TouchLastUsed(ctx, uuid.New())
	if err == nil {
		t.Error("expected error from TouchLastUsed with cancelled context")
	}
}

func TestRepository_GetByName_DBHit(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	name := uniqueName(t)
	created, err := repo.Create(ctx, CreateProviderRequest{
		Name: name, BaseURL: "https://dbhit-test.example.com", APIKey: "sk-dbhit",
	}, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Create caches the provider. Invalidate cache so GetByName hits DB directly.
	InvalidateProviderCache()

	found, err := repo.GetByName(ctx, name)
	if err != nil {
		t.Fatalf("GetByName after cache invalidation: %v", err)
	}
	if found.ID != created.ID {
		t.Errorf("GetByName ID = %v, want %v", found.ID, created.ID)
	}
}

// ===========================================================================
// Tests moved from provider_coverage_test.go
// ===========================================================================

// TestGetByIDs_AllCached tests the path where all requested IDs are in cache.
// This exercises the early return path: if len(uncachedIDs) == 0 { return result, nil }
func TestGetByIDs_AllCached(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create two providers
	p1, err := repo.Create(ctx, CreateProviderRequest{
		Name: uniqueName(t), BaseURL: "https://allcached1.example.com", APIKey: "sk-cached1",
	}, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err != nil {
		t.Fatalf("Create p1: %v", err)
	}

	p2, err := repo.Create(ctx, CreateProviderRequest{
		Name: uniqueName(t), BaseURL: "https://allcached2.example.com", APIKey: "sk-cached2",
	}, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err != nil {
		t.Fatalf("Create p2: %v", err)
	}

	// Pre-populate cache by calling Get for both providers
	_, err = repo.Get(ctx, p1.ID)
	if err != nil {
		t.Fatalf("Get p1: %v", err)
	}
	_, err = repo.Get(ctx, p2.ID)
	if err != nil {
		t.Fatalf("Get p2: %v", err)
	}

	// GetByIDs should return both from cache without DB query
	result, err := repo.GetByIDs(ctx, []uuid.UUID{p1.ID, p2.ID})
	if err != nil {
		t.Fatalf("GetByIDs: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 results from cache, got %d", len(result))
	}
	if _, ok := result[p1.ID]; !ok {
		t.Error("p1 not found in cached result")
	}
	if _, ok := result[p2.ID]; !ok {
		t.Error("p2 not found in cached result")
	}
}

// TestList_Empty tests the path where the providers table is empty.
// This ensures List handles zero rows gracefully.
func TestList_Empty(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Delete all providers to ensure empty table
	// First, list existing providers
	existing, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List initial: %v", err)
	}

	// Delete each existing provider
	for _, p := range existing {
		if err := repo.Delete(ctx, p.ID); err != nil {
			t.Fatalf("Delete %v: %v", p.ID, err)
		}
	}

	// Now List should return empty slice, no error
	providers, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List on empty table: %v", err)
	}
	if len(providers) != 0 {
		t.Errorf("expected 0 providers on empty table, got %d", len(providers))
	}
}

// TestGetByIDs_UncachedDBFetch tests that GetByIDs correctly fetches providers
// from the database when all entries are uncached (cache was invalidated).
// This exercises the full DB query path: rows.Next(), cacheProvider, and rows.Err().
func TestGetByIDs_UncachedDBFetch(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create two providers
	p1, err := repo.Create(ctx, CreateProviderRequest{
		Name: uniqueName(t), BaseURL: "https://uncached1.example.com", APIKey: "sk-uncached1",
	}, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err != nil {
		t.Fatalf("Create p1: %v", err)
	}

	p2, err := repo.Create(ctx, CreateProviderRequest{
		Name: uniqueName(t), BaseURL: "https://uncached2.example.com", APIKey: "sk-uncached2",
	}, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err != nil {
		t.Fatalf("Create p2: %v", err)
	}

	// Invalidate cache so both must be fetched from DB
	InvalidateProviderCache()

	result, err := repo.GetByIDs(ctx, []uuid.UUID{p1.ID, p2.ID})
	if err != nil {
		t.Fatalf("GetByIDs after cache invalidation: %v", err)
	}

	if len(result) != 2 {
		t.Fatalf("expected 2 results from DB fetch, got %d", len(result))
	}
	if p, ok := result[p1.ID]; !ok {
		t.Error("p1 not found in result from DB fetch")
	} else if p.Name != p1.Name {
		t.Errorf("p1 Name = %q, want %q", p.Name, p1.Name)
	}
	if p, ok := result[p2.ID]; !ok {
		t.Error("p2 not found in result from DB fetch")
	} else if p.Name != p2.Name {
		t.Errorf("p2 Name = %q, want %q", p.Name, p2.Name)
	}

	// Verify both are now cached after the DB fetch
	if !IsCachedByID(p1.ID) {
		t.Error("p1 should be cached after GetByIDs DB fetch")
	}
	if !IsCachedByID(p2.ID) {
		t.Error("p2 should be cached after GetByIDs DB fetch")
	}
}

// TestGetByIDs_PartialCacheInvalidation tests that GetByIDs correctly handles
// a mix of cached and uncached IDs after partial cache invalidation.
func TestGetByIDs_PartialCacheInvalidation(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	// Create two providers
	p1, err := repo.Create(ctx, CreateProviderRequest{
		Name: uniqueName(t), BaseURL: "https://partial1.example.com", APIKey: "sk-partial1",
	}, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err != nil {
		t.Fatalf("Create p1: %v", err)
	}

	p2, err := repo.Create(ctx, CreateProviderRequest{
		Name: uniqueName(t), BaseURL: "https://partial2.example.com", APIKey: "sk-partial2",
	}, []byte("enc"), []byte("nonce"), []byte("salt"))
	if err != nil {
		t.Fatalf("Create p2: %v", err)
	}

	// Pre-populate cache for p1 only
	_, err = repo.Get(ctx, p1.ID)
	if err != nil {
		t.Fatalf("Get p1: %v", err)
	}

	// Invalidate cache for p1 only, so both need DB fetch
	InvalidateProviderCache()

	// Now re-cache p1 by getting it
	_, err = repo.Get(ctx, p1.ID)
	if err != nil {
		t.Fatalf("Get p1 after invalidation: %v", err)
	}

	// p1 is cached, p2 is not. GetByIDs should return both.
	result, err := repo.GetByIDs(ctx, []uuid.UUID{p1.ID, p2.ID})
	if err != nil {
		t.Fatalf("GetByIDs: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 results, got %d", len(result))
	}
	// p2 should now be cached after the DB fetch
	if !IsCachedByID(p2.ID) {
		t.Error("p2 should be cached after GetByIDs fetched it from DB")
	}
}
