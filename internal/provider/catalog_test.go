package provider

import (
	"testing"
)

// Test GetDeepSeekModels and GetDeepSeekModelSpec
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
		if spec.InputPricePerMillionCacheHit < 0 {
			t.Errorf("catalog[%d] (%s): InputPricePerMillionCacheHit = %f, want >= 0", i, spec.ModelID, spec.InputPricePerMillionCacheHit)
		}
		if spec.InputPricePerMillionCacheMiss < 0 {
			t.Errorf("catalog[%d] (%s): InputPricePerMillionCacheMiss = %f, want >= 0", i, spec.ModelID, spec.InputPricePerMillionCacheMiss)
		}
		if spec.OutputPricePerMillion < 0 {
			t.Errorf("catalog[%d] (%s): OutputPricePerMillion = %f, want >= 0", i, spec.ModelID, spec.OutputPricePerMillion)
		}
	}
}

func TestGetDeepSeekModelSpec_Found(t *testing.T) {
	catalog := GetDeepSeekModels()
	if len(catalog) == 0 {
		t.Fatal("catalog is empty")
	}
	first := catalog[0]
	result := GetDeepSeekModelSpec(first.ModelID)
	if result == nil {
		t.Fatalf("expected non-nil for %q", first.ModelID)
		return
	}
	if result.ModelID != first.ModelID {
		t.Errorf("ModelID = %q, want %q", result.ModelID, first.ModelID)
	}
}

func TestGetDeepSeekModelSpec_NotFound(t *testing.T) {
	result := GetDeepSeekModelSpec("nonexistent-model-xyz")
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
		if spec.MaxOutputTokens <= 0 {
			t.Errorf("catalog[%d] (%s): MaxOutputTokens = %d, want > 0", i, spec.ModelID, spec.MaxOutputTokens)
		}
		if spec.InputPricePerMillion < 0 {
			t.Errorf("catalog[%d] (%s): InputPricePerMillion = %f, want >= 0", i, spec.ModelID, spec.InputPricePerMillion)
		}
		if spec.OutputPricePerMillion < 0 {
			t.Errorf("catalog[%d] (%s): OutputPricePerMillion = %f, want >= 0", i, spec.ModelID, spec.OutputPricePerMillion)
		}
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

// Test GetGooglePricingCatalog and LookupGooglePricing
func TestGetGooglePricingCatalog_NonEmpty(t *testing.T) {
	catalog := GetGooglePricingCatalog()
	if len(catalog) == 0 {
		t.Error("GetGooglePricingCatalog should return non-empty catalog")
	}
}

func TestGetGooglePricingCatalog_AllFieldsValid(t *testing.T) {
	catalog := GetGooglePricingCatalog()
	for i, spec := range catalog {
		if spec.ModelID == "" {
			t.Errorf("catalog[%d]: ModelID is empty", i)
		}
		if spec.DisplayName == "" {
			t.Errorf("catalog[%d] (%s): DisplayName is empty", i, spec.ModelID)
		}
		if spec.InputPricePerMillion < 0 {
			t.Errorf("catalog[%d] (%s): InputPricePerMillion = %f, want >= 0", i, spec.ModelID, spec.InputPricePerMillion)
		}
		if spec.OutputPricePerMillion < 0 {
			t.Errorf("catalog[%d] (%s): OutputPricePerMillion = %f, want >= 0", i, spec.ModelID, spec.OutputPricePerMillion)
		}
	}
}

func TestLookupGooglePricing_Found(t *testing.T) {
	catalog := GetGooglePricingCatalog()
	if len(catalog) == 0 {
		t.Fatal("catalog is empty")
	}
	first := catalog[0]
	result := LookupGooglePricing(catalog, first.ModelID)
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

func TestLookupGooglePricing_NotFound(t *testing.T) {
	catalog := GetGooglePricingCatalog()
	result := LookupGooglePricing(catalog, "nonexistent-model-xyz")
	if result != nil {
		t.Errorf("expected nil for unknown model, got %+v", result)
	}
}

func TestLookupGooglePricing_EmptyCatalog(t *testing.T) {
	result := LookupGooglePricing([]GoogleModelPricing{}, "models/gemini-3.1-pro-preview")
	if result != nil {
		t.Error("expected nil for empty catalog")
	}
}

// Test GetOpenCodeGoCatalog
func TestGetOpenCodeGoCatalog_NonEmpty(t *testing.T) {
	catalog := GetOpenCodeGoCatalog()
	if len(catalog) == 0 {
		t.Error("GetOpenCodeGoCatalog should return non-empty catalog")
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

// Test GetOpenCodeZenCatalog
func TestGetOpenCodeZenCatalog_NonEmpty(t *testing.T) {
	catalog := GetOpenCodeZenCatalog()
	if len(catalog) == 0 {
		t.Error("GetOpenCodeZenCatalog should return non-empty catalog")
	}
}

func TestGetOpenCodeZenCatalog_AllFieldsValid(t *testing.T) {
	catalog := GetOpenCodeZenCatalog()
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

// Test GetXAICatalog
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
