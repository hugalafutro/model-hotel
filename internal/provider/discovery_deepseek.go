package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
		log.Printf("[discovery] error: deepseek fetch models failed for provider %s: %v", provider.ID, err)
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[discovery] error: deepseek returned status %d for provider %s", resp.StatusCode, provider.ID)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var openAIResp OpenAIModelsResponse
	if err := json.Unmarshal(bodyBytes, &openAIResp); err != nil {
		log.Printf("[discovery] error: deepseek json decode failed for provider %s: %v", provider.ID, err)
		return nil, fmt.Errorf("failed to decode response: %w", err)
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

	log.Printf("[discovery] deepseek discovered %d models for provider %s", len(models), provider.ID)
	return models, nil
}

func (d *DiscoveryService) GetDeepSeekBalance(ctx context.Context, provider *Provider, masterKey string) (*DeepSeekBalanceResponse, error) {
	apiKey, err := auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, provider.KeySalt, masterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt API key: %w", err)
	}

	raw := util.SanitizeBaseURL(provider.BaseURL)
	baseURL := strings.TrimSuffix(strings.TrimSuffix(raw, "/"), "/v1")
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
