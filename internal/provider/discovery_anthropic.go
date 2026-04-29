package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

func (d *DiscoveryService) discoverAnthropic(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	raw := util.SanitizeBaseURL(provider.BaseURL)
	baseURL := strings.TrimSuffix(strings.TrimSuffix(raw, "/"), "/v1")

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
			log.Printf("[discovery] error: anthropic fetch models failed for provider %s: %v", provider.ID, err)
			return nil, fmt.Errorf("failed to fetch models: %w", err)
		}

		bodyBytes, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}

		if resp.StatusCode != http.StatusOK {
			log.Printf("[discovery] error: anthropic returned status %d for provider %s", resp.StatusCode, provider.ID)
			return nil, fmt.Errorf("unexpected status code %d: %s", resp.StatusCode, string(bodyBytes))
		}

		var pageResp AnthropicModelsResponse
		if err := json.Unmarshal(bodyBytes, &pageResp); err != nil {
			log.Printf("[discovery] error: anthropic json decode failed for provider %s: %v", provider.ID, err)
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

	log.Printf("[discovery] anthropic: discovered %d models for provider %s", len(models), provider.ID)
	return models, nil
}