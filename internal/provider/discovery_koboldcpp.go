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

// KoboldCPPVersionResponse is the response from /api/extra/version. Besides
// identifying the server, it reports whether the loaded chat model was given
// vision or audio adapters, which is the only place KoboldCPP says so.
//
// Its sibling flags are deliberately not read: `transcribe` means a separate
// Whisper model is loaded for /api/extra/transcribe, and `tts`, `txt2img` and
// `embeddings` are likewise separate endpoints. None of them say anything about
// what the chat model accepts.
type KoboldCPPVersionResponse struct {
	Result  string `json:"result"`
	Version string `json:"version"`
	Vision  bool   `json:"vision"`
	Audio   bool   `json:"audio"`
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

// KoboldCPPContextResponse is the response from
// /api/extra/true_max_context_length, KoboldCPP's context-size endpoint.
type KoboldCPPContextResponse struct {
	Value int `json:"value"`
}

func (d *DiscoveryService) discoverKoboldCPP(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	// Strip /v1 suffix if present — native endpoints are at the root
	apiBase := strings.TrimSuffix(baseURL, "/v1")

	// Step 1: Verify it's KoboldCPP via /api/extra/version
	versionInfo, err := d.koboldcppVersion(ctx, apiBase, apiKey)
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

	// Step 3: Context size, from the endpoint that reports it
	contextLength := d.koboldcppContextLength(ctx, apiBase, apiKey)

	// Step 4: Build model with conservative defaults
	caps := model.Capability{
		Streaming:   true,
		ToolCalling: false, // Conservative — tool calling uses custom format
	}
	capJSON, _ := json.Marshal(caps)

	// The version endpoint reports the adapters the loaded chat model was
	// given, so a vision or audio KoboldCPP is not filed as text-only.
	inputModalities := []string{"text"}
	if versionInfo.Vision {
		inputModalities = append(inputModalities, "image")
	}
	if versionInfo.Audio {
		inputModalities = append(inputModalities, "audio")
	}
	modalitiesJSON, _ := json.Marshal(inputModalities)

	// KoboldCPP's /models has no type; NormalizeModelClassification's name
	// heuristics classify embedding/reranker models out of the chat picker.

	m := &model.Model{
		ID:              uuid.New(),
		ProviderID:      provider.ID,
		ModelID:         modelID,
		Name:            modelID,
		DisplayName:     modelID,
		Description:     fmt.Sprintf("KoboldCPP %s model", versionInfo.Version),
		Capabilities:    string(capJSON),
		Params:          "{}",
		InputModalities: string(modalitiesJSON),
		ContextLength:   contextLength,
		OwnedBy:         "koboldcpp",
		Enabled:         true,
	}

	// Context length comes from the live /api/extra/true_max_context_length
	// probe, so mark it live: a reload with a different context size
	// propagates and is reported.
	models := []*model.Model{m}
	markLiveMeta(models)

	debuglog.Info("discovery: koboldcpp discovered model", "model", modelID, "provider", provider.Name, "provider_id", provider.ID)
	return models, nil
}

func (d *DiscoveryService) koboldcppVersion(ctx context.Context, apiBase, apiKey string) (*KoboldCPPVersionResponse, error) {
	url := apiBase + "/api/extra/version"
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, err
	}
	// A server started with --password rejects every route, native ones
	// included, so the key belongs on this request as much as on /models.
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := d.doDiscoveryRequestPrebuilt(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var versionResp KoboldCPPVersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&versionResp); err != nil {
		return nil, fmt.Errorf("failed to decode: %w", err)
	}

	if !strings.EqualFold(versionResp.Result, "koboldcpp") {
		return nil, fmt.Errorf("not a KoboldCPP server (got %q)", versionResp.Result)
	}

	return &versionResp, nil
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

// koboldcppContextLength reads the loaded model's context size from
// /api/extra/true_max_context_length, which KoboldCPP has served since v1.50.
// It returns nil when the endpoint is missing or unreadable: an unknown context
// size is left unset rather than guessed.
func (d *DiscoveryService) koboldcppContextLength(ctx context.Context, apiBase, apiKey string) *int {
	url := apiBase + "/api/extra/true_max_context_length"
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := d.doDiscoveryRequestPrebuilt(ctx, req)
	if err != nil {
		debuglog.Info("discovery: koboldcpp context length unavailable", "error", err)
		return nil
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		debuglog.Info("discovery: koboldcpp context length unavailable", "status", resp.StatusCode)
		return nil
	}

	var out KoboldCPPContextResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		debuglog.Info("discovery: koboldcpp context length undecodable", "error", err)
		return nil
	}
	if out.Value <= 0 {
		return nil
	}
	return &out.Value
}
