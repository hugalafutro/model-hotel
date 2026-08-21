package provider

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// TestLoadCatalog_ValidJSON verifies that loadCatalog successfully parses
// a known embedded JSON file into the expected Go type.
func TestLoadCatalog_ValidJSON(t *testing.T) {
	catalog := loadCatalog[[]OpenCodeModelSpec]("opencode_zen.json")
	if len(catalog) == 0 {
		t.Error("opencode_zen.json should contain at least one entry")
	}
	first := catalog[0]
	if first.ModelID == "" {
		t.Error("first entry should have a non-empty ModelID")
	}
	if first.ContextLength <= 0 {
		t.Errorf("first entry (%s): ContextLength = %d, want > 0", first.ModelID, first.ContextLength)
	}
}

// TestLoadCatalog_AllCatalogsParse verifies every embedded JSON catalog parses
// without panicking.
//
// Only the catalogs that carry models the live listing cannot supply on its own
// are required to be non-empty. The pricing catalogs are OVERRIDE channels: a
// row that merely restated models.dev has been deleted, because the catalog
// wins over models.dev and so a stale duplicate silently overrides correct data
// (xAI's retired models metered 6x wrong that way until an audit caught it). An
// empty pricing catalog therefore means "models.dev is right about everything
// here", which is the healthy state, not a missing file.
func TestLoadCatalog_AllCatalogsParse(t *testing.T) {
	type testCase struct {
		name         string
		fn           func() int
		mustHaveRows bool
	}
	cases := []testCase{
		// Union / probe catalogs: emptying these would lose models outright.
		// opencode_zen must keep its zero-priced free-model rows — the keyless
		// discovery path can only surface free models the catalog identifies.
		{"opencode_zen", func() int { return len(loadCatalog[[]OpenCodeModelSpec]("opencode_zen.json")) }, true},
		{"xai", func() int { return len(loadCatalog[[]OpenCodeModelSpec]("xai.json")) }, true},
		{"zai", func() int { return len(loadCatalog[[]ZAICodingModelSpec]("zai.json")) }, true},
		{"deepseek", func() int { return len(loadCatalog[[]DeepSeekModelSpec]("deepseek.json")) }, true},
		{"openai", func() int { return len(loadCatalog[[]OpenAIModelSpec]("openai.json")) }, true},
		// Pricing overrides: legitimately empty.
		// opencode_go has a live /models listing and full models.dev coverage;
		// its rows exist only to override either of those when they drift.
		{"opencode_go", func() int { return len(loadCatalog[[]OpenCodeModelSpec]("opencode_go.json")) }, false},
		{"anthropic", func() int { return len(loadCatalog[[]AnthropicPricingSpec]("anthropic.json")) }, false},
		{"google", func() int { return len(loadCatalog[[]GoogleModelPricing]("google.json")) }, false},
		{"cohere", func() int { return len(loadCatalog[[]CoherePricingEntry]("cohere.json")) }, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := tc.fn()
			if tc.mustHaveRows && n == 0 {
				t.Errorf("%s catalog should have at least one entry", tc.name)
			}
		})
	}
}

// TestLoadCatalog_ModalityStringsParse checks every embedded catalog row whose
// modalities are carried as an embedded JSON-array string.
//
// Nothing validates those at load time: loadCatalog only unmarshals the outer
// struct, so `[text,image]` survives all the way to NormalizeModels, where
// parseModalityList fails silently and the row degrades to text-only. No crash,
// no log, no failing test — which is why this asserts the shape directly.
func TestLoadCatalog_ModalityStringsParse(t *testing.T) {
	check := func(t *testing.T, id, field, raw string) {
		t.Helper()
		if raw == "" {
			return
		}
		var mods []string
		if err := json.Unmarshal([]byte(raw), &mods); err != nil {
			t.Errorf("%s %s = %q, which is not a JSON string array: %v", id, field, raw, err)
		}
	}
	for _, file := range []string{"opencode_zen.json", "opencode_go.json", "xai.json"} {
		for _, e := range loadCatalog[[]OpenCodeModelSpec](file) {
			check(t, file+"/"+e.ModelID, "input_modalities", e.InputModalities)
			check(t, file+"/"+e.ModelID, "output_modalities", e.OutputModalities)
		}
	}
	for _, e := range loadCatalog[[]OpenAIModelSpec]("openai.json") {
		check(t, "openai.json/"+e.ModelID, "input_modalities", e.InputModalities)
		check(t, "openai.json/"+e.ModelID, "output_modalities", e.OutputModalities)
	}
	for _, e := range loadCatalog[[]DeepSeekModelSpec]("deepseek.json") {
		check(t, "deepseek.json/"+e.ModelID, "input_modalities", e.InputModalities)
	}
}

