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

func (d *DiscoveryService) discoverNanoGPT(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	debuglog.Info("discovery: starting nanogpt discovery", "provider", provider.Name, "provider_id", provider.ID)
	baseURL := util.SanitizeBaseURL(provider.BaseURL)

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+apiKey)
	headers.Set("Content-Type", "application/json")

	bodyBytes, err := d.fetchURL(ctx, "GET", baseURL+"/models?detailed=true", headers)
	if err != nil {
		debuglog.Error("discovery: nanogpt http request failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("nanogpt: failed to fetch models for provider %s: %w", provider.Name, err)
	}

	var nanoResp NanoGPTDetailedResponse
	if err := json.Unmarshal(bodyBytes, &nanoResp); err != nil {
		debuglog.Error("discovery: nanogpt decode response failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("nanogpt: failed to decode response for provider %s: %w", provider.Name, err)
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

		paramsMap := map[string]interface{}{}
		if m.Subscription != nil {
			paramsMap["subscription_included"] = m.Subscription.Included
			paramsMap["subscription_note"] = m.Subscription.Note
		}
		paramsJSON, _ := json.Marshal(paramsMap)

		// Pricing fields are optional: a nil (omitted) price stays nil so it is not
		// marked live and can't overwrite a stored value with 0 on a partial
		// response; a present value (including a real 0) is taken as authoritative.
		inPricePerMill := m.Pricing.Prompt
		outPricePerMill := m.Pricing.Completion

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

	// NanoGPT reports pricing and context straight from its live /models
	// payload, so mark those fields live: genuine provider changes overwrite on
	// upsert and surface in the discovery diff (id-only providers stay fill-only).
	markLiveMeta(models)

	debuglog.Info("discovery: nanogpt discovered models", "models", len(models), "provider", provider.Name, "provider_id", provider.ID)
	return models, nil
}

// GetNanoGPTUsage retrieves usage information from a NanoGPT provider.
func (d *DiscoveryService) GetNanoGPTUsage(ctx context.Context, provider *Provider, masterKey string) (*NanoGPTUsageResponse, error) {
	var usage NanoGPTUsageResponse
	if err := d.fetchQuotaJSON(ctx, provider, masterKey, "/usage", "nanogpt", "usage", &usage); err != nil {
		return nil, err
	}
	return &usage, nil
}
