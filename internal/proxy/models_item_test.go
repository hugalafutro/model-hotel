package proxy

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
)

func ptrInt(v int) *int           { return &v }
func ptrFloat(v float64) *float64 { return &v }

func TestModelToOpenAIItem_BasicFields(t *testing.T) {
	ts := time.Date(2024, 6, 15, 12, 0, 0, 0, time.UTC)
	m := &model.Model{
		ModelID:      "gpt-4",
		ProviderName: "OpenAI",
		CreatedAt:    ts,
	}

	item := modelToOpenAIItem(m, "openai/gpt-4", "OpenAI")

	if item["id"] != "openai/gpt-4" {
		t.Errorf("Expected id='openai/gpt-4', got %v", item["id"])
	}
	if item["object"] != "model" {
		t.Errorf("Expected object='model', got %v", item["object"])
	}
	if item["created"] != ts.Unix() {
		t.Errorf("Expected created=%d, got %v", ts.Unix(), item["created"])
	}
	if item["owned_by"] != "OpenAI" {
		t.Errorf("Expected owned_by='OpenAI', got %v", item["owned_by"])
	}
	if item["provider"] != "OpenAI" {
		t.Errorf("Expected provider='OpenAI', got %v", item["provider"])
	}
}

func TestModelToOpenAIItem_OwnedByFallback(t *testing.T) {
	m := &model.Model{
		ModelID:      "test",
		OwnedBy:      "CustomOrg",
		ProviderName: "Provider",
		CreatedAt:    time.Now(),
	}
	item := modelToOpenAIItem(m, "p/test", "Provider")
	if item["owned_by"] != "CustomOrg" {
		t.Errorf("Expected owned_by='CustomOrg', got %v", item["owned_by"])
	}

	m.OwnedBy = ""
	item = modelToOpenAIItem(m, "p/test", "Provider")
	if item["owned_by"] != "Provider" {
		t.Errorf("Expected owned_by fallback to ProviderName, got %v", item["owned_by"])
	}
}

func TestModelToOpenAIItem_ContextLength(t *testing.T) {
	m := &model.Model{CreatedAt: time.Now()}
	item := modelToOpenAIItem(m, "id", "prov")
	if _, ok := item["context_length"]; ok {
		t.Error("Expected no context_length when nil")
	}

	m.ContextLength = ptrInt(128000)
	item = modelToOpenAIItem(m, "id", "prov")
	if item["context_length"] != 128000 {
		t.Errorf("Expected context_length=128000, got %v", item["context_length"])
	}
	if item["max_context_length"] != 128000 {
		t.Errorf("Expected max_context_length=128000, got %v", item["max_context_length"])
	}
}

func TestModelToOpenAIItem_MaxOutputTokens(t *testing.T) {
	m := &model.Model{CreatedAt: time.Now()}
	item := modelToOpenAIItem(m, "id", "prov")
	if _, ok := item["max_output_tokens"]; ok {
		t.Error("Expected no max_output_tokens when nil")
	}

	m.MaxOutputTokens = ptrInt(4096)
	item = modelToOpenAIItem(m, "id", "prov")
	if item["max_output_tokens"] != 4096 {
		t.Errorf("Expected max_output_tokens=4096, got %v", item["max_output_tokens"])
	}
}

func TestModelToOpenAIItem_DisplayNameAndName(t *testing.T) {
	m := &model.Model{CreatedAt: time.Now()}
	item := modelToOpenAIItem(m, "id", "prov")
	if _, ok := item["name"]; ok {
		t.Error("Expected no name when both DisplayName and Name are empty")
	}

	m.Name = "base-model"
	item = modelToOpenAIItem(m, "id", "prov")
	if item["name"] != "base-model" {
		t.Errorf("Expected name='base-model', got %v", item["name"])
	}

	m.DisplayName = "Pretty Name"
	item = modelToOpenAIItem(m, "id", "prov")
	if item["name"] != "Pretty Name" {
		t.Errorf("Expected name='Pretty Name' (DisplayName takes precedence), got %v", item["name"])
	}
}

func TestModelToOpenAIItem_Description(t *testing.T) {
	m := &model.Model{CreatedAt: time.Now()}
	item := modelToOpenAIItem(m, "id", "prov")
	if _, ok := item["description"]; ok {
		t.Error("Expected no description when empty")
	}

	m.Description = "A powerful model"
	item = modelToOpenAIItem(m, "id", "prov")
	if item["description"] != "A powerful model" {
		t.Errorf("Expected description set, got %v", item["description"])
	}
}

func TestModelToOpenAIItem_Modality(t *testing.T) {
	m := &model.Model{CreatedAt: time.Now()}
	item := modelToOpenAIItem(m, "id", "prov")
	if _, ok := item["modality"]; ok {
		t.Error("Expected no modality when empty")
	}

	m.Modality = "text+image"
	item = modelToOpenAIItem(m, "id", "prov")
	if item["modality"] != "text+image" {
		t.Errorf("Expected modality='text+image', got %v", item["modality"])
	}
}

