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

// discoverMiniMax fetches the live model list from the MiniMax intl platform
// (api.minimax.io) OpenAI-compatible /models endpoint. The listing is
// metadata-bare (id/owner only) and there is no embedded catalog: models
// become live stubs and models.dev backfills context, pricing, and
// capabilities downstream.
func (d *DiscoveryService) discoverMiniMax(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+apiKey)
	headers.Set("Content-Type", "application/json")

	bodyBytes, err := d.fetchURL(ctx, "GET", baseURL+"/models", headers)
	if err != nil {
		debuglog.Error("discovery: minimax fetch models failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("minimax: failed to fetch models for provider %s: %w", provider.Name, err)
	}

	var openAIResp OpenAIModelsResponse
	if err := json.Unmarshal(bodyBytes, &openAIResp); err != nil {
		debuglog.Error("discovery: minimax json decode failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("minimax: failed to decode response for provider %s: %w", provider.Name, err)
	}

	live := make([]*model.Model, 0, len(openAIResp.Data))
	for _, m := range openAIResp.Data {
		live = append(live, liveModelStub(m.ID, m.OwnedBy, provider.ID))
	}
	// Empty-but-successful listing: return empty rather than erroring so
	// RecordMissingModels stays a no-op instead of disabling live-only models.
	if len(live) == 0 {
		debuglog.Warn("discovery: minimax /models returned no models, skipping", "provider", provider.Name, "provider_id", provider.ID)
		return live, nil
	}
	markLiveMeta(live)
	debuglog.Info("discovery: minimax discovered models", "provider", provider.Name, "provider_id", provider.ID, "models", len(live))
	return live, nil
}

// GetMiniMaxQuota retrieves Token Plan window quota for a MiniMax provider
// from its /token_plan/remains endpoint. The payload is passed through as-is
// (including base_resp): MiniMax reports business errors such as 2062 "no
// active token plan subscription" inside an HTTP 200, and the dashboard
// decides badge visibility from base_resp and the per-entry status fields.
func (d *DiscoveryService) GetMiniMaxQuota(ctx context.Context, provider *Provider, masterKey string) (*MiniMaxQuotaResponse, error) {
	var quota MiniMaxQuotaResponse
	if err := d.fetchQuotaJSON(ctx, provider, masterKey, "/token_plan/remains", "minimax", "quota", &quota); err != nil {
		return nil, err
	}
	return &quota, nil
}
