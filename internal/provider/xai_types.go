package provider

// XAILanguageModel represents a language model from the XAI /v1/language-models endpoint.
type XAILanguageModel struct {
	ID                         string   `json:"id"`
	Fingerprint                string   `json:"fingerprint"`
	Created                    int64    `json:"created"`
	Object                     string   `json:"object"`
	OwnedBy                    string   `json:"owned_by"`
	Version                    string   `json:"version"`
	InputModalities            []string `json:"input_modalities"`
	OutputModalities           []string `json:"output_modalities"`
	PromptTextTokenPrice       int      `json:"prompt_text_token_price"`
	CachedPromptTextTokenPrice int      `json:"cached_prompt_text_token_price"`
	PromptImageTokenPrice      int      `json:"prompt_image_token_price"`
	CompletionTextTokenPrice   int      `json:"completion_text_token_price"`
	SearchPrice                int      `json:"search_price"`
	Aliases                    []string `json:"aliases"`
}

// XAILanguageModelsResponse is the response from the XAI /v1/language-models endpoint.
type XAILanguageModelsResponse struct {
	Models []XAILanguageModel `json:"models"`
}

// XAIImageGenerationModel represents an image generation model from the XAI /v1/image-generation-models endpoint.
type XAIImageGenerationModel struct {
	ID               string   `json:"id"`
	Fingerprint      string   `json:"fingerprint"`
	MaxPromptLength  int      `json:"max_prompt_length"`
	Created          int64    `json:"created"`
	Object           string   `json:"object"`
	OwnedBy          string   `json:"owned_by"`
	Version          string   `json:"version"`
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
	ImagePrice       int64    `json:"image_price"`
	Aliases          []string `json:"aliases"`
}

// XAIImageGenerationModelsResponse is the response from the XAI /v1/image-generation-models endpoint.
type XAIImageGenerationModelsResponse struct {
	Models []XAIImageGenerationModel `json:"models"`
}
