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
		ModelID:                       "deepseek-v4-flash",
		ContextLength:                 1000000,
		MaxOutputTokens:               384000,
		Reasoning:                     false,
		InputPricePerMillionCacheHit:  0.028,
		InputPricePerMillionCacheMiss: 0.139,
		OutputPricePerMillion:         0.278,
	},
	{
		ModelID:                       "deepseek-v4-pro",
		ContextLength:                 1000000,
		MaxOutputTokens:               384000,
		Reasoning:                     true,
		InputPricePerMillionCacheHit:  0.139,
		InputPricePerMillionCacheMiss: 1.667,
		OutputPricePerMillion:         3.333,
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
