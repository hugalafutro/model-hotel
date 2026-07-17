package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
		return
	}

	// Verify we can look up the model
	spec := cache.Lookup("gpt-4")
	if spec == nil {
		t.Fatal("expected to find gpt-4 in cache")
		return
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
		//nolint:gosec // test-only: test server URL
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

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
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
		return
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
		return
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
		return
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

// ---------------------------------------------------------------------------
// LoadModelsDev error paths
// ---------------------------------------------------------------------------

func TestLoadModelsDevWithClient_Non200Status(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("not found"))
	}))
	defer server.Close()

	client := &http.Client{Transport: &modelsDevTestTransport{url: server.URL}}

	err := LoadModelsDevWithClient(context.Background(), client)

	if err == nil {
		t.Fatal("expected error for non-200 status")
		return
	}
	if !strings.Contains(err.Error(), "unexpected status 404") {
		t.Errorf("expected 'unexpected status 404' error, got: %v", err)
	}
}

func TestLoadModelsDevWithClient_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("not valid json {{{"))
	}))
	defer server.Close()

	client := &http.Client{Transport: &modelsDevTestTransport{url: server.URL}}

	err := LoadModelsDevWithClient(context.Background(), client)

	if err == nil {
		t.Fatal("expected error for invalid JSON")
		return
	}
	if !strings.Contains(err.Error(), "failed to parse JSON") {
		t.Errorf("expected 'failed to parse JSON' error, got: %v", err)
	}
}

func TestLoadModelsDevWithClient_EmptyBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	}))
	defer server.Close()

	client := &http.Client{Transport: &modelsDevTestTransport{url: server.URL}}

	err := LoadModelsDevWithClient(context.Background(), client)

	// Empty object is valid JSON, should succeed but with empty cache
	if err != nil {
		t.Fatalf("expected no error for empty body, got: %v", err)
	}

	cache := GetModelsDevCache()
	if cache == nil {
		t.Fatal("expected cache to be loaded")
		return
	}

	// Verify cache is empty
	spec := cache.Lookup("any-model")
	if spec != nil {
		t.Error("expected nil for any model in empty cache")
	}
}

func TestLoadModelsDevWithClient_ContextCancelled(t *testing.T) {
	// Test that context cancellation is respected
	// We use a direct HTTP client without the custom transport to ensure
	// context cancellation propagates correctly
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// Create a client that will fail because context is cancelled
	client := &http.Client{}

	err := LoadModelsDevWithClient(ctx, client)

	if err == nil {
		t.Fatal("expected error for cancelled context")
		return
	}
	if !strings.Contains(err.Error(), "context canceled") && !strings.Contains(err.Error(), "context cancelled") {
		t.Errorf("expected context cancellation error, got: %v", err)
	}
}

func TestLoadModelsDevWithClient_NilProvider(t *testing.T) {
	// Test that nil providers are skipped
	responseJSON := `{"provider1": null, "provider2": {"id": "p2", "models": {"m1": {"id": "m1", "name": "M1", "modalities": {"input": ["text"], "output": ["text"]}, "cost": {"input": 1, "output": 2}, "limit": {"context": 100, "output": 50}, "attachment": false, "reasoning": false, "tool_call": false, "open_weights": false}}}}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(responseJSON))
	}))
	defer server.Close()

	client := &http.Client{Transport: &modelsDevTestTransport{url: server.URL}}

	err := LoadModelsDevWithClient(context.Background(), client)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	cache := GetModelsDevCache()
	spec := cache.Lookup("m1")
	if spec == nil {
		t.Fatal("expected to find m1")
		return
	}
}

func TestLoadModelsDevWithClient_NilModel(t *testing.T) {
	// Test that nil models are skipped
	responseJSON := `{"provider1": {"id": "p1", "models": {"m1": null, "m2": {"id": "m2", "name": "M2", "modalities": {"input": ["text"], "output": ["text"]}, "cost": {"input": 1, "output": 2}, "limit": {"context": 100, "output": 50}, "attachment": false, "reasoning": false, "tool_call": false, "open_weights": false}}}}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(responseJSON))
	}))
	defer server.Close()

	client := &http.Client{Transport: &modelsDevTestTransport{url: server.URL}}

	err := LoadModelsDevWithClient(context.Background(), client)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}

	cache := GetModelsDevCache()
	if cache.Lookup("m1") != nil {
		t.Error("expected nil for null model m1")
	}
	if cache.Lookup("m2") == nil {
		t.Error("expected to find m2")
	}
}

