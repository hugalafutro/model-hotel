package provider

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
)

func TestLookupOpenCodeCatalog(t *testing.T) {
	catalog := []OpenCodeModelSpec{
		{ModelID: "gpt-4", DisplayName: "GPT-4"},
		{ModelID: "claude-3", DisplayName: "Claude 3"},
	}
	tests := []struct {
		name    string
		modelID string
		want    string // DisplayName of found model, or "" if nil
		isNil   bool
	}{
		{"found gpt-4", "gpt-4", "GPT-4", false},
		{"found claude-3", "claude-3", "Claude 3", false},
		{"not found", "gemini-pro", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LookupOpenCodeCatalog(catalog, tt.modelID)
			if tt.isNil {
				if got != nil {
					t.Errorf("LookupOpenCodeCatalog() = %v, want nil", got)
				}
			} else {
				if got == nil {
					t.Errorf("LookupOpenCodeCatalog() = nil, want non-nil")
				} else if got.DisplayName != tt.want {
					t.Errorf("LookupOpenCodeCatalog().DisplayName = %q, want %q", got.DisplayName, tt.want)
				}
			}
		})
	}
}

func TestLookupOpenCodeCatalogEmpty(t *testing.T) {
	got := LookupOpenCodeCatalog(nil, "anything")
	if got != nil {
		t.Errorf("LookupOpenCodeCatalog(nil, ...) = %v, want nil", got)
	}
}

func TestOpenCodeCatalogToModel(t *testing.T) {
	pid := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	spec := &OpenCodeModelSpec{
		ModelID:               "test-model",
		DisplayName:           "Test Model",
		Description:           "A test model",
		ContextLength:         8192,
		MaxOutputTokens:       4096,
		Streaming:             true,
		Reasoning:             false,
		ToolCalling:           true,
		StructuredOutput:      false,
		Vision:                true,
		Modality:              "text",
		InputModalities:       "text,image",
		OutputModalities:      "text",
		InputPricePerMillion:  ptrFloat(3.0),
		OutputPricePerMillion: ptrFloat(15.0),
	}

	m := OpenCodeCatalogToModel(spec, pid, "opencode")

	if m.ModelID != "test-model" {
		t.Errorf("ModelID = %q, want %q", m.ModelID, "test-model")
	}
	if m.ProviderID != pid {
		t.Errorf("ProviderID = %v, want %v", m.ProviderID, pid)
	}
	if m.OwnedBy != "opencode" {
		t.Errorf("OwnedBy = %q, want %q", m.OwnedBy, "opencode")
	}
	if !m.Enabled {
		t.Errorf("Enabled = false, want true")
	}
	if m.Name != "test-model" {
		t.Errorf("Name = %q, want %q", m.Name, "test-model")
	}
	if m.DisplayName != "Test Model" {
		t.Errorf("DisplayName = %q, want %q", m.DisplayName, "Test Model")
	}
	if m.Modality != "text" {
		t.Errorf("Modality = %q, want %q", m.Modality, "text")
	}

	// Verify capabilities JSON
	var caps model.Capability
	if err := json.Unmarshal([]byte(m.Capabilities), &caps); err != nil {
		t.Fatalf("Failed to unmarshal capabilities: %v", err)
	}
	if !caps.Streaming {
		t.Errorf("Capabilities.Streaming = false, want true")
	}
	if !caps.Vision {
		t.Errorf("Capabilities.Vision = false, want true")
	}
	if !caps.ToolCalling {
		t.Errorf("Capabilities.ToolCalling = false, want true")
	}
	if caps.Reasoning {
		t.Errorf("Capabilities.Reasoning = true, want false")
	}

	// Verify prices
	if m.InputPricePerMillion == nil || *m.InputPricePerMillion != 3.0 {
		t.Errorf("InputPricePerMillion = %v, want 3.0", m.InputPricePerMillion)
	}
	if m.OutputPricePerMillion == nil || *m.OutputPricePerMillion != 15.0 {
		t.Errorf("OutputPricePerMillion = %v, want 15.0", m.OutputPricePerMillion)
	}

	// Verify context/output lengths
	if m.ContextLength == nil || *m.ContextLength != 8192 {
		t.Errorf("ContextLength = %v, want 8192", m.ContextLength)
	}
	if m.MaxOutputTokens == nil || *m.MaxOutputTokens != 4096 {
		t.Errorf("MaxOutputTokens = %v, want 4096", m.MaxOutputTokens)
	}
}

