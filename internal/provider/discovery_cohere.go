package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

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
	nativeBaseURL := toCohereNativeURL(baseURL)

	pricingCatalog := GetCoherePricingCatalog()
	models := make([]*model.Model, 0)

	// Paginate through all model pages
	pageToken := ""
	for {
		url := fmt.Sprintf("%s/v1/models?endpoint=chat&page_size=100", nativeBaseURL)
		if pageToken != "" {
			url += "&page_token=" + pageToken
		}

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+apiKey)
		req.Header.Set("Content-Type", "application/json")

		resp, err := d.httpClient.Do(req)
		if err != nil {
			debuglog.Error("discovery: cohere http request failed", "provider", provider.ID, "error", err)
			return nil, fmt.Errorf("failed to fetch models: %w", err)
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			debuglog.Error("discovery: cohere non-200 status", "status", resp.StatusCode, "provider", provider.ID, "body", util.SanitizeLogBody(string(bodyBytes), 2000))
			return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(bodyBytes))
		}

		var cohereResp CohereModelsResponse
		if err := json.Unmarshal(bodyBytes, &cohereResp); err != nil {
			debuglog.Error("discovery: cohere failed to decode response", "provider", provider.ID, "error", err)
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		for _, cm := range cohereResp.Models {
			// Skip deprecated models
			if cm.IsDeprecated {
				debuglog.Info("discovery: cohere skipping deprecated model", "model", cm.Name)
				continue
			}

			pricing := LookupCoherePricing(pricingCatalog, cm.Name)

			// Build capabilities from API features array
			caps := cohereFeaturesToCapabilities(cm.Features)
			capJSON, _ := json.Marshal(caps)

			// Determine modality from features
			hasVision := containsString(cm.Features, "vision")
			modality := "text"
			inputMods := `["text"]`
			outputMods := `["text"]`
			if hasVision {
				inputMods = `["text","image"]`
			}

			// Build model entry
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
				// Minimal entry for models not in pricing catalog
				modelEntry.DisplayName = cm.Name
				debuglog.Warn("discovery: cohere model not in pricing catalog", "model", cm.Name)
			}

			models = append(models, modelEntry)
		}

		// Check for next page
		if cohereResp.NextPageToken == "" {
			break
		}
		pageToken = cohereResp.NextPageToken
	}

	debuglog.Info("discovery: cohere discovered models", "models", len(models), "provider", provider.ID)
	return models, nil
}

// toCohereNativeURL converts a compatibility API base URL to the native API base URL.
// Input:  "https://api.cohere.ai/compatibility/v1" or "https://api.cohere.com"
// Output: "https://api.cohere.com"
func toCohereNativeURL(baseURL string) string {
	u := strings.TrimSuffix(baseURL, "/")
	// If the base URL points to the compatibility endpoint, switch to native
	if strings.HasPrefix(u, "https://api.cohere.ai") {
		return "https://api.cohere.com"
	}
	// Already pointing at native API or a custom URL
	return u
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
