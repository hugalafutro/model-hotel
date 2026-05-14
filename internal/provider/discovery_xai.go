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

	// Step 1: Try the rich /language-models endpoint
	langModels, err := d.discoverXAILanguageModels(ctx, provider, apiKey, baseURL)
	if err == nil && len(langModels) > 0 {
		return langModels, nil
	}

	// Step 2: If we got a 403/429 (zero-balance or rate-limited account), fall back to catalog
	if isNoAccessError(err) {
		debuglog.Warn("discovery: xai /language-models returned no-access, falling back to catalog", "status", errorStatusCode(err), "provider", provider.ID)
		return d.discoverXAIFromCatalog(provider), nil
	}

	// Step 3: If rich endpoint failed for other reasons, try minimal /models
	minimalModels, err2 := d.discoverXAIMinimalModels(ctx, provider, apiKey, baseURL)
	if err2 == nil && len(minimalModels) > 0 {
		return minimalModels, nil
	}

	// Step 4: If /models also returned 403/429, fall back to catalog
	if isNoAccessError(err2) {
		debuglog.Warn("discovery: xai /models also returned no-access, falling back to catalog", "status", errorStatusCode(err2), "provider", provider.ID)
		return d.discoverXAIFromCatalog(provider), nil
	}

	// Both failed with real errors
	return nil, fmt.Errorf("xAI: failed to discover models for provider %s: both endpoints returned errors", provider.ID)
}

func (d *DiscoveryService) discoverXAILanguageModels(ctx context.Context, provider *Provider, apiKey, baseURL string) ([]*model.Model, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/language-models", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("xAI: failed to create request for provider %s: %w", provider.ID, err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("xAI: http request failed for provider %s: %w", provider.ID, err)
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xAI: failed to read response for provider %s: %w", provider.ID, err)
	}

	if resp.StatusCode == http.StatusForbidden {
		return nil, &httpError{StatusCode: resp.StatusCode, Body: string(bodyBytes)}
	}
	if resp.StatusCode != http.StatusOK {
		debuglog.Error("discovery: xai language-models non-200 status", "status", resp.StatusCode, "provider", provider.ID, "body", util.SanitizeLogBody(string(bodyBytes), 2000))
		return nil, fmt.Errorf("xAI: unexpected status %d for provider %s", resp.StatusCode, provider.ID)
	}

	var langResp XAILanguageModelsResponse
	if err := json.Unmarshal(bodyBytes, &langResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	catalog := GetXAICatalog()
	models := make([]*model.Model, 0, len(langResp.Models))

	for _, lm := range langResp.Models {
		spec := LookupOpenCodeCatalog(catalog, lm.ID)

		// Convert xAI pricing: cents per 100M tokens -> dollars per 1M tokens
		inputPrice := float64(lm.PromptTextTokenPrice) / 100.0
		cachePrice := float64(lm.CachedPromptTextTokenPrice) / 100.0
		outputPrice := float64(lm.CompletionTextTokenPrice) / 100.0

		// Build capabilities from API data
		hasVision := slices.Contains(lm.InputModalities, "image")
		streaming := true        // xAI supports streaming on all models
		reasoning := false       // Default; override from catalog if available
		toolCalling := true      // xAI supports tool calling on language models
		structuredOutput := true // xAI supports structured output

		if spec != nil {
			reasoning = spec.Reasoning
		}

		caps := model.Capability{
			Streaming:        streaming,
			Reasoning:        reasoning,
			ToolCalling:      toolCalling,
			StructuredOutput: structuredOutput,
			Vision:           hasVision,
		}
		capJSON, _ := json.Marshal(caps)

		inputMods, _ := json.Marshal(lm.InputModalities)
		outputMods, _ := json.Marshal(lm.OutputModalities)

		m := &model.Model{
			ID:                    uuid.New(),
			ProviderID:            provider.ID,
			ModelID:               lm.ID,
			Name:                  lm.ID,
			DisplayName:           lm.ID,
			Description:           fmt.Sprintf("xAI language model (v%s)", lm.Version),
			Capabilities:          string(capJSON),
			Params:                "{}",
			Modality:              "text",
			InputModalities:       string(inputMods),
			OutputModalities:      string(outputMods),
			ContextLength:         nil, // Not provided by xAI API
			MaxOutputTokens:       nil, // Not provided by xAI API
			InputPricePerMillion:  &inputPrice,
			OutputPricePerMillion: &outputPrice,
			OwnedBy:               lm.OwnedBy,
			Enabled:               true,
		}

		if cachePrice > 0 {
			m.InputPricePerMillionCacheHit = &cachePrice
		}

		// Override context/capabilities from catalog if available
		if spec != nil {
			if spec.ContextLength > 0 {
				m.ContextLength = &spec.ContextLength
			}
			if spec.MaxOutputTokens > 0 {
				m.MaxOutputTokens = &spec.MaxOutputTokens
			}
			m.DisplayName = spec.DisplayName
			m.Description = spec.Description
			if spec.Modality != "" {
				m.Modality = spec.Modality
			}
			if spec.InputModalities != "" {
				m.InputModalities = spec.InputModalities
			}
			if spec.OutputModalities != "" {
				m.OutputModalities = spec.OutputModalities
			}
		}

		models = append(models, m)
	}

	debuglog.Info("discovery: xai discovered language models", "models", len(models), "provider", provider.ID)
	return models, nil
}

