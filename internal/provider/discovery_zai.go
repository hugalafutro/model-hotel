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

func (d *DiscoveryService) discoverZAICoding(_ context.Context, provider *Provider, _ string) ([]*model.Model, error) {
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

	debuglog.Info("discovery: zai-coding discovered models from catalog", "provider", provider.ID, "models", len(catalog))

	return models, nil
}

// GetZAICodingQuota retrieves quota information for a ZAI Coding provider.
func (d *DiscoveryService) GetZAICodingQuota(ctx context.Context, provider *Provider, masterKey string) (*ZAICodingQuotaResponse, error) {
	apiKey, err := auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, provider.KeySalt, masterKey)
	if err != nil {
		return nil, fmt.Errorf("zai-coding: failed to decrypt API key for provider %s: %w", provider.ID, err)
	}

	quotaURL := "https://api.z.ai/api/monitor/usage/quota/limit"

	req, err := http.NewRequestWithContext(ctx, "GET", quotaURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("zai-coding: failed to create request for provider %s: %w", provider.ID, err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.doQuotaRequestWithRetry(ctx, req, provider.ID.String(), "zai-coding")
	if err != nil {
		debuglog.Error("discovery: zai-coding quota fetch failed", "provider", provider.ID, "error", err)
		return nil, fmt.Errorf("zai-coding: failed to fetch quota for provider %s: %w", provider.ID, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		debuglog.Error("discovery: zai-coding quota fetch non-200 status", "provider", provider.ID, "status", resp.StatusCode, "body", util.SanitizeLogBody(string(body), 2000))
		return nil, fmt.Errorf("zai-coding: unexpected status code %d for provider %s", resp.StatusCode, provider.ID)
	}

	var quota ZAICodingQuotaResponse
	if err := json.NewDecoder(resp.Body).Decode(&quota); err != nil {
		debuglog.Error("discovery: zai-coding quota decode failed", "provider", provider.ID, "error", err)
		return nil, fmt.Errorf("zai-coding: failed to decode response for provider %s: %w", provider.ID, err)
	}

	return &quota, nil
}
