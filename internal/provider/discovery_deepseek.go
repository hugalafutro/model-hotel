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

func (d *DiscoveryService) discoverDeepSeek(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+apiKey)
	headers.Set("Content-Type", "application/json")

	bodyBytes, err := d.fetchURL(ctx, "GET", baseURL+"/models", headers)
	if err != nil {
		debuglog.Error("discovery: deepseek fetch models failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("deepseek: failed to fetch models for provider %s: %w", provider.Name, err)
	}

	var openAIResp OpenAIModelsResponse
	if err := json.Unmarshal(bodyBytes, &openAIResp); err != nil {
		debuglog.Error("discovery: deepseek json decode failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("deepseek: failed to decode response for provider %s: %w", provider.Name, err)
	}

	// Live /models only carries id + owner; merge unions it with the catalog
	// (live wins, catalog backfills context/max-output/pricing/reasoning). The
	// old hardcoded 128k/8k default for uncatalogued models is dropped: such a
	// model now becomes a clean stub enriched by models.dev, the same as every
	// other provider (DeepSeek models are 1M/384K, so the old default was stale).
	live := make([]*model.Model, 0, len(openAIResp.Data))
	for _, m := range openAIResp.Data {
		live = append(live, liveModelStub(m.ID, m.OwnedBy, provider.ID))
	}
	// Empty-but-successful listing: return empty rather than the catalog so
	// RecordMissingModels stays a no-op instead of disabling live-only models.
	if len(live) == 0 {
		debuglog.Warn("discovery: deepseek /models returned no models, skipping", "provider", provider.Name, "provider_id", provider.ID)
		return live, nil
	}

	merged := mergeLiveAndCatalog(live, deepseekCatalogModels(provider.ID))
	debuglog.Info("discovery: deepseek discovered models", "provider", provider.Name, "provider_id", provider.ID, "live", len(live), "catalog", len(GetDeepSeekModels()), "merged", len(merged))
	return merged, nil
}

// GetDeepSeekBalance retrieves the account balance from DeepSeek.
func (d *DiscoveryService) GetDeepSeekBalance(ctx context.Context, provider *Provider, masterKey string) (*DeepSeekBalanceResponse, error) {
	var balance DeepSeekBalanceResponse
	if err := d.fetchQuotaJSON(ctx, provider, masterKey, "/user/balance", "deepseek", "balance", &balance); err != nil {
		return nil, err
	}
	return &balance, nil
}
