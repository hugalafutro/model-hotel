package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

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
		debuglog.Error("discovery: opencode-zen http request failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("opencode-zen: failed to fetch models for provider %s: %w", provider.Name, err)
	}

	var openAIResp OpenAIModelsResponse
	if err := json.Unmarshal(bodyBytes, &openAIResp); err != nil {
		debuglog.Error("discovery: opencode-zen failed to decode response", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("opencode-zen: failed to decode response for provider %s: %w", provider.Name, err)
	}

	catalog := GetOpenCodeZenCatalog()

	// Keyless providers can only reach free models, identified by a zero-priced
	// catalog entry. Emit exactly those (with catalog metadata) and do NOT union
	// the rest of the catalog, preserving the original keyless behavior — a
	// keyless caller must not be shown models it cannot use.
	if len(provider.EncryptedKey) == 0 {
		models := make([]*model.Model, 0, len(openAIResp.Data))
		for _, m := range openAIResp.Data {
			spec := LookupOpenCodeCatalog(catalog, m.ID)
			if spec == nil || spec.InputPricePerMillion > 0 || spec.OutputPricePerMillion > 0 {
				debuglog.Info("discovery: opencode-zen skipping paid/unknown model (keyless)", "model", m.ID, "provider", provider.Name, "provider_id", provider.ID)
				continue
			}
			models = append(models, OpenCodeCatalogToModel(spec, provider.ID, "opencode"))
		}
		debuglog.Info("discovery: opencode-zen discovered free models (keyless)", "models", len(models), "provider", provider.Name, "provider_id", provider.ID)
		return models, nil
	}

	// Keyed providers: union the live listing with the full catalog (live wins,
	// catalog backfills metadata and adds models the listing omits).
	catalogModels := opencodeCatalogModels(catalog, provider.ID, "opencode")
	live := make([]*model.Model, 0, len(openAIResp.Data))
	for _, m := range openAIResp.Data {
		live = append(live, liveModelStub(m.ID, m.OwnedBy, provider.ID))
	}

	merged := mergeLiveAndCatalog(live, catalogModels)
	debuglog.Info("discovery: opencode-zen discovered models", "models", len(merged), "provider", provider.Name, "provider_id", provider.ID, "live", len(live), "catalog", len(catalogModels))
	return merged, nil
}
