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

	// Keyless providers can only reach free models. Zen's listing does not
	// say which those are; models.dev's opencode entry does, by pricing them
	// at zero, so keyless discovery keeps exactly the live models models.dev
	// prices at zero and never shows a keyless caller a model it cannot use.
	// Without the models.dev cache nothing can be told free, so nothing is
	// kept; a keyed provider surfaces every model the listing carries and
	// leaves the metadata to models.dev enrichment.
	keyless := len(provider.EncryptedKey) == 0
	cache := GetModelsDevCache()
	live := make([]*model.Model, 0, len(openAIResp.Data))
	for _, m := range openAIResp.Data {
		free := cache.FreeOnProvider("opencode-zen", m.ID)
		if keyless && !free {
			debuglog.Info("discovery: opencode-zen skipping paid/unknown model (keyless)", "model", m.ID, "provider", provider.Name, "provider_id", provider.ID)
			continue
		}
		stub := liveModelStub(m.ID, m.OwnedBy, provider.ID)
		if free {
			// Enrichment reads a zero on models.dev as "no figure", which is
			// what it means on the subscription entries. On Zen's entry it
			// means free, so the price is written here as a known zero rather
			// than left absent and reported as unpriced on every scan.
			stub.InputPricePerMillion, stub.OutputPricePerMillion = new(float64), new(float64)
			stub.StampPriceSources(model.PriceSourceModelsDev)
		}
		live = append(live, stub)
	}
	if keyless && cache == nil && len(openAIResp.Data) > 0 {
		debuglog.Warn("discovery: opencode-zen keyless discovery needs models.dev to tell the free models apart, none kept", "provider", provider.Name, "provider_id", provider.ID)
	}
	// Empty-but-successful listing: return empty so RecordMissingModels stays
	// a no-op instead of disabling live-only models.
	if len(live) == 0 {
		debuglog.Warn("discovery: opencode-zen /models returned no usable models, skipping", "provider", provider.Name, "provider_id", provider.ID, "keyless", keyless)
		return live, nil
	}
	// The catalog backfills only: it holds the input modalities models.dev
	// gets wrong for a few free models, and must never union a model back in
	// once Zen has dropped it from the listing.
	live = backfillLiveFromCatalog(live, opencodeCatalogModels(GetOpenCodeZenCatalog(), provider.ID, "opencode"))
	debuglog.Info("discovery: opencode-zen discovered models", "models", len(live), "provider", provider.Name, "provider_id", provider.ID, "keyless", keyless)
	return live, nil
}
