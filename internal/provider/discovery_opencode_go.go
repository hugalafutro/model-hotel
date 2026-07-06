package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

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

	resp, err := d.doDiscoveryRequestPrebuilt(ctx, req)
	if err != nil {
		debuglog.Error("discovery: opencode-go http request failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("opencode-go: failed to fetch models for provider %s: %w", provider.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	catalog := opencodeCatalogModels(GetOpenCodeGoCatalog(), provider.ID, "opencode")

	// If the /models endpoint is gone (404), fall back to the full catalog.
	// Other non-200s return an error so a transient outage aborts the scan
	// instead of disabling live-only models that the catalog doesn't list.
	if resp.StatusCode == http.StatusNotFound {
		debuglog.Warn("discovery: opencode-go /models returned 404, falling back to catalog", "provider", provider.Name, "provider_id", provider.ID)
		return catalog, nil
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
