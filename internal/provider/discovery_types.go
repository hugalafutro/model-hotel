package provider

// NeuralWattQuotaBalance contains balance/credit information.
type NeuralWattQuotaBalance struct {
	CreditsRemainingUSD float64 `json:"credits_remaining_usd"`
	TotalCreditsUSD     float64 `json:"total_credits_usd"`
	CreditsUsedUSD      float64 `json:"credits_used_usd"`
	AccountingMethod    string  `json:"accounting_method"`
}

// NeuralWattQuotaUsagePeriod contains cost/request/token/energy usage.
type NeuralWattQuotaUsagePeriod struct {
	CostUSD   float64 `json:"cost_usd"`
	Requests  int64   `json:"requests"`
	Tokens    int64   `json:"tokens"`
	EnergyKWh float64 `json:"energy_kwh"`
}

// NeuralWattQuotaUsage contains lifetime and current_month usage.
type NeuralWattQuotaUsage struct {
	Lifetime     NeuralWattQuotaUsagePeriod `json:"lifetime"`
	CurrentMonth NeuralWattQuotaUsagePeriod `json:"current_month"`
}

// NeuralWattQuotaLimits contains rate limit tier info.
type NeuralWattQuotaLimits struct {
	OverageLimitUSD *float64 `json:"overage_limit_usd"`
	RateLimitTier   string   `json:"rate_limit_tier"`
}

// NeuralWattQuotaSubscription contains plan and billing details.
type NeuralWattQuotaSubscription struct {
	Plan               string  `json:"plan"`
	Status             string  `json:"status"`
	BillingInterval    string  `json:"billing_interval"`
	CurrentPeriodStart string  `json:"current_period_start"`
	CurrentPeriodEnd   string  `json:"current_period_end"`
	AutoRenew          bool    `json:"auto_renew"`
	KWhIncluded        float64 `json:"kwh_included"`
	KWhUsed            float64 `json:"kwh_used"`
	KWhRemaining       float64 `json:"kwh_remaining"`
	InOverage          bool    `json:"in_overage"`
}

// NeuralWattQuotaKey contains key metadata.
type NeuralWattQuotaKey struct {
	Name      string   `json:"name"`
	Allowance *float64 `json:"allowance"`
}

// NeuralWattQuotaResponse is the API response from NeuralWatt /v1/quota.
type NeuralWattQuotaResponse struct {
	SnapshotAt   string                      `json:"snapshot_at"`
	Balance      NeuralWattQuotaBalance      `json:"balance"`
	Usage        NeuralWattQuotaUsage        `json:"usage"`
	Limits       NeuralWattQuotaLimits       `json:"limits"`
	Subscription NeuralWattQuotaSubscription `json:"subscription"`
	Key          NeuralWattQuotaKey          `json:"key"`
}

// OpenAIModel represents a model from the OpenAI API.
type OpenAIModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

// OpenAIModelsResponse is the response from the OpenAI models endpoint.
type OpenAIModelsResponse struct {
	Object string        `json:"object"`
	Data   []OpenAIModel `json:"data"`
}

// NanoGPTArchitecture describes the architecture of a NanoGPT model.
type NanoGPTArchitecture struct {
	Modality         string   `json:"modality"`
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
}

// NanoGPTCapabilities lists the capabilities of a NanoGPT model.
type NanoGPTCapabilities struct {
	Vision            bool `json:"vision"`
	VideoInput        bool `json:"video_input"`
	AudioInput        bool `json:"audio_input"`
	Reasoning         bool `json:"reasoning"`
	ToolCalling       bool `json:"tool_calling"`
	ParallelToolCalls bool `json:"parallel_tool_calls"`
	StructuredOutput  bool `json:"structured_output"`
	PDFUpload         bool `json:"pdf_upload"`
}

