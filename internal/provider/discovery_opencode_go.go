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
		return nil, fmt.Errorf("opencode-go: failed to create request for provider %s: %w", provider.Name, err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		debuglog.Error("discovery: opencode-go http request failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("opencode-go: failed to fetch models for provider %s: %w", provider.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	// If /models endpoint is not available, fall back to full catalog.
	// This handles the case where the endpoint may be removed or rate-limited.
	if resp.StatusCode == http.StatusNotFound {
		debuglog.Warn("discovery: opencode-go /models returned 404, falling back to catalog", "provider", provider.Name, "provider_id", provider.ID)
		catalog := GetOpenCodeGoCatalog()
		models := make([]*model.Model, 0, len(catalog))
		for i := range catalog {
			models = append(models, OpenCodeCatalogToModel(&catalog[i], provider.ID, "opencode"))
		}
		return models, nil
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("opencode-go: failed to read response for provider %s: %w", provider.Name, err)
	}

	if resp.StatusCode != http.StatusOK {
		debuglog.Error("discovery: opencode-go unexpected status", "provider", provider.Name, "provider_id", provider.ID, "status", resp.StatusCode, "body", util.SanitizeLogBody(string(bodyBytes), 2000))
		return nil, fmt.Errorf("opencode-go: unexpected status code %d for provider %s", resp.StatusCode, provider.Name)
	}

	var openAIResp OpenAIModelsResponse
	if err := json.Unmarshal(bodyBytes, &openAIResp); err != nil {
		debuglog.Error("discovery: opencode-go json decode failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("opencode-go: failed to decode response for provider %s: %w", provider.Name, err)
	}

	catalog := GetOpenCodeGoCatalog()

	models := make([]*model.Model, 0, len(openAIResp.Data))
	for _, m := range openAIResp.Data {
		spec := LookupOpenCodeCatalog(catalog, m.ID)
		if spec == nil {
			debuglog.Warn("discovery: opencode-go model not in catalog", "provider", provider.Name, "provider_id", provider.ID, "model", m.ID)
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

	debuglog.Info("discovery: opencode-go discovered models", "provider", provider.Name, "provider_id", provider.ID, "models", len(models))
	return models, nil
}
