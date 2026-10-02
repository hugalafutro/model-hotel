package provider

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/jsonfault"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// SGLangModelInfo is SGLang's GET /get_model_info: the one model a server
// process serves, with what its launch flags and the model's own config say it
// can do. Nothing in the OpenAI listing carries any of this.
type SGLangModelInfo struct {
	ModelPath             string `json:"model_path"`
	ServedModelName       string `json:"served_model_name"`
	IsGeneration          *bool  `json:"is_generation"`
	ReasoningParser       string `json:"reasoning_parser"`
	ToolCallParser        string `json:"tool_call_parser"`
	HasImageUnderstanding bool   `json:"has_image_understanding"`
	HasAudioUnderstanding bool   `json:"has_audio_understanding"`
	ModelType             string `json:"model_type"`
}

// discoverSGLang reads the OpenAI listing for the served names and the
// context length, and /get_model_info for the class and the capabilities.
// A server whose info route does not answer (an older SGLang, a proxy in
// front of it) keeps the listing alone, which is what custom would read.
func (d *DiscoveryService) discoverSGLang(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	bodyBytes, err := d.fetchURL(ctx, "GET", baseURL+"/models", bearerHeader(apiKey))
	if err != nil {
		return nil, fmt.Errorf("sglang: failed to fetch models for provider %s: %w", provider.Name, err)
	}
	var listing OpenAIModelsResponse
	if err := json.Unmarshal(bodyBytes, &listing); err != nil {
		return nil, fmt.Errorf("sglang: failed to decode response for provider %s: %s", provider.Name, jsonfault.Describe(err, len(bodyBytes)))
	}

	var info *SGLangModelInfo
	if infoBytes, err := d.fetchURL(ctx, "GET", localServerOrigin(baseURL)+"/get_model_info", bearerHeader(apiKey)); err != nil {
		debuglog.Warn("discovery: sglang model info unavailable, listing taken as is",
			"provider", provider.Name, "provider_id", provider.ID, "error", err)
	} else {
		var parsed SGLangModelInfo
		if err := json.Unmarshal(infoBytes, &parsed); err != nil || !isSGLangModelInfo(infoBytes) {
			debuglog.Warn("discovery: sglang model info unreadable, listing taken as is",
				"provider", provider.Name, "provider_id", provider.ID, "error", jsonfault.Describe(err, len(infoBytes)))
		} else {
			info = &parsed
		}
	}

	models := make([]*model.Model, 0, len(listing.Data))
	for _, entry := range listing.Data {
		models = append(models, buildSGLangModel(provider, entry, info))
	}
	debuglog.Info("discovery: sglang discovered models", "models", len(models), "provider", provider.Name, "provider_id", provider.ID, "model_info", info != nil)
	return models, nil
}

// buildSGLangModel files one listed model. The info route describes the one
// model the process serves; every listed name is that model (SGLang lists the
// served name, and adapters of it), so the info applies to each. An embedding
// server (is_generation false) states the embedding class; a generation
// server is chat, with reasoning when a reasoning parser is configured, tool
// calling when a tool-call parser is, and image or audio input when the model
// understands them. Structured output is always on: SGLang constrains any
// generation model through its grammar backend. Without the info the model is
// what the listing says, as for custom.
func buildSGLangModel(provider *Provider, entry OpenAIModel, info *SGLangModelInfo) *model.Model {
	m := &model.Model{
		ID:           uuid.New(),
		ProviderID:   provider.ID,
		ModelID:      entry.ID,
		Name:         entry.ID,
		DisplayName:  entry.ID,
		Description:  "SGLang model",
		Capabilities: "{}",
		Params:       "{}",
		OwnedBy:      "sglang",
		Enabled:      true,
	}
	applyListingExtras(m, entry)
	if info == nil {
		caps := model.Capability{Streaming: true}
		capJSON, _ := json.Marshal(caps)
		m.Capabilities = string(capJSON)
		return m
	}
	if info.IsGeneration != nil && !*info.IsGeneration {
		m.Modality = "embedding"
		return m
	}
	input := []string{"text"}
	if info.HasImageUnderstanding {
		input = append(input, "image")
	}
	if info.HasAudioUnderstanding {
		input = append(input, "audio")
	}
	caps := model.Capability{
		Streaming:        true,
		StructuredOutput: true,
		Reasoning:        info.ReasoningParser != "",
		ToolCalling:      info.ToolCallParser != "",
		Vision:           info.HasImageUnderstanding,
		AudioInput:       info.HasAudioUnderstanding,
	}
	capJSON, _ := json.Marshal(caps)
	m.Modality = "chat"
	m.Capabilities = string(capJSON)
	m.InputModalities = marshalModalityList(input)
	m.OutputModalities = marshalModalityList([]string{"text"})
	return m
}

// isSGLangModelInfo reports whether body is SGLang's /get_model_info answer:
// a model_path string beside an is_generation boolean, which no other server
// pairs on that route. An error envelope (SGLang answers an unknown route
// with {"detail": ...}) has neither.
func isSGLangModelInfo(body []byte) bool {
	var info struct {
		ModelPath    *string `json:"model_path"`
		IsGeneration *bool   `json:"is_generation"`
	}
	return json.Unmarshal(body, &info) == nil && info.ModelPath != nil && *info.ModelPath != "" && info.IsGeneration != nil
}
