package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/hugalafutro/model-hotel/internal/model"
)

func TestOpenAIDiscoveryHybrid(t *testing.T) {
	catalog := GetOpenAIModels()
	if len(catalog) == 0 {
		t.Fatal("openai catalog is empty")
	}

	t.Logf("OpenAI catalog has %d entries", len(catalog))

	// Verify lookup works
	for _, spec := range catalog {
		found := LookupOpenAICatalog(catalog, spec.ModelID)
		if found == nil {
			t.Errorf("LookupOpenAICatalog failed for %s", spec.ModelID)
		}
		if found != nil && found.DisplayName != spec.DisplayName {
			t.Errorf("LookupOpenAICatalog returned wrong spec for %s: got %s, want %s", spec.ModelID, found.DisplayName, spec.DisplayName)
		}
	}

	// Simulate API response with some known and some unknown models
	apiModels := []OpenAIModel{
		{ID: "gpt-5.5", Object: "model", OwnedBy: "system"},
		{ID: "gpt-5.4", Object: "model", OwnedBy: "system"},
		{ID: "gpt-5-nano", Object: "model", OwnedBy: "system"},
		{ID: "some-future-model", Object: "model", OwnedBy: "system"},
	}

	result := make([]*model.Model, 0, len(apiModels))
	for _, m := range apiModels {
		spec := LookupOpenAICatalog(catalog, m.ID)
		if spec != nil {
			caps := model.Capability{
				Streaming:        spec.Streaming,
				Reasoning:        spec.Reasoning,
				ToolCalling:      spec.ToolCalling,
				StructuredOutput: spec.StructuredOutput,
				Vision:           spec.Vision,
			}
			capJSON, _ := json.Marshal(caps)
			contextLen := spec.ContextLength
			maxOutput := spec.MaxOutputTokens
			inPrice := spec.InputPricePerMillion
			outPrice := spec.OutputPricePerMillion

			entry := &model.Model{
				ID:                    uuid.New(),
				ProviderID:            uuid.UUID{},
				ModelID:               m.ID,
				Name:                  m.ID,
				DisplayName:           spec.DisplayName,
				Description:           spec.Description,
				Capabilities:          string(capJSON),
				Params:                "{}",
				Modality:              spec.Modality,
				InputModalities:       spec.InputModalities,
				OutputModalities:      spec.OutputModalities,
				ContextLength:         &contextLen,
				MaxOutputTokens:       &maxOutput,
				InputPricePerMillion:  &inPrice,
				OutputPricePerMillion: &outPrice,
				OwnedBy:               m.OwnedBy,
				Enabled:               true,
			}
			if spec.InputPricePerMillionCacheHit > 0 {
				cacheHitPrice := spec.InputPricePerMillionCacheHit
				entry.InputPricePerMillionCacheHit = &cacheHitPrice
			}
			result = append(result, entry)
		} else {
			capJSON, _ := json.Marshal(model.Capability{Streaming: true})
			result = append(result, &model.Model{
				ID:               uuid.New(),
				ProviderID:       uuid.UUID{},
				ModelID:          m.ID,
				Name:             m.ID,
				DisplayName:      m.ID,
				Capabilities:     string(capJSON),
				Params:           "{}",
				Modality:         "text",
				InputModalities:  "[]",
				OutputModalities: "[]",
				OwnedBy:          m.OwnedBy,
				Enabled:          true,
			})
		}
	}

	if len(result) != 4 {
		t.Fatalf("expected 4 models, got %d", len(result))
	}

	// Check catalog-matched model has pricing
	if result[0].InputPricePerMillion == nil || *result[0].InputPricePerMillion != 5.00 {
		t.Errorf("gpt-5.5 input price wrong: got %v", result[0].InputPricePerMillion)
	}
	if result[0].DisplayName != "GPT 5.5" {
		t.Errorf("gpt-5.5 display name wrong: got %s", result[0].DisplayName)
	}
	if result[0].ContextLength == nil || *result[0].ContextLength != 272000 {
		t.Errorf("gpt-5.5 context length wrong: got %v", result[0].ContextLength)
	}
	if result[0].InputPricePerMillionCacheHit == nil || *result[0].InputPricePerMillionCacheHit != 0.50 {
		t.Errorf("gpt-5.5 cache hit price wrong: got %v", result[0].InputPricePerMillionCacheHit)
	}

	// Check unknown model gets minimal entry
	if result[3].InputPricePerMillion != nil {
		t.Errorf("unknown model should have nil pricing, got %v", result[3].InputPricePerMillion)
	}
	if result[3].DisplayName != "some-future-model" {
		t.Errorf("unknown model DisplayName should be model ID, got %s", result[3].DisplayName)
	}

	t.Logf("All hybrid discovery assertions passed")
}

