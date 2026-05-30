package provider

// DeepSeekModelSpec contains specification and pricing for a DeepSeek model.
type DeepSeekModelSpec struct {
	ModelID                       string  `json:"model_id"`
	ContextLength                 int     `json:"context_length"`
	MaxOutputTokens               int     `json:"max_output_tokens"`
	Reasoning                     bool    `json:"reasoning"`
	InputPricePerMillionCacheHit  float64 `json:"input_price_per_million_cache_hit,omitempty"`
	InputPricePerMillionCacheMiss float64 `json:"input_price_per_million_cache_miss"`
	OutputPricePerMillion         float64 `json:"output_price_per_million"`
}

var deepseekCatalog = loadCatalog[[]DeepSeekModelSpec]("deepseek.json")

// GetDeepSeekModels returns the full DeepSeek model catalog.
func GetDeepSeekModels() []DeepSeekModelSpec {
	return deepseekCatalog
}

// GetDeepSeekModelSpec returns the spec for a specific DeepSeek model ID.
func GetDeepSeekModelSpec(modelID string) *DeepSeekModelSpec {
	for _, spec := range deepseekCatalog {
		if spec.ModelID == modelID {
			return &spec
		}
	}
	return nil
}
