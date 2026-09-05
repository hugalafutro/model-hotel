package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// anthropic.json is an override channel and is legitimately empty; every row it
// DOES ship must still be self-consistent and resolvable.
func TestAnthropicPricingLookup(t *testing.T) {
	catalog := GetAnthropicPricing()
	t.Logf("Anthropic pricing catalog has %d entries", len(catalog))

	for _, spec := range catalog {
		found := LookupAnthropicPricing(catalog, spec.ModelID)
		if found == nil {
			t.Errorf("LookupAnthropicPricing failed for %s", spec.ModelID)
		}
	}

	// Unknown model should return nil
	notFound := LookupAnthropicPricing(catalog, "claude-future-model")
	if notFound != nil {
		t.Error("expected nil for unknown model")
	}
}

func TestAnthropicDiscoveryWithMockServer(t *testing.T) {
	page1 := `{
		"data": [
			{"id": "claude-opus-4-7", "type": "model", "display_name": "Claude Opus 4.7", "created_at": "2025-01-01T00:00:00Z", "max_input_tokens": 200000, "max_tokens": 32768, "capabilities": {"image_input": {"supported": true}, "pdf_input": {"supported": true}, "structured_outputs": {"supported": true}, "batch": {"supported": true}, "citations": {"supported": false}, "code_execution": {"supported": false}}},
			{"id": "claude-sonnet-4-6", "type": "model", "display_name": "Claude Sonnet 4.6", "created_at": "2025-01-01T00:00:00Z", "max_input_tokens": 200000, "max_tokens": 16384, "capabilities": {"image_input": {"supported": true}, "pdf_input": {"supported": true}, "structured_outputs": {"supported": true}, "batch": {"supported": true}, "citations": {"supported": false}, "code_execution": {"supported": false}}}
		],
		"has_more": false,
		"first_id": "claude-opus-4-7",
		"last_id": "claude-sonnet-4-6"
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(page1))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := NewDiscoveryService(nil, nil)
	prov := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	ctx := context.Background()
	models, err := svc.discoverAnthropic(ctx, prov, "test-key")
	if err != nil {
		t.Fatalf("discoverAnthropic failed: %v", err)
	}

	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}

	// Check claude-opus-4-7
	m1 := models[0]
	if m1.ModelID != "claude-opus-4-7" {
		t.Errorf("expected claude-opus-4-7, got %s", m1.ModelID)
	}
	if m1.DisplayName != "Claude Opus 4.7" {
		t.Errorf("expected 'Claude Opus 4.7', got '%s'", m1.DisplayName)
	}
	if m1.ContextLength == nil || *m1.ContextLength != 200000 {
		t.Errorf("expected context 200000, got %v", m1.ContextLength)
	}
	if m1.MaxOutputTokens == nil || *m1.MaxOutputTokens != 32768 {
		t.Errorf("expected max_output 32768, got %v", m1.MaxOutputTokens)
	}
	// With no override shipped for this model, discovery must leave pricing
	// unset for models.dev enrichment to fill rather than fabricating a zero.
	// See TestAnthropicPricingLookupDated for the override lookup itself.
	if m1.InputPricePerMillion != nil {
		t.Errorf("expected no catalog price, got %v", *m1.InputPricePerMillion)
	}
	if m1.OutputPricePerMillion != nil {
		t.Errorf("expected no catalog price, got %v", *m1.OutputPricePerMillion)
	}
	if m1.OwnedBy != "anthropic" {
		t.Errorf("expected owned_by 'anthropic', got '%s'", m1.OwnedBy)
	}

	// Check capabilities parsed from API
	var caps model.Capability
	json.Unmarshal([]byte(m1.Capabilities), &caps)
	if !caps.Vision {
		t.Error("expected Vision=true for opus")
	}
	if !caps.StructuredOutput {
		t.Error("expected StructuredOutput=true for opus")
	}
	if !caps.ToolCalling {
		t.Error("expected ToolCalling=true (default)")
	}
	if !caps.Streaming {
		t.Error("expected Streaming=true (default)")
	}

	// Image input lands in the modality arrays; the endpoint class is derived
	// by the pipeline's NormalizeModels step.
	if m1.InputModalities != `["text","image","pdf"]` {
		t.Errorf("expected input modalities [\"text\",\"image\",\"pdf\"], got '%s'", m1.InputModalities)
	}
	NormalizeModels(models)
	if m1.Modality != "chat" {
		t.Errorf("expected derived class 'chat', got '%s'", m1.Modality)
	}

	// Check claude-sonnet-4-6
	m2 := models[1]
	if m2.ModelID != "claude-sonnet-4-6" {
		t.Errorf("expected claude-sonnet-4-6, got %s", m2.ModelID)
	}
	if m2.InputPricePerMillion != nil {
		t.Errorf("expected no catalog price, got %v", *m2.InputPricePerMillion)
	}

	t.Logf("Anthropic mock server test passed - %d models discovered", len(models))

	// The same listing behind an operator-entered Messages endpoint parses
	// identically, owner included. Leaving OwnedBy empty for this type was tried
	// and reverted: models.dev enrichment fills an empty OwnedBy from the model
	// FAMILY, so claude-fable-5 reported owned_by "anthropic" under an anthropic
	// provider and "claude-fable" under an anthropic-messages one. A consistent
	// owner beats a per-provider-type one.
	custom := &Provider{ID: uuid.New(), BaseURL: server.URL, ProviderType: "anthropic-messages"}
	customModels, err := svc.discoverAnthropic(ctx, custom, "test-key")
	if err != nil {
		t.Fatalf("discoverAnthropic for anthropic-messages failed: %v", err)
	}
	if len(customModels) != 2 {
		t.Fatalf("expected 2 models from the custom endpoint, got %d", len(customModels))
	}
	if customModels[0].OwnedBy != "anthropic" {
		t.Errorf("owned_by = %q, want the same owner both types report", customModels[0].OwnedBy)
	}
	if customModels[0].ModelID != "claude-opus-4-7" {
		t.Errorf("custom endpoint model = %q, want the listing parsed the same way", customModels[0].ModelID)
	}
}

func TestAnthropicDiscoverypagination(t *testing.T) {
	page1 := `{
		"data": [
			{"id": "claude-opus-4-7", "type": "model", "display_name": "Claude Opus 4.7", "created_at": "2025-01-01T00:00:00Z", "max_input_tokens": 200000, "max_tokens": 32768, "capabilities": {"image_input": {"supported": true}, "pdf_input": {"supported": true}, "structured_outputs": {"supported": true}, "batch": {"supported": true}, "citations": {"supported": false}, "code_execution": {"supported": false}}}
		],
		"has_more": true,
		"first_id": "claude-opus-4-7",
		"last_id": "claude-opus-4-7"
	}`

	page2 := `{
		"data": [
			{"id": "claude-haiku-4-5", "type": "model", "display_name": "Claude Haiku 4.5", "created_at": "2025-01-01T00:00:00Z", "max_input_tokens": 200000, "max_tokens": 8192, "capabilities": {"image_input": {"supported": false}, "pdf_input": {"supported": false}, "structured_outputs": {"supported": true}, "batch": {"supported": true}, "citations": {"supported": false}, "code_execution": {"supported": false}}}
		],
		"has_more": false,
		"first_id": "claude-haiku-4-5",
		"last_id": "claude-haiku-4-5"
	}`

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("after_id") == "" {
			w.Write([]byte(page1))
		} else {
			w.Write([]byte(page2))
		}
	}))
	defer server.Close()

	svc := NewDiscoveryService(nil, nil)
	prov := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	ctx := context.Background()
	models, err := svc.discoverAnthropic(ctx, prov, "test-key")
	if err != nil {
		t.Fatalf("discoverAnthropic failed: %v", err)
	}

	if len(models) != 2 {
		t.Fatalf("expected 2 models from 2 pages, got %d", len(models))
	}
	if callCount != 2 {
		t.Errorf("expected 2 API calls for pagination, got %d", callCount)
	}

	// Verify both models are present
	ids := map[string]bool{}
	for _, m := range models {
		ids[m.ModelID] = true
	}
	if !ids["claude-opus-4-7"] || !ids["claude-haiku-4-5"] {
		t.Errorf("expected both models from pagination, got IDs: %v", ids)
	}

	t.Logf("Anthropic pagination test passed - %d models from %d pages", len(models), callCount)
}

func TestAnthropicDiscoverynoCapabilities(t *testing.T) {
	page1 := `{
		"data": [
			{"id": "claude-future-model", "type": "model", "display_name": "Claude Future", "created_at": "2025-01-01T00:00:00Z", "max_input_tokens": 500000, "max_tokens": 65536}
		],
		"has_more": false,
		"first_id": "claude-future-model",
		"last_id": "claude-future-model"
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(page1))
	}))
	defer server.Close()

	svc := NewDiscoveryService(nil, nil)
	prov := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL,
	}

	ctx := context.Background()
	models, err := svc.discoverAnthropic(ctx, prov, "test-key")
	if err != nil {
		t.Fatalf("discoverAnthropic failed: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}

	m := models[0]
	if m.ModelID != "claude-future-model" {
		t.Errorf("expected claude-future-model, got %s", m.ModelID)
	}
	if m.DisplayName != "Claude Future" {
		t.Errorf("expected 'Claude Future', got '%s'", m.DisplayName)
	}
	if m.ContextLength == nil || *m.ContextLength != 500000 {
		t.Errorf("expected context 500000, got %v", m.ContextLength)
	}
	if m.MaxOutputTokens == nil || *m.MaxOutputTokens != 65536 {
		t.Errorf("expected max output 65536, got %v", m.MaxOutputTokens)
	}
	// No pricing for unknown model
	if m.InputPricePerMillion != nil {
		t.Errorf("unknown model should have nil pricing, got %v", m.InputPricePerMillion)
	}
	// Capabilities should have defaults (streaming, tool_calling)
	var caps model.Capability
	json.Unmarshal([]byte(m.Capabilities), &caps)
	if !caps.Streaming {
		t.Error("expected Streaming=true by default")
	}
	if !caps.ToolCalling {
		t.Error("expected ToolCalling=true by default")
	}

	t.Logf("Anthropic no-capabilities test passed")
}

func TestStripAnthropicDate(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"claude-opus-4-5-20251101", "claude-opus-4-5"},
		{"claude-opus-4-1-20250805", "claude-opus-4-1"},
		{"claude-sonnet-4-5-20250929", "claude-sonnet-4-5"},
		{"claude-sonnet-4-20250514", "claude-sonnet-4"},
		{"claude-haiku-4-5-20251001", "claude-haiku-4-5"},
		{"claude-opus-4-6", "claude-opus-4-6"},
		{"claude-opus-4-7", "claude-opus-4-7"},
		{"claude-sonnet-4-6", "claude-sonnet-4-6"},
		{"claude-haiku-4-5", "claude-haiku-4-5"},
		{"claude-3-5-sonnet-20241022", "claude-3-5-sonnet"},
	}

	for _, tc := range tests {
		result := stripAnthropicDate(tc.input)
		if result != tc.expected {
			t.Errorf("stripAnthropicDate(%q) = %q, want %q", tc.input, result, tc.expected)
		}
	}
}