// NanoGPTPricing contains pricing information for a NanoGPT model. Prompt and
// Completion are pointers so an omitted price decodes as nil ("unknown") rather
// than a synthetic 0: a nil price is left unmarked-live and can't overwrite a
// stored value on upsert, while a present value (including a real 0) is kept.
type NanoGPTPricing struct {
	Prompt     *float64 `json:"prompt"`
	Completion *float64 `json:"completion"`
	Currency   string   `json:"currency"`
	Unit       string   `json:"unit"`
}

// NanoGPTSubscription describes subscription details for a NanoGPT model.
type NanoGPTSubscription struct {
	Included bool   `json:"included"`
	Note     string `json:"note"`
}

// NanoGPTModel represents a model from the NanoGPT API.
type NanoGPTModel struct {
	ID              string               `json:"id"`
	Name            string               `json:"name"`
	Description     string               `json:"description"`
	ContextLength   *int                 `json:"context_length"`
	MaxOutputTokens *int                 `json:"max_output_tokens"`
	OwnedBy         string               `json:"owned_by"`
	Architecture    NanoGPTArchitecture  `json:"architecture"`
	Capabilities    NanoGPTCapabilities  `json:"capabilities"`
	Pricing         NanoGPTPricing       `json:"pricing"`
	Subscription    *NanoGPTSubscription `json:"subscription"`
}

// NanoGPTDetailedResponse is the detailed response from the NanoGPT models endpoint.
type NanoGPTDetailedResponse struct {
	Object string         `json:"object"`
	Data   []NanoGPTModel `json:"data"`
}

// NanoGPTImageModel represents an entry from the NanoGPT image-model catalog
// (GET /api/models/image). Image models live on a separate endpoint from chat
// models and bill per image rather than per token, so they carry no token
// pricing here. Cost maps each supported resolution to a per-image USD price.
type NanoGPTImageModel struct {
	Name         string               `json:"name"`
	Model        string               `json:"model"`
	Description  string               `json:"description"`
	OwnedBy      string               `json:"provider"`
	IconLabel    string               `json:"iconLabel"`
	Cost         map[string]float64   `json:"cost"`
	Subscription *NanoGPTSubscription `json:"subscription"`
}

// NanoGPTImageModelsResponse is the response from the NanoGPT image-model
// catalog endpoint, which nests models by media type under models.image, keyed
// by model ID.
type NanoGPTImageModelsResponse struct {
	Models struct {
		Image map[string]NanoGPTImageModel `json:"image"`
	} `json:"models"`
}

// NanoGPTUsageLimits describes usage limits for a NanoGPT account.
type NanoGPTUsageLimits struct {
	WeeklyInputTokens *int64 `json:"weeklyInputTokens"`
	DailyInputTokens  *int64 `json:"dailyInputTokens"`
	DailyImages       *int64 `json:"dailyImages"`
}

// NanoGPTUsageTokenInfo contains token usage information.
type NanoGPTUsageTokenInfo struct {
	Used        int64   `json:"used"`
	Remaining   int64   `json:"remaining"`
	PercentUsed float64 `json:"percentUsed"`
	ResetAt     int64   `json:"resetAt"`
}

// NanoGPTUsageDailyImages contains daily image generation usage information.
type NanoGPTUsageDailyImages struct {
	Used        int64   `json:"used"`
	Remaining   int64   `json:"remaining"`
	PercentUsed float64 `json:"percentUsed"`
	ResetAt     int64   `json:"resetAt"`
}

// NanoGPTUsagePeriod describes the current billing period.
type NanoGPTUsagePeriod struct {
	CurrentPeriodEnd string `json:"currentPeriodEnd"`
}