func TestOpenCodeCatalogToModelWithCacheHitPrice(t *testing.T) {
	pid := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	spec := &OpenCodeModelSpec{
		ModelID:                      "cached-model",
		DisplayName:                  "Cached",
		InputPricePerMillion:         ptrFloat(1.0),
		InputPricePerMillionCacheHit: ptrFloat(0.5),
		OutputPricePerMillion:        ptrFloat(2.0),
	}

	m := OpenCodeCatalogToModel(spec, pid, "xai")

	if m.InputPricePerMillionCacheHit == nil || *m.InputPricePerMillionCacheHit != 0.5 {
		t.Errorf("InputPricePerMillionCacheHit = %v, want 0.5", m.InputPricePerMillionCacheHit)
	}
}

func TestOpenCodeCatalogToModelNoCacheHitPrice(t *testing.T) {
	pid := uuid.MustParse("00000000-0000-0000-0000-000000000001")
	spec := &OpenCodeModelSpec{
		ModelID:               "no-cache-model",
		DisplayName:           "No Cache",
		InputPricePerMillion:  ptrFloat(1.0),
		OutputPricePerMillion: ptrFloat(2.0),
	}

	m := OpenCodeCatalogToModel(spec, pid, "opencode")

	if m.InputPricePerMillionCacheHit != nil {
		t.Errorf("InputPricePerMillionCacheHit = %v, want nil", m.InputPricePerMillionCacheHit)
	}
}

// A row that states no price is a metadata override: the model leaves the
// converter unpriced, so models.dev fills the price instead of the row
// pinning it at $0.
func TestOpenCodeCatalogToModel_NoPriceStaysUnpriced(t *testing.T) {
	spec := &OpenCodeModelSpec{ModelID: "metadata-only", DisplayName: "Metadata", ContextLength: 1000, MaxOutputTokens: 100}

	m := OpenCodeCatalogToModel(spec, uuid.New(), "xai")

	if m.InputPricePerMillion != nil || m.OutputPricePerMillion != nil || m.InputPricePerMillionCacheHit != nil {
		t.Errorf("prices = %v/%v/%v, want all nil", m.InputPricePerMillion, m.OutputPricePerMillion, m.InputPricePerMillionCacheHit)
	}
	if m.ContextLength == nil || *m.ContextLength != 1000 {
		t.Errorf("ContextLength = %v, want 1000", m.ContextLength)
	}
	if m.PriceSources != (model.PriceSources{}) {
		t.Errorf("PriceSources = %+v, want none for a row that states no price", m.PriceSources)
	}
}

// A price a catalog row states is recorded as the catalog's, and only that
// price: an absent one stays unsourced for models.dev to fill and stamp.
func TestOpenCodeCatalogToModel_StampsCatalogSource(t *testing.T) {
	spec := &OpenCodeModelSpec{ModelID: "m", InputPricePerMillionCacheHit: ptrFloat(30)}

	m := OpenCodeCatalogToModel(spec, uuid.New(), "openai")

	if want := (model.PriceSources{CacheHit: model.PriceSourceCatalog}); m.PriceSources != want {
		t.Errorf("PriceSources = %+v, want %+v", m.PriceSources, want)
	}
}

// A converted model owns its price pointers. Aliasing the row's fields would
// let a write through one discovered model rewrite the embedded catalog for
// every provider, for the life of the process.
func TestOpenCodeCatalogToModel_PricesNotAliased(t *testing.T) {
	spec := &OpenCodeModelSpec{ModelID: "m", InputPricePerMillion: ptrFloat(1), InputPricePerMillionCacheHit: ptrFloat(0.1), OutputPricePerMillion: ptrFloat(2)}

	m := OpenCodeCatalogToModel(spec, uuid.New(), "xai")
	*m.InputPricePerMillion, *m.InputPricePerMillionCacheHit, *m.OutputPricePerMillion = 999, 999, 999

	if *spec.InputPricePerMillion != 1 || *spec.InputPricePerMillionCacheHit != 0.1 || *spec.OutputPricePerMillion != 2 {
		t.Errorf("catalog row mutated through a discovered model: %v/%v/%v", *spec.InputPricePerMillion, *spec.InputPricePerMillionCacheHit, *spec.OutputPricePerMillion)
	}
}

func ptrFloat(v float64) *float64 { return &v }
