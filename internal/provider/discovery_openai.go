package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

func (d *DiscoveryService) discoverOpenAI(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeAPIURL(provider.BaseURL)

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+apiKey)
	headers.Set("Content-Type", "application/json")

	bodyBytes, err := d.fetchURL(ctx, "GET", baseURL+"/v1/models", headers)
	if err != nil {
		debuglog.Error("discovery: openai fetch models failed", "provider", provider.ID, "error", err)
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}

	var openAIResp OpenAIModelsResponse
	if err := json.Unmarshal(bodyBytes, &openAIResp); err != nil {
		debuglog.Error("discovery: openai json decode failed", "provider", provider.ID, "error", err)
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
			debuglog.Warn("discovery: openai model not in catalog", "model", m.ID)
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

	debuglog.Info("discovery: openai discovered models", "models", len(models), "provider", provider.ID)
	return models, nil
}
