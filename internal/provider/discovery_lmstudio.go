package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// LMStudio /v1/models response (OpenAI-compatible)
type LMStudioModelsResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	} `json:"data"`
}

func (d *DiscoveryService) discoverLMStudio(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)

	url := baseURL + "/models"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		log.Printf("[discovery] lmstudio: http request failed for provider %s: %v", provider.ID, err)
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(body))
	}

	var modelsResp LMStudioModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	models := make([]*model.Model, 0, len(modelsResp.Data))
	for _, m := range modelsResp.Data {
		caps := model.Capability{
			Streaming:       true,
			StructuredOutput: true, // LM Studio supports response_format with JSON schema
		}
		capJSON, _ := json.Marshal(caps)

		// LM Studio model IDs use creator/model-name format
		displayName := m.ID
		ownedBy := m.OwnedBy
		if ownedBy == "" {
			ownedBy = "lmstudio"
		}

		model := &model.Model{
			ID:               uuid.New(),
			ProviderID:       provider.ID,
			ModelID:          m.ID,
			Name:             m.ID,
			DisplayName:      displayName,
			Description:      "LM Studio local model",
			Capabilities:     string(capJSON),
			Params:           "{}",
			Modality:         "text",
			InputModalities:  `["text"]`,
			OutputModalities: `["text"]`,
			OwnedBy:          ownedBy,
			Enabled:          true,
		}

		models = append(models, model)
	}

	log.Printf("[discovery] lmstudio: discovered %d models for provider %s", len(models), provider.ID)
	return models, nil
}