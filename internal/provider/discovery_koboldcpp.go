package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// KoboldCPPVersionResponse is the response from /api/extra/version. Besides
// identifying the server, it is the only place KoboldCPP says what is loaded:
// `vision` and `audio` describe the adapters the chat model was given, and
// `txt2img`, `tts`, `transcribe` and `embeddings` each mean a separate side
// model serves that endpoint. None of the side flags say anything about what
// the chat model accepts.
//
// LLM is a pointer because builds older than the flag omit it; those always
// had a chat model, so an absent flag reads as loaded.
type KoboldCPPVersionResponse struct {
	Result     string `json:"result"`
	Version    string `json:"version"`
	LLM        *bool  `json:"llm"`
	Vision     bool   `json:"vision"`
	Audio      bool   `json:"audio"`
	Txt2Img    bool   `json:"txt2img"`
	TTS        bool   `json:"tts"`
	Transcribe bool   `json:"transcribe"`
	Embeddings bool   `json:"embeddings"`
}

// KoboldCPPContextResponse is the response from
// /api/extra/true_max_context_length, KoboldCPP's context-size endpoint.
type KoboldCPPContextResponse struct {
	Value int `json:"value"`
}

// KoboldCPPSDModel is one entry of /sdapi/v1/sd-models, the A1111-compatible
// listing that is the only place KoboldCPP names its image model.
type KoboldCPPSDModel struct {
	ModelName string `json:"model_name"`
}

// KoboldCPP serves one model per side endpoint and ignores the request's model
// field, so the side models only need an ID the proxy can route by. The image
// model is the one KoboldCPP names; the rest are never named anywhere, so they
// carry fixed IDs that also stay put when the file behind them is swapped.
const (
	koboldcppImageFallbackID = "koboldcpp/image"
	koboldcppTTSID           = "koboldcpp/tts"
	koboldcppSTTID           = "koboldcpp/whisper"
	koboldcppEmbeddingsID    = "koboldcpp/embeddings"
)

func (d *DiscoveryService) discoverKoboldCPP(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	// Strip /v1 suffix if present — native endpoints are at the root
	apiBase := strings.TrimSuffix(baseURL, "/v1")

	// Step 1: Verify it's KoboldCPP via /api/extra/version
	versionInfo, err := d.koboldcppVersion(ctx, apiBase, apiKey)
	if err != nil {
		return nil, fmt.Errorf("koboldcpp: version check failed for provider %s: %w", provider.Name, err)
	}

	models := []*model.Model{}

	// Step 2: the chat model. A server started without one still lists a
	// placeholder named "inactive" on /models, so the llm flag decides where
	// the build reports it.
	if versionInfo.LLM == nil || *versionInfo.LLM {
		chat, err := d.koboldcppChatModel(ctx, provider, baseURL, apiBase, apiKey, versionInfo)
		if err != nil {
			return nil, err
		}
		if chat != nil {
			models = append(models, chat)
		}
	}

	// Step 3: the side models the version flags report. The chat model is
	// named after its file, so a chat file called tts.gguf would share
	// koboldcpp/tts; the chat model keeps the ID, since the upsert is keyed
	// on it and a second row with that ID would overwrite its class.
	for _, side := range d.koboldcppSideModels(ctx, provider, apiBase, apiKey, versionInfo) {
		if len(models) > 0 && models[0].ModelID == side.ModelID {
			debuglog.Info("discovery: koboldcpp side model shares the chat model's ID, skipped", "model", side.ModelID, "provider", provider.Name, "provider_id", provider.ID)
			continue
		}
		models = append(models, side)
	}

	// The chat model's context length comes from the live
	// /api/extra/true_max_context_length probe, so mark it live: a reload with
	// a different context size propagates and is reported. Side models carry
	// no context length, so nothing on them is marked.
	markLiveMeta(models)

	if len(models) == 0 {
		debuglog.Info("discovery: koboldcpp no model loaded", "provider", provider.Name, "provider_id", provider.ID)
		return models, nil
	}
	debuglog.Info("discovery: koboldcpp discovered models", "models", len(models), "provider", provider.Name, "provider_id", provider.ID)
	return models, nil
}

// koboldcppChatModel builds the loaded chat model, or returns nil when /models
// lists none or only the "inactive" placeholder. The placeholder check covers
// builds too old to report the llm flag; a loaded model is always listed as
// koboldcpp/<file>, so no real model carries that bare name.
func (d *DiscoveryService) koboldcppChatModel(ctx context.Context, provider *Provider, baseURL, apiBase, apiKey string, versionInfo *KoboldCPPVersionResponse) (*model.Model, error) {
	modelID, err := d.koboldcppLoadedModel(ctx, baseURL, apiKey)
	if err != nil {
		return nil, fmt.Errorf("koboldcpp: model listing failed for provider %s: %w", provider.Name, err)
	}
	if modelID == "" || modelID == "inactive" {
		return nil, nil
	}

	contextLength := d.koboldcppContextLength(ctx, apiBase, apiKey)

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
	return &model.Model{
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
	}, nil
}

