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
		InputPricePerMillion:  3.0,
		OutputPricePerMillion: 15.0,
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
		InputPricePerMillion:         1.0,
		InputPricePerMillionCacheHit: 0.5,
		OutputPricePerMillion:        2.0,
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
		InputPricePerMillion:  1.0,
		OutputPricePerMillion: 2.0,
	}

	m := OpenCodeCatalogToModel(spec, pid, "opencode")

	if m.InputPricePerMillionCacheHit != nil {
		t.Errorf("InputPricePerMillionCacheHit = %v, want nil", m.InputPricePerMillionCacheHit)
	}
}
