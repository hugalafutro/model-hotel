package provider

import (
	"testing"
)

// TestLoadCatalog_ValidJSON verifies that loadCatalog successfully parses
// a known embedded JSON file into the expected Go type.
func TestLoadCatalog_ValidJSON(t *testing.T) {
	catalog := loadCatalog[[]OpenCodeModelSpec]("opencode_go.json")
	if len(catalog) == 0 {
		t.Error("opencode_go.json should contain at least one entry")
	}
	first := catalog[0]
	if first.ModelID == "" {
		t.Error("first entry should have a non-empty ModelID")
	}
	if first.ContextLength <= 0 {
		t.Errorf("first entry (%s): ContextLength = %d, want > 0", first.ModelID, first.ContextLength)
	}
}

// TestLoadCatalog_AllCatalogsParse verifies every embedded JSON catalog
// parses without panicking and returns a non-empty result.
func TestLoadCatalog_AllCatalogsParse(t *testing.T) {
	type testCase struct {
		name string
		fn   func() int
	}
	cases := []testCase{
		{"opencode_go", func() int { return len(loadCatalog[[]OpenCodeModelSpec]("opencode_go.json")) }},
		{"opencode_zen", func() int { return len(loadCatalog[[]OpenCodeModelSpec]("opencode_zen.json")) }},
		{"xai", func() int { return len(loadCatalog[[]OpenCodeModelSpec]("xai.json")) }},
		{"zai", func() int { return len(loadCatalog[[]ZAICodingModelSpec]("zai.json")) }},
		{"deepseek", func() int { return len(loadCatalog[[]DeepSeekModelSpec]("deepseek.json")) }},
		{"openai", func() int { return len(loadCatalog[[]OpenAIModelSpec]("openai.json")) }},
		{"anthropic", func() int { return len(loadCatalog[[]AnthropicPricingSpec]("anthropic.json")) }},
		{"google", func() int { return len(loadCatalog[[]GoogleModelPricing]("google.json")) }},
		{"cohere", func() int { return len(loadCatalog[[]CoherePricingEntry]("cohere.json")) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := tc.fn()
			if n == 0 {
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