// koboldcppSideModels returns one model per side endpoint the version flags
// report loaded. Each states its endpoint class explicitly, which
// NormalizeModelClassification treats as final and fills the modality arrays
// from.
func (d *DiscoveryService) koboldcppSideModels(ctx context.Context, provider *Provider, apiBase, apiKey string, versionInfo *KoboldCPPVersionResponse) []*model.Model {
	imageID, imageNamed := "", false
	if versionInfo.Txt2Img {
		imageID, imageNamed = d.koboldcppImageModelID(ctx, apiBase, apiKey)
	}
	side := []struct {
		loaded          bool
		id, class, what string
	}{
		{versionInfo.Txt2Img && imageNamed, imageID, "image", "image generation"},
		{versionInfo.TTS, koboldcppTTSID, "tts", "text-to-speech"},
		{versionInfo.Transcribe, koboldcppSTTID, "stt", "speech-to-text"},
		{versionInfo.Embeddings, koboldcppEmbeddingsID, "embedding", "embeddings"},
	}
	var models []*model.Model
	for _, s := range side {
		if !s.loaded {
			continue
		}
		id := s.id
		models = append(models, &model.Model{
			ID:           uuid.New(),
			ProviderID:   provider.ID,
			ModelID:      id,
			Name:         id,
			DisplayName:  id,
			Description:  fmt.Sprintf("KoboldCPP %s %s model", versionInfo.Version, s.what),
			Capabilities: "{}",
			Params:       "{}",
			Modality:     s.class,
			OwnedBy:      "koboldcpp",
			Enabled:      true,
		})
	}
	return models
}

// koboldcppImageModelID names the image model from /sdapi/v1/sd-models, which
// KoboldCPP always answers with the one loaded model. A server that has no such
// listing (404) or names nothing gets the fixed fallback ID: the flag already
// proved the endpoint is served, and the name is missing for good.
//
// Any other failure may be transient, and falling back then would swap the
// model's ID for one scan and back on the next, leaving a stray model and a
// missing-scan strike on the real one. So ok is false and the image model sits
// this scan out, which is one strike on an ID that stays put.
func (d *DiscoveryService) koboldcppImageModelID(ctx context.Context, apiBase, apiKey string) (id string, ok bool) {
	bodyBytes, err := d.fetchURL(ctx, "GET", apiBase+"/sdapi/v1/sd-models", bearerHeader(apiKey))
	if err != nil {
		status := errorStatusCode(err)
		debuglog.Info("discovery: koboldcpp image model listing failed", "status", status, "error", err)
		if status == http.StatusNotFound {
			return koboldcppImageFallbackID, true
		}
		return "", false
	}
	var list []KoboldCPPSDModel
	if err := json.Unmarshal(bodyBytes, &list); err != nil {
		debuglog.Info("discovery: koboldcpp image model listing undecodable", "error", err)
		return "", false
	}
	if len(list) == 0 || strings.TrimSpace(list[0].ModelName) == "" {
		debuglog.Info("discovery: koboldcpp image model listing names no model")
		return koboldcppImageFallbackID, true
	}
	return "koboldcpp/" + strings.TrimSpace(list[0].ModelName), true
}

func (d *DiscoveryService) koboldcppVersion(ctx context.Context, apiBase, apiKey string) (*KoboldCPPVersionResponse, error) {
	// A server started with --password rejects every route, native ones
	// included, so the key belongs on this request as much as on /models.
	bodyBytes, err := d.fetchURL(ctx, "GET", apiBase+"/api/extra/version", bearerHeader(apiKey))
	if err != nil {
		return nil, err
	}

	var versionResp KoboldCPPVersionResponse
	if err := json.Unmarshal(bodyBytes, &versionResp); err != nil {
		return nil, fmt.Errorf("failed to decode: %w", err)
	}

	if !isKoboldCPPVersion(versionResp) {
		return nil, fmt.Errorf("not a KoboldCPP server (got %q)", versionResp.Result)
	}

	return &versionResp, nil
}

// isKoboldCPPVersion reports whether an /api/extra/version payload is
// KoboldCPP identifying itself, the one fingerprint that names the product.
func isKoboldCPPVersion(v KoboldCPPVersionResponse) bool {
	return strings.EqualFold(v.Result, "koboldcpp")
}

func (d *DiscoveryService) koboldcppLoadedModel(ctx context.Context, baseURL, apiKey string) (string, error) {
	bodyBytes, err := d.fetchURL(ctx, "GET", baseURL+"/models", bearerHeader(apiKey))
	if err != nil {
		return "", err
	}

	var modelsResp OpenAIModelsResponse
	if err := json.Unmarshal(bodyBytes, &modelsResp); err != nil {
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
	bodyBytes, err := d.fetchURL(ctx, "GET", apiBase+"/api/extra/true_max_context_length", bearerHeader(apiKey))
	if err != nil {
		debuglog.Info("discovery: koboldcpp context length unavailable", "status", errorStatusCode(err), "error", err)
		return nil
	}

	var out KoboldCPPContextResponse
	if err := json.Unmarshal(bodyBytes, &out); err != nil {
		debuglog.Info("discovery: koboldcpp context length undecodable", "error", err)
		return nil
	}
	if out.Value <= 0 {
		return nil
	}
	return &out.Value
}
