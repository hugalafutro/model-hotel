package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/user/llm-proxy/internal/auth"
	"github.com/user/llm-proxy/internal/model"
	"github.com/user/llm-proxy/internal/util"
)

func (d *DiscoveryService) discoverDeepSeek(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	raw := util.SanitizeBaseURL(provider.BaseURL)
	baseURL := strings.TrimSuffix(strings.TrimSuffix(raw, "/"), "/v1")
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var openAIResp OpenAIModelsResponse
	if err := json.Unmarshal(bodyBytes, &openAIResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	catalog := GetDeepSeekModels()
	catalogMap := make(map[string]DeepSeekModelSpec)
	for _, spec := range catalog {
		catalogMap[spec.ModelID] = spec
	}

	models := make([]*model.Model, 0, len(openAIResp.Data))
	for _, m := range openAIResp.Data {
		spec, ok := catalogMap[m.ID]
		if !ok {
			continue
		}

		contextLen := spec.ContextLength
		maxOutput := spec.MaxOutputTokens

		caps := model.Capability{
			Streaming:   true,
			Reasoning:   spec.Reasoning,
			ToolCalling: true,
		}
		capJSON, _ := json.Marshal(caps)

		inPricePerMill := spec.InputPricePerMillionCacheMiss
		outPricePerMill := spec.OutputPricePerMillion
		inPriceCacheHit := spec.InputPricePerMillionCacheHit

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
			InputPricePerMillion:         &inPricePerMill,
			InputPricePerMillionCacheHit: &inPriceCacheHit,
			OutputPricePerMillion:        &outPricePerMill,
			OwnedBy:                      m.OwnedBy,
			Enabled:                      true,
		})
	}

	return models, nil
}

func (d *DiscoveryService) GetDeepSeekBalance(ctx context.Context, provider *Provider, masterKey string) (*DeepSeekBalanceResponse, error) {
	apiKey, err := auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, provider.KeySalt, masterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt API key: %w", err)
	}

	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	balanceURL := baseURL + "/user/balance"

	req, err := http.NewRequestWithContext(ctx, "GET", balanceURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch balance: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var balance DeepSeekBalanceResponse
	if err := json.NewDecoder(resp.Body).Decode(&balance); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &balance, nil
}
