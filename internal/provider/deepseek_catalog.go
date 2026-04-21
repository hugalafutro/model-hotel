package provider

type DeepSeekModelSpec struct {
	ModelID                       string
	ContextLength                 int
	MaxOutputTokens               int
	Reasoning                     bool
	InputPricePerMillionCacheHit  float64
	InputPricePerMillionCacheMiss float64
	OutputPricePerMillion         float64
}

var deepseekCatalog = []DeepSeekModelSpec{
	{
		ModelID:                       "deepseek-chat",
		ContextLength:                 128000,
		MaxOutputTokens:               8192,
		Reasoning:                     false,
		InputPricePerMillionCacheHit:  0.028,
		InputPricePerMillionCacheMiss: 0.28,
		OutputPricePerMillion:         0.42,
	},
	{
		ModelID:                       "deepseek-reasoner",
		ContextLength:                 128000,
		MaxOutputTokens:               32768,
		Reasoning:                     true,
		InputPricePerMillionCacheHit:  0.028,
		InputPricePerMillionCacheMiss: 0.28,
		OutputPricePerMillion:         0.42,
	},
}

func GetDeepSeekModels() []DeepSeekModelSpec {
	return deepseekCatalog
}

func GetDeepSeekModelSpec(modelID string) *DeepSeekModelSpec {
	for _, spec := range deepseekCatalog {
		if spec.ModelID == modelID {
			return &spec
		}
	}
	return nil
}
