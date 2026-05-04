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

// OpenRouterKeyData represents the key-level data from GET /api/v1/key.
type OpenRouterKeyData struct {
	Label              string   `json:"label"`
	Limit              *float64 `json:"limit"`
	LimitReset         string   `json:"limit_reset"`
	LimitRemaining     *float64 `json:"limit_remaining"`
	IncludeByokInLimit bool     `json:"include_byok_in_limit"`
	Usage              float64  `json:"usage"`
	UsageDaily         float64  `json:"usage_daily"`
	UsageWeekly        float64  `json:"usage_weekly"`
	UsageMonthly       float64  `json:"usage_monthly"`
	ByokUsage          float64  `json:"byok_usage"`
	IsFreeTier         bool     `json:"is_free_tier"`
}

// OpenRouterKeyResponse is the top-level response from GET /api/v1/key.
type OpenRouterKeyResponse struct {
	Data OpenRouterKeyData `json:"data"`
}

// OpenRouterCreditsData represents the credits data from GET /api/v1/credits.
type OpenRouterCreditsData struct {
	TotalCredits float64 `json:"total_credits"`
	TotalUsage   float64 `json:"total_usage"`
}

// OpenRouterCreditsResponse is the top-level response from GET /api/v1/credits.
type OpenRouterCreditsResponse struct {
	Data OpenRouterCreditsData `json:"data"`
}

// OpenRouterBalance combines key info and credits into a unified balance response.
type OpenRouterBalance struct {
	Label              string   `json:"label"`
	Limit              *float64 `json:"limit"`
	LimitRemaining     *float64 `json:"limit_remaining"`
	Usage              float64  `json:"usage"`
	CreditsTotal       float64  `json:"credits_total"`
	CreditsUsed        float64  `json:"credits_used"`
	CreditsRemaining   float64  `json:"credits_remaining"`
	IsFreeTier         bool     `json:"is_free_tier"`
}