func TestOpenAIDiscoveryWithMockServer(t *testing.T) {
	apiResponse := `{"object":"list","data":[{"id":"gpt-5.5","object":"model","created":1700000000,"owned_by":"system"},{"id":"unknown-model-xyz","object":"model","created":1700000000,"owned_by":"system"}]}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(apiResponse))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	svc := NewDiscoveryService()
	prov := &Provider{
		ID:      uuid.New(),
		BaseURL: server.URL + "/v1",
	}

	// Test with empty key (should still work for mock)
	ctx := context.Background()
	models, err := svc.discoverOpenAI(ctx, prov, "test-key")
	if err != nil {
		t.Fatalf("discoverOpenAI failed: %v", err)
	}

	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}

	// First model should be catalog-matched
	m1 := models[0]
	if m1.ModelID != "gpt-5.5" {
		t.Errorf("expected gpt-5.5, got %s", m1.ModelID)
	}
	if m1.DisplayName != "GPT 5.5" {
		t.Errorf("expected 'GPT 5.5', got '%s'", m1.DisplayName)
	}
	if m1.InputPricePerMillion == nil || *m1.InputPricePerMillion != 5.00 {
		t.Errorf("expected input price 5.00, got %v", m1.InputPricePerMillion)
	}
	if m1.OutputPricePerMillion == nil || *m1.OutputPricePerMillion != 30.00 {
		t.Errorf("expected output price 30.00, got %v", m1.OutputPricePerMillion)
	}
	if m1.InputPricePerMillionCacheHit == nil || *m1.InputPricePerMillionCacheHit != 0.50 {
		t.Errorf("expected cache hit price 0.50, got %v", m1.InputPricePerMillionCacheHit)
	}
	if m1.ContextLength == nil || *m1.ContextLength != 272000 {
		t.Errorf("expected context length 272000, got %v", m1.ContextLength)
	}

	// Check capabilities
	var caps model.Capability
	json.Unmarshal([]byte(m1.Capabilities), &caps)
	if !caps.Streaming {
		t.Error("expected Streaming=true")
	}
	if !caps.Reasoning {
		t.Error("expected Reasoning=true for gpt-5.5")
	}
	if !caps.ToolCalling {
		t.Error("expected ToolCalling=true for gpt-5.5")
	}

	// Second model should be minimal/unknown
	m2 := models[1]
	if m2.ModelID != "unknown-model-xyz" {
		t.Errorf("expected unknown-model-xyz, got %s", m2.ModelID)
	}
	if m2.DisplayName != "unknown-model-xyz" {
		t.Errorf("expected 'unknown-model-xyz', got '%s'", m2.DisplayName)
	}
	if m2.InputPricePerMillion != nil {
		t.Errorf("unknown model should have nil input price, got %v", m2.InputPricePerMillion)
	}
	if m2.OutputPricePerMillion != nil {
		t.Errorf("unknown model should have nil output price, got %v", m2.OutputPricePerMillion)
	}

	t.Logf("Mock server discovery test passed - %d models discovered", len(models))
}

func TestOpenAIDiscoveryLiveAPI(t *testing.T) {
	// This test is skipped unless explicitly enabled
	if testing.Short() {
		t.Skip("skipping live API test in short mode")
	}

	apiKey := "sk-proj-DUMMY_REPLACE_WITH_YOUR_KEY_FOR_LIVE_TESTS"

	svc := NewDiscoveryService()
	prov := &Provider{
		ID:      uuid.New(),
		BaseURL: "https://api.openai.com/v1",
	}

	ctx := context.Background()
	models, err := svc.discoverOpenAI(ctx, prov, apiKey)
	if err != nil {
		t.Fatalf("discoverOpenAI failed: %v", err)
	}

	fmt.Printf("Discovered %d models from OpenAI\n", len(models))

	catalogMatches := 0
	minimalEntries := 0
	for _, m := range models {
		if m.InputPricePerMillion != nil {
			catalogMatches++
		} else {
			minimalEntries++
		}
	}

	fmt.Printf("  Catalog-matched: %d, Minimal entries: %d\n", catalogMatches, minimalEntries)

	if catalogMatches == 0 {
		t.Error("expected at least some catalog-matched models")
	}

	// Verify specific catalog-matched model
	for _, m := range models {
		if m.ModelID == "gpt-5.5" {
			if m.DisplayName != "GPT 5.5" {
				t.Errorf("gpt-5.5 display name: got %s", m.DisplayName)
			}
			if m.InputPricePerMillion == nil || *m.InputPricePerMillion != 5.00 {
				t.Errorf("gpt-5.5 input price: got %v", m.InputPricePerMillion)
			}
			if m.OutputPricePerMillion == nil || *m.OutputPricePerMillion != 30.00 {
				t.Errorf("gpt-5.5 output price: got %v", m.OutputPricePerMillion)
			}
			if m.ContextLength == nil || *m.ContextLength != 272000 {
				t.Errorf("gpt-5.5 context: got %v", m.ContextLength)
			}
			if m.InputPricePerMillionCacheHit == nil || *m.InputPricePerMillionCacheHit != 0.50 {
				t.Errorf("gpt-5.5 cache hit: got %v", m.InputPricePerMillionCacheHit)
			}

			var caps model.Capability
			json.Unmarshal([]byte(m.Capabilities), &caps)
			if !caps.Reasoning {
				t.Error("gpt-5.5 should have Reasoning=true")
			}
			if !caps.ToolCalling {
				t.Error("gpt-5.5 should have ToolCalling=true")
			}
			if !caps.StructuredOutput {
				t.Error("gpt-5.5 should have StructuredOutput=true")
			}

			t.Logf("OK: gpt-5.5 -> display=%s, ctx=%d, in=$%.2f, out=$%.2f, cache=$%.2f",
				m.DisplayName, *m.ContextLength, *m.InputPricePerMillion, *m.OutputPricePerMillion, *m.InputPricePerMillionCacheHit)
			break
		}
	}

	// Spot-check an unknown model
	for _, m := range models {
		if m.ModelID == "text-embedding-3-small" {
			if m.InputPricePerMillion != nil {
				t.Errorf("embedding model should have nil pricing, got %v", m.InputPricePerMillion)
			}
			if m.DisplayName != "text-embedding-3-small" {
				t.Errorf("unknown model should use ID as DisplayName, got %s", m.DisplayName)
			}
			t.Logf("OK: unknown model %s -> display=%s, pricing=nil (minimal entry)", m.ModelID, m.DisplayName)
			break
		}
	}
}
