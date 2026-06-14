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
			return catalog, nil
		}
		// Step 3: Other failure -> try the minimal /models endpoint.
		live, err = d.discoverXAIMinimalModels(ctx, provider, apiKey, baseURL)
		if err != nil {
			// Step 4: /models also no-access -> catalog only.
			if isNoAccessError(err) {
				debuglog.Warn("discovery: xai /models also returned no-access, using catalog", "status", errorStatusCode(err), "provider", provider.Name, "provider_id", provider.ID)
				return catalog, nil
			}
			return nil, fmt.Errorf("xAI: failed to discover models for provider %s: both endpoints returned errors", provider.Name)
		}
	} else if len(live) == 0 {
		// Rich endpoint succeeded but listed nothing; try the minimal endpoint
		// before relying on the catalog alone.
		if minimal, mErr := d.discoverXAIMinimalModels(ctx, provider, apiKey, baseURL); mErr == nil && len(minimal) > 0 {
			live = minimal
		}
	}

	// Union live with the catalog: live wins per field, catalog backfills the
	// gaps (xAI's API omits context length, max output, reasoning) and unions in
	// any catalog model the listing endpoints do not advertise (xAI keeps older
	// grok models callable without listing them).
	merged := mergeLiveAndCatalog(live, catalog)
	debuglog.Info("discovery: xai merged live + catalog", "provider", provider.Name, "provider_id", provider.ID, "live", len(live), "catalog", len(catalog), "merged", len(merged))
	return merged, nil
}

func (d *DiscoveryService) discoverXAILanguageModels(ctx context.Context, provider *Provider, apiKey, baseURL string) ([]*model.Model, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/language-models", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("xAI: failed to create request for provider %s: %w", provider.Name, err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
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

func (d *DiscoveryService) discoverXAIMinimalModels(ctx context.Context, provider *Provider, apiKey, baseURL string) ([]*model.Model, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/models", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("xAI: failed to create request for provider %s: %w", provider.Name, err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
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
