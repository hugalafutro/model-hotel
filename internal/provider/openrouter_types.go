package provider

// OpenRouterModel represents a single model from GET /api/v1/models.
type OpenRouterModel struct {
	ID                  string                      `json:"id"`
	CanonicalSlug       string                      `json:"canonical_slug"`
	Name                string                      `json:"name"`
	Description         string                      `json:"description"`
	ContextLength       int                         `json:"context_length"`
	Architecture        OpenRouterArchitecture      `json:"architecture"`
	Pricing             OpenRouterPricing           `json:"pricing"`
	TopProvider         OpenRouterTopProvider       `json:"top_provider"`
	SupportedParameters []string                    `json:"supported_parameters"`
	PerRequestLimits    *OpenRouterPerRequestLimits `json:"per_request_limits,omitempty"`
}

type OpenRouterArchitecture struct {
	Modality         string   `json:"modality"`
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
	Tokenizer        string   `json:"tokenizer"`
	InstructType     *string  `json:"instruct_type"`
}

// OpenRouterPricing — all values are per-token strings.
// Convert to $/1M by multiplying by 1_000_000.
type OpenRouterPricing struct {
	Prompt          string `json:"prompt"`
	Completion      string `json:"completion"`
	InputCacheRead  string `json:"input_cache_read,omitempty"`
	InputCacheWrite string `json:"input_cache_write,omitempty"`
	WebSearch       string `json:"web_search,omitempty"`
	Reasoning       string `json:"reasoning,omitempty"`
}

type OpenRouterTopProvider struct {
	ContextLength       int  `json:"context_length"`
	MaxCompletionTokens int  `json:"max_completion_tokens"`
	IsModerated         bool `json:"is_moderated"`
}

type OpenRouterPerRequestLimits struct {
	MaxTokens *int `json:"max_tokens,omitempty"`
}

// OpenRouterModelsResponse is the top-level response from GET /api/v1/models.
type OpenRouterModelsResponse struct {
	Data []OpenRouterModel `json:"data"`
}
