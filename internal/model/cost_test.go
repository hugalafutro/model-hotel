package model

import (
	"math"
	"testing"
)

func TestCostUSD(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	priced := &Model{InputPricePerMillion: f(1), InputPricePerMillionCacheHit: f(0.1), OutputPricePerMillion: f(4)}
	noCache := &Model{InputPricePerMillion: f(1), OutputPricePerMillion: f(4)}
	free := &Model{InputPricePerMillion: f(0), OutputPricePerMillion: f(0)}

	for _, tc := range []struct {
		name string
		m    *Model
		u    Usage
		want float64
		ok   bool
	}{
		{"nil model", nil, Usage{Prompt: 10}, 0, false},
		{"no input price", &Model{OutputPricePerMillion: f(1)}, Usage{Prompt: 10}, 0, false},
		{"no output price", &Model{InputPricePerMillion: f(1)}, Usage{Prompt: 10}, 0, false},
		{"free model prices to zero", free, Usage{Prompt: 1000, Completion: 1000}, 0, true},
		{"no cache split", priced, Usage{Prompt: 1_000_000, Completion: 500_000}, 1 + 2, true},
		{"cache hit takes the cache price", priced, Usage{Prompt: 1_000_000, PromptCacheHit: 800_000, PromptCacheMiss: 200_000}, 0.08 + 0.2, true},
		// A walked group's rejected prompts sit outside the serving candidate's
		// split; they take the input price rather than pricing to nothing.
		{"prompt beyond the cache split", priced, Usage{Prompt: 1_500_000, PromptCacheHit: 800_000, PromptCacheMiss: 200_000}, 0.08 + 0.2 + 0.5, true},
		// A split with no cache-hit price falls back to the whole prompt at the
		// input price rather than pricing the hit tokens at nothing.
		{"cache hit without a cache price", noCache, Usage{Prompt: 1_000_000, PromptCacheHit: 800_000, PromptCacheMiss: 200_000}, 1, true},
		{"nothing charged", priced, Usage{}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.m.CostUSD(tc.u)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if math.Abs(got-tc.want) > 1e-12 {
				t.Errorf("cost = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCostUSD_RefusesUnpriceablePrices: a listing can state "NaN", "Inf" or a
// negative price and a parser that only checks for a parse error stores it.
// Such a model is unpriced, so the row stays NULL rather than carrying a cost
// the budget cannot compare or one that pays the spender.
func TestCostUSD_RefusesUnpriceablePrices(t *testing.T) {
	one := 1.0
	for name, bad := range map[string]float64{"nan": math.NaN(), "inf": math.Inf(1), "negative": -0.5} {
		b := bad
		for _, m := range []*Model{
			{InputPricePerMillion: &b, OutputPricePerMillion: &one},
			{InputPricePerMillion: &one, OutputPricePerMillion: &b},
			{InputPricePerMillion: &one, OutputPricePerMillion: &one, InputPricePerMillionCacheHit: &b},
		} {
			if _, ok := m.CostUSD(Usage{Prompt: 10, PromptCacheHit: 4, PromptCacheMiss: 6, Completion: 5}); ok {
				t.Errorf("%s price on %+v: priced, want unpriced", name, m)
			}
		}
	}
	if _, ok := (&Model{InputPricePerMillion: &one, OutputPricePerMillion: &one}).CostUSD(Usage{Prompt: 1, Completion: 1}); !ok {
		t.Error("a finite non-negative price must still price")
	}
}
