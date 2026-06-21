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

// TestLiveModelStub_ValidJSONBFields guards against the regression where a stub
// left JSONB columns (capabilities, params, modalities) as "" — invalid JSON
// that fails the DB upsert when neither the catalog nor models.dev backfill them.
func TestLiveModelStub_ValidJSONBFields(t *testing.T) {
	m := liveModelStub("whisper-1", "openai", uuid.New())
	for name, field := range map[string]string{
		"capabilities":      m.Capabilities,
		"params":            m.Params,
		"input_modalities":  m.InputModalities,
		"output_modalities": m.OutputModalities,
	} {
		if !json.Valid([]byte(field)) {
			t.Errorf("%s = %q is not valid JSON (would break JSONB upsert)", name, field)
		}
	}
	if m.InputModalities != "[]" || m.OutputModalities != "[]" {
		t.Errorf("modalities should default to empty arrays, got in=%q out=%q", m.InputModalities, m.OutputModalities)
	}
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

// TestMergeLiveAndCatalog_LiveMetaProvenance verifies the per-field source
// flags: a value the provider payload supplied is marked live (it overwrites on
// upsert), while a value the catalog backfilled is left non-live (fill-only, so
// a degraded later scan can't flip it).
func TestMergeLiveAndCatalog_LiveMetaProvenance(t *testing.T) {
	pid := uuid.New()
	live := []*model.Model{{
		ProviderID:           pid,
		ModelID:              "glm-5.1",
		Name:                 "glm-5.1",
		Capabilities:         "{}",
		InputModalities:      "[]",
		InputPricePerMillion: floatPtr(1.5), // provider-reported -> live
		ContextLength:        nil,           // catalog will backfill -> NOT live
	}}
	catalog := []*model.Model{{
		ProviderID:           pid,
		ModelID:              "glm-5.1",
		ContextLength:        intPtr(200000),
		InputPricePerMillion: floatPtr(99),
	}}

	out := mergeLiveAndCatalog(live, catalog)
	m := out[0]
	if !m.LiveMeta.InputPrice {
		t.Error("InputPrice came from the provider payload; want LiveMeta.InputPrice=true")
	}
	if m.LiveMeta.ContextLength {
		t.Error("ContextLength was catalog-backfilled; want LiveMeta.ContextLength=false")
	}
}

// TestMergeLiveAndCatalog_CatalogOnlyIsNotLive verifies a catalog-only model
// (no live match) carries no live flags, so its curated pricing/context stays
// fill-only rather than masquerading as provider-reported.
func TestMergeLiveAndCatalog_CatalogOnlyIsNotLive(t *testing.T) {
	pid := uuid.New()
	live := []*model.Model{{ProviderID: pid, ModelID: "glm-5.1", Name: "glm-5.1"}}
	catalog := []*model.Model{
		{ProviderID: pid, ModelID: "glm-5.1", Name: "glm-5.1"},
		{ProviderID: pid, ModelID: "glm-5.2", Name: "glm-5.2",
			ContextLength: intPtr(200000), InputPricePerMillion: floatPtr(2)},
	}

	out := mergeLiveAndCatalog(live, catalog)
	for _, m := range out {
		if m.ModelID == "glm-5.2" && (m.LiveMeta.ContextLength || m.LiveMeta.InputPrice) {
			t.Errorf("catalog-only model must stay non-live, got %+v", m.LiveMeta)
		}
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

// TestBackfillFromCatalog_NilSrcIsNoop guards the nil-catalog-match guard: a
// live model with no catalog counterpart must be returned untouched.
func TestBackfillFromCatalog_NilSrcIsNoop(t *testing.T) {
	dst := &model.Model{ModelID: "m", Name: "Live Name"}
	backfillFromCatalog(dst, nil)
	if dst.Name != "Live Name" {
		t.Errorf("nil src must not mutate dst, got Name=%q", dst.Name)
	}
}

// TestBackfillFromCatalog_FillsEmptyTextFields verifies the catalog fills the
// empty Name/Description/OwnedBy of a sparse live model without overwriting the
// values the live API did provide.
func TestBackfillFromCatalog_FillsEmptyTextFields(t *testing.T) {
	dst := &model.Model{
		ModelID:     "glm-5.2",
		Name:        "", // empty -> catalog wins
		Description: "", // empty -> catalog wins
		OwnedBy:     "zai",
	}
	src := &model.Model{
		ModelID:     "glm-5.2",
		Name:        "GLM 5.2",
		Description: "Flagship model",
		OwnedBy:     "should-not-overwrite",
	}
	backfillFromCatalog(dst, src)

	if dst.Name != "GLM 5.2" {
		t.Errorf("empty Name should be backfilled, got %q", dst.Name)
	}
	if dst.Description != "Flagship model" {
		t.Errorf("empty Description should be backfilled, got %q", dst.Description)
	}
	if dst.OwnedBy != "zai" {
		t.Errorf("non-empty OwnedBy must be preserved, got %q", dst.OwnedBy)
	}
}

// TestBackfillLiveFromCatalog_EmptyCatalogReturnsLive covers the early return
// for an empty catalog: the live slice is handed back unchanged (and its live
// meta still flagged), with no nil-map dereference.
func TestBackfillLiveFromCatalog_EmptyCatalogReturnsLive(t *testing.T) {
	pid := uuid.New()
	live := []*model.Model{{ProviderID: pid, ModelID: "m1", InputPricePerMillion: floatPtr(3)}}

	got := backfillLiveFromCatalog(live, nil)

	if len(got) != 1 || got[0].ModelID != "m1" {
		t.Fatalf("empty catalog should return live unchanged, got %+v", got)
	}
	// markLiveMeta ran before the early return, so the provider-set price is live.
	if !got[0].LiveMeta.InputPrice {
		t.Error("live-set price should be flagged live even with an empty catalog")
	}
}

// TestIsEmptyModalities covers both the empty-sentinel set and a real payload,
// which must report false so a genuine modalities value is never discarded.
func TestIsEmptyModalities(t *testing.T) {
	for _, s := range []string{"", " ", "[]", "{}", "null"} {
		if !isEmptyModalities(s) {
			t.Errorf("isEmptyModalities(%q) = false, want true", s)
		}
	}
	if isEmptyModalities(`["text","image"]`) {
		t.Error(`isEmptyModalities(["text","image"]) = true, want false`)
	}
}
