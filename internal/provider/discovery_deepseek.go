package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

func (d *DiscoveryService) discoverDeepSeek(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+apiKey)
	headers.Set("Content-Type", "application/json")

	bodyBytes, err := d.fetchURL(ctx, "GET", baseURL+"/models", headers)
	if err != nil {
		debuglog.Error("discovery: deepseek fetch models failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("deepseek: failed to fetch models for provider %s: %w", provider.Name, err)
	}

	var openAIResp OpenAIModelsResponse
	if err := json.Unmarshal(bodyBytes, &openAIResp); err != nil {
		debuglog.Error("discovery: deepseek json decode failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("deepseek: failed to decode response for provider %s: %w", provider.Name, err)
	}

	catalog := GetDeepSeekModels()
	catalogMap := make(map[string]DeepSeekModelSpec)
	for _, spec := range catalog {
		catalogMap[spec.ModelID] = spec
	}

	models := make([]*model.Model, 0, len(openAIResp.Data))
	for _, m := range openAIResp.Data {
		contextLen := 128000
		maxOutput := 8192
		reasoning := false
		inPriceCacheHit := 0.0
		inPriceCacheMiss := 0.0
		outPrice := 0.0

		if spec, ok := catalogMap[m.ID]; ok {
			contextLen = spec.ContextLength
			maxOutput = spec.MaxOutputTokens
			reasoning = spec.Reasoning
			inPriceCacheHit = spec.InputPricePerMillionCacheHit
			inPriceCacheMiss = spec.InputPricePerMillionCacheMiss
			outPrice = spec.OutputPricePerMillion
		}

		caps := model.Capability{
			Streaming:   true,
			Reasoning:   reasoning,
			ToolCalling: true,
		}
		capJSON, _ := json.Marshal(caps)

		models = append(models, &model.Model{
			ID:                           uuid.New(),
			ProviderID:                   provider.ID,
			ModelID:                      m.ID,
			Name:                         m.ID,
			DisplayName:                  m.ID,
			Capabilities:                 string(capJSON),
			Params:                       "{}",
			Modality:                     "text",
			InputModalities:              "[]",
			OutputModalities:             "[]",
			ContextLength:                &contextLen,
			MaxOutputTokens:              &maxOutput,
			InputPricePerMillion:         &inPriceCacheMiss,
			InputPricePerMillionCacheHit: &inPriceCacheHit,
			OutputPricePerMillion:        &outPrice,
			OwnedBy:                      m.OwnedBy,
			Enabled:                      true,
		})
	}

	debuglog.Info("discovery: deepseek discovered models", "models", len(models), "provider", provider.Name, "provider_id", provider.ID)
	return models, nil
}

// GetDeepSeekBalance retrieves the account balance from DeepSeek.
func (d *DiscoveryService) GetDeepSeekBalance(ctx context.Context, provider *Provider, masterKey string) (*DeepSeekBalanceResponse, error) {
	var balance DeepSeekBalanceResponse
	if err := d.fetchQuotaJSON(ctx, provider, masterKey, "/user/balance", "deepseek", "balance", &balance); err != nil {
		return nil, err
	}
	return &balance, nil
}
