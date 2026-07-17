package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"slices"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

func (d *DiscoveryService) discoverXAI(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	catalog := d.discoverXAIFromCatalog(provider)

	// Step 1: Try the rich /language-models endpoint.
	live, err := d.discoverXAILanguageModels(ctx, provider, apiKey, baseURL)
	if err != nil {
		// Step 2: 403/429 (zero-balance or rate-limited account) -> catalog only.
		if isNoAccessError(err) {
			debuglog.Warn("discovery: xai /language-models returned no-access, using catalog", "status", errorStatusCode(err), "provider", provider.Name, "provider_id", provider.ID)
			return d.appendXAIImageModels(ctx, provider, apiKey, baseURL, catalog), nil
		}
		// Step 3: Other failure -> try the minimal /models endpoint.
		live, err = d.discoverXAIMinimalModels(ctx, provider, apiKey, baseURL)
		if err != nil {
			// Step 4: /models also no-access -> catalog only.
			if isNoAccessError(err) {
				debuglog.Warn("discovery: xai /models also returned no-access, using catalog", "status", errorStatusCode(err), "provider", provider.Name, "provider_id", provider.ID)
				return d.appendXAIImageModels(ctx, provider, apiKey, baseURL, catalog), nil
			}
			return nil, fmt.Errorf("xAI: failed to discover models for provider %s: both endpoints returned errors", provider.Name)
		}
	} else if len(live) == 0 {
		// Rich endpoint succeeded but listed nothing; try the minimal endpoint.
		minimal, mErr := d.discoverXAIMinimalModels(ctx, provider, apiKey, baseURL)
		if mErr != nil {
			// Don't swallow a real error: log it so an outage isn't hidden behind
			// the "no models" warning below (we still don't propagate it — empty
			// live is handled safely by returning an empty set).
			debuglog.Warn("discovery: xai /language-models listed nothing and /models also failed", "provider", provider.Name, "provider_id", provider.ID, "error", mErr)
		} else if len(minimal) > 0 {
			live = minimal
		}
	}

	// Both endpoints succeeded but listed no models (distinct from the no-access
	// 403/429 path above, which intentionally returns the catalog). Return empty
	// rather than unioning the catalog, so RecordMissingModels stays a no-op
	// instead of disabling every live-only model.
	if len(live) == 0 {
		debuglog.Warn("discovery: xai endpoints returned no models, skipping", "provider", provider.Name, "provider_id", provider.ID)
		return live, nil
	}

	// Union live with the catalog: live wins per field, catalog backfills the
	// gaps (xAI's API omits context length, max output, reasoning) and unions in
	// any catalog model the listing endpoints do not advertise (xAI keeps older
	// grok models callable without listing them).
	merged := mergeLiveAndCatalog(live, catalog)
	merged = d.appendXAIImageModels(ctx, provider, apiKey, baseURL, merged)

	debuglog.Info("discovery: xai merged live + catalog", "provider", provider.Name, "provider_id", provider.ID, "live", len(live), "catalog", len(catalog), "merged", len(merged))
	return merged, nil
}

