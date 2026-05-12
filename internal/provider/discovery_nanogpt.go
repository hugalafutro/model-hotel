package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

func (d *DiscoveryService) discoverNanoGPT(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	debuglog.Info("discovery: starting nanogpt discovery", "provider", provider.ID)
	baseURL := util.SanitizeBaseURL(provider.BaseURL)

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+apiKey)
	headers.Set("Content-Type", "application/json")

	bodyBytes, err := d.fetchURL(ctx, "GET", baseURL+"/models?detailed=true", headers)
	if err != nil {
		debuglog.Error("discovery: nanogpt http request failed", "provider", provider.ID, "error", err)
		return nil, fmt.Errorf("nanogpt: failed to fetch models for provider %s: %w", provider.ID, err)
	}

	var nanoResp NanoGPTDetailedResponse
	if err := json.Unmarshal(bodyBytes, &nanoResp); err != nil {
		debuglog.Error("discovery: nanogpt decode response failed", "provider", provider.ID, "error", err)
		return nil, fmt.Errorf("nanogpt: failed to decode response for provider %s: %w", provider.ID, err)
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

		var inPricePerMill *float64
		var outPricePerMill *float64
		{
			v := m.Pricing.Prompt
			inPricePerMill = &v
		}
		{
			v := m.Pricing.Completion
			outPricePerMill = &v
		}

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

	debuglog.Info("discovery: nanogpt discovered models", "models", len(models), "provider", provider.ID)
	return models, nil
}

// GetNanoGPTUsage retrieves usage information from a NanoGPT provider.
func (d *DiscoveryService) GetNanoGPTUsage(ctx context.Context, provider *Provider, masterKey string) (*NanoGPTUsageResponse, error) {
	apiKey, err := auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, provider.KeySalt, masterKey)
	if err != nil {
		return nil, fmt.Errorf("nanogpt: failed to decrypt API key for provider %s: %w", provider.ID, err)
	}

	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	usageURL := baseURL + "/usage"

	req, err := http.NewRequestWithContext(ctx, "GET", usageURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("nanogpt: failed to create request for provider %s: %w", provider.ID, err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.doQuotaRequestWithRetry(ctx, req, provider.ID.String(), "nanogpt")
	if err != nil {
		return nil, fmt.Errorf("nanogpt: failed to fetch usage for provider %s: %w", provider.ID, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		debuglog.Error("discovery: nanogpt usage non-200 status", "status", resp.StatusCode, "provider", provider.ID, "body", util.SanitizeLogBody(string(body), 2000))
		return nil, fmt.Errorf("nanogpt: unexpected status code %d for provider %s", resp.StatusCode, provider.ID)
	}

	var usage NanoGPTUsageResponse
	if err := json.NewDecoder(resp.Body).Decode(&usage); err != nil {
		return nil, fmt.Errorf("nanogpt: failed to decode usage response for provider %s: %w", provider.ID, err)
	}

	return &usage, nil
}