func TestLoadModelsDevWithClient_FirstProviderWins(t *testing.T) {
	// Test that when same model ID appears in multiple providers, only one is kept
	// Note: Go map iteration order is non-deterministic, so we can't guarantee
	// which provider "wins", but we verify that exactly one entry exists
	responseJSON := `{
		"provider1": {
			"id": "p1",
			"models": {
				"shared-model": {"id": "shared-model", "name": "From Provider 1", "modalities": {"input": ["text"], "output": ["text"]}, "cost": {"input": 1, "output": 2}, "limit": {"context": 100, "output": 50}, "attachment": false, "reasoning": false, "tool_call": false, "open_weights": false}
			}
		},
		"provider2": {
			"id": "p2",
			"models": {
				"shared-model": {"id": "shared-model", "name": "From Provider 2", "modalities": {"input": ["text"], "output": ["text"]}, "cost": {"input": 1, "output": 2}, "limit": {"context": 100, "output": 50}, "attachment": false, "reasoning": false, "tool_call": false, "open_weights": false}
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
		t.Fatalf("expected no error, got: %v", err)
	}

	cache := GetModelsDevCache()
	spec := cache.Lookup("shared-model")
	if spec == nil {
		t.Fatal("expected to find shared-model")
		return
	}
	// One provider should win (deterministic within a single run due to map iteration)
	if spec.Name != "From Provider 1" && spec.Name != "From Provider 2" {
		t.Errorf("expected 'From Provider 1' or 'From Provider 2', got '%s'", spec.Name)
	}
}

func TestModelsDevCacheLookup_NilCache(t *testing.T) {
	var cache *ModelsDevCache

	spec := cache.Lookup("any-model")
	if spec != nil {
		t.Error("expected nil from nil cache")
	}
}

func TestModelsDevCacheLookupFuzzy_NilCache(t *testing.T) {
	var cache *ModelsDevCache

	spec := cache.LookupFuzzy("any-model")
	if spec != nil {
		t.Error("expected nil from nil cache")
	}
}

func TestModelsDevCacheLookupFuzzy_PrefixMatch(t *testing.T) {
	spec := &ModelsDevModelSpec{
		ID:   "claude-3-5-sonnet",
		Name: "Claude 3.5 Sonnet",
	}

	cache := &ModelsDevCache{}
	cache.mu.Lock()
	cache.byID = map[string]*ModelsDevModelSpec{
		"claude-3-5-sonnet": spec,
	}
	cache.loaded = true
	cache.mu.Unlock()

	// Should find claude-3-5-sonnet when looking for claude-3-5-sonnet-20241022
	found := cache.LookupFuzzy("claude-3-5-sonnet-20241022")
	if found == nil {
		t.Fatal("expected to find model with prefix match")
		return
	}
	if found.Name != "Claude 3.5 Sonnet" {
		t.Errorf("expected name 'Claude 3.5 Sonnet', got %v", found.Name)
	}
}

func TestModelsDevCacheLookupFuzzy_LongestPrefixWins(t *testing.T) {
	shortSpec := &ModelsDevModelSpec{
		ID:   "gpt-4",
		Name: "GPT-4",
	}
	longSpec := &ModelsDevModelSpec{
		ID:   "gpt-4-turbo",
		Name: "GPT-4 Turbo",
	}

	cache := &ModelsDevCache{}
	cache.mu.Lock()
	cache.byID = map[string]*ModelsDevModelSpec{
		"gpt-4":       shortSpec,
		"gpt-4-turbo": longSpec,
	}
	cache.loaded = true
	cache.mu.Unlock()

	// Should find gpt-4-turbo (longest prefix) when looking for gpt-4-turbo-2024-01-01
	found := cache.LookupFuzzy("gpt-4-turbo-2024-01-01")
	if found == nil {
		t.Fatal("expected to find model with longest prefix match")
		return
	}
	if found.Name != "GPT-4 Turbo" {
		t.Errorf("expected name 'GPT-4 Turbo', got %v", found.Name)
	}
}

func TestModelsDevCacheLookupFuzzy_VersionSuffix(t *testing.T) {
	spec := &ModelsDevModelSpec{
		ID:   "claude-sonnet-4",
		Name: "Claude Sonnet 4",
	}

	cache := &ModelsDevCache{}
	cache.mu.Lock()
	cache.byID = map[string]*ModelsDevModelSpec{
		"claude-sonnet-4": spec,
	}
	cache.loaded = true
	cache.mu.Unlock()

	// Should find claude-sonnet-4 when looking for claude-sonnet-4-20250514
	found := cache.LookupFuzzy("claude-sonnet-4-20250514")
	if found == nil {
		t.Fatal("expected to find model with version suffix stripped")
		return
	}
	if found.Name != "Claude Sonnet 4" {
		t.Errorf("expected name 'Claude Sonnet 4', got %v", found.Name)
	}
}

func TestModelsDevCacheLookupFuzzy_PrefixMatchRejectsVariant(t *testing.T) {
	// Regression test: "gpt-5-search-api" should NOT match "gpt-5"
	// because "search-api" is a model variant, not a date/version suffix.
	gpt5Spec := &ModelsDevModelSpec{
		ID:   "gpt-5",
		Name: "GPT 5",
	}

	cache := &ModelsDevCache{}
	cache.mu.Lock()
	cache.byID = map[string]*ModelsDevModelSpec{
		"gpt-5": gpt5Spec,
	}
	cache.loaded = true
	cache.mu.Unlock()

	tests := []struct {
		name     string
		modelID  string
		wantNil  bool
		wantName string // expected Name if not nil
	}{
		{
			name:     "variant_suffix_rejected",
			modelID:  "gpt-5-search-api",
			wantNil:  true,
			wantName: "",
		},
		{
			name:     "variant_with_date_rejected",
			modelID:  "gpt-5-search-api-2025-10-14",
			wantNil:  true,
			wantName: "",
		},
		{
			name:     "date_suffix_accepted",
			modelID:  "gpt-5-2025-08-07",
			wantNil:  false,
			wantName: "GPT 5",
		},
		{
			name:     "compact_date_suffix_accepted",
			modelID:  "gpt-5-20250807",
			wantNil:  false,
			wantName: "GPT 5",
		},
		{
			name:     "year_suffix_accepted",
			modelID:  "gpt-5-2025",
			wantNil:  false,
			wantName: "GPT 5",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			found := cache.LookupFuzzy(tt.modelID)
			if tt.wantNil {
				if found != nil {
					t.Errorf("LookupFuzzy(%q) = %q, want nil (should not match variant)", tt.modelID, found.Name)
				}
			} else {
				if found == nil {
					t.Fatalf("LookupFuzzy(%q) = nil, want non-nil", tt.modelID)
					return
				}
				if found.Name != tt.wantName {
					t.Errorf("LookupFuzzy(%q) = %q, want %q", tt.modelID, found.Name, tt.wantName)
				}
			}
		})
	}
}

func TestLooksLikeDateOrVersion(t *testing.T) {
	tests := []struct {
		input string
		want  bool
	}{
		// Date patterns - should match
		{"2024-08-06", true},
		{"20240806", true},
		{"2025-10-14", true},
		{"2025", true},    // year-only
		{"2024-08", true}, // year-month

		// Version-like patterns - should match
		{"20250514", true}, // compact date as version

		// Model variant patterns - should NOT match
		{"search-api", false},
		{"mini", false},
		{"search-api-2025-10-14", false},
		{"pro", false},
		{"chat-latest", false},
		{"nano", false},

		// Edge cases
		{"123", false},      // too short (< 4 digits)
		{"abc", false},      // not numeric
		{"2024-ab", false},  // month not numeric
		{"2024-0", true},    // 1-digit month (valid short form)
		{"2024-12", true},   // 2-digit month
		{"2024-999", false}, // 3-digit "month" rejected (too long)
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := looksLikeDateOrVersion(tt.input)
			if got != tt.want {
				t.Errorf("looksLikeDateOrVersion(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestModelsDevCacheEnrichModel_NotFound(t *testing.T) {
	cache := &ModelsDevCache{}
	cache.mu.Lock()
	cache.byID = make(map[string]*ModelsDevModelSpec)
	cache.loaded = true
	cache.mu.Unlock()

	m := &model.Model{
		ModelID:     "unknown-model",
		DisplayName: "",
	}

	enriched := cache.EnrichModel(m)
	if enriched {
		t.Error("expected no enrichment for unknown model")
	}
}

func TestModelsDevCacheEnrichModel_ExistingDataNotOverwritten(t *testing.T) {
	spec := &ModelsDevModelSpec{
		ID:   "test-model",
		Name: "Enriched Name",
		Limit: ModelsDevLimit{
			Context: 8192,
		},
		Cost: ModelsDevCost{
			Input:  0.03,
			Output: 0.06,
		},
	}

	cache := &ModelsDevCache{}
	cache.mu.Lock()
	cache.byID = map[string]*ModelsDevModelSpec{
		"test-model": spec,
	}
	cache.loaded = true
	cache.mu.Unlock()

	// Model with existing data should not be overwritten
	existingName := "Existing Name"
	existingContext := 4096
	m := &model.Model{
		ModelID:       "test-model",
		DisplayName:   existingName,
		ContextLength: &existingContext,
		Capabilities:  "{}",
	}

	_ = cache.EnrichModel(m)
	// Should still enrich other fields but not name or context
	if m.DisplayName != existingName {
		t.Errorf("expected display name to remain '%s', got '%s'", existingName, m.DisplayName)
	}
	if *m.ContextLength != existingContext {
		t.Errorf("expected context length to remain %d, got %d", existingContext, *m.ContextLength)
	}
}

func TestModelsDevCacheEnrichModel_CacheReadPrice(t *testing.T) {
	cacheReadPrice := 0.0075
	spec := &ModelsDevModelSpec{
		ID:   "test-model",
		Name: "Test Model",
		Cost: ModelsDevCost{
			Input:     0.03,
			Output:    0.06,
			CacheRead: &cacheReadPrice,
		},
		Modalities: ModelsDevModalities{
			Input:  []string{"text"},
			Output: []string{"text"},
		},
		Limit:      ModelsDevLimit{Context: 8192, Output: 4096},
		Attachment: false,
		Reasoning:  false,
		ToolCall:   false,
	}

	cache := &ModelsDevCache{}
	cache.mu.Lock()
	cache.byID = map[string]*ModelsDevModelSpec{
		"test-model": spec,
	}
	cache.loaded = true
	cache.mu.Unlock()

	m := &model.Model{
		ModelID:      "test-model",
		DisplayName:  "",
		Capabilities: "{}",
		Modality:     "text",
	}

	cache.EnrichModel(m)

	if m.InputPricePerMillionCacheHit == nil || *m.InputPricePerMillionCacheHit != cacheReadPrice {
		t.Errorf("expected cache read price %f, got %v", cacheReadPrice, m.InputPricePerMillionCacheHit)
	}
}

func TestModelsDevCacheEnrichModel_Capabilities(t *testing.T) {
	structuredOutput := true
	spec := &ModelsDevModelSpec{
		ID:               "test-model",
		Name:             "Test Model",
		Reasoning:        true,
		ToolCall:         true,
		StructuredOutput: &structuredOutput,
		Attachment:       true,
		Modalities: ModelsDevModalities{
			Input:  []string{"text"},
			Output: []string{"text"},
		},
		Cost:  ModelsDevCost{Input: 1, Output: 2},
		Limit: ModelsDevLimit{Context: 100, Output: 50},
	}

	cache := &ModelsDevCache{}
	cache.mu.Lock()
	cache.byID = map[string]*ModelsDevModelSpec{
		"test-model": spec,
	}
	cache.loaded = true
	cache.mu.Unlock()

	m := &model.Model{
		ModelID:      "test-model",
		DisplayName:  "",
		Capabilities: "{}",
		Modality:     "text",
	}

	cache.EnrichModel(m)

	var caps model.Capability
	json.Unmarshal([]byte(m.Capabilities), &caps)

	if !caps.Reasoning {
		t.Error("expected reasoning capability to be set")
	}
	if !caps.ToolCalling {
		t.Error("expected tool calling capability to be set")
	}
	if !caps.StructuredOutput {
		t.Error("expected structured output capability to be set")
	}
	if !caps.Vision {
		t.Error("expected vision capability to be set (from attachment)")
	}
}

func TestModelsDevCacheEnrichModels_NilCache(t *testing.T) {
	var cache *ModelsDevCache

	models := []*model.Model{
		{ModelID: "model-1"},
	}

	count := cache.EnrichModels(models)
	if count != 0 {
		t.Errorf("expected 0 enriched models from nil cache, got %d", count)
	}
}

func TestModelsDevInterleavedUnmarshalJSON_InvalidJSON(t *testing.T) {
	var inter ModelsDevInterleaved
	err := json.Unmarshal([]byte("invalid"), &inter)

	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestModelsDevCacheEnrichModel_InvalidCapabilitiesJSON(t *testing.T) {
	spec := &ModelsDevModelSpec{
		ID:   "test-model",
		Name: "Test Model",
		Modalities: ModelsDevModalities{
			Input:  []string{"text"},
			Output: []string{"text"},
		},
		Cost:  ModelsDevCost{Input: 1, Output: 2},
		Limit: ModelsDevLimit{Context: 100, Output: 50},
	}

	cache := &ModelsDevCache{}
	cache.mu.Lock()
	cache.byID = map[string]*ModelsDevModelSpec{
		"test-model": spec,
	}
	cache.loaded = true
	cache.mu.Unlock()

	m := &model.Model{
		ModelID:      "test-model",
		DisplayName:  "",
		Capabilities: "invalid json {{{",
		Modality:     "text",
	}

	// Should not panic, should handle gracefully
	enriched := cache.EnrichModel(m)
	if !enriched {
		t.Error("expected enrichment to occur despite invalid capabilities JSON")
	}
}

func TestModelsDevCacheEnrichModel_ModalityEnrichment(t *testing.T) {
	spec := &ModelsDevModelSpec{
		ID:   "vision-model",
		Name: "Vision Model",
		Modalities: ModelsDevModalities{
			Input:  []string{"text", "image"},
			Output: []string{"text"},
		},
		Cost:  ModelsDevCost{Input: 1, Output: 2},
		Limit: ModelsDevLimit{Context: 100, Output: 50},
	}

	cache := &ModelsDevCache{}
	cache.mu.Lock()
	cache.byID = map[string]*ModelsDevModelSpec{
		"vision-model": spec,
	}
	cache.loaded = true
	cache.mu.Unlock()

	m := &model.Model{
		ModelID:          "vision-model",
		DisplayName:      "",
		Capabilities:     "{}",
		Modality:         "", // Empty to trigger enrichment
		InputModalities:  "",
		OutputModalities: "",
	}

	cache.EnrichModel(m)

	// Enrichment fills the arrays only; the modality class is derived later
	// by NormalizeModelClassification.
	if m.Modality != "" {
		t.Errorf("expected modality left untouched by enrichment, got '%s'", m.Modality)
	}
	if m.InputModalities == "" || m.InputModalities == "[]" {
		t.Error("expected input modalities to be set")
	}
	if m.OutputModalities == "" || m.OutputModalities == "[]" {
		t.Error("expected output modalities to be set")
	}

	NormalizeModelClassification(m)
	if m.Modality != "chat" {
		t.Errorf("expected derived class 'chat', got '%s'", m.Modality)
	}
}

func TestModelsDevCacheEnrichModels_MultipleModels(t *testing.T) {
	spec := &ModelsDevModelSpec{
		ID:   "model-1",
		Name: "Model 1",
		Limit: ModelsDevLimit{
			Context: 4096,
		},
		Modalities: ModelsDevModalities{Input: []string{"text"}, Output: []string{"text"}},
		Cost:       ModelsDevCost{Input: 1, Output: 2},
	}

	cache := &ModelsDevCache{}
	cache.mu.Lock()
	cache.byID = map[string]*ModelsDevModelSpec{
		"model-1": spec,
	}
	cache.loaded = true
	cache.mu.Unlock()

	models := []*model.Model{
		{ModelID: "model-1", DisplayName: "", Capabilities: "{}"},
		{ModelID: "model-2", DisplayName: "", Capabilities: "{}"},
		{ModelID: "model-1", DisplayName: "", Capabilities: "{}"},
	}

	enrichedCount := cache.EnrichModels(models)
	if enrichedCount != 2 {
		t.Errorf("expected 2 enriched models, got %d", enrichedCount)
	}
}

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
