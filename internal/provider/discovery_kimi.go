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

// kimiCodeModel is one entry in the Kimi Code /models listing. The endpoint is
// OpenAI-shaped but carries rich extras (display name, context length,
// capability flags) that discovery maps directly; these model IDs (k3,
// kimi-for-coding…) are absent from models.dev, so the live listing is the
// only metadata source.
type kimiCodeModel struct {
	ID                string `json:"id"`
	DisplayName       string `json:"display_name"`
	ContextLength     int    `json:"context_length"`
	SupportsReasoning bool   `json:"supports_reasoning"`
	SupportsImageIn   bool   `json:"supports_image_in"`
	SupportsVideoIn   bool   `json:"supports_video_in"`
}

// kimiCodeModelsResponse is the Kimi Code /models response envelope.
type kimiCodeModelsResponse struct {
	Data []kimiCodeModel `json:"data"`
}

// discoverKimiCode fetches the live model list from the Kimi Code
// (api.kimi.com/coding) OpenAI-compatible /models endpoint. There is no
// embedded catalog: the listing's own metadata is authoritative, and a fetch
// error aborts the scan so a transient failure cannot let
// RecordMissingModels disable live-only models.
func (d *DiscoveryService) discoverKimiCode(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+apiKey)
	headers.Set("Content-Type", "application/json")

	bodyBytes, err := d.fetchURL(ctx, "GET", baseURL+"/models", headers)
	if err != nil {
		return nil, fmt.Errorf("kimi-code: failed to fetch models for provider %s: %w", provider.Name, err)
	}

	var resp kimiCodeModelsResponse
	if err := json.Unmarshal(bodyBytes, &resp); err != nil {
		return nil, fmt.Errorf("kimi-code: failed to decode models for provider %s: %w", provider.Name, err)
	}

	models := make([]*model.Model, 0, len(resp.Data))
	for _, m := range resp.Data {
		models = append(models, kimiCodeLiveModel(m, provider.ID))
	}
	markLiveMeta(models)
	debuglog.Info("discovery: kimi-code discovered models", "provider", provider.Name, "provider_id", provider.ID, "models", len(models))
	return models, nil
}

// kimiCodeLiveModel maps one listing entry onto a model. ToolCalling is always
// true: the endpoint exists to serve coding agents, where tool use is the
// core function of every tier's models.
func kimiCodeLiveModel(m kimiCodeModel, providerID uuid.UUID) *model.Model {
	caps := model.Capability{
		Streaming:   true,
		Reasoning:   m.SupportsReasoning,
		ToolCalling: true,
		Vision:      m.SupportsImageIn,
		VideoInput:  m.SupportsVideoIn,
	}
	capJSON, _ := json.Marshal(caps)

	inputMods := `["text"]`
	switch {
	case m.SupportsImageIn && m.SupportsVideoIn:
		inputMods = `["text","image","video"]`
	case m.SupportsImageIn:
		inputMods = `["text","image"]`
	}

	display := m.DisplayName
	if display == "" {
		display = m.ID
	}

	mm := &model.Model{
		ID:               uuid.New(),
		ProviderID:       providerID,
		ModelID:          m.ID,
		Name:             m.ID,
		DisplayName:      display,
		Capabilities:     string(capJSON),
		Params:           "{}",
		InputModalities:  inputMods,
		OutputModalities: `["text"]`,
		OwnedBy:          "moonshotai",
		Enabled:          true,
	}
	if m.ContextLength > 0 {
		cl := m.ContextLength
		mm.ContextLength = &cl
	}
	return mm
}

// GetKimiCodeQuota retrieves quota information for a Kimi Code provider from
// its /usages endpoint.
func (d *DiscoveryService) GetKimiCodeQuota(ctx context.Context, provider *Provider, masterKey string) (*KimiCodeQuotaResponse, error) {
	apiKey, err := auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, provider.KeySalt, masterKey)
	if err != nil {
		return nil, fmt.Errorf("kimi-code: failed to decrypt API key for provider %s: %w", provider.Name, err)
	}

	quotaURL := util.SanitizeBaseURL(provider.BaseURL) + "/usages"

	req, err := http.NewRequestWithContext(ctx, "GET", quotaURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("kimi-code: failed to create request for provider %s: %w", provider.Name, err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.doQuotaRequestWithRetry(ctx, req, provider.ID.String(), provider.Name, "kimi-code")
	if err != nil {
		debuglog.Error("discovery: kimi-code quota fetch failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("kimi-code: failed to fetch quota for provider %s: %w", provider.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		if authErr := quotaAuthError("kimi-code", provider, resp.StatusCode, body); authErr != nil {
			return nil, authErr
		}
		debuglog.Error("discovery: kimi-code quota fetch non-200 status", "provider", provider.Name, "provider_id", provider.ID, "status", resp.StatusCode, "body", util.SanitizeLogBody(string(body), 2000))
		return nil, fmt.Errorf("kimi-code: unexpected status code %d for provider %s", resp.StatusCode, provider.Name)
	}

	var quota KimiCodeQuotaResponse
	if err := json.NewDecoder(resp.Body).Decode(&quota); err != nil {
		debuglog.Error("discovery: kimi-code quota decode failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("kimi-code: failed to decode response for provider %s: %w", provider.Name, err)
	}

	return &quota, nil
}
