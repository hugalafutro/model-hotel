package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

func (d *DiscoveryService) discoverCohere(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)

	// Derive native API URL from compat URL.
	// Stored base URL will be "https://api.cohere.ai/compatibility/v1"
	// Native API base is "https://api.cohere.com"
	nativeBaseURL := util.CohereNativeBaseURL(baseURL)

	// Fetch the pricing catalog once and thread it through both endpoint fetches.
	pricingCatalog := GetCoherePricingCatalog()

	models, err := d.fetchCohereModels(ctx, provider, apiKey, nativeBaseURL, "chat", pricingCatalog)
	if err != nil {
		return nil, err
	}

	// Rerank models live behind the same models API under a separate endpoint
	// filter and are served by the proxy's /v1/rerank passthrough. A failure
	// here degrades gracefully: chat discovery already succeeded, so keep it.
	rerankModels, err := d.fetchCohereModels(ctx, provider, apiKey, nativeBaseURL, "rerank", pricingCatalog)
	if err != nil {
		debuglog.Warn("discovery: cohere rerank model fetch failed, keeping chat models", "provider", provider.Name, "provider_id", provider.ID, "error", err)
	} else {
		models = append(models, rerankModels...)
	}

	debuglog.Info("discovery: cohere discovered models", "models", len(models), "provider", provider.Name, "provider_id", provider.ID)
	return models, nil
}

// fetchCohereModels pages through the native /v1/models API filtered to one
// endpoint family ("chat" or "rerank") and builds model rows for it.
func (d *DiscoveryService) fetchCohereModels(ctx context.Context, provider *Provider, apiKey, nativeBaseURL, endpoint string, pricingCatalog []CoherePricingEntry) ([]*model.Model, error) {
	models := make([]*model.Model, 0)

	// Paginate through all model pages
	pageToken := ""
	for {
		url := fmt.Sprintf("%s/v1/models?endpoint=%s&page_size=100", nativeBaseURL, endpoint)
		if pageToken != "" {
			url += "&page_token=" + pageToken
		}

		req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := d.httpClient.Do(req)
		if err != nil {
			debuglog.Error("discovery: cohere http request failed", "provider", provider.Name, "provider_id", provider.ID, "endpoint", endpoint, "error", err)
			return nil, fmt.Errorf("failed to fetch models: %w", err)
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			debuglog.Error("discovery: cohere non-200 status", "status", resp.StatusCode, "provider", provider.Name, "provider_id", provider.ID, "endpoint", endpoint, "body", util.SanitizeLogBody(string(bodyBytes), 2000))
			return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(bodyBytes))
		}

		var cohereResp CohereModelsResponse
		if err := json.Unmarshal(bodyBytes, &cohereResp); err != nil {
			debuglog.Error("discovery: cohere failed to decode response", "provider", provider.Name, "provider_id", provider.ID, "endpoint", endpoint, "error", err)
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		for _, cm := range cohereResp.Models {
			// Skip deprecated models
			if cm.IsDeprecated {
				debuglog.Info("discovery: cohere skipping deprecated model", "model", cm.Name)
				continue
			}
			models = append(models, buildCohereModel(provider, pricingCatalog, cm, endpoint))
		}

		// Check for next page
		if cohereResp.NextPageToken == "" {
			break
		}
		pageToken = cohereResp.NextPageToken
	}

	return models, nil
}

// buildCohereModel converts one Cohere API model entry into a model row.
// Rerank models carry no chat capabilities and are billed per search rather
// than per token, so their price pointers stay nil (unknown).
func buildCohereModel(provider *Provider, pricingCatalog []CoherePricingEntry, cm CohereNativeModel, endpoint string) *model.Model {
	caps := model.Capability{}
	modality := "text"
	inputMods := `["text"]`
	outputMods := `["text"]`

	if endpoint == "rerank" {
		modality = "rerank"
	} else {
		// Build capabilities from API features array
		caps = cohereFeaturesToCapabilities(cm.Features)
		// Determine modality from features
		if slices.Contains(cm.Features, "vision") {
			inputMods = `["text","image"]`
		}
	}
	capJSON, _ := json.Marshal(caps)

	modelEntry := &model.Model{
		ID:               uuid.New(),
		ProviderID:       provider.ID,
		ModelID:          cm.Name,
		Name:             cm.Name,
		Capabilities:     string(capJSON),
		Params:           "{}",
		Modality:         modality,
		InputModalities:  inputMods,
		OutputModalities: outputMods,
		ContextLength:    &cm.ContextLength,
		OwnedBy:          "cohere",
		Enabled:          true,
	}

	pricing := LookupCoherePricing(pricingCatalog, cm.Name)
	if pricing != nil {
		modelEntry.DisplayName = pricing.DisplayName
		modelEntry.Description = pricing.Description
		maxOutput := pricing.MaxOutputTokens
		modelEntry.MaxOutputTokens = &maxOutput
		inPrice := pricing.InputPricePerMillion
		outPrice := pricing.OutputPricePerMillion
		modelEntry.InputPricePerMillion = &inPrice
		modelEntry.OutputPricePerMillion = &outPrice
	} else {
		// Minimal entry for models not in pricing catalog. Expected for
		// rerank models (search-unit billing has no per-token price).
		modelEntry.DisplayName = cm.Name
		if endpoint != "rerank" {
			debuglog.Warn("discovery: cohere model not in pricing catalog", "model", cm.Name)
		}
	}

	return modelEntry
}

// cohereFeaturesToCapabilities converts a Cohere features array to our Capability struct.
func cohereFeaturesToCapabilities(features []string) model.Capability {
	caps := model.Capability{
		Streaming: true, // All Cohere chat models support streaming
	}
	for _, f := range features {
		switch f {
		case "tools", "tool_choice":
			caps.ToolCalling = true
		case "json_mode", "json_schema":
			caps.StructuredOutput = true
		case "reasoning":
			caps.Reasoning = true
		case "vision":
			caps.Vision = true
		}
	}
	return caps
}
