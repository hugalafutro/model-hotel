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
	Completion      int
}

// Priceable reports whether a stored price can price anything: present,
// finite and not negative. A listing can state "NaN", "Inf" or a negative
// figure and a parser that only checks for a parse error stores it as a real
// price; a row priced from one would carry a cost the budget cannot compare
// (NaN is never at or above a cap) or one that pays the spender. Such a price
// counts as absent: the model is unpriced and its rows stay NULL, the state
// every reader already handles. NaN fails the comparison on its own; only
// +Inf needs naming.
func Priceable(p *float64) bool {
	return p != nil && !math.IsInf(*p, 0) && *p >= 0
}

// PriceOrNil passes a priceable figure through and turns anything else into the
// absent price it counts as, so a discovery driver stores "no price" rather
// than a figure no reader can use.
func PriceOrNil(p *float64) *float64 {
	if !Priceable(p) {
		return nil
	}
	return p
}

// CostUSD prices usage at the model's stored per-million prices. ok is false
// when the model holds no priceable input or output price (absent, or a figure
// Priceable refuses), in which case the cost is unknown rather than zero.
// Cache-hit tokens take the cache-hit price when the model has a priceable
// one and the input price otherwise; completion tokens, reasoning included,
// take the output price.
//
// The prompt can exceed the cache split: a failover group that rejected an
// earlier candidate's 2xx adds that candidate's prompt to the row, while the
// split is the serving candidate's alone. The excess takes the input price,
// so every prompt token the row carries is priced.
func (m *Model) CostUSD(u Usage) (cost float64, ok bool) {
	if m == nil || !Priceable(m.InputPricePerMillion) || !Priceable(m.OutputPricePerMillion) {
		return 0, false
	}
	prompt := float64(u.Prompt) * *m.InputPricePerMillion
	if u.PromptCacheHit > 0 && Priceable(m.InputPricePerMillionCacheHit) {
		prompt = float64(u.PromptCacheHit)**m.InputPricePerMillionCacheHit +
			float64(u.PromptCacheMiss)**m.InputPricePerMillion +
			float64(max(0, u.Prompt-u.PromptCacheHit-u.PromptCacheMiss))**m.InputPricePerMillion
	}
	output := float64(u.Completion) * *m.OutputPricePerMillion
	cost = (prompt + output) / 1e6
	// A finite price a listing stated as 1e308 still overflows the product; an
	// infinite cost is as unusable as no cost.
	if math.IsInf(cost, 0) {
		return 0, false
	}
	return cost, true
}
