package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/jsonfault"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

func (d *DiscoveryService) discoverNanoGPT(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	debuglog.Info("discovery: starting nanogpt discovery", "provider", provider.Name, "provider_id", provider.ID)
	baseURL := util.SanitizeBaseURL(provider.BaseURL)

	headers := bearerJSONHeader(apiKey)

	// NanoGPT hides its decision models (output_modalities ["decisions"],
	// served through the response_format "questions" shim on chat
	// completions, which the proxy forwards unchanged) from the plain listing;
	// asking for text and decisions keeps them discovered beside the chat
	// models instead of retiring them as missing.
	bodyBytes, err := d.fetchURL(ctx, "GET", baseURL+"/models?detailed=true&output_modalities=text,decisions", headers)
	if err != nil {
		debuglog.Error("discovery: nanogpt http request failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("nanogpt: failed to fetch models for provider %s: %w", provider.Name, err)
	}

	var nanoResp NanoGPTDetailedResponse
	if err := json.Unmarshal(bodyBytes, &nanoResp); err != nil {
		debuglog.Error("discovery: nanogpt decode response failed", "provider", provider.Name, "provider_id", provider.ID, "error", jsonfault.Describe(err, len(bodyBytes)))
		return nil, fmt.Errorf("nanogpt: failed to decode response for provider %s: %s", provider.Name, jsonfault.Describe(err, len(bodyBytes)))
	}

	models := make([]*model.Model, 0, len(nanoResp.Data))
	for _, m := range nanoResp.Data {
		caps := model.Capability{
			Streaming:         true,
			Vision:            m.Capabilities.Vision,
			VideoInput:        m.Capabilities.VideoInput,
			AudioInput:        m.Capabilities.AudioInput,
			Reasoning:         m.Capabilities.Reasoning,
			ToolCalling:       m.Capabilities.ToolCalling,
			ParallelToolCalls: m.Capabilities.ParallelToolCalls,
			StructuredOutput:  m.Capabilities.StructuredOutput,
			PDFUpload:         m.Capabilities.PDFUpload,
		}
		capJSON, _ := json.Marshal(caps)

		inputModJSON, _ := json.Marshal(m.Architecture.InputModalities)
		outputModJSON, _ := json.Marshal(m.Architecture.OutputModalities)

		displayName := m.Name
		if displayName == "" {
			displayName = m.ID
		}

		paramsMap := map[string]any{}
		if m.Subscription != nil {
			paramsMap["subscription_included"] = m.Subscription.Included
			paramsMap["subscription_note"] = m.Subscription.Note
		}
		paramsJSON, _ := json.Marshal(paramsMap)

		// Pricing fields are optional: a nil (omitted) price stays nil so it is not
		// marked live and can't overwrite a stored value with 0 on a partial
		// response; a present value (including a real 0) is taken as authoritative.
		// A negative figure is not a price; JSON cannot carry NaN or Inf, but the
		// same guard keeps every path to a stored price honest.
		inPricePerMill := model.PriceOrNil(m.Pricing.Prompt)
		outPricePerMill := model.PriceOrNil(m.Pricing.Completion)

		models = append(models, &model.Model{
			ID:                    uuid.New(),
			ProviderID:            provider.ID,
			ModelID:               m.ID,
			Name:                  m.Name,
			Description:           m.Description,
			DisplayName:           displayName,
			Capabilities:          string(capJSON),
			Params:                string(paramsJSON),
			Modality:              m.Architecture.Modality,
			InputModalities:       string(inputModJSON),
			OutputModalities:      string(outputModJSON),
			ContextLength:         m.ContextLength,
			MaxOutputTokens:       m.MaxOutputTokens,
			InputPricePerMillion:  inPricePerMill,
			OutputPricePerMillion: outPricePerMill,
			OwnedBy:               m.OwnedBy,
			Enabled:               true,
		})
	}

	// Image-generation and embedding models live on their own catalog
	// endpoints, not in the chat /models payload. Both are best-effort: a
	// catalog failure never drops the chat models already in hand. A catalog
	// that answers 404 is one the host does not serve (a NanoGPT-compatible
	// base without it), so the scan stays a complete listing; any other
	// failure is an outage, and the listing comes back with
	// ErrCatalogFallback so a transient miss cannot retire the models that
	// catalog would have listed.
	partial := false
	imageModels, imgErr := d.discoverNanoGPTImageModels(ctx, provider, apiKey)
	switch {
	case imgErr == nil:
		models = append(models, imageModels...)
	case nanoGPTCatalogAbsent(imgErr):
		debuglog.Debug("discovery: nanogpt image catalog not served", "provider", provider.Name, "provider_id", provider.ID)
	default:
		partial = true
		debuglog.Warn("discovery: nanogpt image-model discovery failed", "provider", provider.Name, "provider_id", provider.ID, "error", imgErr)
	}
	embeddingModels, embErr := d.discoverNanoGPTEmbeddingModels(ctx, provider, apiKey)
	switch {
	case embErr == nil:
		models = append(models, embeddingModels...)
	case nanoGPTCatalogAbsent(embErr):
		debuglog.Debug("discovery: nanogpt embedding catalog not served", "provider", provider.Name, "provider_id", provider.ID)
	default:
		partial = true
		debuglog.Warn("discovery: nanogpt embedding-model discovery failed", "provider", provider.Name, "provider_id", provider.ID, "error", embErr)
	}

	// NanoGPT reports pricing and context straight from its live /models
	// payload, so mark those fields live: genuine provider changes overwrite on
	// upsert and surface in the discovery diff (id-only providers stay fill-only).
	markLiveMeta(models)

	debuglog.Info("discovery: nanogpt discovered models", "models", len(models), "partial", partial, "provider", provider.Name, "provider_id", provider.ID)
	if partial {
		return models, ErrCatalogFallback
	}
	return models, nil
}

// nanoGPTCatalogAbsent reports a side catalog the host does not serve at all
// (HTTP 404), as opposed to one that failed.
func nanoGPTCatalogAbsent(err error) bool {
	he := httpErrorFrom(err)
	return he != nil && he.StatusCode == http.StatusNotFound
}

// discoverNanoGPTImageModels fetches the NanoGPT image-model catalog and maps it
// to models with an "image" output modality. When the provider is configured
// against the subscription base (.../api/subscription/v1) only subscription-
// included models are registered, mirroring how the chat catalog only lists the
// subscriber's models; a non-subscription base registers every image model, all
// of which are usable pay-as-you-go.
func (d *DiscoveryService) discoverNanoGPTImageModels(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	catalogURL, err := nanoGPTImageCatalogURL(provider.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("nanogpt: bad base URL for image catalog: %w", err)
	}

	headers := bearerJSONHeader(apiKey)

	bodyBytes, err := d.fetchURL(ctx, "GET", catalogURL, headers)
	if err != nil {
		return nil, fmt.Errorf("nanogpt: failed to fetch image catalog: %w", err)
	}

	var resp NanoGPTImageModelsResponse
	if err := json.Unmarshal(bodyBytes, &resp); err != nil {
		return nil, fmt.Errorf("nanogpt: failed to decode image catalog: %s", jsonfault.Describe(err, len(bodyBytes)))
	}

	subscriptionOnly := strings.Contains(provider.BaseURL, "/subscription")

	// Iterate in sorted ID order so discovery output is deterministic (Go map
	// iteration order is randomised) and the discovery diff stays stable.
	ids := slices.Sorted(maps.Keys(resp.Models.Image))

	models := make([]*model.Model, 0, len(ids))
	for _, id := range ids {
		im := resp.Models.Image[id]
		included := im.Subscription != nil && im.Subscription.Included
		if subscriptionOnly && !included {
			continue
		}

		modelID := im.Model
		if modelID == "" {
			modelID = id
		}

		inputMods := []string{"text"}
		if nanoGPTImageAcceptsImageInput(im.IconLabel) {
			inputMods = []string{"text", "image"}
		}
		inputModJSON, _ := json.Marshal(inputMods)
		outputModJSON, _ := json.Marshal([]string{"image"})

		paramsMap := map[string]any{
			"image_generation":      true,
			"subscription_included": included,
		}
		if len(im.Cost) > 0 {
			paramsMap["image_cost"] = im.Cost
		}
		paramsJSON, _ := json.Marshal(paramsMap)

		displayName := im.Name
		if displayName == "" {
			displayName = modelID
		}

		// Image models bill per image, not per token, so token price fields stay
		// nil (unknown) rather than a synthetic 0. The per-image cost is preserved
		// in params for reference.
		models = append(models, &model.Model{
			ID:               uuid.New(),
			ProviderID:       provider.ID,
			ModelID:          modelID,
			Name:             im.Name,
			Description:      im.Description,
			DisplayName:      displayName,
			Capabilities:     "{}",
			Params:           string(paramsJSON),
			Modality:         "image",
			InputModalities:  string(inputModJSON),
			OutputModalities: string(outputModJSON),
			OwnedBy:          im.OwnedBy,
			Enabled:          true,
		})
	}

	debuglog.Info("discovery: nanogpt discovered image models", "models", len(models), "subscription_only", subscriptionOnly, "provider", provider.Name, "provider_id", provider.ID)
	return models, nil
}

// discoverNanoGPTEmbeddingModels fetches the embedding-model catalog and files
// each entry under the embedding class with its per-token input price, context
// and vector size. The subscription rule is the image catalog's: on the
// subscription base only models the catalog marks included_in_subscription
// are registered (an absent flag is not an inclusion), a pay-as-you-go base
// registers every model.
func (d *DiscoveryService) discoverNanoGPTEmbeddingModels(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	catalogURL, err := nanoGPTCatalogURL(provider.BaseURL, "/api/v1/embedding-models")
	if err != nil {
		return nil, fmt.Errorf("nanogpt: bad base URL for embedding catalog: %w", err)
	}

	bodyBytes, err := d.fetchURL(ctx, "GET", catalogURL, bearerJSONHeader(apiKey))
	if err != nil {
		return nil, fmt.Errorf("nanogpt: failed to fetch embedding catalog: %w", err)
	}

	var resp NanoGPTEmbeddingModelsResponse
	if err := json.Unmarshal(bodyBytes, &resp); err != nil {
		return nil, fmt.Errorf("nanogpt: failed to decode embedding catalog: %s", jsonfault.Describe(err, len(bodyBytes)))
	}

	subscriptionOnly := strings.Contains(provider.BaseURL, "/subscription")

	models := make([]*model.Model, 0, len(resp.Data))
	for _, em := range resp.Data {
		// The catalog shares the OpenAI list shape with the chat listing, so a
		// host that answers every path with its chat models would otherwise
		// file each of them as an embedding model too. A real catalog row
		// carries a vector size or a usable per-token price (a negative or
		// non-finite figure is no price, the same rule PriceOrNil applies).
		price := model.PriceOrNil(em.Pricing.PerMillionTokens)
		if em.ID == "" || (em.Dimensions == nil && price == nil) {
			continue
		}
		included := em.IncludedInSubscription != nil && *em.IncludedInSubscription
		if subscriptionOnly && !included {
			continue
		}

		inputMods := em.InputModalities
		if len(inputMods) == 0 {
			inputMods = []string{"text"}
		}
		inputModJSON, _ := json.Marshal(inputMods)
		outputModJSON, _ := json.Marshal([]string{"embedding"})

		paramsMap := map[string]any{"subscription_included": included}
		if em.Dimensions != nil {
			paramsMap["dimensions"] = *em.Dimensions
		}
		paramsJSON, _ := json.Marshal(paramsMap)

		displayName := em.Name
		if displayName == "" {
			displayName = em.ID
		}

		// Embeddings bill input tokens only. When the catalog priced the input
		// side the output side is stated as a real 0, not left unknown: CostUSD
		// prices a request only when both sides are known, so a nil output
		// price would meter every embedding call at zero. A row the catalog
		// left unpriced keeps both sides nil, so ReportUnpricedModels still
		// names it.
		var outPrice *float64
		if price != nil {
			zero := 0.0
			outPrice = &zero
		}
		models = append(models, &model.Model{
			ID:                    uuid.New(),
			ProviderID:            provider.ID,
			ModelID:               em.ID,
			Name:                  em.Name,
			Description:           em.Description,
			DisplayName:           displayName,
			Capabilities:          "{}",
			Params:                string(paramsJSON),
			Modality:              "embedding",
			InputModalities:       string(inputModJSON),
			OutputModalities:      string(outputModJSON),
			ContextLength:         em.MaxTokens,
			InputPricePerMillion:  price,
			OutputPricePerMillion: outPrice,
			OwnedBy:               em.OwnedBy,
			Enabled:               true,
		})
	}

	debuglog.Info("discovery: nanogpt discovered embedding models", "models", len(models), "subscription_only", subscriptionOnly, "provider", provider.Name, "provider_id", provider.ID)
	return models, nil
}

// nanoGPTImageCatalogURL derives the image-model catalog URL (/api/models/image)
// from the provider base URL, which may be the subscription base, a bare host,
// or a custom NanoGPT-compatible base.
func nanoGPTImageCatalogURL(baseURL string) (string, error) {
	return nanoGPTCatalogURL(baseURL, "/api/models/image")
}

// nanoGPTCatalogURL puts a host-rooted catalog path on the provider's host:
// the catalogs live beside, not under, whichever /v1 base the provider uses.
func nanoGPTCatalogURL(baseURL, path string) (string, error) {
	u, err := url.Parse(util.SanitizeBaseURL(baseURL))
	if err != nil {
		return "", util.URLParseReason(err)
	}
	if u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("missing scheme or host in %q", baseURL)
	}
	return u.Scheme + "://" + u.Host + path, nil
}

// nanoGPTImageAcceptsImageInput reports whether an image model's iconLabel marks
// it as an editing model (image-to-image), which additionally takes image input.
func nanoGPTImageAcceptsImageInput(iconLabel string) bool {
	l := strings.ToLower(iconLabel)
	return l == "both" || strings.Contains(l, "edit") || strings.Contains(l, "image-to-image") || strings.Contains(l, "img2img")
}

// GetNanoGPTUsage retrieves usage information from a NanoGPT provider.
func (d *DiscoveryService) GetNanoGPTUsage(ctx context.Context, provider *Provider, masterKey string) (*NanoGPTUsageResponse, error) {
	var usage NanoGPTUsageResponse
	if err := d.fetchQuotaJSON(ctx, provider, masterKey, "/usage", "nanogpt", "usage", &usage); err != nil {
		return nil, err
	}
	return &usage, nil
}
