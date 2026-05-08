package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// ---------------------------------------------------------------------------
// LoadModelsDev
// ---------------------------------------------------------------------------

func TestLoadModelsDev_Success(t *testing.T) {
	responseJSON := `{
		"openai": {
			"id": "openai",
			"name": "OpenAI",
			"api": "openai",
			"doc": "https://platform.openai.com/docs",
			"models": {
				"gpt-4": {
					"id": "gpt-4",
					"name": "GPT-4",
					"modalities": {
						"input": ["text"],
						"output": ["text"]
					},
					"cost": {
						"input": 0.03,
						"output": 0.06
					},
					"limit": {
						"context": 8192,
						"output": 4096
					}
				}
			}
		}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(responseJSON))
	}))
	defer server.Close()

	// Create a test client that uses the server URL
	client := &http.Client{Transport: &modelsDevTestTransport{url: server.URL}}

	err := LoadModelsDevWithClient(context.Background(), client)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	// Verify cache was loaded
	cache := GetModelsDevCache()
	if cache == nil {
		t.Fatal("expected cache to be loaded")
	}

	// Verify we can look up the model
	spec := cache.Lookup("gpt-4")
	if spec == nil {
		t.Fatal("expected to find gpt-4 in cache")
	}
	if spec.Name != "GPT-4" {
		t.Errorf("expected name 'GPT-4', got %v", spec.Name)
	}
}

// testTransport overrides the URL for models.dev API calls
// Note: This is a simplified version that directly modifies the request
// We use this instead of the one in discovery_main_test.go to avoid conflicts
type modelsDevTestTransport struct {
	url string
}

func (t *modelsDevTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Simple approach: if the request is for models.dev API, redirect to our test server
	if req.URL.String() == modelsDevAPIURL {
		// Create a new request to our test server
		testReq, err := http.NewRequest(req.Method, t.url, req.Body)
		if err != nil {
			return nil, err
		}
		// Copy headers
		for key, values := range req.Header {
			for _, value := range values {
				testReq.Header.Add(key, value)
			}
		}
		return http.DefaultTransport.RoundTrip(testReq)
	}
	return http.DefaultTransport.RoundTrip(req)
}

// ---------------------------------------------------------------------------
// GetModelsDevCache
// ---------------------------------------------------------------------------

func TestGetModelsDevCache_NotLoaded(t *testing.T) {
	// Ensure cache is not loaded
	modelsDevCache.mu.Lock()
	modelsDevCache.loaded = false
	modelsDevCache.byID = nil
	modelsDevCache.mu.Unlock()

	cache := GetModelsDevCache()
	if cache != nil {
		t.Error("expected nil cache when not loaded")
	}
}

func TestGetModelsDevCache_AfterLoad(t *testing.T) {
	responseJSON := `{
		"openai": {
			"id": "openai",
			"name": "OpenAI",
			"api": "openai",
			"doc": "https://platform.openai.com/docs",
			"models": {
				"gpt-4": {
					"id": "gpt-4",
					"name": "GPT-4",
					"modalities": {
						"input": ["text"],
						"output": ["text"]
					},
					"cost": {
						"input": 0.03,
						"output": 0.06
					},
					"limit": {
						"context": 8192,
						"output": 4096
					}
				}
			}
		}
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(responseJSON))
	}))
	defer server.Close()

	client := &http.Client{Transport: &modelsDevTestTransport{url: server.URL}}

	err := LoadModelsDevWithClient(context.Background(), client)
	if err != nil {
		t.Fatalf("failed to load cache: %v", err)
	}

	cache := GetModelsDevCache()
	if cache == nil {
		t.Error("expected cache to be returned after load")
	}
}

// ---------------------------------------------------------------------------
// ModelsDevCache.Lookup
// ---------------------------------------------------------------------------

func TestModelsDevCacheLookup_NotFound(t *testing.T) {
	cache := &ModelsDevCache{}
	cache.mu.Lock()
	cache.byID = make(map[string]*ModelsDevModelSpec)
	cache.loaded = true
	cache.mu.Unlock()

	spec := cache.Lookup("nonexistent-model")
	if spec != nil {
		t.Error("expected nil for nonexistent model")
	}
}

func TestModelsDevCacheLookup_Found(t *testing.T) {
	spec := &ModelsDevModelSpec{
		ID:   "test-model",
		Name: "Test Model",
	}

	cache := &ModelsDevCache{}
	cache.mu.Lock()
	cache.byID = map[string]*ModelsDevModelSpec{
		"test-model": spec,
	}
	cache.loaded = true
	cache.mu.Unlock()

	found := cache.Lookup("test-model")
	if found == nil {
		t.Fatal("expected to find model")
	}
	if found.Name != "Test Model" {
		t.Errorf("expected name 'Test Model', got %v", found.Name)
	}
}

