package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

func (d *DiscoveryService) discoverAnthropic(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeAPIURL(provider.BaseURL)

	pricingCatalog := GetAnthropicPricing()

	var allModels []AnthropicModelInfo
	afterID := ""

	for {
		url := baseURL + "/v1/models?limit=100"
		if afterID != "" {
			url += "&after_id=" + afterID
		}

		req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}
		req.Header.Set("x-api-key", apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
		req.Header.Set("Content-Type", "application/json")

		resp, err := d.httpClient.Do(req)
		if err != nil {
			debuglog.Error("discovery: anthropic fetch models failed", "provider", provider.ID, "error", err)
			return nil, fmt.Errorf("failed to fetch models: %w", err)
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			debuglog.Error("discovery: anthropic returned non-200 status", "status", resp.StatusCode, "provider", provider.ID, "body", util.SanitizeLogBody(string(bodyBytes), 2000))
			return nil, fmt.Errorf("unexpected status code %d", resp.StatusCode)
		}

		var pageResp AnthropicModelsResponse
		if err := json.Unmarshal(bodyBytes, &pageResp); err != nil {
			debuglog.Error("discovery: anthropic json decode failed", "provider", provider.ID, "error", err)
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}

		allModels = append(allModels, pageResp.Data...)

		if !pageResp.HasMore || len(pageResp.Data) == 0 {
			break
		}
		afterID = pageResp.LastID
	}

	models := make([]*model.Model, 0, len(allModels))
	for _, m := range allModels {
		displayName := m.ID
		if m.DisplayName != "" {
			displayName = m.DisplayName
		}

		caps := model.Capability{
			Streaming:   true,
			ToolCalling: true,
		}
		modality := "text"
		inputMods := `["text"]`

		if m.Capabilities != nil {
			if m.Capabilities.ImageInput.Supported {
				caps.Vision = true
				modality = "vision"
				inputMods = `["text","image"]`
			}
			if m.Capabilities.PDFInput.Supported {
				caps.PDFUpload = true
				if modality == "text" {
					modality = "vision"
					inputMods = `["text","image"]`
				}
			}
			if m.Capabilities.StructuredOutputs.Supported {
				caps.StructuredOutput = true
			}
		}

		capJSON, _ := json.Marshal(caps)

		modelEntry := &model.Model{
			ID:               uuid.New(),
			ProviderID:       provider.ID,
			ModelID:          m.ID,
			Name:             m.ID,
			DisplayName:      displayName,
			Capabilities:     string(capJSON),
			Params:           "{}",
			Modality:         modality,
			InputModalities:  inputMods,
			OutputModalities: "[]",
			ContextLength:    m.MaxInputTokens,
			MaxOutputTokens:  m.MaxTokens,
			OwnedBy:          "anthropic",
			Enabled:          true,
		}

		if pricing := LookupAnthropicPricing(pricingCatalog, m.ID); pricing != nil {
			inPrice := pricing.InputPricePerMillion
			outPrice := pricing.OutputPricePerMillion
			modelEntry.InputPricePerMillion = &inPrice
			modelEntry.OutputPricePerMillion = &outPrice

			if pricing.InputPricePerMillionCacheHit > 0 {
				cacheHitPrice := pricing.InputPricePerMillionCacheHit
				modelEntry.InputPricePerMillionCacheHit = &cacheHitPrice
			}
		}

		models = append(models, modelEntry)
	}

	debuglog.Info("discovery: anthropic discovered models", "models", len(models), "provider", provider.ID)
	return models, nil
}
