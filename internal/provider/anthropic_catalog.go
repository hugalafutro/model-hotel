// Package provider provides LLM provider discovery and management.
package provider

// AnthropicPricingSpec contains pricing information for an Anthropic model.
type AnthropicPricingSpec struct {
	ModelID                      string  `json:"model_id"`
	InputPricePerMillion         float64 `json:"input_price_per_million"`
	InputPricePerMillionCacheHit float64 `json:"input_price_per_million_cache_hit"`
	OutputPricePerMillion        float64 `json:"output_price_per_million"`
}

var anthropicPricing = loadCatalog[[]AnthropicPricingSpec]("anthropic.json")

// GetAnthropicPricing returns the full Anthropic pricing catalog.
func GetAnthropicPricing() []AnthropicPricingSpec {
	return anthropicPricing
}

// LookupAnthropicPricing finds pricing for a model ID, stripping date suffixes if needed.
func LookupAnthropicPricing(catalog []AnthropicPricingSpec, modelID string) *AnthropicPricingSpec {
	id := func(e *AnthropicPricingSpec) string { return e.ModelID }
	if spec := lookupByModelID(catalog, modelID, id); spec != nil {
		return spec
	}
	if baseID := stripAnthropicDate(modelID); baseID != modelID {
		return lookupByModelID(catalog, baseID, id)
	}
	return nil
}

// stripAnthropicDate drops a trailing "-YYYYMMDD" release stamp, so a dated
// model ID falls back to the undated catalog row.
func stripAnthropicDate(modelID string) string {
	if n := len(modelID); n > 9 && modelID[n-9] == '-' && isNumeric(modelID[n-8:]) {
		return modelID[:n-9]
	}
	return modelID
}
