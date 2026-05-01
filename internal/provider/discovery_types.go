package provider

type OpenAIModel struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

type OpenAIModelsResponse struct {
	Object string         `json:"object"`
	Data   []OpenAIModel `json:"data"`
}

type NanoGPTArchitecture struct {
	Modality         string   `json:"modality"`
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
}

type NanoGPTCapabilities struct {
	Vision            bool `json:"vision"`
	VideoInput       bool `json:"video_input"`
	AudioInput       bool `json:"audio_input"`
	Reasoning        bool `json:"reasoning"`
	ToolCalling      bool `json:"tool_calling"`
	ParallelToolCalls bool `json:"parallel_tool_calls"`
	StructuredOutput  bool `json:"structured_output"`
	PDFUpload        bool `json:"pdf_upload"`
}

type NanoGPTPricing struct {
	Prompt     float64 `json:"prompt"`
	Completion float64 `json:"completion"`
	Currency   string  `json:"currency"`
	Unit       string  `json:"unit"`
}

type NanoGPTSubscription struct {
	Included bool   `json:"included"`
	Note     string `json:"note"`
}

type NanoGPTModel struct {
	ID              string                `json:"id"`
	Name            string                `json:"name"`
	Description     string                `json:"description"`
	ContextLength   *int                  `json:"context_length"`
	MaxOutputTokens *int                  `json:"max_output_tokens"`
	OwnedBy         string                `json:"owned_by"`
	Architecture    NanoGPTArchitecture   `json:"architecture"`
	Capabilities    NanoGPTCapabilities   `json:"capabilities"`
	Pricing         NanoGPTPricing       `json:"pricing"`
	Subscription    *NanoGPTSubscription  `json:"subscription"`
}

type NanoGPTDetailedResponse struct {
	Object string         `json:"object"`
	Data   []NanoGPTModel `json:"data"`
}

type NanoGPTUsageLimits struct {
	WeeklyInputTokens *int64 `json:"weeklyInputTokens"`
	DailyInputTokens  *int64 `json:"dailyInputTokens"`
	DailyImages       *int64 `json:"dailyImages"`
}

type NanoGPTUsageTokenInfo struct {
	Used       int64  `json:"used"`
	Remaining  int64  `json:"remaining"`
	PercentUsed float64 `json:"percentUsed"`
	ResetAt    int64  `json:"resetAt"`
}

type NanoGPTUsageDailyImages struct {
	Used        int64   `json:"used"`
	Remaining   int64   `json:"remaining"`
	PercentUsed float64 `json:"percentUsed"`
	ResetAt     int64   `json:"resetAt"`
}

type NanoGPTUsagePeriod struct {
	CurrentPeriodEnd string `json:"currentPeriodEnd"`
}

type NanoGPTUsageResponse struct {
	Active             bool                  `json:"active"`
	Provider           string                `json:"provider"`
	ProviderStatus     string                `json:"providerStatus"`
	ProviderStatusRaw  string                `json:"providerStatusRaw"`
	StripeSubscription string                `json:"stripeSubscriptionId"`
	CancellationReason *string               `json:"cancellationReason"`
	CanceledAt        *string               `json:"canceledAt"`
	EndedAt           *string               `json:"endedAt"`
	CancelAt          *string               `json:"cancelAt"`
	CancelAtPeriodEnd bool                  `json:"cancelAtPeriodEnd"`
	Limits            NanoGPTUsageLimits    `json:"limits"`
	AllowOverage      bool                  `json:"allowOverage"`
	Period            NanoGPTUsagePeriod    `json:"period"`
	DailyImages       *NanoGPTUsageDailyImages `json:"dailyImages"`
	DailyInputTokens  *NanoGPTUsageTokenInfo `json:"dailyInputTokens"`
	WeeklyInputTokens *NanoGPTUsageTokenInfo `json:"weeklyInputTokens"`
	State             string                `json:"state"`
	GraceUntil        *string               `json:"graceUntil"`
}

