package model

// Usage is the token breakdown a served request charged, as the request log
// stores it: cache-hit and cache-miss prompt tokens sum to the prompt when a
// provider reported a cache split, and both read 0 when it did not. Reasoning
// tokens are not a member: providers report them inside completion_tokens
// (OpenAI's own fixture in this repository has total = prompt + completion
// with reasoning nested under completion_tokens_details), so pricing them
// again would charge a reasoning model's thinking twice.
type Usage struct {
	Prompt          int
	PromptCacheHit  int
	PromptCacheMiss int
	Completion      int
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
	if m == nil || m.InputPricePerMillion == nil || m.OutputPricePerMillion == nil {
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
