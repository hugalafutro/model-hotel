package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
)

func TestAnthropicPricingLookup(t *testing.T) {
	catalog := GetAnthropicPricing()
	if len(catalog) == 0 {
		t.Fatal("anthropic pricing catalog is empty")
	}

	t.Logf("Anthropic pricing catalog has %d entries", len(catalog))

	for _, spec := range catalog {
		found := LookupAnthropicPricing(catalog, spec.ModelID)
		if found == nil {
			t.Errorf("LookupAnthropicPricing failed for %s", spec.ModelID)
		}
	}

	// Check specific entry
	spec := LookupAnthropicPricing(catalog, "claude-opus-4-7")
	if spec == nil {
		t.Fatal("claude-opus-4-7 not found in catalog")
	}
	if spec.InputPricePerMillion != 5.00 {
		t.Errorf("claude-opus-4-7 input price: got %.2f, want 5.00", spec.InputPricePerMillion)
	}
	if spec.OutputPricePerMillion != 25.00 {
		t.Errorf("claude-opus-4-7 output price: got %.2f, want 25.00", spec.OutputPricePerMillion)
	}
	if spec.InputPricePerMillionCacheHit != 0.50 {
		t.Errorf("claude-opus-4-7 cache hit price: got %.2f, want 0.50", spec.InputPricePerMillionCacheHit)
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
			//nolint:gosec // test-only: error handling not critical
			w.Write([]byte(page1))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := NewDiscoveryService()
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
	if m1.InputPricePerMillion == nil || *m1.InputPricePerMillion != 5.00 {
		t.Errorf("expected input price 5.00, got %v", m1.InputPricePerMillion)
	}
	if m1.OutputPricePerMillion == nil || *m1.OutputPricePerMillion != 25.00 {
		t.Errorf("expected output price 25.00, got %v", m1.OutputPricePerMillion)
	}
	if m1.InputPricePerMillionCacheHit == nil || *m1.InputPricePerMillionCacheHit != 0.50 {
		t.Errorf("expected cache hit price 0.50, got %v", m1.InputPricePerMillionCacheHit)
	}
	if m1.OwnedBy != "anthropic" {
		t.Errorf("expected owned_by 'anthropic', got '%s'", m1.OwnedBy)
	}

	// Check capabilities parsed from API
	var caps model.Capability
	//nolint:gosec // test-only: error handling not critical
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

	// Check modality derived from image_input
	if m1.Modality != "vision" {
		t.Errorf("expected modality 'vision', got '%s'", m1.Modality)
	}

	// Check claude-sonnet-4-6
	m2 := models[1]
	if m2.ModelID != "claude-sonnet-4-6" {
		t.Errorf("expected claude-sonnet-4-6, got %s", m2.ModelID)
	}
	if m2.InputPricePerMillion == nil || *m2.InputPricePerMillion != 3.00 {
		t.Errorf("expected sonnet input price 3.00, got %v", m2.InputPricePerMillion)
	}

	t.Logf("Anthropic mock server test passed - %d models discovered", len(models))
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
			//nolint:gosec // test-only: mock server response
			w.Write([]byte(page1))
		} else {
			//nolint:gosec // test-only: mock server response
			w.Write([]byte(page2))
		}
	}))
	defer server.Close()

	svc := NewDiscoveryService()
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
		//nolint:gosec // test-only: mock server response
		w.Write([]byte(page1))
	}))
	defer server.Close()

	svc := NewDiscoveryService()
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
	//nolint:gosec // test-only
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

func TestAnthropicPricingLookupDated(t *testing.T) {
	catalog := GetAnthropicPricing()

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
		{"claude-opus-4-20250514", true, 5.00, 25.00},
		{"claude-sonnet-4-6", true, 3.00, 15.00},
		{"claude-sonnet-4-5-20250929", true, 3.00, 15.00},
		{"claude-sonnet-4-20250514", true, 3.00, 15.00},
		{"claude-haiku-4-5-20251001", true, 1.00, 5.00},
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

func TestAnthropicDiscoveryLiveAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping live API test in short mode")
	}
	if os.Getenv("LIVE_API_TESTS") == "" {
		t.Skip("skipping live API test (set LIVE_API_TESTS=1 to enable)")
	}

	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Fatal("ANTHROPIC_API_KEY environment variable is required for live API tests")
	}

	svc := NewDiscoveryService()
	prov := &Provider{
		ID:      uuid.New(),
		BaseURL: "https://api.anthropic.com",
	}

	ctx := context.Background()
	models, err := svc.discoverAnthropic(ctx, prov, apiKey)
	if err != nil {
		t.Fatalf("discoverAnthropic failed: %v", err)
	}

	fmt.Printf("Discovered %d models from Anthropic\n", len(models))

	pricingMatched := 0
	for _, m := range models {
		if m.InputPricePerMillion != nil {
			pricingMatched++
		}
	}
	fmt.Printf("  Pricing-matched: %d, No pricing: %d\n", pricingMatched, len(models)-pricingMatched)

	if pricingMatched == 0 {
		t.Error("expected at least some pricing-matched models")
	}

	for _, m := range models {
		var caps model.Capability
		//nolint:gosec // test-only
		json.Unmarshal([]byte(m.Capabilities), &caps)
		ctxLen := "<nil>"
		if m.ContextLength != nil {
			ctxLen = fmt.Sprintf("%d", *m.ContextLength)
		}
		maxOut := "<nil>"
		if m.MaxOutputTokens != nil {
			maxOut = fmt.Sprintf("%d", *m.MaxOutputTokens)
		}
		inPrice := "<nil>"
		if m.InputPricePerMillion != nil {
			inPrice = fmt.Sprintf("$%.2f", *m.InputPricePerMillion)
		}
		outPrice := "<nil>"
		if m.OutputPricePerMillion != nil {
			outPrice = fmt.Sprintf("$%.2f", *m.OutputPricePerMillion)
		}
		cachePrice := "<nil>"
		if m.InputPricePerMillionCacheHit != nil {
			cachePrice = fmt.Sprintf("$%.2f", *m.InputPricePerMillionCacheHit)
		}
		fmt.Printf("  %s display=%s ctx=%s max_out=%s in=%s out=%s cache=%s vision=%v struct=%v pdf=%v\n",
			m.ModelID, m.DisplayName, ctxLen, maxOut,
			inPrice, outPrice, cachePrice,
			caps.Vision, caps.StructuredOutput, caps.PDFUpload)
	}

	for _, m := range models {
		if m.ModelID == "claude-opus-4-7" || m.ModelID == "claude-opus-4-6" {
			if m.InputPricePerMillion == nil {
				t.Errorf("%s should have pricing", m.ModelID)
			}
			if m.ContextLength == nil {
				t.Errorf("%s should have context length from API", m.ModelID)
			}
			if m.DisplayName == "" {
				t.Errorf("%s should have display name from API", m.ModelID)
			}
		}
		if m.ModelID == "claude-opus-4-5-20251101" {
			if m.InputPricePerMillion == nil {
				t.Error("claude-opus-4-5-20251101 should have pricing from catalog strip")
			}
			if *m.InputPricePerMillion != 5.00 {
				t.Errorf("claude-opus-4-5-20251101 input price: got %.2f, want 5.00", *m.InputPricePerMillion)
			}
			if m.DisplayName != "Claude Opus 4.5" {
				t.Errorf("claude-opus-4-5-20251101 display name: got %s, want 'Claude Opus 4.5'", m.DisplayName)
			}
		}
	}
}
