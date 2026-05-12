package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

func (d *DiscoveryService) discoverOpenRouter(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	url := fmt.Sprintf("%s/models", baseURL)

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+apiKey)
	headers.Set("Content-Type", "application/json")

	bodyBytes, err := d.fetchURL(ctx, "GET", url, headers)
	if err != nil {
		debuglog.Error("discovery: openrouter http request failed", "provider", provider.ID, "error", err)
		return nil, fmt.Errorf("openrouter: failed to fetch models for provider %s: %w", provider.ID, err)
	}

	var orResp OpenRouterModelsResponse
	if err := json.Unmarshal(bodyBytes, &orResp); err != nil {
		debuglog.Error("discovery: openrouter failed to decode response", "provider", provider.ID, "error", err)
		return nil, fmt.Errorf("openrouter: failed to decode response for provider %s: %w", provider.ID, err)
	}

	models := make([]*model.Model, 0, len(orResp.Data))
	for _, orm := range orResp.Data {
		// Skip auto-routing aliases (e.g., "~anthropic/claude-haiku-latest")
		if strings.HasPrefix(orm.ID, "~") {
			continue
		}

		// Skip non-text-output models (image generation only, embedding only, etc.)
		if !isOpenRouterChatModel(orm) {
			debuglog.Info("discovery: openrouter skipping non-chat model", "model", orm.ID, "modality", orm.Architecture.Modality)
			continue
		}

		// Parse pricing: per-token string → $/1M
		inPrice, outPrice := parseOpenRouterPricing(orm.Pricing)

		// Parse cache pricing if available
		var cachePrice *float64
		if orm.Pricing.InputCacheRead != "" && orm.Pricing.InputCacheRead != "0" {
			if v, err := strconv.ParseFloat(orm.Pricing.InputCacheRead, 64); err == nil {
				perMil := v * 1_000_000
				cachePrice = &perMil
			}
		}

		// Build capabilities from supported_parameters
		caps := openRouterParamsToCapabilities(orm.SupportedParameters)
		capJSON, _ := json.Marshal(caps)

		// Use context_length from model, fall back to top_provider
		contextLen := orm.ContextLength
		if contextLen == 0 && orm.TopProvider.ContextLength > 0 {
			contextLen = orm.TopProvider.ContextLength
		}

		// Build modalities from architecture
		inputMods, _ := json.Marshal(orm.Architecture.InputModalities)
		outputMods, _ := json.Marshal(orm.Architecture.OutputModalities)

		modelEntry := &model.Model{
			ID:                           uuid.New(),
			ProviderID:                   provider.ID,
			ModelID:                      orm.ID,
			Name:                         orm.ID,
			DisplayName:                  orm.Name,
			Description:                  orm.Description,
			Capabilities:                 string(capJSON),
			Params:                       "{}",
			Modality:                     orm.Architecture.Modality,
			InputModalities:              string(inputMods),
			OutputModalities:             string(outputMods),
			ContextLength:                &contextLen,
			InputPricePerMillion:         &inPrice,
			OutputPricePerMillion:        &outPrice,
			InputPricePerMillionCacheHit: cachePrice,
			OwnedBy:                      strings.SplitN(orm.ID, "/", 2)[0], // "openai" from "openai/gpt-4.1"
			Enabled:                      true,
		}

		// Set max output tokens from top_provider if available
		if orm.TopProvider.MaxCompletionTokens > 0 {
			maxOutput := orm.TopProvider.MaxCompletionTokens
			modelEntry.MaxOutputTokens = &maxOutput
		}

		models = append(models, modelEntry)
	}

	debuglog.Info("discovery: openrouter discovered models", "models", len(models), "provider", provider.ID, "total", len(orResp.Data))
	return models, nil
}

// isOpenRouterChatModel returns true if the model can produce text output for chat.
func isOpenRouterChatModel(orm OpenRouterModel) bool {
	for _, mod := range orm.Architecture.OutputModalities {
		if mod == "text" || mod == "code" {
			return true
		}
	}
	// Fallback: check modality string
	m := orm.Architecture.Modality
	return strings.Contains(m, "->text") || strings.Contains(m, "->code")
}

// parseOpenRouterPricing converts per-token string pricing to $/1M floats.
func parseOpenRouterPricing(pricing OpenRouterPricing) (float64, float64) {
	inPrice, _ := strconv.ParseFloat(pricing.Prompt, 64)
	outPrice, _ := strconv.ParseFloat(pricing.Completion, 64)
	return inPrice * 1_000_000, outPrice * 1_000_000
}

