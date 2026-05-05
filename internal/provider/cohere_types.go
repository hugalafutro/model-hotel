package provider

// CohereNativeModel represents a single model from GET /v1/models
type CohereNativeModel struct {
	Name             string                  `json:"name"`
	Endpoints        []string                `json:"endpoints"`
	Finetuned        bool                    `json:"finetuned"`
	ContextLength    int                     `json:"context_length"`
	TokenizerURL     string                  `json:"tokenizer_url"`
	Features         []string                `json:"features"`
	DefaultEndpoints []string                `json:"default_endpoints"`
	IsDeprecated     bool                    `json:"is_deprecated"`
	SamplingDefaults *CohereSamplingDefaults `json:"sampling_defaults,omitempty"`
}

type CohereSamplingDefaults struct {
	Temperature      float64 `json:"temperature,omitempty"`
	K                int     `json:"k,omitempty"`
	P                float64 `json:"p,omitempty"`
	FrequencyPenalty float64 `json:"frequency_penalty,omitempty"`
	PresencePenalty  float64 `json:"presence_penalty,omitempty"`
	MaxTokensPerDoc  int     `json:"max_tokens_per_doc,omitempty"`
}

// CohereModelsResponse is the response from GET /v1/models
type CohereModelsResponse struct {
	Models        []CohereNativeModel `json:"models"`
	NextPageToken string              `json:"next_page_token,omitempty"`
}
