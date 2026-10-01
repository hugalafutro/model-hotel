package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"math"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

func (d *DiscoveryService) discoverOpenAI(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)

	// bearerHeader, not a bare Set: an OpenAI-compatible local server (LM
	// Studio, KoboldCPP started without --password) would otherwise be sent a
	// literal "Bearer " with nothing behind it.
	headers := bearerHeader(apiKey)
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
		// A plain /models listing carries no model type; self-hosted
		// embedding/reranker models (and OpenAI's own text-embedding-*,
		// tts-*, whisper-* models) are classified out of the chat picker by
		// NormalizeModelClassification's name heuristics.
		live = append(live, applyListingExtras(liveModelStub(m.ID, m.OwnedBy, provider.ID), m))
	}

	// A provider the operator added as custom or as a self-hosted server is
	// neither backfilled here nor enriched by models.dev: it serves whatever
	// its operator loaded, and a model it names gpt-5.5-pro is not OpenAI's.
	//
	// Otherwise backfill-only (no union): discoverOpenAI is also the fallback
	// for the generic openai type on unknown hosts, so the gpt-5.x catalog must
	// enrich matching models without adding phantom OpenAI models to it. For
	// real OpenAI the catalog is a subset of the live listing, so there is
	// nothing to union regardless, and models.dev enriches the rest. An empty
	// listing stays empty, so RecordMissingModels is a no-op.
	if operatorServedProvider(provider) {
		debuglog.Info("discovery: openai-compatible discovered models", "provider", provider.Name, "provider_id", provider.ID, "live", len(live))
		return live, nil
	}
	backfilled := backfillLiveFromCatalog(live, opencodeCatalogModels(openaiCatalog, provider.ID, "openai"))
	debuglog.Info("discovery: openai discovered models", "provider", provider.Name, "provider_id", provider.ID, "live", len(live), "catalog", len(GetOpenAIModels()))
	return backfilled, nil
}

// applyListingExtras takes what a self-hosted server adds to the plain /models
// entry. The input modalities are its own statement of what the model takes
// (llama.cpp reports image input for a model loaded with a vision projector);
// the output modalities are not read, since llama.cpp reports text output for
// its embedding and reranking models too, and the name decides those. The
// context length is what the server runs the model with, so it is marked live:
// llama.cpp's meta.n_ctx, only there while the model is loaded, or vLLM's
// max_model_len, part of the server's configuration and so on every scan.
// A valid meta.n_ctx wins; an absent or malformed one falls back to
// max_model_len, and a scan that finds neither leaves the stored value alone.
// A listing that carries none of these (OpenAI's own, most servers) is
// unaffected.
func applyListingExtras(m *model.Model, entry OpenAIModel) *model.Model {
	if input := listingInputModalities(entry.Architecture); len(input) > 0 {
		m.InputModalities = marshalModalityList(input)
	}
	n := listingContext(entry.Meta)
	if n == 0 {
		n = wholePositive(entry.MaxModelLen)
	}
	if n > 0 {
		m.ContextLength = &n
		m.MarkLiveMetaFromCurrent()
	}
	return m
}

// listingInputModalities reads architecture.input_modalities, returning nil for
// any other shape.
func listingInputModalities(raw json.RawMessage) []string {
	var arch struct {
		InputModalities []string `json:"input_modalities"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &arch) != nil {
		return nil
	}
	return canonicalizeModalityList(arch.InputModalities)
}

// listingContext reads meta.n_ctx (see wholePositive), returning 0 for any
// other shape.
func listingContext(raw json.RawMessage) int {
	var meta struct {
		NCtx json.RawMessage `json:"n_ctx"`
	}
	if len(raw) == 0 || json.Unmarshal(raw, &meta) != nil {
		return 0
	}
	return wholePositive(meta.NCtx)
}

// wholePositive reads a JSON value as a positive whole number no larger than
// an int32 (written as an integer, a float such as 4096.0, or a quoted number,
// which decoding into json.Number accepts), returning 0 for anything else.
func wholePositive(raw json.RawMessage) int {
	var n json.Number
	if len(raw) == 0 || json.Unmarshal(raw, &n) != nil {
		return 0
	}
	f, err := n.Float64()
	if err != nil || f <= 0 || f > math.MaxInt32 || f != math.Trunc(f) {
		return 0
	}
	return int(f)
}