// GetOpenRouterBalance retrieves credits and usage info from OpenRouter.
func (d *DiscoveryService) GetOpenRouterBalance(ctx context.Context, provider *Provider, masterKey string) (*OpenRouterBalance, error) {
	apiKey, err := auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, provider.KeySalt, masterKey)
	if err != nil {
		return nil, fmt.Errorf("openrouter: failed to decrypt API key for provider %s: %w", provider.ID, err)
	}

	baseURL := util.SanitizeBaseURL(provider.BaseURL)

	// Fetch credits (actual account balance) from /api/v1/credits
	creditsURL := baseURL + "/credits"
	creditsReq, err := http.NewRequestWithContext(ctx, "GET", creditsURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("openrouter: failed to create credits request for provider %s: %w", provider.ID, err)
	}
	creditsReq.Header.Set("Authorization", "Bearer "+apiKey)
	creditsReq.Header.Set("Content-Type", "application/json")

	creditsResp, err := d.doQuotaRequestWithRetry(ctx, creditsReq, provider.ID.String(), "openrouter")
	if err != nil {
		return nil, fmt.Errorf("openrouter: failed to fetch credits for provider %s: %w", provider.ID, err)
	}
	defer func() { _ = creditsResp.Body.Close() }()

	if creditsResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(creditsResp.Body)
		debuglog.Error("discovery: openrouter credits non-200 status", "status", creditsResp.StatusCode, "provider", provider.ID, "body", util.SanitizeLogBody(string(body), 2000))
		return nil, fmt.Errorf("openrouter: unexpected status code %d from credits endpoint for provider %s", creditsResp.StatusCode, provider.ID)
	}

	var creditsData OpenRouterCreditsResponse
	if err := json.NewDecoder(creditsResp.Body).Decode(&creditsData); err != nil {
		return nil, fmt.Errorf("openrouter: failed to decode credits response for provider %s: %w", provider.ID, err)
	}

	// Fetch key info (limits, usage) from /api/v1/key
	keyURL := baseURL + "/key"
	keyReq, err := http.NewRequestWithContext(ctx, "GET", keyURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("openrouter: failed to create key request for provider %s: %w", provider.ID, err)
	}
	keyReq.Header.Set("Authorization", "Bearer "+apiKey)
	keyReq.Header.Set("Content-Type", "application/json")

	keyResp, err := d.doQuotaRequestWithRetry(ctx, keyReq, provider.ID.String(), "openrouter")
	if err != nil {
		return nil, fmt.Errorf("openrouter: failed to fetch key info for provider %s: %w", provider.ID, err)
	}
	defer func() { _ = keyResp.Body.Close() }()

	if keyResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(keyResp.Body)
		debuglog.Error("discovery: openrouter key info non-200 status", "status", keyResp.StatusCode, "provider", provider.ID, "body", util.SanitizeLogBody(string(body), 2000))
		return nil, fmt.Errorf("openrouter: unexpected status code %d from key endpoint for provider %s", keyResp.StatusCode, provider.ID)
	}

	var keyData OpenRouterKeyResponse
	if err := json.NewDecoder(keyResp.Body).Decode(&keyData); err != nil {
		return nil, fmt.Errorf("openrouter: failed to decode key response for provider %s: %w", provider.ID, err)
	}

	remaining := creditsData.Data.TotalCredits - creditsData.Data.TotalUsage
	if remaining < 0 {
		remaining = 0
	}

	return &OpenRouterBalance{
		Label:            keyData.Data.Label,
		Limit:            keyData.Data.Limit,
		LimitReset:       keyData.Data.LimitReset,
		LimitRemaining:   keyData.Data.LimitRemaining,
		Usage:            keyData.Data.Usage,
		UsageDaily:       keyData.Data.UsageDaily,
		UsageWeekly:      keyData.Data.UsageWeekly,
		UsageMonthly:     keyData.Data.UsageMonthly,
		CreditsTotal:     creditsData.Data.TotalCredits,
		CreditsUsed:      creditsData.Data.TotalUsage,
		CreditsRemaining: remaining,
		IsFreeTier:       keyData.Data.IsFreeTier,
	}, nil
}

// openRouterParamsToCapabilities maps supported_parameters to our Capability struct.
func openRouterParamsToCapabilities(params []string) model.Capability {
	caps := model.Capability{
		Streaming: true, // All OpenRouter models support streaming
	}
	for _, p := range params {
		switch p {
		case "tools":
			caps.ToolCalling = true
		case "reasoning":
			caps.Reasoning = true
		case "structured_outputs":
			caps.StructuredOutput = true
		}
	}
	return caps
}
