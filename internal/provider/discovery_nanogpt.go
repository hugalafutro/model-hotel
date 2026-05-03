package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/google/uuid"
	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

func (d *DiscoveryService) discoverNanoGPT(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	log.Printf("[discovery] starting nanogpt discovery for provider %s", provider.ID)
	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/models?detailed=true", nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		log.Printf("[discovery] error: nanogpt http request failed for provider %s: %v", provider.ID, err)
		return nil, fmt.Errorf("failed to fetch models: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[discovery] error: nanogpt returned status %d for provider %s: %s", resp.StatusCode, provider.ID, string(body))
		return nil, fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	var nanoResp NanoGPTDetailedResponse
	if err := json.NewDecoder(resp.Body).Decode(&nanoResp); err != nil {
		log.Printf("[discovery] error: failed to decode nanogpt response for provider %s: %v", provider.ID, err)
		return nil, fmt.Errorf("failed to decode response: %w", err)
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

	log.Printf("[discovery] nanogpt discovered %d models for provider %s", len(models), provider.ID)
	return models, nil
}

func (d *DiscoveryService) GetNanoGPTUsage(ctx context.Context, provider *Provider, masterKey string) (*NanoGPTUsageResponse, error) {
	apiKey, err := auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, provider.KeySalt, masterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt API key: %w", err)
	}

	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	usageURL := baseURL + "/usage"

	req, err := http.NewRequestWithContext(ctx, "GET", usageURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.doQuotaRequestWithRetry(ctx, req, provider.ID.String(), "nanogpt")
	if err != nil {
		return nil, fmt.Errorf("failed to fetch usage: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[discovery] error: nanogpt usage: non-200 status %d for provider %s: %s", resp.StatusCode, provider.ID, util.SanitizeLogBody(string(body), 200))
		return nil, fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	var usage NanoGPTUsageResponse
	if err := json.NewDecoder(resp.Body).Decode(&usage); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &usage, nil
}