// TestLoadCatalog_InvalidPath panics on missing file.
func TestLoadCatalog_InvalidPath(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("loadCatalog with invalid path should panic")
		}
	}()
	loadCatalog[[]OpenCodeModelSpec]("nonexistent.json")
}

// Note: Invalid JSON panic paths cannot be tested because embed.FS
// contents are fixed at compile time. The panic-on-read-error path
// is covered by TestLoadCatalog_InvalidPath above.

func TestLoadCatalog_DeepSeekCatalog(t *testing.T) {
	catalog := loadCatalog[[]DeepSeekModelSpec]("deepseek.json")
	if len(catalog) == 0 {
		t.Error("deepseek.json should contain at least one entry")
	}

	first := catalog[0]
	if first.ModelID == "" {
		t.Error("first entry should have a non-empty ModelID")
	}
	if first.ContextLength <= 0 {
		t.Errorf("first entry (%s): ContextLength = %d, want > 0", first.ModelID, first.ContextLength)
	}
}

// TestDeepSeekCatalog_VisionModel covers the one DeepSeek model models.dev has
// never heard of. Without a row here it discovers as a bare stub: no price (so
// it meters at zero), no context window, and no vision flag despite being the
// only DeepSeek model that accepts an image.
func TestDeepSeekCatalog_VisionModel(t *testing.T) {
	spec := GetDeepSeekModelSpec("deepseek-v4-flash-vision-exp")
	if spec == nil {
		t.Fatal("deepseek.json must carry deepseek-v4-flash-vision-exp: models.dev has no entry for it")
	}
	if !spec.Vision {
		t.Error("the vision model must declare Vision")
	}
	if spec.InputPricePerMillionCacheMiss <= 0 || spec.OutputPricePerMillion <= 0 {
		t.Error("the vision model must carry prices, or it meters at zero")
	}

	m := deepseekSpecToModel(spec, uuid.New())
	if m.InputModalities != `["text","image"]` {
		t.Errorf("InputModalities = %s, want [\"text\",\"image\"]", m.InputModalities)
	}
	var caps model.Capability
	if err := json.Unmarshal([]byte(m.Capabilities), &caps); err != nil {
		t.Fatalf("unmarshal capabilities: %v", err)
	}
	if !caps.Vision {
		t.Error("Vision capability should reach the model")
	}
}

// TestDeepSeekCatalog_Prices pins DeepSeek's published off-peak rates. Nothing
// else can catch a typo here: models.dev still carries the pre-V4 figures, so a
// diff against it reports every one of these rows as diverging by design.
func TestDeepSeekCatalog_Prices(t *testing.T) {
	// cache-hit / cache-miss / output, dollars per million tokens.
	want := map[string][3]float64{
		"deepseek-chat":                {0.007, 0.22, 0.66},
		"deepseek-reasoner":            {0.007, 0.22, 0.66},
		"deepseek-v4-flash":            {0.007, 0.22, 0.66},
		"deepseek-v4-flash-vision-exp": {0.007, 0.22, 0.66},
		"deepseek-v4-pro":              {0.022, 0.66, 1.98},
	}
	catalog := GetDeepSeekModels()
	if len(catalog) != len(want) {
		t.Errorf("catalog has %d rows, want %d", len(catalog), len(want))
	}
	for id, w := range want {
		spec := GetDeepSeekModelSpec(id)
		if spec == nil {
			t.Errorf("deepseek.json is missing %q", id)
			continue
		}
		got := [3]float64{
			spec.InputPricePerMillionCacheHit,
			spec.InputPricePerMillionCacheMiss,
			spec.OutputPricePerMillion,
		}
		if got != w {
			t.Errorf("%s prices = %v, want %v (cache-hit/cache-miss/output)", id, got, w)
		}
	}
}

