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
