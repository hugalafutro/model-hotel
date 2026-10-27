package model

import "math"

// Usage is the token breakdown a served request charged, as the request log
// stores it: cache-hit and cache-miss prompt tokens sum to the prompt when a
// provider reported a cache split, and both read 0 when it did not. Reasoning
// tokens are not a member: in the normalized usage the log stores they are
// inside completion (OpenAI-compatible providers report them that way, and
// the Gemini adapter folds thoughts into completion), so pricing them again
// would charge a reasoning model's thinking twice.
type Usage struct {
	Prompt          int
	PromptCacheHit  int
	PromptCacheMiss int
	// PromptCacheWrite is the part of Prompt a provider wrote into its prompt
	// cache (Anthropic's cache_creation_input_tokens): a subset of the misses
	// when a split was reported, of the plain prompt otherwise (a write without
	// a read reports no split). Those tokens take the input price plus the
	// write premium; 0 when the provider reports none.
	PromptCacheWrite int
	// PromptCacheWrite1h is the part of PromptCacheWrite written with a 1-hour
	// TTL (Anthropic's cache_creation.ephemeral_1h_input_tokens), which takes
	// the larger 1-hour premium instead.
	PromptCacheWrite1h int
	Completion         int
}

// priceable reports whether a stored price can price anything: present,
// finite and not negative. A listing can state "NaN", "Inf" or a negative
// figure and a parser that only checks for a parse error stores it as a real
// price; a row priced from one would carry a cost the budget cannot compare
// (NaN is never at or above a cap) or one that pays the spender. Such a price
// counts as absent: the model is unpriced and its rows stay NULL, the state
// every reader already handles. NaN fails the comparison on its own; only
// +Inf needs naming.
func priceable(p *float64) bool {
	return p != nil && !math.IsInf(*p, 0) && *p >= 0
}

// PriceOrNil passes a priceable figure through and turns anything else into the
// absent price it counts as, so a discovery driver stores "no price" rather
// than a figure no reader can use.
func PriceOrNil(p *float64) *float64 {
	if !priceable(p) {
		return nil
	}
	return p
}

// CacheWritePremium is the surcharge on a prompt token written into the
// provider's cache, as a fraction of the input price: Anthropic's 5-minute
// cache write costs 1.25x the input price. CacheWrite1hPremium is the same for
// a 1-hour write (2x), which only an Anthropic-in client can request with
// cache_control.ttl; the translated routes never place one.
const (
	CacheWritePremium   = 0.25
	CacheWrite1hPremium = 1.0
)

// CostUSD prices usage at the model's stored per-million prices. ok is false
// when the model holds no priceable input or output price (absent, or a figure
// priceable refuses), in which case the cost is unknown rather than zero.
// Cache-hit tokens take the cache-hit price when the model has a priceable
// one and the input price otherwise; cache-write tokens, never hits, add
// CacheWritePremium of the input price on top (Anthropic bills a 5-minute
// cache write at 1.25x and is the only provider reporting writes); completion
// tokens, reasoning included, take the output price.
//
// The prompt can exceed the cache split (a provider that reports a total but
// no split, or only part of one). The excess takes the input price, so every
// prompt token the caller passes is priced.
func (m *Model) CostUSD(u Usage) (cost float64, ok bool) {
	if m == nil || !priceable(m.InputPricePerMillion) || !priceable(m.OutputPricePerMillion) {
		return 0, false
	}
	prompt := float64(u.Prompt) * *m.InputPricePerMillion
	if u.PromptCacheHit > 0 && priceable(m.InputPricePerMillionCacheHit) {
		prompt = float64(u.PromptCacheHit)**m.InputPricePerMillionCacheHit +
			float64(u.PromptCacheMiss)**m.InputPricePerMillion +
			float64(max(0, u.Prompt-u.PromptCacheHit-u.PromptCacheMiss))**m.InputPricePerMillion
	}
	// The premium only: the written tokens are already priced once as misses
	// (or as plain prompt when the provider reported no split). Capped by the
	// prompt that is not a hit, so a provider reporting more writes than it
	// read cannot put the premium on tokens priced at the cache-hit rate; the
	// 1-hour share is capped by the write it is a part of.
	write := min(max(0, u.PromptCacheWrite), max(0, u.Prompt-max(0, u.PromptCacheHit)))
	write1h := min(max(0, u.PromptCacheWrite1h), write)
	prompt += (float64(write-write1h)*CacheWritePremium + float64(write1h)*CacheWrite1hPremium) * *m.InputPricePerMillion
	output := float64(u.Completion) * *m.OutputPricePerMillion
	cost = (prompt + output) / 1e6
	// A finite price a listing stated as 1e308 still overflows the product; an
	// infinite cost is as unusable as no cost.
	if math.IsInf(cost, 0) {
		return 0, false
	}
	return cost, true
}

// SearchCostUSD prices search units at the model's stored per-thousand search
// price, the unit a rerank model bills in. ok is false when the model holds no
// priceable search price, in which case the cost is unknown rather than zero.
func (m *Model) SearchCostUSD(units int) (cost float64, ok bool) {
	if m == nil || !priceable(m.SearchPricePerThousand) {
		return 0, false
	}
	cost = float64(units) * *m.SearchPricePerThousand / 1000
	if math.IsInf(cost, 0) {
		return 0, false
	}
	return cost, true
}
