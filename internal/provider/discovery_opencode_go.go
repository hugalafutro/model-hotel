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

func (d *DiscoveryService) discoverOpenCodeGo(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/models", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		debuglog.Error("discovery: opencode-go http request failed", "provider", provider.ID, "error", err)
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	// If /models endpoint is not available, fall back to full catalog.
	// This handles the case where the endpoint may be removed or rate-limited.
	if resp.StatusCode == http.StatusNotFound {
		debuglog.Warn("discovery: opencode-go /models returned 404, falling back to catalog", "provider", provider.ID)
		catalog := GetOpenCodeGoCatalog()
		models := make([]*model.Model, 0, len(catalog))
		for i := range catalog {
			models = append(models, OpenCodeCatalogToModel(&catalog[i], provider.ID, "opencode"))
		}
		return models, nil
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		debuglog.Error("discovery: opencode-go unexpected status", "provider", provider.ID, "status", resp.StatusCode, "body", util.SanitizeLogBody(string(bodyBytes), 2000))
		return nil, fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	var openAIResp OpenAIModelsResponse
	if err := json.Unmarshal(bodyBytes, &openAIResp); err != nil {
		debuglog.Error("discovery: opencode-go json decode failed", "provider", provider.ID, "error", err)
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	catalog := GetOpenCodeGoCatalog()

	models := make([]*model.Model, 0, len(openAIResp.Data))
	for _, m := range openAIResp.Data {
		spec := LookupOpenCodeCatalog(catalog, m.ID)
		if spec == nil {
			debuglog.Warn("discovery: opencode-go model not in catalog", "provider", provider.ID, "model", m.ID)
			// Model exists in API but not in our catalog — create minimal entry
			// (preserves forward compatibility when new models are added)
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

	debuglog.Info("discovery: opencode-go discovered models", "provider", provider.ID, "models", len(models))
	return models, nil
}
