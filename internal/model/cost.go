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

// PricedAt reports whether a stored price can price anything: present, finite
// and not negative. A listing can state "NaN", "Inf" or a negative figure and
// a parser that only checks for a parse error stores it as a real price; a
// row priced from one would carry a cost the budget cannot compare (NaN is
// never at or above a cap) or one that pays the spender. Such a model is
// unpriced, and its rows stay NULL, the state every reader already handles.
func PricedAt(p *float64) bool {
	return p != nil && !math.IsNaN(*p) && !math.IsInf(*p, 0) && *p >= 0
}

// CostUSD prices usage at the model's stored per-million prices. ok is false
// when the model holds no input or output price, in which case the cost is
// unknown rather than zero. Cache-hit tokens take the cache-hit price when the
// model has one and the input price otherwise; completion tokens, reasoning
// included, take the output price.
//
// The prompt can exceed the cache split: a failover group that rejected an
// earlier candidate's 2xx adds that candidate's prompt to the row, while the
// split is the serving candidate's alone. The excess takes the input price,
// so every prompt token the row carries is priced.
func (m *Model) CostUSD(u Usage) (cost float64, ok bool) {
	if m == nil || !PricedAt(m.InputPricePerMillion) || !PricedAt(m.OutputPricePerMillion) {
		return 0, false
	}
	if u.PromptCacheHit > 0 && m.InputPricePerMillionCacheHit != nil && !PricedAt(m.InputPricePerMillionCacheHit) {
		return 0, false
	}
	prompt := float64(u.Prompt) * *m.InputPricePerMillion
	if u.PromptCacheHit > 0 && m.InputPricePerMillionCacheHit != nil {
		prompt = float64(u.PromptCacheHit)**m.InputPricePerMillionCacheHit +
			float64(u.PromptCacheMiss)**m.InputPricePerMillion +
			float64(max(0, u.Prompt-u.PromptCacheHit-u.PromptCacheMiss))**m.InputPricePerMillion
	}
	output := float64(u.Completion) * *m.OutputPricePerMillion
	return (prompt + output) / 1e6, true
}