// NanoGPTUsageResponse is the response from the NanoGPT usage endpoint.
type NanoGPTUsageResponse struct {
	Active             bool                     `json:"active"`
	Provider           string                   `json:"provider"`
	ProviderStatus     string                   `json:"providerStatus"`
	ProviderStatusRaw  string                   `json:"providerStatusRaw"`
	StripeSubscription string                   `json:"stripeSubscriptionId"`
	CancellationReason *string                  `json:"cancellationReason"`
	CanceledAt         *string                  `json:"canceledAt"`
	EndedAt            *string                  `json:"endedAt"`
	CancelAt           *string                  `json:"cancelAt"`
	CancelAtPeriodEnd  bool                     `json:"cancelAtPeriodEnd"`
	Limits             NanoGPTUsageLimits       `json:"limits"`
	AllowOverage       bool                     `json:"allowOverage"`
	Period             NanoGPTUsagePeriod       `json:"period"`
	DailyImages        *NanoGPTUsageDailyImages `json:"dailyImages"`
	DailyInputTokens   *NanoGPTUsageTokenInfo   `json:"dailyInputTokens"`
	WeeklyInputTokens  *NanoGPTUsageTokenInfo   `json:"weeklyInputTokens"`
	State              string                   `json:"state"`
	GraceUntil         *string                  `json:"graceUntil"`
}

// DeepSeekBalanceInfo contains balance information for a DeepSeek account.
type DeepSeekBalanceInfo struct {
	Currency        string `json:"currency"`
	TotalBalance    string `json:"total_balance"`
	GrantedBalance  string `json:"granted_balance"`
	ToppedUpBalance string `json:"topped_up_balance"`
}

// DeepSeekBalanceResponse is the response from the DeepSeek balance endpoint.
type DeepSeekBalanceResponse struct {
	IsAvailable  bool                  `json:"is_available"`
	BalanceInfos []DeepSeekBalanceInfo `json:"balance_infos"`
}

// OllamaTagsModelDetails contains details about an Ollama model.
type OllamaTagsModelDetails struct {
	ParentModel       string   `json:"parent_model"`
	Format            string   `json:"format"`
	Family            string   `json:"family"`
	Families          []string `json:"families"`
	ParameterSize     string   `json:"parameter_size"`
	QuantizationLevel string   `json:"quantization_level"`
}

// OllamaTagsModel represents a model from the Ollama tags endpoint.
type OllamaTagsModel struct {
	Name       string                 `json:"name"`
	Model      string                 `json:"model"`
	ModifiedAt string                 `json:"modified_at"`
	Size       int64                  `json:"size"`
	Digest     string                 `json:"digest"`
	Details    OllamaTagsModelDetails `json:"details"`
}

// OllamaTagsResponse is the response from the Ollama tags endpoint.
type OllamaTagsResponse struct {
	Models []OllamaTagsModel `json:"models"`
}

// OllamaShowDetails contains detailed information about an Ollama model.
type OllamaShowDetails struct {
	ParentModel       string   `json:"parent_model"`
	Format            string   `json:"format"`
	Family            string   `json:"family"`
	Families          []string `json:"families"`
	ParameterSize     string   `json:"parameter_size"`
	QuantizationLevel string   `json:"quantization_level"`
}

// OllamaShowResponse is the response from the Ollama show endpoint.
type OllamaShowResponse struct {
	Details      OllamaShowDetails `json:"details"`
	ModelInfo    map[string]any    `json:"model_info"`
	Capabilities []string          `json:"capabilities"`
	ModifiedAt   string            `json:"modified_at"`
}

// ZAICodingQuotaUsageDetail contains usage details for a ZAI Coding model.
type ZAICodingQuotaUsageDetail struct {
	ModelCode string `json:"modelCode"`
	Usage     int64  `json:"usage"`
}

// ZAICodingQuotaLimit contains quota limit information for ZAI Coding.
type ZAICodingQuotaLimit struct {
	Type          string                      `json:"type"`
	Unit          int                         `json:"unit"`
	Number        int                         `json:"number"`
	Usage         int64                       `json:"usage"`
	CurrentValue  int64                       `json:"currentValue"`
	Remaining     int64                       `json:"remaining"`
	Percentage    float64                     `json:"percentage"`
	NextResetTime int64                       `json:"nextResetTime"`
	UsageDetails  []ZAICodingQuotaUsageDetail `json:"usageDetails,omitempty"`
}

