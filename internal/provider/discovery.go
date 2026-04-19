package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/user/llm-proxy/internal/auth"
	"github.com/user/llm-proxy/internal/model"
)

type DiscoveryService struct {
	httpClient *http.Client
}

func NewDiscoveryService() *DiscoveryService {
	return &DiscoveryService{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

type OpenAIModelsResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	} `json:"data"`
}

func (d *DiscoveryService) DiscoverModels(ctx context.Context, provider *Provider, masterKey string) ([]*model.Model, error) {
	apiKey, err := auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, masterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt API key: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", provider.BaseURL+"/v1/models", nil)
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

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(body))
	}

	var openAIResp OpenAIModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&openAIResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	models := make([]*model.Model, 0, len(openAIResp.Data))
	for _, m := range openAIResp.Data {
		capabilities := model.Capability{
			Vision:    hasVisionCapability(m.ID),
			Streaming: true,
		}

		capJSON, _ := json.Marshal(capabilities)

		models = append(models, &model.Model{
			ID:           uuid.New(),
			ProviderID:   provider.ID,
			ModelID:      m.ID,
			DisplayName:  m.ID,
			Capabilities: string(capJSON),
			Params:       "{}",
			Enabled:      true,
		})
	}

	return models, nil
}

func hasVisionCapability(modelID string) bool {
	visionModels := []string{
		"gpt-4-vision-preview",
		"gpt-4o",
		"gpt-4o-mini",
		"claude-3-opus",
		"claude-3-sonnet",
		"claude-3-haiku",
		"claude-3.5-sonnet",
	}

	for _, v := range visionModels {
		if modelID == v {
			return true
		}
	}

	return false
}