// appendXAIImageModels unions image-generation models onto base, deduplicated by
// model ID. Image models live on a separate endpoint (/image-generation-models)
// with its own access from chat/language models, so this runs on every return
// path including the catalog-only no-access fallback: an account can lack
// /language-models access while still generating images. Best-effort: a failure
// (e.g. no image scope) leaves base untouched.
func (d *DiscoveryService) appendXAIImageModels(ctx context.Context, provider *Provider, apiKey, baseURL string, base []*model.Model) []*model.Model {
	imageModels, err := d.discoverXAIImageModels(ctx, provider, apiKey, baseURL)
	if err != nil {
		debuglog.Warn("discovery: xai image-model discovery failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return base
	}
	existing := make(map[string]struct{}, len(base))
	for _, m := range base {
		existing[m.ModelID] = struct{}{}
	}
	for _, im := range imageModels {
		if _, dup := existing[im.ModelID]; !dup {
			base = append(base, im)
		}
	}
	return base
}

func (d *DiscoveryService) discoverXAILanguageModels(ctx context.Context, provider *Provider, apiKey, baseURL string) ([]*model.Model, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/language-models", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("xAI: failed to create request for provider %s: %w", provider.Name, err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.doDiscoveryRequestPrebuilt(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("xAI: http request failed for provider %s: %w", provider.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xAI: failed to read response for provider %s: %w", provider.Name, err)
	}

	if resp.StatusCode == http.StatusForbidden {
		return nil, &httpError{StatusCode: resp.StatusCode, Body: string(bodyBytes)}
	}
	if resp.StatusCode != http.StatusOK {
		debuglog.Error("discovery: xai language-models non-200 status", "status", resp.StatusCode, "provider", provider.Name, "provider_id", provider.ID, "body", util.SanitizeLogBody(string(bodyBytes), 2000))
		return nil, fmt.Errorf("xAI: unexpected status %d for provider %s", resp.StatusCode, provider.Name)
	}

	var langResp XAILanguageModelsResponse
	if err := json.Unmarshal(bodyBytes, &langResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	models := make([]*model.Model, 0, len(langResp.Models))

	for _, lm := range langResp.Models {
		// Convert xAI pricing: cents per 100M tokens -> dollars per 1M tokens.
		inputPrice := float64(lm.PromptTextTokenPrice) / 100.0
		cachePrice := float64(lm.CachedPromptTextTokenPrice) / 100.0
		outputPrice := float64(lm.CompletionTextTokenPrice) / 100.0

		// Capabilities the xAI API guarantees for language models. Reasoning is
		// left to the catalog (the API does not report it); mergeLiveAndCatalog
		// OR-merges it in.
		hasVision := slices.Contains(lm.InputModalities, "image")
		caps := model.Capability{
			Streaming:        true,
			ToolCalling:      true,
			StructuredOutput: true,
			Vision:           hasVision,
		}
		capJSON, _ := json.Marshal(caps)

		inputMods, _ := json.Marshal(lm.InputModalities)
		outputMods, _ := json.Marshal(lm.OutputModalities)

		// Only fields the live API actually provides are set here. Modality,
		// Description, ContextLength and MaxOutputTokens are intentionally left
		// empty/nil so the catalog (and then models.dev) backfill them without a
		// fabricated placeholder masking the richer catalog value.
		m := &model.Model{
			ID:               uuid.New(),
			ProviderID:       provider.ID,
			ModelID:          lm.ID,
			Name:             lm.ID,
			DisplayName:      lm.ID, // placeholder == model_id; catalog name wins
			Capabilities:     string(capJSON),
			Params:           "{}",
			InputModalities:  string(inputMods),
			OutputModalities: string(outputMods),
			OwnedBy:          lm.OwnedBy,
			Enabled:          true,
		}
		if inputPrice > 0 {
			m.InputPricePerMillion = &inputPrice
		}
		if outputPrice > 0 {
			m.OutputPricePerMillion = &outputPrice
		}
		if cachePrice > 0 {
			m.InputPricePerMillionCacheHit = &cachePrice
		}

		models = append(models, m)
	}

	debuglog.Info("discovery: xai discovered language models", "models", len(models), "provider", provider.Name, "provider_id", provider.ID)
	return models, nil
}

// discoverXAIImageModels fetches the xAI image-generation catalog and maps it to
// models with an "image" output modality. xAI serves image generation on the
// same base as chat (/v1/images/generations), so unlike NanoGPT no base rewrite
// is needed; only registration was missing. Image models bill per image, not per
// token, so token price fields stay nil and the xAI image price (in xAI's native
// units) is preserved in params.
func (d *DiscoveryService) discoverXAIImageModels(ctx context.Context, provider *Provider, apiKey, baseURL string) ([]*model.Model, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/image-generation-models", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("xAI: failed to create image-models request for provider %s: %w", provider.Name, err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.doDiscoveryRequestPrebuilt(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("xAI: image-models request failed for provider %s: %w", provider.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xAI: failed to read image-models response for provider %s: %w", provider.Name, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("xAI: unexpected status %d from image-models for provider %s", resp.StatusCode, provider.Name)
	}

	var imgResp XAIImageGenerationModelsResponse
	if err := json.Unmarshal(bodyBytes, &imgResp); err != nil {
		return nil, fmt.Errorf("xAI: failed to decode image-models response: %w", err)
	}

	models := make([]*model.Model, 0, len(imgResp.Models))
	for _, im := range imgResp.Models {
		inputMods := im.InputModalities
		if len(inputMods) == 0 {
			inputMods = []string{"text"}
		}
		outputMods := im.OutputModalities
		if len(outputMods) == 0 {
			outputMods = []string{"image"}
		}
		inputModJSON, _ := json.Marshal(inputMods)
		outputModJSON, _ := json.Marshal(outputMods)

		paramsMap := map[string]any{"image_generation": true}
		if im.ImagePrice > 0 {
			paramsMap["image_price"] = im.ImagePrice
		}
		paramsJSON, _ := json.Marshal(paramsMap)

		models = append(models, &model.Model{
			ID:               uuid.New(),
			ProviderID:       provider.ID,
			ModelID:          im.ID,
			Name:             im.ID,
			DisplayName:      im.ID,
			Capabilities:     "{}",
			Params:           string(paramsJSON),
			Modality:         "image",
			InputModalities:  string(inputModJSON),
			OutputModalities: string(outputModJSON),
			OwnedBy:          im.OwnedBy,
			Enabled:          true,
		})
	}

	debuglog.Info("discovery: xai discovered image models", "models", len(models), "provider", provider.Name, "provider_id", provider.ID)
	return models, nil
}

