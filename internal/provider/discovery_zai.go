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

func (d *DiscoveryService) discoverZAICoding(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	catalog := zaiCodingCatalogModels(provider.ID)

	live, err := d.discoverZAICodingLive(ctx, provider, apiKey)
	if err != nil {
		// Abort the scan rather than falling back to the catalog: a transient
		// failure must not let DisableMissingModels disable models that are only
		// in the live listing (the Z.ai catalog is a subset). Aborting preserves
		// the existing models; the catalog union still runs on a successful fetch.
		debuglog.Error("discovery: zai-coding live /models failed, aborting scan", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, err
	}
	if len(live) == 0 {
		// Successful fetch but empty list: return empty (not the catalog) so the
		// discovered set stays empty and DisableMissingModels is a no-op,
		// instead of disabling every live-only model.
		debuglog.Warn("discovery: zai-coding live /models returned no models, skipping", "provider", provider.Name, "provider_id", provider.ID)
		return live, nil
	}

	merged := mergeLiveAndCatalog(live, catalog)
	debuglog.Info("discovery: zai-coding discovered models", "provider", provider.Name, "provider_id", provider.ID, "live", len(live), "catalog", len(catalog), "merged", len(merged))
	return merged, nil
}

// discoverZAICodingLive fetches the live model list from the Z.ai
// OpenAI-compatible /models endpoint. It returns minimal models (id + owner);
// richer metadata (context length, capabilities, modality) is backfilled from
// the embedded catalog and models.dev by the caller.
func (d *DiscoveryService) discoverZAICodingLive(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)

	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+apiKey)
	headers.Set("Content-Type", "application/json")

	bodyBytes, err := d.fetchURL(ctx, "GET", baseURL+"/models", headers)
	if err != nil {
		return nil, fmt.Errorf("zai-coding: failed to fetch models for provider %s: %w", provider.Name, err)
	}

	var resp OpenAIModelsResponse
	if err := json.Unmarshal(bodyBytes, &resp); err != nil {
		return nil, fmt.Errorf("zai-coding: failed to decode models for provider %s: %w", provider.Name, err)
	}

	models := make([]*model.Model, 0, len(resp.Data))
	for _, m := range resp.Data {
		models = append(models, zaiCodingLiveModel(m.ID, m.OwnedBy, provider.ID))
	}
	return models, nil
}

// zaiCodingLiveModel builds a minimal model from a live /models entry. Only the
// id and owner are authoritative from the live listing; everything else is left
// for the catalog and models.dev to backfill. It reuses liveModelStub so the
// base capabilities (streaming) and empty JSONB modalities match every other
// provider's live stub — a Z.ai model with no catalog/models.dev coverage must
// still be marked streaming-capable.
func zaiCodingLiveModel(id, ownedBy string, providerID uuid.UUID) *model.Model {
	owner := ownedBy
	// The Z.ai listing reports owned_by "z-ai"; normalize to the catalog's
	// "zhipu" convention so models from both sources group consistently.
	if owner == "" || owner == "z-ai" {
		owner = "zhipu"
	}
	return liveModelStub(id, owner, providerID)
}

// zaiCodingCatalogModels converts the embedded Z.ai catalog into models.
func zaiCodingCatalogModels(providerID uuid.UUID) []*model.Model {
	specs := GetZAICodingModels()
	models := make([]*model.Model, 0, len(specs))
	for _, spec := range specs {
		models = append(models, zaiCodingSpecToModel(spec, providerID))
	}
	return models
}

// zaiCodingSpecToModel converts a single catalog spec into a model.
func zaiCodingSpecToModel(spec ZAICodingModelSpec, providerID uuid.UUID) *model.Model {
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

	return &model.Model{
		ID:               uuid.New(),
		ProviderID:       providerID,
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
	}
}

// GetZAICodingQuota retrieves quota information for a ZAI Coding provider.
func (d *DiscoveryService) GetZAICodingQuota(ctx context.Context, provider *Provider, masterKey string) (*ZAICodingQuotaResponse, error) {
	apiKey, err := auth.Decrypt(provider.EncryptedKey, provider.KeyNonce, provider.KeySalt, masterKey)
	if err != nil {
		return nil, fmt.Errorf("zai-coding: failed to decrypt API key for provider %s: %w", provider.Name, err)
	}

	quotaURL := "https://api.z.ai/api/monitor/usage/quota/limit"

	req, err := http.NewRequestWithContext(ctx, "GET", quotaURL, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("zai-coding: failed to create request for provider %s: %w", provider.Name, err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.doQuotaRequestWithRetry(ctx, req, provider.ID.String(), provider.Name, "zai-coding")
	if err != nil {
		debuglog.Error("discovery: zai-coding quota fetch failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("zai-coding: failed to fetch quota for provider %s: %w", provider.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		debuglog.Error("discovery: zai-coding quota fetch non-200 status", "provider", provider.Name, "provider_id", provider.ID, "status", resp.StatusCode, "body", util.SanitizeLogBody(string(body), 2000))
		return nil, fmt.Errorf("zai-coding: unexpected status code %d for provider %s", resp.StatusCode, provider.Name)
	}

	var quota ZAICodingQuotaResponse
	if err := json.NewDecoder(resp.Body).Decode(&quota); err != nil {
		debuglog.Error("discovery: zai-coding quota decode failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("zai-coding: failed to decode response for provider %s: %w", provider.Name, err)
	}

	return &quota, nil
}
