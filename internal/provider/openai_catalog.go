package provider

// OpenAIModelSpec defines pricing and capabilities for an OpenAI model.
type OpenAIModelSpec struct {
	ModelID                      string  `json:"model_id"`
	DisplayName                  string  `json:"display_name"`
	Description                  string  `json:"description"`
	ContextLength                int     `json:"context_length"`
	MaxOutputTokens              int     `json:"max_output_tokens"`
	Modality                     string  `json:"modality"`
	InputModalities              string  `json:"input_modalities"`
	OutputModalities             string  `json:"output_modalities"`
	Streaming                    bool    `json:"streaming"`
	Reasoning                    bool    `json:"reasoning"`
	ToolCalling                  bool    `json:"tool_calling"`
	StructuredOutput             bool    `json:"structured_output"`
	Vision                       bool    `json:"vision"`
	InputPricePerMillion         float64 `json:"input_price_per_million"`
	InputPricePerMillionCacheHit float64 `json:"input_price_per_million_cache_hit,omitempty"`
	OutputPricePerMillion        float64 `json:"output_price_per_million"`
}

var openaiCatalog = loadCatalog[[]OpenAIModelSpec]("openai.json")

// GetOpenAIModels returns the OpenAI model catalog.
func GetOpenAIModels() []OpenAIModelSpec {
	return openaiCatalog
}

// LookupOpenAICatalog finds a model spec in the OpenAI catalog.
func LookupOpenAICatalog(catalog []OpenAIModelSpec, modelID string) *OpenAIModelSpec {
	for i := range catalog {
		if catalog[i].ModelID == modelID {
			return &catalog[i]
		}
	}
	return nil
}