// TestDeepSeekCatalog_ThinkingModes pins which rows report reasoning. DeepSeek
// V4 models default to thinking mode, and deepseek-chat is the one alias that
// selects the non-thinking preset on the same underlying deepseek-v4-flash.
// Verified against the live API: a bare "say hi" to deepseek-v4-flash returns
// reasoning_content with 16 reasoning tokens, the same call to deepseek-chat
// returns none.
func TestDeepSeekCatalog_ThinkingModes(t *testing.T) {
	want := map[string]bool{
		"deepseek-chat":                false,
		"deepseek-reasoner":            true,
		"deepseek-v4-flash":            true,
		"deepseek-v4-pro":              true,
		"deepseek-v4-flash-vision-exp": true,
	}
	for id, reasoning := range want {
		spec := GetDeepSeekModelSpec(id)
		if spec == nil {
			t.Errorf("deepseek.json is missing %q", id)
			continue
		}
		if spec.Reasoning != reasoning {
			t.Errorf("%s: Reasoning = %v, want %v", id, spec.Reasoning, reasoning)
		}
		m := deepseekSpecToModel(spec, uuid.New())
		var caps model.Capability
		if err := json.Unmarshal([]byte(m.Capabilities), &caps); err != nil {
			t.Fatalf("%s: unmarshal capabilities: %v", id, err)
		}
		if caps.Reasoning != reasoning {
			t.Errorf("%s: capability Reasoning = %v, want %v", id, caps.Reasoning, reasoning)
		}
	}
}

// TestDeepSeekCatalog_DefaultsToTextOnly guards the other side of that field:
// every other DeepSeek model omits input_modalities and must stay text-only.
func TestDeepSeekCatalog_DefaultsToTextOnly(t *testing.T) {
	spec := GetDeepSeekModelSpec("deepseek-chat")
	if spec == nil {
		t.Fatal("deepseek.json must keep deepseek-chat: the live listing does not return it")
	}
	m := deepseekSpecToModel(spec, uuid.New())
	if m.InputModalities != `["text"]` {
		t.Errorf("InputModalities = %s, want [\"text\"]", m.InputModalities)
	}
}

func TestLoadCatalog_XAICatalog(t *testing.T) {
	catalog := loadCatalog[[]OpenCodeModelSpec]("xai.json")
	if len(catalog) == 0 {
		t.Error("xai.json should contain at least one entry")
	}

	first := catalog[0]
	if first.ModelID == "" {
		t.Error("first entry should have a non-empty ModelID")
	}
	if first.ContextLength <= 0 {
		t.Errorf("first entry (%s): ContextLength = %d, want > 0", first.ModelID, first.ContextLength)
	}
}

func TestLoadCatalog_ZAICatalog(t *testing.T) {
	catalog := loadCatalog[[]ZAICodingModelSpec]("zai.json")
	if len(catalog) == 0 {
		t.Error("zai.json should contain at least one entry")
	}

	first := catalog[0]
	if first.ModelID == "" {
		t.Error("first entry should have a non-empty ModelID")
	}
	if first.ContextLength <= 0 {
		t.Errorf("first entry (%s): ContextLength = %d, want > 0", first.ModelID, first.ContextLength)
	}
}

// google.json is an override channel and is legitimately empty while models.dev
// is correct about every Google model, so this validates the shape of whatever
// rows exist rather than demanding rows exist.
func TestLoadCatalog_GooglePricingCatalog(t *testing.T) {
	for _, e := range loadCatalog[[]GoogleModelPricing]("google.json") {
		if e.ModelID == "" {
			t.Error("every entry should have a non-empty ModelID")
		}
	}
}

// anthropic.json is an override channel; see TestLoadCatalog_GooglePricingCatalog.
func TestLoadCatalog_AnthropicPricingCatalog(t *testing.T) {
	for _, e := range loadCatalog[[]AnthropicPricingSpec]("anthropic.json") {
		if e.ModelID == "" {
			t.Error("every entry should have a non-empty ModelID")
		}
	}
}

// cohere.json keeps only the models models.dev has no price for.
func TestLoadCatalog_CoherePricingCatalog(t *testing.T) {
	for _, e := range loadCatalog[[]CoherePricingEntry]("cohere.json") {
		if e.ModelID == "" {
			t.Error("every entry should have a non-empty ModelID")
		}
	}
}