type DeepSeekBalanceInfo struct {
	Currency        string `json:"currency"`
	TotalBalance    string `json:"total_balance"`
	GrantedBalance  string `json:"granted_balance"`
	ToppedUpBalance string `json:"topped_up_balance"`
}

type DeepSeekBalanceResponse struct {
	IsAvailable  bool                  `json:"is_available"`
	BalanceInfos []DeepSeekBalanceInfo `json:"balance_infos"`
}

type OllamaTagsModelDetails struct {
	ParentModel       string   `json:"parent_model"`
	Format            string   `json:"format"`
	Family            string   `json:"family"`
	Families          []string `json:"families"`
	ParameterSize     string   `json:"parameter_size"`
	QuantizationLevel string   `json:"quantization_level"`
}

type OllamaTagsModel struct {
	Name       string                  `json:"name"`
	Model      string                  `json:"model"`
	ModifiedAt string                  `json:"modified_at"`
	Size       int64                   `json:"size"`
	Digest     string                  `json:"digest"`
	Details    OllamaTagsModelDetails `json:"details"`
}

type OllamaTagsResponse struct {
	Models []OllamaTagsModel `json:"models"`
}

type OllamaShowDetails struct {
	ParentModel       string   `json:"parent_model"`
	Format            string   `json:"format"`
	Family            string   `json:"family"`
	Families          []string `json:"families"`
	ParameterSize     string   `json:"parameter_size"`
	QuantizationLevel string   `json:"quantization_level"`
}

type OllamaShowResponse struct {
	Details      OllamaShowDetails    `json:"details"`
	ModelInfo    map[string]any       `json:"model_info"`
	Capabilities []string             `json:"capabilities"`
	ModifiedAt   string               `json:"modified_at"`
}

type ZAICodingQuotaUsageDetail struct {
	ModelCode string `json:"modelCode"`
	Usage     int64  `json:"usage"`
}

type ZAICodingQuotaLimit struct {
	Type           string                 `json:"type"`
	Unit           int                    `json:"unit"`
	Number         int                    `json:"number"`
	Usage          int64                  `json:"usage"`
	CurrentValue   int64                  `json:"currentValue"`
	Remaining      int64                  `json:"remaining"`
	Percentage     float64                `json:"percentage"`
	NextResetTime  int64                  `json:"nextResetTime"`
	UsageDetails   []ZAICodingQuotaUsageDetail  `json:"usageDetails,omitempty"`
}

type ZAICodingQuotaData struct {
	Limits []ZAICodingQuotaLimit `json:"limits"`
	Level  string          `json:"level"`
}

type ZAICodingQuotaResponse struct {
	Code    int                `json:"code"`
	Msg     string             `json:"msg"`
	Data    ZAICodingQuotaData `json:"data"`
	Success bool         `json:"success"`
}

type AnthropicCapSupport struct {
	Supported bool `json:"supported"`
}

type AnthropicCapabilities struct {
	Batch             AnthropicCapSupport `json:"batch"`
	Citations         AnthropicCapSupport `json:"citations"`
	CodeExecution     AnthropicCapSupport `json:"code_execution"`
	ImageInput         AnthropicCapSupport `json:"image_input"`
	PDFInput           AnthropicCapSupport `json:"pdf_input"`
	StructuredOutputs  AnthropicCapSupport `json:"structured_outputs"`
}

type AnthropicModelInfo struct {
	ID             string                 `json:"id"`
	Type           string                 `json:"type"`
	DisplayName    string                 `json:"display_name"`
	CreatedAt      string                 `json:"created_at"`
	MaxInputTokens *int                   `json:"max_input_tokens"`
	MaxTokens      *int                   `json:"max_tokens"`
	Capabilities   *AnthropicCapabilities `json:"capabilities"`
}

type AnthropicModelsResponse struct {
	Data    []AnthropicModelInfo `json:"data"`
	HasMore bool                 `json:"has_more"`
	FirstID string               `json:"first_id"`
	LastID  string               `json:"last_id"`
}