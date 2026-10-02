package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/jsonfault"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// SGLangModelInfo is SGLang's GET /get_model_info: the one model the
// answering process serves, with what its launch flags and the model's own
// config say it can do. Nothing in the OpenAI listing carries any of this.
type SGLangModelInfo struct {
	ServedModelName       string   `json:"served_model_name"`
	IsGeneration          bool     `json:"is_generation"`
	ReasoningParser       string   `json:"reasoning_parser"`
	ToolCallParser        string   `json:"tool_call_parser"`
	HasImageUnderstanding bool     `json:"has_image_understanding"`
	HasAudioUnderstanding bool     `json:"has_audio_understanding"`
	Architectures         []string `json:"architectures"`
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
		switch err := json.Unmarshal(infoBytes, &parsed); {
		case err != nil:
			debuglog.Warn("discovery: sglang model info unreadable, listing taken as is",
				"provider", provider.Name, "provider_id", provider.ID, "error", jsonfault.Describe(err, len(infoBytes)))
		case !isSGLangModelInfo(infoBytes):
			// Decodable but not the route's shape: a proxy or another server
			// answering 200 with its own body.
			debuglog.Warn("discovery: sglang model info is not SGLang's, listing taken as is",
				"provider", provider.Name, "provider_id", provider.ID, "bytes", len(infoBytes))
		default:
			info = &parsed
		}
	}

	// An info that names no served model can only be matched to a listing of
	// one; applied to a merged listing it would describe models it never saw.
	if info != nil && info.ServedModelName == "" && len(listing.Data) != 1 {
		debuglog.Warn("discovery: sglang model info names no model, listing of several taken as is",
			"provider", provider.Name, "provider_id", provider.ID, "models", len(listing.Data))
		info = nil
	}
	models := make([]*model.Model, 0, len(listing.Data))
	for _, entry := range listing.Data {
		models = append(models, buildSGLangModel(provider, entry, info))
	}
	debuglog.Info("discovery: sglang discovered models", "models", len(models), "provider", provider.Name, "provider_id", provider.ID, "model_info", info != nil)
	return models, nil
}

// buildSGLangModel files one listed model. The info route describes the one
// model the answering process serves, so it applies to the entry of that name
// and to an adapter whose parent is that name; any other listed name (the
// SGLang router merges several workers' listings, and the info is one
// worker's) is what the listing says, as for custom. A server that does not
// generate (is_generation false) serves embeddings, or reranking when its
// architecture is a sequence classifier (the cross-encoder rerankers SGLang
// serves on /v1/rerank; a reward or classifier model is the same
// architecture and is filed the same way, which the model probe then
// disproves); a generation server is chat, with reasoning when a
// reasoning parser is configured, tool calling when a tool-call parser is,
// and image or audio input when the model understands them. Structured
// output is always on: SGLang constrains any generation model through its
// grammar backend.
func buildSGLangModel(provider *Provider, entry OpenAIModel, info *SGLangModelInfo) *model.Model {
	m := newServedModel(provider, entry.ID, "sglang", "SGLang model")
	applyListingExtras(m, entry)
	if info == nil || (info.ServedModelName != "" && entry.ID != info.ServedModelName && listingString(entry.Parent) != info.ServedModelName) {
		caps := model.Capability{Streaming: true}
		capJSON, _ := json.Marshal(caps)
		m.Capabilities = string(capJSON)
		return m
	}
	if !info.IsGeneration {
		m.Modality = "embedding"
		for _, arch := range info.Architectures {
			if strings.HasSuffix(arch, "ForSequenceClassification") {
				m.Modality = "rerank"
				break
			}
		}
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
// with {"detail": ...}) has neither. Discovery asks the same question of the
// body before trusting it, so a decoded info always carries both.
func isSGLangModelInfo(body []byte) bool {
	var info struct {
		ModelPath    *string `json:"model_path"`
		IsGeneration *bool   `json:"is_generation"`
	}
	return json.Unmarshal(body, &info) == nil && info.ModelPath != nil && *info.ModelPath != "" && info.IsGeneration != nil
}