// TestAnthropicPricingLookupDated covers the date-stripping fallback: an
// override written against the undated family id must also answer for the dated
// ids Anthropic's listing actually returns.
//
// It runs against a fixture rather than GetAnthropicPricing(). The shipped
// catalog is an override channel and is currently empty, because every row in
// it merely restated models.dev and a stale duplicate silently overrides
// correct data. Testing the lookup against a fixture keeps this behaviour
// covered regardless of whether any override happens to be shipped.
func TestAnthropicPricingLookupDated(t *testing.T) {
	catalog := []AnthropicPricingSpec{
		{ModelID: "claude-opus-4-7", InputPricePerMillion: 5, InputPricePerMillionCacheHit: 0.5, OutputPricePerMillion: 25},
		{ModelID: "claude-opus-4-6", InputPricePerMillion: 5, InputPricePerMillionCacheHit: 0.5, OutputPricePerMillion: 25},
		{ModelID: "claude-opus-4-5", InputPricePerMillion: 5, InputPricePerMillionCacheHit: 0.5, OutputPricePerMillion: 25},
		{ModelID: "claude-opus-4-1", InputPricePerMillion: 15, InputPricePerMillionCacheHit: 1.5, OutputPricePerMillion: 75},
		{ModelID: "claude-sonnet-4-6", InputPricePerMillion: 3, InputPricePerMillionCacheHit: 0.3, OutputPricePerMillion: 15},
		{ModelID: "claude-sonnet-4-5", InputPricePerMillion: 3, InputPricePerMillionCacheHit: 0.3, OutputPricePerMillion: 15},
		{ModelID: "claude-haiku-4-5", InputPricePerMillion: 1, InputPricePerMillionCacheHit: 0.1, OutputPricePerMillion: 5},
	}

	tests := []struct {
		modelID     string
		found       bool
		inputPrice  float64
		outputPrice float64
	}{
		{"claude-opus-4-7", true, 5.00, 25.00},
		{"claude-opus-4-6", true, 5.00, 25.00},
		{"claude-opus-4-5-20251101", true, 5.00, 25.00},
		{"claude-opus-4-1-20250805", true, 15.00, 75.00},
		{"claude-sonnet-4-6", true, 3.00, 15.00},
		{"claude-sonnet-4-5-20250929", true, 3.00, 15.00},
		{"claude-haiku-4-5-20251001", true, 1.00, 5.00},
		// Retired 2026-06-15 and absent from the fixture, so their dated IDs
		// must not resolve — date-stripping must not invent a family match.
		{"claude-opus-4-20250514", false, 0, 0},
		{"claude-sonnet-4-20250514", false, 0, 0},
		{"claude-future-model", false, 0, 0},
	}

	for _, tc := range tests {
		result := LookupAnthropicPricing(catalog, tc.modelID)
		if tc.found {
			if result == nil {
				t.Errorf("LookupAnthropicPricing(%q) = nil, expected found", tc.modelID)
				continue
			}
			if result.InputPricePerMillion != tc.inputPrice {
				t.Errorf("LookupAnthropicPricing(%q).InputPricePerMillion = %.2f, want %.2f", tc.modelID, result.InputPricePerMillion, tc.inputPrice)
			}
			if result.OutputPricePerMillion != tc.outputPrice {
				t.Errorf("LookupAnthropicPricing(%q).OutputPricePerMillion = %.2f, want %.2f", tc.modelID, result.OutputPricePerMillion, tc.outputPrice)
			}
		} else if result != nil {
			t.Errorf("LookupAnthropicPricing(%q) = %+v, expected nil", tc.modelID, result)
		}
	}
}

// TestAnthropicDiscoveryLiveAPI moved to discovery_live_test.go (//go:build live).
