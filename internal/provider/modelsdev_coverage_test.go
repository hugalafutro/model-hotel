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

func TestLoadModelsDev_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := LoadModelsDev(ctx)
	if err == nil {
		t.Error("expected error from LoadModelsDev with canceled context")
	}
}

// ---------------------------------------------------------------------------
// ResetModelsDevCache
// ---------------------------------------------------------------------------

func TestResetModelsDevCache(t *testing.T) {
	// Set up a mock server with valid models.dev data
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"test-provider":{"id":"test","name":"Test","api":"openai","models":{"test-model":{"id":"test-model","name":"Test Model","attachment":false,"reasoning":false,"tool_call":false,"modalities":{"input":["text"],"output":["text"]},"open_weights":false,"cost":{"input":0,"output":0},"limit":{"context":1000,"output":100}}}}}`))
	}))
	defer mockServer.Close()

	// Load the cache with data
	client := &http.Client{}
	ctx := context.Background()
	err := LoadModelsDevWithClient(ctx, client)
	if err != nil {
		t.Fatalf("failed to load models.dev cache: %v", err)
	}

	// Verify cache has data
	cache := GetModelsDevCache()
	if cache == nil {
		t.Fatal("expected cache to be loaded before reset")
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
	result := cache.EnrichModel(m)
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
	}

	m := &model.Model{ModelID: "nonexistent-model"}
	result := cache.EnrichModel(m)
	if result {
		t.Error("expected false when model not found in cache")
	}
}

// ---------------------------------------------------------------------------
// EnrichModels edge cases
// ---------------------------------------------------------------------------

func TestEnrichModels_NilCache(t *testing.T) {
	var cache *ModelsDevCache
	models := []*model.Model{{ModelID: "test"}}
	count := cache.EnrichModels(models)
	if count != 0 {
		t.Errorf("expected 0 for nil cache, got %d", count)
	}
}

func TestEnrichModels_EmptyList(t *testing.T) {
	setupCacheWithModels(t, map[string]*ModelsDevModelSpec{})

	cache := GetModelsDevCache()
	if cache == nil {
		t.Fatal("expected cache to be loaded")
	}

	models := []*model.Model{}
	count := cache.EnrichModels(models)
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
// modalityFromModelsDev additional coverage
// ---------------------------------------------------------------------------

func TestModalityFromModelsDev_AllModalities(t *testing.T) {
	mods := ModelsDevModalities{
		Input:  []string{"text", "image", "audio", "video"},
		Output: []string{"text"},
	}
	got := modalityFromModelsDev(mods)
	if got != "video" {
		t.Errorf("expected 'video' when all modalities present, got %q", got)
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
