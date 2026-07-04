package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// LMStudioModelsResponse is the OpenAI-compatible models response from LMStudio.
type LMStudioModelsResponse struct {
	Object string `json:"object"`
	Data   []struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	} `json:"data"`
}

// LMStudioV0ModelsResponse is LM Studio's native REST API model listing
// (GET /api/v0/models). Unlike the OpenAI-compatible /v1/models, it reports a
// model `type` ("llm" | "vlm" | "embeddings") and a max_context_length, letting
// discovery hide embedding models from the chat picker and fill a context
// length the OpenAI listing never provides.
type LMStudioV0ModelsResponse struct {
	Object string            `json:"object"`
	Data   []LMStudioV0Model `json:"data"`
}

// LMStudioV0Model is a single entry from LM Studio's /api/v0/models.
type LMStudioV0Model struct {
	ID               string `json:"id"`
	Type             string `json:"type"` // "llm" | "vlm" | "embeddings"
	Publisher        string `json:"publisher"`
	Arch             string `json:"arch"`
	MaxContextLength int    `json:"max_context_length"`
}

func (d *DiscoveryService) discoverLMStudio(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	// Prefer LM Studio's native /api/v0/models: it reports the model type
	// (llm/vlm/embeddings) and a context length. Fall back to the
	// OpenAI-compatible /v1/models listing when the native endpoint isn't
	// available (older LM Studio, or a different OpenAI-compatible server that
	// happens to sit on the LM Studio port).
	models, err := d.discoverLMStudioNative(ctx, provider, apiKey)
	if err == nil {
		return models, nil
	}
	debuglog.Info("discovery: lmstudio native endpoint unavailable, falling back to /v1/models",
		"provider", provider.Name, "provider_id", provider.ID, "error", err)
	return d.discoverLMStudioOpenAI(ctx, provider, apiKey)
}

// discoverLMStudioNative queries LM Studio's native /api/v0/models endpoint.
func (d *DiscoveryService) discoverLMStudioNative(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	// The native REST API is rooted at /api/v0, sibling to the OpenAI-compatible
	// /v1 mount, so strip any /v1 suffix before appending it.
	apiBase := util.SanitizeAPIURL(provider.BaseURL)
	url := apiBase + "/api/v0/models"

	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("lmstudio: failed to create native request for provider %s: %w", provider.Name, err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("lmstudio: native request failed for provider %s: %w", provider.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("lmstudio: native endpoint status %d for provider %s: %s", resp.StatusCode, provider.Name, string(body))
	}

	var modelsResp LMStudioV0ModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return nil, fmt.Errorf("lmstudio: failed to decode native response for provider %s: %w", provider.Name, err)
	}
	// A well-formed but empty payload from a non-LM-Studio server would leave us
	// with nothing; treat it as "endpoint not really there" and let the caller
	// fall back to /v1/models.
	if len(modelsResp.Data) == 0 {
		return nil, fmt.Errorf("lmstudio: native endpoint returned no models for provider %s", provider.Name)
	}

	models := make([]*model.Model, 0, len(modelsResp.Data))
	for _, m := range modelsResp.Data {
		models = append(models, buildLMStudioNativeModel(provider, m))
	}

	// Context length comes from the live native probe, so mark it live: a model
	// reloaded with a different context window propagates and is reported.
	markLiveMeta(models)

	debuglog.Info("discovery: lmstudio discovered models (native)", "models", len(models), "provider", provider.Name, "provider_id", provider.ID)
	return models, nil
}

// buildLMStudioNativeModel maps a native /api/v0 model entry to a model.Model,
// using the reported type to set the modality (so embedding models are hidden
// from the chat picker).
func buildLMStudioNativeModel(provider *Provider, m LMStudioV0Model) *model.Model {
	caps := model.Capability{
		Streaming:        true,
		StructuredOutput: true, // LM Studio supports response_format with JSON schema
	}

	modality := "text"
	inputMods := `["text"]`
	outputMods := `["text"]`
	switch m.Type {
	case "embeddings":
		modality = "embedding"
		outputMods = `["embedding"]`
	case "vlm":
		caps.Vision = true
		modality = "vision"
		inputMods = `["text","image"]`
	}
	capJSON, _ := json.Marshal(caps)

	ownedBy := m.Publisher
	if ownedBy == "" {
		ownedBy = "lmstudio"
	}

	var contextLength *int
	if m.MaxContextLength > 0 {
		cl := m.MaxContextLength
		contextLength = &cl
	}

	return &model.Model{
		ID:               uuid.New(),
		ProviderID:       provider.ID,
		ModelID:          m.ID,
		Name:             m.ID,
		DisplayName:      m.ID,
		Description:      "LM Studio local model",
		Capabilities:     string(capJSON),
		Params:           "{}",
		Modality:         modality,
		InputModalities:  inputMods,
		OutputModalities: outputMods,
		ContextLength:    contextLength,
		OwnedBy:          ownedBy,
		Enabled:          true,
	}
}

// discoverLMStudioOpenAI is the fallback that reads the OpenAI-compatible
// /v1/models listing. That listing carries no model type, so embedding and
// reranker models are classified by name heuristic to keep them out of the
// chat picker.
func (d *DiscoveryService) discoverLMStudioOpenAI(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)

	url := baseURL + "/models"
	req, err := http.NewRequestWithContext(ctx, "GET", url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("lmstudio: failed to create request for provider %s: %w", provider.Name, err)
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := d.httpClient.Do(req)
	if err != nil {
		debuglog.Error("discovery: lmstudio http request failed", "provider", provider.Name, "provider_id", provider.ID, "error", err)
		return nil, fmt.Errorf("lmstudio: failed to fetch models for provider %s: %w", provider.Name, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("lmstudio: unexpected status %d for provider %s: %s", resp.StatusCode, provider.Name, string(body))
	}

	var modelsResp LMStudioModelsResponse
	if err := json.NewDecoder(resp.Body).Decode(&modelsResp); err != nil {
		return nil, fmt.Errorf("lmstudio: failed to decode response for provider %s: %w", provider.Name, err)
	}

	models := make([]*model.Model, 0, len(modelsResp.Data))
	for _, m := range modelsResp.Data {
		caps := model.Capability{
			Streaming:        true,
			StructuredOutput: true, // LM Studio supports response_format with JSON schema
		}
		capJSON, _ := json.Marshal(caps)

		// LM Studio model IDs use creator/model-name format
		displayName := m.ID
		ownedBy := m.OwnedBy
		if ownedBy == "" {
			ownedBy = "lmstudio"
		}

		modality := "text"
		outputMods := `["text"]`
		// The OpenAI listing has no type, so recognise embedding/reranker models
		// by name to keep them out of the chat picker.
		if mod := inferNonChatModality(m.ID); mod != "" {
			modality = mod
			if mod == "embedding" {
				outputMods = `["embedding"]`
			}
		}

		//nolint:gocritic // model variable shadows import but context makes it clear
		model := &model.Model{
			ID:               uuid.New(),
			ProviderID:       provider.ID,
			ModelID:          m.ID,
			Name:             m.ID,
			DisplayName:      displayName,
			Description:      "LM Studio local model",
			Capabilities:     string(capJSON),
			Params:           "{}",
			Modality:         modality,
			InputModalities:  `["text"]`,
			OutputModalities: outputMods,
			OwnedBy:          ownedBy,
			Enabled:          true,
		}

		models = append(models, model)
	}

	debuglog.Info("discovery: lmstudio discovered models", "models", len(models), "provider", provider.Name, "provider_id", provider.ID)
	return models, nil
}
