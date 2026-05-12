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

func (d *DiscoveryService) discoverOpenCodeZen(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+apiKey)
	headers.Set("Content-Type", "application/json")

	bodyBytes, err := d.fetchURL(ctx, "GET", baseURL+"/models", headers)
	if err != nil {
		debuglog.Error("discovery: opencode-zen http request failed", "provider", provider.ID, "error", err)
		return nil, fmt.Errorf("opencode-zen: failed to fetch models for provider %s: %w", provider.ID, err)
	}

	var openAIResp OpenAIModelsResponse
	if err := json.Unmarshal(bodyBytes, &openAIResp); err != nil {
		debuglog.Error("discovery: opencode-zen failed to decode response", "provider", provider.ID, "error", err)
		return nil, fmt.Errorf("opencode-zen: failed to decode response for provider %s: %w", provider.ID, err)
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