func TestModelToOpenAIItem_Capabilities(t *testing.T) {
	m := &model.Model{CreatedAt: time.Now()}

	// Empty capabilities omitted
	item := modelToOpenAIItem(m, "id", "prov")
	if _, ok := item["capabilities"]; ok {
		t.Error("Expected no capabilities when empty")
	}

	// Empty JSON object omitted
	m.Capabilities = "{}"
	item = modelToOpenAIItem(m, "id", "prov")
	if _, ok := item["capabilities"]; ok {
		t.Error("Expected no capabilities when '{}''")
	}

	// Valid capabilities
	m.Capabilities = `{"tool_call":true,"vision":false}`
	item = modelToOpenAIItem(m, "id", "prov")
	caps, ok := item["capabilities"].(map[string]interface{})
	if !ok {
		t.Fatal("Expected capabilities to be map[string]interface{}")
	}
	if caps["tool_call"] != true {
		t.Errorf("Expected tool_call=true, got %v", caps["tool_call"])
	}

	// Invalid JSON silently omitted
	m.Capabilities = `{invalid}`
	item = modelToOpenAIItem(m, "id", "prov")
	if _, ok := item["capabilities"]; ok {
		t.Error("Expected no capabilities for invalid JSON")
	}
}

func TestModelToOpenAIItem_Modalities(t *testing.T) {
	m := &model.Model{CreatedAt: time.Now()}
	item := modelToOpenAIItem(m, "id", "prov")

	// Empty modalities omitted
	for _, key := range []string{"input_modalities", "output_modalities"} {
		if _, ok := item[key]; ok {
			t.Errorf("Expected no %s when empty", key)
		}
	}

	// Empty JSON array omitted
	m.InputModalities = "[]"
	m.OutputModalities = "[]"
	item = modelToOpenAIItem(m, "id", "prov")
	if _, ok := item["input_modalities"]; ok {
		t.Error("Expected no input_modalities when '[]'")
	}
	if _, ok := item["output_modalities"]; ok {
		t.Error("Expected no output_modalities when '[]'")
	}

	// Valid modalities
	m.InputModalities = `["text","image"]`
	m.OutputModalities = `["text"]`
	item = modelToOpenAIItem(m, "id", "prov")
	inMod, ok := item["input_modalities"].([]string)
	if !ok {
		t.Fatal("Expected input_modalities to be []string")
	}
	if len(inMod) != 2 || inMod[0] != "text" || inMod[1] != "image" {
		t.Errorf("Expected [text, image], got %v", inMod)
	}

	// Invalid JSON silently omitted
	m.InputModalities = `[invalid]`
	item = modelToOpenAIItem(m, "id", "prov")
	if _, ok := item["input_modalities"]; ok {
		t.Error("Expected no input_modalities for invalid JSON")
	}
}

func TestModelToOpenAIItem_Pricing(t *testing.T) {
	m := &model.Model{CreatedAt: time.Now()}
	item := modelToOpenAIItem(m, "id", "prov")
	if _, ok := item["input_price_per_million"]; ok {
		t.Error("Expected no input_price_per_million when nil")
	}
	if _, ok := item["output_price_per_million"]; ok {
		t.Error("Expected no output_price_per_million when nil")
	}

	m.InputPricePerMillion = ptrFloat(3.0)
	m.OutputPricePerMillion = ptrFloat(15.0)
	item = modelToOpenAIItem(m, "id", "prov")
	if item["input_price_per_million"] != 3.0 {
		t.Errorf("Expected input_price_per_million=3.0, got %v", item["input_price_per_million"])
	}
	if item["output_price_per_million"] != 15.0 {
		t.Errorf("Expected output_price_per_million=15.0, got %v", item["output_price_per_million"])
	}
}

func TestModelToOpenAIItem_FullModel(t *testing.T) {
	ts := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	m := &model.Model{
		ID:                    uuid.MustParse("00000000-0000-0000-0000-000000000001"),
		ModelID:               "claude-3-opus",
		Name:                  "claude-3-opus-20240229",
		DisplayName:           "Claude 3 Opus",
		Description:           "Most capable model",
		OwnedBy:               "Anthropic",
		ProviderName:          "Anthropic",
		Modality:              "text+image",
		Capabilities:          `{"tool_call":true}`,
		InputModalities:       `["text","image"]`,
		OutputModalities:      `["text"]`,
		ContextLength:         ptrInt(200000),
		MaxOutputTokens:       ptrInt(4096),
		InputPricePerMillion:  ptrFloat(15.0),
		OutputPricePerMillion: ptrFloat(75.0),
		CreatedAt:             ts,
	}

	item := modelToOpenAIItem(m, "anthropic/claude-3-opus", "Anthropic")

	if item["id"] != "anthropic/claude-3-opus" {
		t.Errorf("id mismatch: %v", item["id"])
	}
	if item["name"] != "Claude 3 Opus" {
		t.Errorf("name should prefer DisplayName: %v", item["name"])
	}
	if item["owned_by"] != "Anthropic" {
		t.Errorf("owned_by mismatch: %v", item["owned_by"])
	}
	if item["context_length"] != 200000 {
		t.Errorf("context_length mismatch: %v", item["context_length"])
	}
	if item["max_output_tokens"] != 4096 {
		t.Errorf("max_output_tokens mismatch: %v", item["max_output_tokens"])
	}
	if item["input_price_per_million"] != 15.0 {
		t.Errorf("input_price mismatch: %v", item["input_price_per_million"])
	}
}
