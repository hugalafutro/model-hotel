package provider

import (
	"testing"
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
