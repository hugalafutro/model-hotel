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

func (d *DiscoveryService) discoverOpenAI(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+apiKey)
	headers.Set("Content-Type", "application/json")

	bodyBytes, err := d.fetchURL(ctx, "GET", baseURL+"/models", headers)
	if err != nil {
		debuglog.Error("discovery: openai fetch models failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("openai: failed to fetch models for provider %s: %w", provider.Name, err)
	}

	var openAIResp OpenAIModelsResponse
	if err := json.Unmarshal(bodyBytes, &openAIResp); err != nil {
		debuglog.Error("discovery: openai json decode failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("openai: failed to decode response for provider %s: %w", provider.Name, err)
	}

	// Live /models only carries id + owner; merge unions it with the catalog
	// (live wins, catalog backfills the gpt-5.x specs the listing omits, and
	// the ~110 uncatalogued models are enriched by models.dev instead of the
	// old fabricated "text"/"[]" minimal entry).
	live := make([]*model.Model, 0, len(openAIResp.Data))
	for _, m := range openAIResp.Data {
		stub := liveModelStub(m.ID, m.OwnedBy, provider.ID)
		// A plain /models listing carries no model type, so self-hosted
		// embedding/reranker models (and OpenAI's own text-embedding-* models)
		// would otherwise be enriched to modality:"text" and appear in the chat
		// picker. Classify the obvious ones by name; a set modality survives
		// models.dev enrichment, which only fills empty/"text" modalities.
		if mod := inferNonChatModality(m.ID); mod != "" {
			stub.Modality = mod
		}
		live = append(live, stub)
	}

	// Backfill-only (no union): discoverOpenAI is the fallback for unknown/custom
	// hosts, so the gpt-5.x catalog must enrich matching models without adding
	// phantom OpenAI models to a custom provider. For real OpenAI the catalog is
	// a subset of the live listing, so there is nothing to union regardless.
	// models.dev still enriches the rest. An empty listing stays empty, so
	// DisableMissingModels is a no-op.
	backfilled := backfillLiveFromCatalog(live, openaiCatalogModels(provider.ID))
	debuglog.Info("discovery: openai discovered models", "provider", provider.Name, "provider_id", provider.ID, "live", len(live), "catalog", len(GetOpenAIModels()))
	return backfilled, nil
}