// ZAICodingQuotaData contains quota data for ZAI Coding provider.
type ZAICodingQuotaData struct {
	Limits []ZAICodingQuotaLimit `json:"limits"`
	Level  string                `json:"level"`
}

// ZAICodingQuotaResponse is the API response from ZAI Coding quota endpoint.
type ZAICodingQuotaResponse struct {
	Code    int                `json:"code"`
	Msg     string             `json:"msg"`
	Data    ZAICodingQuotaData `json:"data"`
	Success bool               `json:"success"`
}

// KimiCodeQuotaDetail is one limit/remaining/reset block in the Kimi Code
// /usages response. Numeric fields arrive as JSON strings.
type KimiCodeQuotaDetail struct {
	Limit     string `json:"limit"`
	Remaining string `json:"remaining"`
	ResetTime string `json:"resetTime"`
}

// KimiCodeQuotaWindow describes a rolling limit window (e.g. 300 minutes = 5h).
type KimiCodeQuotaWindow struct {
	Duration int    `json:"duration"`
	TimeUnit string `json:"timeUnit"`
}

// KimiCodeQuotaLimit pairs a window with its usage detail.
type KimiCodeQuotaLimit struct {
	Window KimiCodeQuotaWindow `json:"window"`
	Detail KimiCodeQuotaDetail `json:"detail"`
}

// KimiCodeQuotaUser identifies the subscription tier.
type KimiCodeQuotaUser struct {
	UserID     string `json:"userId"`
	Region     string `json:"region"`
	Membership struct {
		Level string `json:"level"`
	} `json:"membership"`
}

// KimiCodeQuotaResponse is the Kimi Code /usages payload, passed through to
// the dashboard as-is.
type KimiCodeQuotaResponse struct {
	User     KimiCodeQuotaUser    `json:"user"`
	Usage    KimiCodeQuotaDetail  `json:"usage"`
	Limits   []KimiCodeQuotaLimit `json:"limits"`
	Parallel struct {
		Limit string `json:"limit"`
	} `json:"parallel"`
	TotalQuota KimiCodeQuotaDetail `json:"totalQuota"`
	SubType    string              `json:"subType"`
}

// AnthropicCapSupport indicates whether an Anthropic capability is supported.
type AnthropicCapSupport struct {
	Supported bool `json:"supported"`
}

// AnthropicCapabilities lists supported capabilities for Anthropic models.
type AnthropicCapabilities struct {
	Batch             AnthropicCapSupport `json:"batch"`
	Citations         AnthropicCapSupport `json:"citations"`
	CodeExecution     AnthropicCapSupport `json:"code_execution"`
	ImageInput        AnthropicCapSupport `json:"image_input"`
	PDFInput          AnthropicCapSupport `json:"pdf_input"`
	StructuredOutputs AnthropicCapSupport `json:"structured_outputs"`
}

// AnthropicModelInfo represents an Anthropic model from the models endpoint.
type AnthropicModelInfo struct {
	ID             string                 `json:"id"`
	Type           string                 `json:"type"`
	DisplayName    string                 `json:"display_name"`
	CreatedAt      string                 `json:"created_at"`
	MaxInputTokens *int                   `json:"max_input_tokens"`
	MaxTokens      *int                   `json:"max_tokens"`
	Capabilities   *AnthropicCapabilities `json:"capabilities"`
}

// AnthropicModelsResponse is the response from the Anthropic models endpoint.
type AnthropicModelsResponse struct {
	Data    []AnthropicModelInfo `json:"data"`
	HasMore bool                 `json:"has_more"`
	FirstID string               `json:"first_id"`
	LastID  string               `json:"last_id"`
}
