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
		// Cache writes are a subset of the misses: priced once as misses, plus
		// the write premium (Anthropic's 1.25x) on the written share only.
		{"cache write adds the premium", priced, Usage{Prompt: 1_000_000, PromptCacheHit: 800_000, PromptCacheMiss: 200_000, PromptCacheWrite: 200_000}, 0.08 + 0.2 + 0.05, true},
		// A first request writes without reading: no split, the whole prompt at
		// the input price, plus the premium on the written part.
		{"cache write without a read", priced, Usage{Prompt: 1_000_000, PromptCacheWrite: 400_000}, 1 + 0.1, true},
		// The premium is capped by the prompt: a write figure beyond it (a
		// provider bug) cannot bill more than the prompt was.
		{"cache write beyond the prompt", priced, Usage{Prompt: 1_000_000, PromptCacheWrite: 5_000_000}, 1 + 0.25, true},
		// And by the non-hit prompt: a write beyond the misses never puts the
		// premium on tokens priced at the cache-hit rate.
		{"cache write beyond the misses", priced, Usage{Prompt: 1_000_000, PromptCacheHit: 800_000, PromptCacheMiss: 200_000, PromptCacheWrite: 900_000}, 0.08 + 0.2 + 0.05, true},
		{"negative cache write is ignored", priced, Usage{Prompt: 1_000_000, PromptCacheWrite: -7}, 1, true},
		// A 1-hour write takes 2x: the 1-hour share at the full premium, the
		// rest at 1.25x. 300k at +100% and 100k at +25% of $1.
		{"1-hour cache write takes its own premium", priced, Usage{Prompt: 1_000_000, PromptCacheWrite: 400_000, PromptCacheWrite1h: 300_000}, 1 + 0.3 + 0.025, true},
		// The 1-hour share is capped by the write it is part of.
		{"1-hour share beyond the write", priced, Usage{Prompt: 1_000_000, PromptCacheWrite: 400_000, PromptCacheWrite1h: 900_000}, 1 + 0.4, true},
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
		} {
			if _, ok := m.CostUSD(Usage{Prompt: 10, PromptCacheHit: 4, PromptCacheMiss: 6, Completion: 5}); ok {
				t.Errorf("%s price on %+v: priced, want unpriced", name, m)
			}
		}
		// A bad cache-hit price counts as absent: the hits take the input price.
		m := &Model{InputPricePerMillion: &one, OutputPricePerMillion: &one, InputPricePerMillionCacheHit: &b}
		if cost, ok := m.CostUSD(Usage{Prompt: 10, PromptCacheHit: 4, PromptCacheMiss: 6, Completion: 5}); !ok || cost != 15/1e6 {
			t.Errorf("%s cache-hit price: got cost=%v ok=%v, want 15e-6 priced at the input price", name, cost, ok)
		}
	}
	if _, ok := (&Model{InputPricePerMillion: &one, OutputPricePerMillion: &one}).CostUSD(Usage{Prompt: 1, Completion: 1}); !ok {
		t.Error("a finite non-negative price must still price")
	}
	huge := math.MaxFloat64
	if _, ok := (&Model{InputPricePerMillion: &huge, OutputPricePerMillion: &one}).CostUSD(Usage{Prompt: 2, Completion: 1}); ok {
		t.Error("a product that overflows to Inf must read as unpriced")
	}
}

func TestSearchCostUSD(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	for _, tc := range []struct {
		name  string
		m     *Model
		units int
		want  float64
		ok    bool
	}{
		{"nil model", nil, 1, 0, false},
		{"token-priced model has no search price", &Model{InputPricePerMillion: f(1), OutputPricePerMillion: f(1)}, 1, 0, false},
		{"one unit at two dollars per thousand", &Model{SearchPricePerThousand: f(2)}, 1, 0.002, true},
		{"three units at the pro price", &Model{SearchPricePerThousand: f(2.5)}, 3, 0.0075, true},
		{"free rerank prices to zero", &Model{SearchPricePerThousand: f(0)}, 5, 0, true},
		{"no units charged", &Model{SearchPricePerThousand: f(2)}, 0, 0, true},
		{"negative price is unpriced", &Model{SearchPricePerThousand: f(-1)}, 1, 0, false},
		{"nan price is unpriced", &Model{SearchPricePerThousand: f(math.NaN())}, 1, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.m.SearchCostUSD(tc.units)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if math.Abs(got-tc.want) > 1e-12 {
				t.Errorf("cost = %v, want %v", got, tc.want)
			}
		})
	}
}
