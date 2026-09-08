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

func (d *DiscoveryService) discoverOpenCodeGo(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	headers := bearerHeader(apiKey)
	headers.Set("Content-Type", "application/json")

	catalog := opencodeCatalogModels(GetOpenCodeGoCatalog(), provider.ID, "opencode")

	bodyBytes, err := d.fetchURL(ctx, "GET", baseURL+"/models", headers)
	if err != nil {
		// If the /models endpoint is gone (404), fall back to the catalog. The
		// catalog is an override channel that is normally empty, so this usually
		// yields no models — which keeps RecordMissingModels a no-op rather than
		// disabling anything, at the cost of a discovery.suspect_scan warning per
		// scan while the 404 persists (an empty result trips the blackout guard in
		// ConfirmMissingModels). Every other failure returns an error so a
		// transient outage aborts the scan instead of disabling live-only models.
		if errorStatusCode(err) == http.StatusNotFound {
			debuglog.Warn("discovery: opencode-go /models returned 404, falling back to catalog", "provider", provider.Name, "provider_id", provider.ID)
			return catalog, nil
		}
		debuglog.Error("discovery: opencode-go http request failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("opencode-go: failed to fetch models for provider %s: %w", provider.Name, statusOnly(err))
	}

	var openAIResp OpenAIModelsResponse
	if err := json.Unmarshal(bodyBytes, &openAIResp); err != nil {
		debuglog.Error("discovery: opencode-go json decode failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("opencode-go: failed to decode response for provider %s: %w", provider.Name, err)
	}

	// Live listing only carries id + owner; merge unions it with the catalog
	// (live wins, catalog backfills metadata and adds models the listing omits).
	live := make([]*model.Model, 0, len(openAIResp.Data))
	for _, m := range openAIResp.Data {
		live = append(live, liveModelStub(m.ID, m.OwnedBy, provider.ID))
	}
	// Empty-but-successful listing: return empty rather than the catalog so
	// RecordMissingModels stays a no-op instead of disabling live-only models.
	if len(live) == 0 {
		debuglog.Warn("discovery: opencode-go /models returned no models, skipping", "provider", provider.Name, "provider_id", provider.ID)
		return live, nil
	}

	merged := mergeLiveAndCatalog(live, catalog)
	debuglog.Info("discovery: opencode-go discovered models", "provider", provider.Name, "provider_id", provider.ID, "live", len(live), "catalog", len(catalog), "merged", len(merged))
	return merged, nil
}
