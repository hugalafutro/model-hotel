package provider

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// intPtr is defined in discovery_nanogpt_test.go (same package).

func floatPtr(v float64) *float64 { return &v }

func capsJSON(c model.Capability) string {
	b, _ := json.Marshal(c)
	return string(b)
}

func TestMergeLiveAndCatalog_LiveWinsCatalogBackfills(t *testing.T) {
	pid := uuid.New()
	live := []*model.Model{{
		ProviderID:           pid,
		ModelID:              "glm-5.1",
		Name:                 "glm-5.1",
		DisplayName:          "glm-5.1", // placeholder == model_id
		Capabilities:         "{}",
		InputModalities:      "[]",
		ContextLength:        nil, // live omitted -> catalog should fill
		InputPricePerMillion: floatPtr(1.5),
	}}
	catalog := []*model.Model{{
		ProviderID:           pid,
		ModelID:              "glm-5.1",
		DisplayName:          "GLM 5.1",
		Description:          "Zhipu flagship",
		Modality:             "text",
		InputModalities:      `["text"]`,
		ContextLength:        intPtr(200000),
		MaxOutputTokens:      intPtr(131072),
		InputPricePerMillion: floatPtr(99), // live already has a value -> must NOT win
		Capabilities:         capsJSON(model.Capability{Streaming: true, Reasoning: true, ToolCalling: true}),
	}}

	out := mergeLiveAndCatalog(live, catalog)
	if len(out) != 1 {
		t.Fatalf("expected 1 merged model, got %d", len(out))
	}
	m := out[0]

	// Catalog backfilled the gaps.
	if m.ContextLength == nil || *m.ContextLength != 200000 {
		t.Errorf("ContextLength: want 200000 from catalog, got %v", m.ContextLength)
	}
	if m.MaxOutputTokens == nil || *m.MaxOutputTokens != 131072 {
		t.Errorf("MaxOutputTokens: want 131072 from catalog, got %v", m.MaxOutputTokens)
	}
	if m.Description != "Zhipu flagship" {
		t.Errorf("Description: want catalog value, got %q", m.Description)
	}
	if m.DisplayName != "GLM 5.1" {
		t.Errorf("DisplayName: placeholder should yield to catalog, got %q", m.DisplayName)
	}
	if m.InputModalities != `["text"]` {
		t.Errorf("InputModalities: empty live should yield to catalog, got %q", m.InputModalities)
	}

	// Live value must win where it was present.
	if m.InputPricePerMillion == nil || *m.InputPricePerMillion != 1.5 {
		t.Errorf("InputPricePerMillion: live 1.5 must win, got %v", m.InputPricePerMillion)
	}

	// Capabilities are OR-merged.
	var caps model.Capability
	if err := json.Unmarshal([]byte(m.Capabilities), &caps); err != nil {
		t.Fatalf("bad capabilities json: %v", err)
	}
	if !caps.Streaming || !caps.Reasoning || !caps.ToolCalling {
		t.Errorf("capabilities should be OR-merged, got %+v", caps)
	}
}

func TestMergeLiveAndCatalog_UnionsCatalogOnlyModels(t *testing.T) {
	pid := uuid.New()
	live := []*model.Model{{ProviderID: pid, ModelID: "glm-5.1", Name: "glm-5.1"}}
	catalog := []*model.Model{
		{ProviderID: pid, ModelID: "glm-5.1", Name: "glm-5.1"},
		{ProviderID: pid, ModelID: "glm-5.2", Name: "glm-5.2", ContextLength: intPtr(200000)}, // not in live yet
	}

	out := mergeLiveAndCatalog(live, catalog)
	if len(out) != 2 {
		t.Fatalf("expected union of 2 models, got %d", len(out))
	}
	var found bool
	for _, m := range out {
		if m.ModelID == "glm-5.2" {
			found = true
		}
	}
	if !found {
		t.Error("catalog-only model glm-5.2 should be unioned into the result")
	}
}

func TestMergeLiveAndCatalog_LiveOnlyPassesThrough(t *testing.T) {
	pid := uuid.New()
	live := []*model.Model{{ProviderID: pid, ModelID: "brand-new-model", Name: "brand-new-model"}}
	out := mergeLiveAndCatalog(live, nil)
	if len(out) != 1 || out[0].ModelID != "brand-new-model" {
		t.Fatalf("live-only model should pass through, got %+v", out)
	}
}

func TestMergeLiveAndCatalog_CaseInsensitiveMatch(t *testing.T) {
	pid := uuid.New()
	live := []*model.Model{{ProviderID: pid, ModelID: "GLM-5.1", Name: "GLM-5.1"}}
	catalog := []*model.Model{{ProviderID: pid, ModelID: "glm-5.1", ContextLength: intPtr(200000)}}

	out := mergeLiveAndCatalog(live, catalog)
	if len(out) != 1 {
		t.Fatalf("case-insensitive ids should merge to 1 model, got %d", len(out))
	}
	if out[0].ContextLength == nil || *out[0].ContextLength != 200000 {
		t.Errorf("catalog backfill should apply across case difference, got %v", out[0].ContextLength)
	}
}

func TestMergeLiveAndCatalog_DedupesLive(t *testing.T) {
	pid := uuid.New()
	live := []*model.Model{
		{ProviderID: pid, ModelID: "glm-5.1", Name: "first"},
		{ProviderID: pid, ModelID: "glm-5.1", Name: "dup"},
	}
	out := mergeLiveAndCatalog(live, nil)
	if len(out) != 1 {
		t.Fatalf("duplicate live ids should collapse, got %d", len(out))
	}
	if out[0].Name != "first" {
		t.Errorf("first live entry should win, got %q", out[0].Name)
	}
}
