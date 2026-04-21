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