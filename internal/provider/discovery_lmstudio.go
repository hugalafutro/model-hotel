package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// LMStudioModelsResponse is the OpenAI-compatible models response from LMStudio.
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
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("lmstudio: failed to create request for provider %s: %w", provider.Name, err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		debuglog.Error("discovery: lmstudio http request failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("lmstudio: failed to fetch models for provider %s: %w", provider.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("lmstudio: unexpected status %d for provider %s: %s", resp.StatusCode, provider.Name, string(body))
	}

	var modelsResp LMStudioModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return nil, fmt.Errorf("lmstudio: failed to decode response for provider %s: %w", provider.Name, err)
	}

	models := make([]*model.Model, 0, len(modelsResp.Data))
	for _, m := range modelsResp.Data {
		caps := model.Capability{
			Streaming:        true,
			StructuredOutput: true, // LM Studio supports response_format with JSON schema
		}
		capJSON, _ := json.Marshal(caps)

		// LM Studio model IDs use creator/model-name format
		displayName := m.ID
		ownedBy := m.OwnedBy
		if ownedBy == "" {
			ownedBy = "lmstudio"
		}

		//nolint:gocritic // model variable shadows import but context makes it clear
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

	debuglog.Info("discovery: lmstudio discovered models", "models", len(models), "provider", provider.Name, "provider_id", provider.ID)
	return models, nil
}
