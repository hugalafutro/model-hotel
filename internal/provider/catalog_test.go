package provider

import (
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// deepseekSpec finds a DeepSeek catalog row by model ID.
func deepseekSpec(modelID string) *DeepSeekModelSpec {
	return lookupByModelID(GetDeepSeekModels(), modelID, func(s *DeepSeekModelSpec) string { return s.ModelID })
}

func TestGetDeepSeekModels_NonEmpty(t *testing.T) {
	catalog := GetDeepSeekModels()
	if len(catalog) == 0 {
		t.Error("GetDeepSeekModels should return non-empty catalog")
	}
}

func TestGetDeepSeekModels_AllFieldsValid(t *testing.T) {
	catalog := GetDeepSeekModels()
	for i, spec := range catalog {
		if spec.ModelID == "" {
			t.Errorf("catalog[%d]: ModelID is empty", i)
		}
		if spec.ContextLength <= 0 {
			t.Errorf("catalog[%d] (%s): ContextLength = %d, want > 0", i, spec.ModelID, spec.ContextLength)
		}
		if spec.MaxOutputTokens <= 0 {
			t.Errorf("catalog[%d] (%s): MaxOutputTokens = %d, want > 0", i, spec.ModelID, spec.MaxOutputTokens)
		}
		for name, p := range map[string]*float64{
			"InputPricePerMillionCacheHit":  spec.InputPricePerMillionCacheHit,
			"InputPricePerMillionCacheMiss": spec.InputPricePerMillionCacheMiss,
			"OutputPricePerMillion":         spec.OutputPricePerMillion,
		} {
			if p != nil && *p < 0 {
				t.Errorf("catalog[%d] (%s): %s = %f, want >= 0", i, spec.ModelID, name, *p)
			}
		}
	}
}

func TestLookupByModelID_Found(t *testing.T) {
	catalog := GetDeepSeekModels()
	if len(catalog) == 0 {
		t.Fatal("catalog is empty")
	}
	first := catalog[0]
	result := deepseekSpec(first.ModelID)
	if result == nil {
		t.Fatalf("expected non-nil for %q", first.ModelID)
		return
	}
	if result.ModelID != first.ModelID {
		t.Errorf("ModelID = %q, want %q", result.ModelID, first.ModelID)
	}
}

func TestLookupByModelID_NotFound(t *testing.T) {
	result := deepseekSpec("nonexistent-model-xyz")
	if result != nil {
		t.Errorf("expected nil for unknown model, got %+v", result)
	}
}

// Test GetCoherePricingCatalog and LookupCoherePricing
func TestGetCoherePricingCatalog_NonEmpty(t *testing.T) {
	catalog := GetCoherePricingCatalog()
	if len(catalog) == 0 {
		t.Error("GetCoherePricingCatalog should return non-empty catalog")
	}
}

func TestGetCoherePricingCatalog_AllFieldsValid(t *testing.T) {
	catalog := GetCoherePricingCatalog()
	for i, spec := range catalog {
		if spec.ModelID == "" {
			t.Errorf("catalog[%d]: ModelID is empty", i)
		}
		if spec.DisplayName == "" {
			t.Errorf("catalog[%d] (%s): DisplayName is empty", i, spec.ModelID)
		}
		if spec.MaxOutputTokens != nil && *spec.MaxOutputTokens <= 0 {
			t.Errorf("catalog[%d] (%s): MaxOutputTokens = %d, want > 0", i, spec.ModelID, *spec.MaxOutputTokens)
		}
		for name, p := range map[string]*float64{
			"InputPricePerMillion":   spec.InputPricePerMillion,
			"OutputPricePerMillion":  spec.OutputPricePerMillion,
			"SearchPricePerThousand": spec.SearchPricePerThousand,
		} {
			if p != nil && *p < 0 {
				t.Errorf("catalog[%d] (%s): %s = %f, want >= 0", i, spec.ModelID, name, *p)
			}
		}
		// A row states one billing unit: per token or per search, never both.
		perToken := spec.InputPricePerMillion != nil || spec.OutputPricePerMillion != nil
		if perToken && spec.SearchPricePerThousand != nil {
			t.Errorf("catalog[%d] (%s): carries both per-token and per-search prices", i, spec.ModelID)
		}
	}
}

// TestBuildCohereModel_RerankRowTakesTheSearchPrice: a rerank row prices per
// search unit from the catalog and carries no per-token price, so the proxy
// can price its rows and the dashboard shows one search price.
func TestBuildCohereModel_RerankRowTakesTheSearchPrice(t *testing.T) {
	prov := &Provider{ID: uuid.New()}
	m := buildCohereModel(prov, GetCoherePricingCatalog(), CohereNativeModel{Name: "rerank-v4.0-pro", ContextLength: 32768}, "rerank")
	if m.SearchPricePerThousand == nil || *m.SearchPricePerThousand != 2.5 {
		t.Fatalf("search price = %v, want 2.5", m.SearchPricePerThousand)
	}
	if m.InputPricePerMillion != nil || m.OutputPricePerMillion != nil {
		t.Errorf("rerank row carries per-token prices %v/%v, want none", m.InputPricePerMillion, m.OutputPricePerMillion)
	}
	if m.PriceSources.Search != model.PriceSourceCatalog || m.PriceSources.Input != "" {
		t.Errorf("sources = %+v, want search=catalog only", m.PriceSources)
	}
	if m.OutputModalities != `["rerank"]` || m.DisplayName != "Rerank v4.0 Pro" {
		t.Errorf("output=%s display=%q", m.OutputModalities, m.DisplayName)
	}
}

func TestLookupCoherePricing_Found(t *testing.T) {
	catalog := GetCoherePricingCatalog()
	if len(catalog) == 0 {
		t.Fatal("catalog is empty")
	}
	first := catalog[0]
	result := LookupCoherePricing(catalog, first.ModelID)
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

func TestLookupCoherePricing_NotFound(t *testing.T) {
	catalog := GetCoherePricingCatalog()
	result := LookupCoherePricing(catalog, "nonexistent-model-xyz")
	if result != nil {
		t.Errorf("expected nil for unknown model, got %+v", result)
	}
}

func TestLookupCoherePricing_EmptyCatalog(t *testing.T) {
	result := LookupCoherePricing([]CoherePricingEntry{}, "command-r-plus")
	if result != nil {
		t.Error("expected nil for empty catalog")
	}
}

func TestGetOpenCodeGoCatalog_AllFieldsValid(t *testing.T) {
	catalog := GetOpenCodeGoCatalog()
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
		if spec.Modality == "" {
			t.Errorf("catalog[%d] (%s): Modality is empty", i, spec.ModelID)
		}
	}
}

func TestGetXAICatalog_NonEmpty(t *testing.T) {
	catalog := GetXAICatalog()
	if len(catalog) == 0 {
		t.Error("GetXAICatalog should return non-empty catalog")
	}
}

func TestGetXAICatalog_AllFieldsValid(t *testing.T) {
	catalog := GetXAICatalog()
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
		if spec.Modality == "" {
			t.Errorf("catalog[%d] (%s): Modality is empty", i, spec.ModelID)
		}
	}
}

// Test GetZAICodingModels
func TestGetZAICodingModels_NonEmpty(t *testing.T) {
	catalog := GetZAICodingModels()
	if len(catalog) == 0 {
		t.Error("GetZAICodingModels should return non-empty catalog")
	}
}

func TestGetZAICodingModels_AllFieldsValid(t *testing.T) {
	catalog := GetZAICodingModels()
	for i, spec := range catalog {
		if spec.ModelID == "" {
			t.Errorf("catalog[%d]: ModelID is empty", i)
		}
		if spec.ContextLength <= 0 {
			t.Errorf("catalog[%d] (%s): ContextLength = %d, want > 0", i, spec.ModelID, spec.ContextLength)
		}
		if spec.MaxOutputTokens <= 0 {
			t.Errorf("catalog[%d] (%s): MaxOutputTokens = %d, want > 0", i, spec.ModelID, spec.MaxOutputTokens)
		}
		if spec.Modality == "" {
			t.Errorf("catalog[%d] (%s): Modality is empty", i, spec.ModelID)
		}
	}
}
