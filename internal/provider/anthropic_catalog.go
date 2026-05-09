// Package provider provides LLM provider discovery and management.
package provider

// AnthropicPricingSpec contains pricing information for an Anthropic model.
type AnthropicPricingSpec struct {
	ModelID                      string
	InputPricePerMillion         float64
	InputPricePerMillionCacheHit float64
	OutputPricePerMillion        float64
}

var anthropicPricing = []AnthropicPricingSpec{
	{ModelID: "claude-opus-4-7", InputPricePerMillion: 5.00, InputPricePerMillionCacheHit: 0.50, OutputPricePerMillion: 25.00},
	{ModelID: "claude-opus-4-6", InputPricePerMillion: 5.00, InputPricePerMillionCacheHit: 0.50, OutputPricePerMillion: 25.00},
	{ModelID: "claude-opus-4-5", InputPricePerMillion: 5.00, InputPricePerMillionCacheHit: 0.50, OutputPricePerMillion: 25.00},
	{ModelID: "claude-opus-4-1", InputPricePerMillion: 15.00, InputPricePerMillionCacheHit: 1.50, OutputPricePerMillion: 75.00},
	{ModelID: "claude-opus-4", InputPricePerMillion: 5.00, InputPricePerMillionCacheHit: 0.50, OutputPricePerMillion: 25.00},
	{ModelID: "claude-sonnet-4-6", InputPricePerMillion: 3.00, InputPricePerMillionCacheHit: 0.30, OutputPricePerMillion: 15.00},
	{ModelID: "claude-sonnet-4-5", InputPricePerMillion: 3.00, InputPricePerMillionCacheHit: 0.30, OutputPricePerMillion: 15.00},
	{ModelID: "claude-sonnet-4", InputPricePerMillion: 3.00, InputPricePerMillionCacheHit: 0.30, OutputPricePerMillion: 15.00},
	{ModelID: "claude-haiku-4-5", InputPricePerMillion: 1.00, InputPricePerMillionCacheHit: 0.10, OutputPricePerMillion: 5.00},
}

// GetAnthropicPricing returns the full Anthropic pricing catalog.
func GetAnthropicPricing() []AnthropicPricingSpec {
	return anthropicPricing
}

// LookupAnthropicPricing finds pricing for a model ID, stripping date suffixes if needed.
func LookupAnthropicPricing(catalog []AnthropicPricingSpec, modelID string) *AnthropicPricingSpec {
	for i := range catalog {
		if catalog[i].ModelID == modelID {
			return &catalog[i]
		}
	}

	baseID := stripAnthropicDate(modelID)
	if baseID != modelID {
		for i := range catalog {
			if catalog[i].ModelID == baseID {
				return &catalog[i]
			}
		}
	}

	return nil
}

func stripAnthropicDate(modelID string) string {
	if len(modelID) < 9 {
		return modelID
	}
	suffix := modelID[len(modelID)-9:]
	if suffix[0] == '-' && len(suffix) == 9 {
		allDigits := true
		for _, c := range suffix[1:] {
			if c < '0' || c > '9' {
				allDigits = false
				break
			}
		}
		if allDigits {
			return modelID[:len(modelID)-9]
		}
	}
	return modelID
}
