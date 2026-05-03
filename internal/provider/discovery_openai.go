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

func (d *DiscoveryService) discoverOpenAI(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	raw := util.SanitizeBaseURL(provider.BaseURL)
	baseURL := strings.TrimSuffix(strings.TrimSuffix(raw, "/"), "/v1")
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		log.Printf("[discovery] error: openai fetch models failed for provider %s: %v", provider.ID, err)
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("[discovery] error: openai non-200 status %d for provider %s: %s", resp.StatusCode, provider.ID, util.SanitizeLogBody(string(bodyBytes), 200))
		return nil, fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	var openAIResp OpenAIModelsResponse
	if err := json.Unmarshal(bodyBytes, &openAIResp); err != nil {
		log.Printf("[discovery] error: openai json decode failed for provider %s: %v", provider.ID, err)
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	catalog := GetOpenAIModels()

	models := make([]*model.Model, 0, len(openAIResp.Data))
	for _, m := range openAIResp.Data {
		spec := LookupOpenAICatalog(catalog, m.ID)
		if spec != nil {
			caps := model.Capability{
				Streaming:        spec.Streaming,
				Reasoning:        spec.Reasoning,
				ToolCalling:      spec.ToolCalling,
				StructuredOutput: spec.StructuredOutput,
				Vision:           spec.Vision,
			}
			capJSON, _ := json.Marshal(caps)

			contextLen := spec.ContextLength
			maxOutput := spec.MaxOutputTokens
			inPrice := spec.InputPricePerMillion
			outPrice := spec.OutputPricePerMillion

			modelEntry := &model.Model{
				ID:                    uuid.New(),
				ProviderID:            provider.ID,
				ModelID:               m.ID,
				Name:                  m.ID,
				DisplayName:           spec.DisplayName,
				Description:           spec.Description,
				Capabilities:          string(capJSON),
				Params:                "{}",
				Modality:              spec.Modality,
				InputModalities:       spec.InputModalities,
				OutputModalities:      spec.OutputModalities,
				ContextLength:         &contextLen,
				MaxOutputTokens:       &maxOutput,
				InputPricePerMillion:  &inPrice,
				OutputPricePerMillion: &outPrice,
				OwnedBy:               m.OwnedBy,
				Enabled:               true,
			}

			if spec.InputPricePerMillionCacheHit > 0 {
				cacheHitPrice := spec.InputPricePerMillionCacheHit
				modelEntry.InputPricePerMillionCacheHit = &cacheHitPrice
			}

			models = append(models, modelEntry)
		} else {
			log.Printf("[discovery] openai: model %q not in catalog, creating minimal entry", m.ID)
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
	}

	log.Printf("[discovery] openai: discovered %d models for provider %s", len(models), provider.ID)
	return models, nil
}