func (d *DiscoveryService) discoverXAIMinimalModels(ctx context.Context, provider *Provider, apiKey, baseURL string) ([]*model.Model, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/models", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("xAI: failed to create request for provider %s: %w", provider.Name, err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.doDiscoveryRequestPrebuilt(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("xAI: http request failed for provider %s: %w", provider.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xAI: failed to read response for provider %s: %w", provider.Name, err)
	}

	if resp.StatusCode == http.StatusForbidden {
		return nil, &httpError{StatusCode: resp.StatusCode, Body: string(bodyBytes)}
	}
	if resp.StatusCode != http.StatusOK {
		debuglog.Error("discovery: xai minimal models non-200 status", "status", resp.StatusCode, "provider", provider.Name, "provider_id", provider.ID, "body", util.SanitizeLogBody(string(bodyBytes), 2000))
		return nil, fmt.Errorf("xAI: unexpected status %d for provider %s", resp.StatusCode, provider.Name)
	}

	var openAIResp XAIModelsResponse
	if err := json.Unmarshal(bodyBytes, &openAIResp); err != nil {
		return nil, fmt.Errorf("xAI: failed to decode minimal models response for provider %s: %w", provider.Name, err)
	}

	models := make([]*model.Model, 0, len(openAIResp.Data))

	// The minimal /models endpoint only carries id + owner. Emit those and let
	// mergeLiveAndCatalog backfill the rest from the catalog and models.dev.
	for _, m := range openAIResp.Data {
		capJSON, _ := json.Marshal(model.Capability{Streaming: true})
		models = append(models, &model.Model{
			ID:           uuid.New(),
			ProviderID:   provider.ID,
			ModelID:      m.ID,
			Name:         m.ID,
			DisplayName:  m.ID,
			Capabilities: string(capJSON),
			Params:       "{}",
			// JSONB columns must hold valid JSON even before catalog backfill.
			InputModalities:  "[]",
			OutputModalities: "[]",
			OwnedBy:          m.OwnedBy,
			Enabled:          true,
		})
	}

	debuglog.Info("discovery: xai discovered minimal models", "models", len(models), "provider", provider.Name, "provider_id", provider.ID)
	return models, nil
}

func (d *DiscoveryService) discoverXAIFromCatalog(provider *Provider) []*model.Model {
	catalog := GetXAICatalog()
	models := make([]*model.Model, 0, len(catalog))
	for i := range catalog {
		models = append(models, OpenCodeCatalogToModel(&catalog[i], provider.ID, "xai"))
	}
	debuglog.Info("discovery: xai using catalog", "models", len(models), "provider", provider.Name, "provider_id", provider.ID)
	return models
}

// httpError wraps an HTTP status code error for no-access detection.
type httpError struct {
	StatusCode int
	Body       string
}

func (e *httpError) Error() string {
	return fmt.Sprintf("unexpected status %d", e.StatusCode)
}

// isNoAccessError returns true if the error indicates the account cannot
// access the API (403 forbidden or 429 rate-limit/quota-exhausted).
// Both mean we should fall back to the static catalog.
func isNoAccessError(err error) bool {
	if err == nil {
		return false
	}
	httpErr := &httpError{}
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode == http.StatusForbidden || httpErr.StatusCode == http.StatusTooManyRequests
	}
	return false
}

// errorStatusCode extracts the HTTP status code from an httpError, or 0.
func errorStatusCode(err error) int {
	httpErr := &httpError{}
	if errors.As(err, &httpErr) {
		return httpErr.StatusCode
	}
	return 0
}
