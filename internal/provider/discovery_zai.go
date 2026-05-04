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
)

func (d *DiscoveryService) discoverZAICoding(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	catalog := GetZAICodingModels()

	models := make([]*model.Model, 0, len(catalog))
	for _, spec := range catalog {
		contextLen := spec.ContextLength
		maxOutput := spec.MaxOutputTokens

		inputMods := `["text"]`
		if spec.Modality == "vision" {
			inputMods = `["text","image","video","file"]`
		}

		caps := model.Capability{
			Streaming:        true,
			Reasoning:        spec.Reasoning,
			ToolCalling:      spec.ToolCalling,
			StructuredOutput: spec.StructuredOutput,
		}
		if spec.Modality == "vision" {
			caps.Vision = true
			caps.VideoInput = true
		}
		capJSON, _ := json.Marshal(caps)

		models = append(models, &model.Model{
			ID:               uuid.New(),
			ProviderID:       provider.ID,
			ModelID:          spec.ModelID,
			Name:             spec.ModelID,
			DisplayName:      spec.ModelID,
			Capabilities:     string(capJSON),
			Params:           "{}",
			Modality:         spec.Modality,
			InputModalities:  inputMods,
			OutputModalities: "[]",
			ContextLength:    &contextLen,
			MaxOutputTokens:  &maxOutput,
			OwnedBy:          "zhipu",
			Enabled:          true,
		})
	}

	log.Printf("[discovery] zai-coding provider %s: discovered %d models from catalog", provider.ID, len(catalog))

	return models, nil
}

func (d *DiscoveryService) GetZAICodingQuota(ctx context.Context, provider *Provider, masterKey string) (*ZAICodingQuotaResponse, error) {
	apiKey, err := auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, provider.KeySalt, masterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt API key: %w", err)
	}

	quotaURL := "https://api.z.ai/api/monitor/usage/quota/limit"

	req, err := http.NewRequestWithContext(ctx, "GET", quotaURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.doQuotaRequestWithRetry(ctx, req, provider.ID.String(), "zai-coding")
	if err != nil {
		log.Printf("[discovery] error: zai-coding provider %s quota fetch failed: %v", provider.ID, err)
		return nil, fmt.Errorf("failed to fetch quota: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		log.Printf("[discovery] error: zai-coding provider %s quota fetch returned status %d: %s", provider.ID, resp.StatusCode, string(body))
		return nil, fmt.Errorf("unexpected status code %d", resp.StatusCode)
	}

	var quota ZAICodingQuotaResponse
	if err := json.NewDecoder(resp.Body).Decode(&quota); err != nil {
		log.Printf("[discovery] error: zai-coding provider %s quota decode failed: %v", provider.ID, err)
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &quota, nil
}


