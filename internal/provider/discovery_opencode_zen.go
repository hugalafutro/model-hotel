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

func (d *DiscoveryService) discoverOpenCodeZen(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		debuglog.Error("discovery: opencode-zen http request failed", "provider", provider.ID, "error", err)
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		debuglog.Error("discovery: opencode-zen non-200 status", "status", resp.StatusCode, "provider", provider.ID, "body", util.SanitizeLogBody(string(bodyBytes), 2000))
		return nil, fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	var openAIResp OpenAIModelsResponse
	if err := json.Unmarshal(bodyBytes, &openAIResp); err != nil {
		debuglog.Error("discovery: opencode-zen failed to decode response", "provider", provider.ID, "error", err)
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	catalog := GetOpenCodeZenCatalog()
	keyless := len(provider.EncryptedKey) == 0

	models := make([]*model.Model, 0, len(openAIResp.Data))
	for _, m := range openAIResp.Data {
		spec := LookupOpenCodeCatalog(catalog, m.ID)

		if keyless {
			if spec == nil || spec.InputPricePerMillion > 0 || spec.OutputPricePerMillion > 0 {
				debuglog.Info("discovery: opencode-zen skipping paid model", "model", m.ID, "provider", provider.ID)
				continue
			}
		}

		if spec == nil {
			debuglog.Warn("discovery: opencode-zen model not in catalog", "model", m.ID)
			capJSON, _ := json.Marshal(model.Capability{Streaming: true})
			models = append(models, &model.Model{
				ID:               uuid.New(),
				ProviderID:       provider.ID,
				ModelID:          m.ID,
				Name:             m.ID,
				DisplayName:      m.ID,
				Capabilities:     string(capJSON),
				Params:           "{}",
				Modality:         "text",
				InputModalities:  "[]",
				OutputModalities: "[]",
				OwnedBy:          m.OwnedBy,
				Enabled:          true,
			})
			continue
		}
		models = append(models, OpenCodeCatalogToModel(spec, provider.ID, "opencode"))
	}

	debuglog.Info("discovery: opencode-zen discovered models", "models", len(models), "provider", provider.ID)
	return models, nil
}
