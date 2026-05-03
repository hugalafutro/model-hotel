package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

func (d *DiscoveryService) discoverOpenRouter(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	url := fmt.Sprintf("%s/models", baseURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		log.Printf("[discovery] openrouter: http request failed for provider %s: %v", provider.ID, err)
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[discovery] openrouter: non-200 status %d from provider %s: %s", resp.StatusCode, provider.ID, util.SanitizeLogBody(string(bodyBytes), 200))
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var orResp OpenRouterModelsResponse
	if err := json.Unmarshal(bodyBytes, &orResp); err != nil {
		log.Printf("[discovery] openrouter: failed to decode response from provider %s: %v", provider.ID, err)
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	models := make([]*model.Model, 0, len(orResp.Data))
	for _, orm := range orResp.Data {
		// Skip auto-routing aliases (e.g., "~anthropic/claude-haiku-latest")
		if strings.HasPrefix(orm.ID, "~") {
			continue
		}

		// Skip non-text-output models (image generation only, embedding only, etc.)
		if !isOpenRouterChatModel(orm) {
			log.Printf("[discovery] openrouter: skipping non-chat model %s (modality: %s)", orm.ID, orm.Architecture.Modality)
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

	log.Printf("[discovery] openrouter: discovered %d models for provider %s (from %d total)", len(models), provider.ID, len(orResp.Data))
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