func (d *DiscoveryService) discoverXAIMinimalModels(ctx context.Context, provider *Provider, apiKey, baseURL string) ([]*model.Model, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/models", http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("xAI: failed to create request for provider %s: %w", provider.ID, err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("xAI: http request failed for provider %s: %w", provider.ID, err)
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("xAI: failed to read response for provider %s: %w", provider.ID, err)
	}

	if resp.StatusCode == http.StatusForbidden {
		return nil, &httpError{StatusCode: resp.StatusCode, Body: string(bodyBytes)}
	}
	if resp.StatusCode != http.StatusOK {
		debuglog.Error("discovery: xai minimal models non-200 status", "status", resp.StatusCode, "provider", provider.ID, "body", util.SanitizeLogBody(string(bodyBytes), 2000))
		return nil, fmt.Errorf("xAI: unexpected status %d for provider %s", resp.StatusCode, provider.ID)
	}

	var openAIResp XAIModelsResponse
	if err := json.Unmarshal(bodyBytes, &openAIResp); err != nil {
		return nil, fmt.Errorf("xAI: failed to decode minimal models response for provider %s: %w", provider.ID, err)
	}

	catalog := GetXAICatalog()
	models := make([]*model.Model, 0, len(openAIResp.Data))

	for _, m := range openAIResp.Data {
		spec := LookupOpenCodeCatalog(catalog, m.ID)
		if spec != nil {
			models = append(models, OpenCodeCatalogToModel(spec, provider.ID, "xai"))
			continue
		}
		// Unknown model — create minimal entry
		capJSON, _ := json.Marshal(model.Capability{Streaming: true})
		models = append(models, &model.Model{
			ID:               uuid.New(),
			ProviderID:       provider.ID,
			ModelID:          m.ID,
			Name:             m.ID,
			DisplayName:      m.ID,
			Capabilities:     string(capJSON),
			Params:           "{}",
			Modality:         "text",
			InputModalities:  "[]",
			OutputModalities: "[]",
			OwnedBy:          m.OwnedBy,
			Enabled:          true,
		})
	}

	debuglog.Info("discovery: xai discovered minimal models", "models", len(models), "provider", provider.ID)
	return models, nil
}

func (d *DiscoveryService) discoverXAIFromCatalog(provider *Provider) []*model.Model {
	catalog := GetXAICatalog()
	models := make([]*model.Model, 0, len(catalog))
	for i := range catalog {
		models = append(models, OpenCodeCatalogToModel(&catalog[i], provider.ID, "xai"))
	}
	debuglog.Info("discovery: xai using catalog", "models", len(models), "provider", provider.ID)
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
