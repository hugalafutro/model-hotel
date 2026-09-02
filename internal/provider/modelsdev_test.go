package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/model"
)

func TestIsNumeric(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"digits", "12345", true},
		{"zero", "0", true},
		{"empty", "", true}, // no non-digit chars, loop doesn't fail
		{"with letters", "123abc", false},
		{"with space", "12 34", false},
		{"negative", "-5", false},
		{"float", "3.14", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNumeric(tt.input); got != tt.want {
				t.Errorf("isNumeric(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestLooksLikeDate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid_date_with_hyphens", "2024-08-06", true},
		{"valid_date_no_hyphens", "20240806", true},
		{"too_short", "2024-08", false},
		{"not_numeric", "abcdefgh", false},
		{"empty", "", false},
		{"still_numeric", "2024-13-45", true},
		{"seven_chars", "2024080", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := looksLikeDate(tt.input); got != tt.want {
				t.Errorf("looksLikeDate(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// ===========================================================================
// Tests moved from modelsdev_coverage_test.go
// ===========================================================================

// ---------------------------------------------------------------------------
// LoadModelsDev
// ---------------------------------------------------------------------------

func TestLoadModelsDev_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Use a mock transport so we don't hit the real network,
	// even though the cancelled context should prevent any request.
	client := &http.Client{Transport: &mockTransport{roundTripFunc: func(_ *http.Request) (*http.Response, error) {
		return nil, context.Canceled
	}}}

	err := LoadModelsDevWithClient(ctx, client)
	if err == nil {
		t.Error("expected error from LoadModelsDevWithClient with canceled context")
	}
}

// ---------------------------------------------------------------------------
// ResetModelsDevCache
// ---------------------------------------------------------------------------

func TestResetModelsDevCache(t *testing.T) {
	// Set up a mock server with valid models.dev data
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api.json" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"test-provider":{"id":"test","name":"Test","api":"openai","models":{"test-model":{"id":"test-model","name":"Test Model","attachment":false,"reasoning":false,"tool_call":false,"modalities":{"input":["text"],"output":["text"]},"open_weights":false,"cost":{"input":0,"output":0},"limit":{"context":1000,"output":100}}}}}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer mockServer.Close()

	// Redirect modelsDevAPIURL to mock server via URL rewriting
	baseTransport := mockServer.Client().Transport
	client := mockServer.Client()
	client.Transport = &mockTransport{roundTripFunc: func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = mockServer.Listener.Addr().String()
		req.URL.Path = "/api.json"
		return baseTransport.RoundTrip(req)
	}}

	// Load the cache with data
	ctx := context.Background()
	err := LoadModelsDevWithClient(ctx, client)
	if err != nil {
		t.Fatalf("failed to load models.dev cache: %v", err)
	}

	// Verify cache has data
	cache := GetModelsDevCache()
	if cache == nil {
		t.Fatal("expected cache to be loaded before reset")
		return
	}

	cache.mu.RLock()
	hasData := cache.loaded && len(cache.byID) > 0
	cache.mu.RUnlock()

	if !hasData {
		t.Fatal("expected cache to have data before reset")
	}

	// Reset the cache
	ResetModelsDevCache()

	// Verify cache is now nil
	cache = GetModelsDevCache()
	if cache != nil {
		t.Error("expected cache to be nil after reset")
	}
}

// ---------------------------------------------------------------------------
// ModelsDevInterleaved.UnmarshalJSON
// ---------------------------------------------------------------------------

func TestModelsDevInterleaved_UnmarshalJSON_InvalidJSON(t *testing.T) {
	var i ModelsDevInterleaved
	err := i.UnmarshalJSON([]byte("not valid json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// Helper to set up cache with specific test data
func setupCacheWithModels(t *testing.T, models map[string]*ModelsDevModelSpec) {
	t.Helper()
	t.Cleanup(func() { ResetModelsDevCache() })
	modelsDevCache.mu.Lock()
	defer modelsDevCache.mu.Unlock()
	modelsDevCache.byID = models
	modelsDevCache.loaded = true
}

// ---------------------------------------------------------------------------
// LookupFuzzy - uncovered paths
// ---------------------------------------------------------------------------

func TestLookupFuzzy_EmptyKeyAndID(t *testing.T) {
	// Set up cache with a model that has empty key (edge case)
	// This tests the path at line 279: if key == "" in the prefix matching loop
	// The load() function skips entries where both map key and spec.ID are empty,
	// but we can manually insert such an entry to test the lookup logic.
	setupCacheWithModels(t, map[string]*ModelsDevModelSpec{
		"": {ID: "", Name: "Empty Key Model"}, // Empty key
	})

	cache := GetModelsDevCache()
	if cache == nil {
		t.Fatal("expected cache to be loaded")
		return
	}

	// When modelID is non-empty and key is empty, HasPrefix("", "anything") = true
	// but remainder would be the full modelID, not empty.
	// When modelID is "" and key is "", HasPrefix("", "") = true and remainder = ""
	// This tests line 286: if remainder == ""
	result := cache.LookupFuzzy("")
	if result == nil {
		t.Error("expected to find model with empty key when searching for empty modelID")
	} else if result.Name != "Empty Key Model" {
		t.Errorf("expected name 'Empty Key Model', got %q", result.Name)
	}
}

func TestLookupFuzzy_ExactPrefixMatchNoRemainder(t *testing.T) {
	// Set up cache with a model
	setupCacheWithModels(t, map[string]*ModelsDevModelSpec{
		"gpt-4-turbo": {ID: "gpt-4-turbo", Name: "GPT-4 Turbo"},
	})

	cache := GetModelsDevCache()
	if cache == nil {
		t.Fatal("expected cache to be loaded")
		return
	}

	// Exact match - this should be caught by step 1 (Lookup), but step 4
	// also handles the case where remainder == ""
	result := cache.LookupFuzzy("gpt-4-turbo")
	if result == nil {
		t.Error("expected to find gpt-4-turbo")
	} else if result.Name != "GPT-4 Turbo" {
		t.Errorf("expected name 'GPT-4 Turbo', got %q", result.Name)
	}
}

// ---------------------------------------------------------------------------
// looksLikeDateOrVersion - three-segment date pattern
// ---------------------------------------------------------------------------

func TestLooksLikeDateOrVersion_ThreeSegmentDate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid_date_segments", "2024-8-6", true},
		{"valid_date_segments_padded", "2024-08-06", true},
		{"invalid_non_numeric_first", "v2024-8-6", false},
		// Note: "2024-aug-6" returns true because the function only checks
		// that parts[0] is numeric and 4 digits (line 492), not all parts.
		// This is existing behavior - the test documents it.
		{"non_numeric_middle_documented", "2024-aug-6", true},
		{"invalid_first_not_4_digits", "24-8-6", false},
		{"invalid_first_5_digits", "20245-8-6", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := looksLikeDateOrVersion(tt.input); got != tt.want {
				t.Errorf("looksLikeDateOrVersion(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Additional Tests for ModelsDevInterleaved
// ---------------------------------------------------------------------------

func TestModelsDevInterleaved_UnmarshalJSON_Bool(t *testing.T) {
	var i ModelsDevInterleaved
	err := i.UnmarshalJSON([]byte("true"))
	if err != nil {
		t.Fatalf("unexpected error unmarshaling bool: %v", err)
	}
	if !i.Bool {
		t.Error("expected Bool to be true")
	}
	if i.Field != "" {
		t.Errorf("expected Field to be empty, got %q", i.Field)
	}
}

func TestModelsDevInterleaved_UnmarshalJSON_Object(t *testing.T) {
	var i ModelsDevInterleaved
	err := i.UnmarshalJSON([]byte(`{"field":"test-field"}`))
	if err != nil {
		t.Fatalf("unexpected error unmarshaling object: %v", err)
	}
	if !i.Bool {
		t.Error("expected Bool to be true for object form")
	}
	if i.Field != "test-field" {
		t.Errorf("expected Field to be 'test-field', got %q", i.Field)
	}
}

// ---------------------------------------------------------------------------
// EnrichModel edge cases
// ---------------------------------------------------------------------------

func TestEnrichModel_NilCache(t *testing.T) {
	var cache *ModelsDevCache
	m := &model.Model{ModelID: "test-model"}
	result := cache.EnrichModel(m, "")
	if result {
		t.Error("expected false for nil cache")
	}
}

func TestEnrichModel_ModelNotFound(t *testing.T) {
	setupCacheWithModels(t, map[string]*ModelsDevModelSpec{
		"other-model": {ID: "other-model", Name: "Other Model"},
	})

	cache := GetModelsDevCache()
	if cache == nil {
		t.Fatal("expected cache to be loaded")
		return
	}

	m := &model.Model{ModelID: "nonexistent-model"}
	result := cache.EnrichModel(m, "")
	if result {
		t.Error("expected false when model not found in cache")
	}
}

func TestEnrichModel_FallsBackToNameForAliasedDeployments(t *testing.T) {
	// Azure deployments are invoked by user-chosen alias (ModelID) while the
	// underlying base-model name lands in Name; when the alias misses the
	// catalog, enrichment must retry with the base-model name.
	setupCacheWithModels(t, map[string]*ModelsDevModelSpec{
		"gpt-4.1-mini": {
			ID:    "gpt-4.1-mini",
			Name:  "GPT-4.1 mini",
			Limit: ModelsDevLimit{Context: 1047576},
		},
	})

	cache := GetModelsDevCache()
	if cache == nil {
		t.Fatal("expected cache to be loaded")
		return
	}

	m := &model.Model{ModelID: "my-fast-gpt", Name: "gpt-4.1-mini"}
	if !cache.EnrichModel(m, "") {
		t.Fatal("expected enrichment via Name fallback")
	}
	if m.ContextLength == nil || *m.ContextLength != 1047576 {
		t.Errorf("ContextLength = %v, want 1047576", m.ContextLength)
	}
	// The alias stays the invokable ID.
	if m.ModelID != "my-fast-gpt" {
		t.Errorf("ModelID = %q, want my-fast-gpt", m.ModelID)
	}
}

// models.dev lists structured output for Google's image-output models, as
// Google's own docs do, and the API refuses JSON mode on every one of them
// (google-gemini/cookbook#1028). Discovery leaves the flag off; the OR-merge
// must not put it back on any provider type that reaches Google's own route.
// A text model keeps taking the flag from the catalog, and so does the same
// image model behind an aggregator, whose claim is its own.
func TestEnrichModel_GoogleImageModelsKeepJSONModeOff(t *testing.T) {
	yes := true
	specs := map[string]*ModelsDevModelSpec{
		"gemini-2.5-flash-image":  {ID: "gemini-2.5-flash-image", StructuredOutput: &yes, ToolCall: true},
		"nano-banana-pro-preview": {ID: "nano-banana-pro-preview", StructuredOutput: &yes},
		"gemini-2.5-flash":        {ID: "gemini-2.5-flash", StructuredOutput: &yes},
	}
	setupCacheWithModels(t, specs)
	// Both Google types look the catalog up under their own provider index
	// only (Exclusive), so the flat index alone finds nothing for them.
	modelsDevCache.mu.Lock()
	modelsDevCache.byProvider = map[string]map[string]*ModelsDevModelSpec{"google": specs, "google-vertex": specs}
	modelsDevCache.mu.Unlock()
	cache := GetModelsDevCache()
	if cache == nil {
		t.Fatal("expected cache to be loaded")
		return
	}
	capsOf := func(t *testing.T, m *model.Model) model.Capability {
		t.Helper()
		var caps model.Capability
		if err := json.Unmarshal([]byte(m.Capabilities), &caps); err != nil {
			t.Fatalf("capabilities %q: %v", m.Capabilities, err)
		}
		return caps
	}
	for _, tc := range []struct {
		modelID, providerType string
		wantStructured        bool
	}{
		{"gemini-2.5-flash-image", "google", false},
		{"gemini-2.5-flash-image", "vertex-express", false},
		{"gemini-2.5-flash-image", "opencode-zen", false},
		{"nano-banana-pro-preview", "google", false},
		{"gemini-2.5-flash", "google", true},
		{"gemini-2.5-flash-image", "openrouter", true},
	} {
		m := &model.Model{ModelID: tc.modelID, Capabilities: `{"vision":true}`}
		cache.EnrichModel(m, tc.providerType)
		caps := capsOf(t, m)
		if caps.StructuredOutput != tc.wantStructured {
			t.Errorf("%s via %s: structured_output = %v, want %v (caps %s)", tc.modelID, tc.providerType, caps.StructuredOutput, tc.wantStructured, m.Capabilities)
		}
		if !caps.Vision {
			t.Errorf("%s via %s: the discovered vision flag was lost", tc.modelID, tc.providerType)
		}
	}
	m := &model.Model{ModelID: "gemini-2.5-flash-image"}
	cache.EnrichModel(m, "google")
	if caps := capsOf(t, m); !caps.ToolCalling || caps.StructuredOutput {
		t.Errorf("only structured output is refused; caps = %s", m.Capabilities)
	}
}

// ---------------------------------------------------------------------------
// EnrichModels edge cases
// ---------------------------------------------------------------------------

func TestEnrichModels_NilCache(t *testing.T) {
	var cache *ModelsDevCache
	models := []*model.Model{{ModelID: "test"}}
	count := cache.EnrichModels(models, "")
	if count != 0 {
		t.Errorf("expected 0 for nil cache, got %d", count)
	}
}

func TestEnrichModels_EmptyList(t *testing.T) {
	setupCacheWithModels(t, map[string]*ModelsDevModelSpec{})

	cache := GetModelsDevCache()
	if cache == nil {
		t.Fatal("expected cache to be loaded")
		return
	}

	models := []*model.Model{}
	count := cache.EnrichModels(models, "")
	if count != 0 {
		t.Errorf("expected 0 for empty model list, got %d", count)
	}
}

// ---------------------------------------------------------------------------
// Lookup edge cases
// ---------------------------------------------------------------------------

func TestLookup_NilCache(t *testing.T) {
	var cache *ModelsDevCache
	result := cache.Lookup("test-model")
	if result != nil {
		t.Error("expected nil for nil cache")
	}
}

// ---------------------------------------------------------------------------
// LookupFuzzy edge cases
// ---------------------------------------------------------------------------

func TestLookupFuzzy_NilCache(t *testing.T) {
	var cache *ModelsDevCache
	result := cache.LookupFuzzy("test-model")
	if result != nil {
		t.Error("expected nil for nil cache")
	}
}

func TestLookupFuzzy_DateSuffixVariants(t *testing.T) {
	// Set up cache with base model names
	setupCacheWithModels(t, map[string]*ModelsDevModelSpec{
		"claude-3-5-sonnet": {ID: "claude-3-5-sonnet", Name: "Claude 3.5 Sonnet"},
		"gpt-4o":            {ID: "gpt-4o", Name: "GPT-4o"},
	})

	cache := GetModelsDevCache()
	if cache == nil {
		t.Fatal("expected cache to be loaded")
		return
	}

	tests := []struct {
		name     string
		modelID  string
		wantName string
	}{
		{"date_suffix_yyyy_mm_dd", "claude-3-5-sonnet-2024-10-22", "Claude 3.5 Sonnet"},
		{"date_suffix_yyyymmdd", "claude-3-5-sonnet-20241022", "Claude 3.5 Sonnet"},
		{"version_suffix_long", "gpt-4o-20240806", "GPT-4o"},
		{"no_match_non_date_suffix", "gpt-4o-search-api", ""}, // Should not match
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := cache.LookupFuzzy(tt.modelID)
			if tt.wantName == "" {
				if result != nil {
					t.Errorf("expected nil for %q, got %v", tt.modelID, result)
				}
			} else {
				if result == nil {
					t.Errorf("expected result for %q, got nil", tt.modelID)
				} else if result.Name != tt.wantName {
					t.Errorf("expected name %q, got %q", tt.wantName, result.Name)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// isNumeric additional coverage
// ---------------------------------------------------------------------------

func TestIsNumeric_SingleDigit(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"zero", "0", true},
		{"nine", "9", true},
		{"five", "5", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNumeric(tt.input); got != tt.want {
				t.Errorf("isNumeric(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// looksLikeDate additional coverage
// ---------------------------------------------------------------------------

func TestLooksLikeDate_NineChars(t *testing.T) {
	// Test that 9-character strings are rejected
	got := looksLikeDate("202408060")
	if got {
		t.Error("expected false for 9-character string")
	}
}

func TestLookupFuzzy_YearOnlySuffix(t *testing.T) {
	// Test the YYYY suffix path: model-2024 → model
	setupCacheWithModels(t, map[string]*ModelsDevModelSpec{
		"test-model": {ID: "test-model", Name: "Test Model"},
	})

	cache := GetModelsDevCache()
	if cache == nil {
		t.Fatal("expected cache to be loaded")
		return
	}

	// Search with -YYYY suffix should find the base model
	result := cache.LookupFuzzy("test-model-2024")
	if result == nil {
		t.Error("expected to find test-model by stripping -2024 suffix")
	} else if result.Name != "Test Model" {
		t.Errorf("expected name 'Test Model', got %q", result.Name)
	}
}

func TestLookupFuzzy_NonMatchingSuffix(t *testing.T) {
	// Test that non-date, non-numeric suffixes don't match via prefix path
	setupCacheWithModels(t, map[string]*ModelsDevModelSpec{
		"test-model": {ID: "test-model", Name: "Test Model"},
	})

	cache := GetModelsDevCache()
	if cache == nil {
		t.Fatal("expected cache to be loaded")
		return
	}

	// "test-model-search-api" should NOT match "test-model" because
	// "search-api" is not a date/version suffix
	result := cache.LookupFuzzy("test-model-search-api")
	if result != nil {
		t.Errorf("expected nil for non-date suffix, got %v", result)
	}
}

func TestLookupFuzzy_VersionSuffixSixDigits(t *testing.T) {
	// Test the version suffix path: model with trailing numeric segment >= 6 digits
	setupCacheWithModels(t, map[string]*ModelsDevModelSpec{
		"claude-sonnet-4": {ID: "claude-sonnet-4", Name: "Claude Sonnet 4"},
	})

	cache := GetModelsDevCache()
	if cache == nil {
		t.Fatal("expected cache to be loaded")
		return
	}

	result := cache.LookupFuzzy("claude-sonnet-4-20250514")
	if result == nil {
		t.Error("expected to find claude-sonnet-4 by stripping -20250514 version suffix")
	} else if result.Name != "Claude Sonnet 4" {
		t.Errorf("expected name 'Claude Sonnet 4', got %q", result.Name)
	}
}

// ---------------------------------------------------------------------------
// Provider-aware enrichment
// ---------------------------------------------------------------------------

// loadCacheFromJSON loads the models.dev cache through the real load() path
// from an inline api.json payload, so the per-provider and cross-provider
// indexes are built exactly as production builds them.
func loadCacheFromJSON(t *testing.T, payload string) *ModelsDevCache {
	t.Helper()
	t.Cleanup(func() { ResetModelsDevCache() })

	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	t.Cleanup(mockServer.Close)

	baseTransport := mockServer.Client().Transport
	client := mockServer.Client()
	client.Transport = &mockTransport{roundTripFunc: func(req *http.Request) (*http.Response, error) {
		req.URL.Scheme = "http"
		req.URL.Host = mockServer.Listener.Addr().String()
		return baseTransport.RoundTrip(req)
	}}

	if err := LoadModelsDevWithClient(context.Background(), client); err != nil {
		t.Fatalf("failed to load models.dev cache: %v", err)
	}
	cache := GetModelsDevCache()
	if cache == nil {
		t.Fatal("expected cache to be loaded")
	}
	return cache
}

// resellerVsCanonicalJSON lists the same bare model ID under a reseller
// ("302ai") and the canonical Z.ai entry ("zai") with different prices. The
// reseller sorts alphabetically BEFORE "zai", so any test that sees the zai
// price win proves canonical ranking, not accidental alphabetical order.
const resellerVsCanonicalJSON = `{
	"302ai": {"id":"302ai","name":"302.AI","api":"","models":{
		"glm-t":{"id":"glm-t","name":"GLM-T via reseller","attachment":false,"reasoning":false,"tool_call":false,"modalities":{"input":["text"],"output":["text"]},"open_weights":false,"cost":{"input":0.286,"output":1.142},"limit":{"context":131072,"output":98304}},
		"reseller-only":{"id":"reseller-only","name":"Reseller Only","attachment":false,"reasoning":false,"tool_call":false,"modalities":{"input":["text"],"output":["text"]},"open_weights":false,"cost":{"input":0.5,"output":1.5},"limit":{"context":1000,"output":100}}
	}},
	"zai": {"id":"zai","name":"Z.AI","api":"","models":{
		"glm-t":{"id":"glm-t","name":"GLM-T","attachment":false,"reasoning":true,"tool_call":true,"modalities":{"input":["text"],"output":["text"]},"open_weights":false,"cost":{"input":1.4,"output":4.4,"cache_read":0.26},"limit":{"context":1000000,"output":131072}}
	}}
}`

func TestEnrichModel_PrefersCanonicalProviderEntry(t *testing.T) {
	// "minimax" is ALSO canonical and sorts before "zai", so the cross-provider
	// index carries minimax's spec for the colliding ID. Only the per-provider
	// lookup can surface zai's own entry — this test fails if EnrichModel falls
	// back to the flat index.
	cache := loadCacheFromJSON(t, `{
		"minimax": {"id":"minimax","name":"MiniMax","api":"","models":{
			"glm-t":{"id":"glm-t","name":"GLM-T via MiniMax","attachment":false,"reasoning":false,"tool_call":false,"modalities":{"input":["text"],"output":["text"]},"open_weights":false,"cost":{"input":9.9,"output":9.9},"limit":{"context":1000,"output":100}}
		}},
		"zai": {"id":"zai","name":"Z.AI","api":"","models":{
			"glm-t":{"id":"glm-t","name":"GLM-T","attachment":false,"reasoning":true,"tool_call":true,"modalities":{"input":["text"],"output":["text"]},"open_weights":false,"cost":{"input":1.4,"output":4.4,"cache_read":0.26},"limit":{"context":1000000,"output":131072}}
		}}
	}`)

	m := &model.Model{ModelID: "glm-t"}
	if !cache.EnrichModel(m, "zai-coding") {
		t.Fatal("expected enrichment")
	}
	if m.InputPricePerMillion == nil || *m.InputPricePerMillion != 1.4 {
		t.Errorf("InputPricePerMillion = %v, want 1.4 (canonical zai entry)", m.InputPricePerMillion)
	}
	if m.OutputPricePerMillion == nil || *m.OutputPricePerMillion != 4.4 {
		t.Errorf("OutputPricePerMillion = %v, want 4.4 (canonical zai entry)", m.OutputPricePerMillion)
	}
	if m.InputPricePerMillionCacheHit == nil || *m.InputPricePerMillionCacheHit != 0.26 {
		t.Errorf("InputPricePerMillionCacheHit = %v, want 0.26 (canonical zai entry)", m.InputPricePerMillionCacheHit)
	}
}

func TestEnrichModel_ExclusiveTypeNeverFallsBack(t *testing.T) {
	cache := loadCacheFromJSON(t, resellerVsCanonicalJSON)

	// "reseller-only" is absent from the canonical zai entry, and zai-coding is
	// a single-vendor (exclusive) type: another models.dev provider's data for
	// the same bare ID is secondhand, so the lookup must NOT fall through to
	// the cross-provider index — the model stays unenriched (and unpriced)
	// until the canonical entry lists it.
	m := &model.Model{ModelID: "reseller-only"}
	if cache.EnrichModel(m, "zai-coding") {
		t.Fatal("exclusive provider type must not enrich from the cross-provider index")
	}
	if m.InputPricePerMillion != nil {
		t.Errorf("InputPricePerMillion = %v, want nil", m.InputPricePerMillion)
	}
}

func TestEnrichModel_NonExclusiveTypeFallsBackToCrossProviderIndex(t *testing.T) {
	cache := loadCacheFromJSON(t, resellerVsCanonicalJSON)

	// "openrouter" is mapped but non-exclusive (aggregator): a miss in its
	// canonical entry falls through to the cross-provider index and still
	// enriches.
	m := &model.Model{ModelID: "reseller-only"}
	if !cache.EnrichModel(m, "openrouter") {
		t.Fatal("expected enrichment via cross-provider fallback")
	}
	if m.InputPricePerMillion == nil || *m.InputPricePerMillion != 0.5 {
		t.Errorf("InputPricePerMillion = %v, want 0.5 (reseller entry)", m.InputPricePerMillion)
	}
}

func TestLoad_CrossProviderIndexRanksCanonicalFirst(t *testing.T) {
	cache := loadCacheFromJSON(t, resellerVsCanonicalJSON)

	// Even with no provider type (unknown/custom hosts), the flat index must
	// deterministically carry the canonical provider's spec for a colliding
	// bare ID — never a random reseller's.
	spec := cache.Lookup("glm-t")
	if spec == nil {
		t.Fatal("expected glm-t in cross-provider index")
	}
	if spec.Cost.Input != 1.4 {
		t.Errorf("cross-provider index Cost.Input = %v, want 1.4 (canonical zai entry)", spec.Cost.Input)
	}
}

func TestEnrichModel_UnmappedProviderTypeUsesCrossProviderIndex(t *testing.T) {
	cache := loadCacheFromJSON(t, resellerVsCanonicalJSON)

	m := &model.Model{ModelID: "glm-t"}
	if !cache.EnrichModel(m, "") {
		t.Fatal("expected enrichment")
	}
	// Canonical-first flat ordering means the zai spec also wins here.
	if m.InputPricePerMillion == nil || *m.InputPricePerMillion != 1.4 {
		t.Errorf("InputPricePerMillion = %v, want 1.4", m.InputPricePerMillion)
	}
}

func TestEnrichModel_CanonicalFuzzyMatchWithinProvider(t *testing.T) {
	cache := loadCacheFromJSON(t, resellerVsCanonicalJSON)

	// A dated variant must fuzzy-resolve inside the canonical provider's own
	// models, not just via the cross-provider index.
	m := &model.Model{ModelID: "glm-t-20260814"}
	if !cache.EnrichModel(m, "zai-coding") {
		t.Fatal("expected fuzzy enrichment within canonical provider")
	}
	if m.InputPricePerMillion == nil || *m.InputPricePerMillion != 1.4 {
		t.Errorf("InputPricePerMillion = %v, want 1.4 (canonical zai entry)", m.InputPricePerMillion)
	}
}

// TestLoadModelsDev_ContextCancelled covers the LoadModelsDev wrapper (which
// uses http.DefaultClient). A cancelled context makes the HTTP request fail
// before any network round-trip, exercising the wrapper + the fetch error path.
func TestLoadModelsDev_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := LoadModelsDev(ctx); err == nil {
		t.Error("expected error from LoadModelsDev with a cancelled context")
	}
}
