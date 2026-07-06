package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// KoboldCPPVersionResponse is the response from /api/extra/version.
type KoboldCPPVersionResponse struct {
	Result  string `json:"result"`
	Version string `json:"version"`
}

// KoboldCPPModelsResponse is the OpenAI-compatible models response from KoboldCPP.
type KoboldCPPModelsResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	} `json:"data"`
}

// KoboldCPPPerfResponse contains performance info from KoboldCPP.
type KoboldCPPPerfResponse struct {
	LastProcessTime    float64 `json:"last_process"`
	LastGenerationTime float64 `json:"last_gen"`
	Queue              int     `json:"queue"`
	MaxContextLength   int     `json:"maxcontextlen"`
	ModelLoaded        bool    `json:"model_loaded"`
}

func (d *DiscoveryService) discoverKoboldCPP(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	// Strip /v1 suffix if present — native endpoints are at the root
	apiBase := strings.TrimSuffix(baseURL, "/v1")

	// Step 1: Verify it's KoboldCPP via /api/extra/version
	version, err := d.koboldcppVersion(ctx, apiBase)
	if err != nil {
		return nil, fmt.Errorf("koboldcpp: version check failed for provider %s: %w", provider.Name, err)
	}

	// Step 2: Get currently loaded model
	modelID, err := d.koboldcppLoadedModel(ctx, baseURL, apiKey)
	if err != nil {
		return nil, fmt.Errorf("koboldcpp: model listing failed for provider %s: %w", provider.Name, err)
	}

	if modelID == "" {
		debuglog.Info("discovery: koboldcpp no model loaded", "provider", provider.Name, "provider_id", provider.ID)
		return []*model.Model{}, nil
	}

	// Step 3: Try to get context length from perf endpoint
	perf, err := d.koboldcppPerf(ctx, apiBase)
	var contextLength *int
	if err == nil && perf.MaxContextLength > 0 {
		contextLength = &perf.MaxContextLength
	}

	// Step 4: Build model with conservative defaults
	caps := model.Capability{
		Streaming:   true,
		ToolCalling: false, // Conservative — tool calling uses custom format
	}
	capJSON, _ := json.Marshal(caps)

	// KoboldCPP's /models has no type; if an embedding/reranker model is what's
	// loaded, classify it by name so it stays out of the chat picker.
	modality := "text"
	outputMods := `["text"]`
	if mod := inferNonChatModality(modelID); mod != "" {
		modality = mod
		_, outputMods = nonChatModalityArrays(mod)
	}

	m := &model.Model{
		ID:               uuid.New(),
		ProviderID:       provider.ID,
		ModelID:          modelID,
		Name:             modelID,
		DisplayName:      modelID,
		Description:      fmt.Sprintf("KoboldCPP %s model", version),
		Capabilities:     string(capJSON),
		Params:           "{}",
		Modality:         modality,
		InputModalities:  `["text"]`,
		OutputModalities: outputMods,
		ContextLength:    contextLength,
		OwnedBy:          "koboldcpp",
		Enabled:          true,
	}

	// Context length comes from the live /api/extra/perf probe, so mark it live:
	// a reload with a different context size propagates and is reported.
	models := []*model.Model{m}
	markLiveMeta(models)

	debuglog.Info("discovery: koboldcpp discovered model", "model", modelID, "provider", provider.Name, "provider_id", provider.ID)
	return models, nil
}

func (d *DiscoveryService) koboldcppVersion(ctx context.Context, apiBase string) (string, error) {
	url := apiBase + "/api/extra/version"
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return "", err
	}

	resp, err := d.doDiscoveryRequestPrebuilt(ctx, req)
	if err != nil {
		return "", fmt.Errorf("http request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var versionResp KoboldCPPVersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&versionResp); err != nil {
		return "", fmt.Errorf("failed to decode: %w", err)
	}

	if versionResp.Result != "KoboldCpp" {
		return "", fmt.Errorf("not a KoboldCPP server (got %q)", versionResp.Result)
	}

	return versionResp.Version, nil
}

func (d *DiscoveryService) koboldcppLoadedModel(ctx context.Context, baseURL, apiKey string) (string, error) {
	url := baseURL + "/models"
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return "", err
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := d.doDiscoveryRequestPrebuilt(ctx, req)
	if err != nil {
		return "", fmt.Errorf("http request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("status %d: %s", resp.StatusCode, string(body))
	}

	var modelsResp KoboldCPPModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return "", fmt.Errorf("failed to decode: %w", err)
	}

	if len(modelsResp.Data) == 0 {
		return "", nil
	}

	return modelsResp.Data[0].ID, nil
}

func (d *DiscoveryService) koboldcppPerf(ctx context.Context, apiBase string) (*KoboldCPPPerfResponse, error) {
	url := apiBase + "/api/extra/perf"
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, err
	}

	resp, err := d.doDiscoveryRequestPrebuilt(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	var perf KoboldCPPPerfResponse
	if err := json.NewDecoder(resp.Body).Decode(&perf); err != nil {
		return nil, fmt.Errorf("failed to decode: %w", err)
	}

	return &perf, nil
}