// ---------------------------------------------------------------------------
// ModelsDevCache.LookupFuzzy
// ---------------------------------------------------------------------------

func TestModelsDevCacheLookupFuzzy_ExactMatch(t *testing.T) {
	spec := &ModelsDevModelSpec{
		ID:   "gpt-4",
		Name: "GPT-4",
	}

	cache := &ModelsDevCache{}
	cache.mu.Lock()
	cache.byID = map[string]*ModelsDevModelSpec{
		"gpt-4": spec,
	}
	cache.loaded = true
	cache.mu.Unlock()

	found := cache.LookupFuzzy("gpt-4")
	if found == nil {
		t.Fatal("expected to find exact match")
	}
	if found.Name != "GPT-4" {
		t.Errorf("expected name 'GPT-4', got %v", found.Name)
	}
}

func TestModelsDevCacheLookupFuzzy_DateSuffix(t *testing.T) {
	spec := &ModelsDevModelSpec{
		ID:   "gpt-4",
		Name: "GPT-4",
	}

	cache := &ModelsDevCache{}
	cache.mu.Lock()
	cache.byID = map[string]*ModelsDevModelSpec{
		"gpt-4": spec,
	}
	cache.loaded = true
	cache.mu.Unlock()

	// Should find gpt-4 when looking for gpt-4-2024-08-06
	found := cache.LookupFuzzy("gpt-4-2024-08-06")
	if found == nil {
		t.Fatal("expected to find model with date suffix")
	}
	if found.Name != "GPT-4" {
		t.Errorf("expected name 'GPT-4', got %v", found.Name)
	}
}

// ---------------------------------------------------------------------------
// ModelsDevCache.EnrichModel
// ---------------------------------------------------------------------------

func TestModelsDevCacheEnrichModel_NilCache(t *testing.T) {
	var cache *ModelsDevCache

	m := &model.Model{
		ModelID:     "test-model",
		DisplayName: "Test Model",
	}

	enriched := cache.EnrichModel(m)
	if enriched {
		t.Error("expected no enrichment with nil cache")
	}
}

func TestModelsDevCacheEnrichModel_EmptyModel(t *testing.T) {
	spec := &ModelsDevModelSpec{
		ID:   "test-model",
		Name: "Enriched Model",
		Limit: ModelsDevLimit{
			Context: 8192,
			Output:  4096,
		},
		Cost: ModelsDevCost{
			Input:  0.03,
			Output: 0.06,
		},
		Modalities: ModelsDevModalities{
			Input:  []string{"text"},
			Output: []string{"text"},
		},
	}

	cache := &ModelsDevCache{}
	cache.mu.Lock()
	cache.byID = map[string]*ModelsDevModelSpec{
		"test-model": spec,
	}
	cache.loaded = true
	cache.mu.Unlock()

	// Test with empty display name to trigger enrichment
	m := &model.Model{
		ModelID:      "test-model",
		DisplayName:  "",
		Capabilities: "{}",
	}

	enriched := cache.EnrichModel(m)
	if !enriched {
		t.Error("expected enrichment to occur")
	}
	if m.DisplayName != "Enriched Model" {
		t.Errorf("expected enriched display name, got %v", m.DisplayName)
	}
	if m.ContextLength == nil || *m.ContextLength != 8192 {
		t.Errorf("expected context length 8192, got %v", m.ContextLength)
	}
}

// ---------------------------------------------------------------------------
// ModelsDevCache.EnrichModels
// ---------------------------------------------------------------------------

func TestModelsDevCacheEnrichModels(t *testing.T) {
	spec := &ModelsDevModelSpec{
		ID:   "model-1",
		Name: "Model 1",
		Limit: ModelsDevLimit{
			Context: 4096,
		},
	}

	cache := &ModelsDevCache{}
	cache.mu.Lock()
	cache.byID = map[string]*ModelsDevModelSpec{
		"model-1": spec,
	}
	cache.loaded = true
	cache.mu.Unlock()

	models := []*model.Model{
		{ModelID: "model-1", DisplayName: "Model 1", Capabilities: "{}"},
		{ModelID: "model-2", DisplayName: "Model 2", Capabilities: "{}"},
	}

	enrichedCount := cache.EnrichModels(models)
	if enrichedCount != 1 {
		t.Errorf("expected 1 enriched model, got %d", enrichedCount)
	}
}
