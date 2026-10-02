package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/jsonfault"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// LocalAICapabilitiesResponse is LocalAI's GET /v1/models/capabilities: the
// OpenAI listing with, per model, the usecases its configuration serves, the
// modalities each side handles, and the context size the server will honour.
type LocalAICapabilitiesResponse struct {
	Object string                     `json:"object"`
	Data   []LocalAICapabilitiesModel `json:"data"`
}

// LocalAICapabilitiesModel is one entry of that listing. Capabilities carries
// LocalAI's usecase names (chat, completion, embeddings, rerank, image, tts,
// transcript, ...) plus the modifiers it adds to a chat model: vision, tools
// and thinking. The modifiers are detected from the loaded model's template,
// so a model listed before its first load may gain them on a later scan.
type LocalAICapabilitiesModel struct {
	ID               string   `json:"id"`
	Capabilities     []string `json:"capabilities"`
	InputModalities  []string `json:"input_modalities"`
	OutputModalities []string `json:"output_modalities"`
	ContextSize      int      `json:"context_size"`
}

// localAIEndpointClasses maps the LocalAI usecases Model Hotel can route to
// the class that serves them, in precedence order: LocalAI tags every model on
// its llama.cpp backend as chat as well, so a reranker reports chat and rerank
// and the dedicated endpoint has to win.
var localAIEndpointClasses = []struct{ usecase, class string }{
	{"rerank", "rerank"},
	{"embeddings", "embedding"},
	{"transcript", "stt"},
	{"tts", "tts"},
	{"image", "image"},
}

// discoverLocalAI reads LocalAI's capabilities listing and files each model
// under the class its usecases name. A LocalAI without the listing (an older
// release) falls back to the plain OpenAI listing, which carries no class and
// leaves the operator to pin everything by hand.
func (d *DiscoveryService) discoverLocalAI(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	bodyBytes, err := d.fetchURL(ctx, "GET", baseURL+"/models/capabilities", bearerHeader(apiKey))
	if err != nil {
		debuglog.Warn("discovery: localai capabilities listing failed, falling back to /v1/models",
			"provider", provider.Name, "provider_id", provider.ID, "error", err)
		return d.discoverOpenAI(ctx, provider, apiKey)
	}

	var resp LocalAICapabilitiesResponse
	if err := json.Unmarshal(bodyBytes, &resp); err != nil {
		return nil, fmt.Errorf("localai: failed to decode capabilities for provider %s: %s", provider.Name, jsonfault.Describe(err, len(bodyBytes)))
	}

	models := make([]*model.Model, 0, len(resp.Data))
	for _, m := range resp.Data {
		built := buildLocalAIModel(provider, m)
		if built == nil {
			debuglog.Info("discovery: localai model serves no endpoint Model Hotel routes, skipped",
				"model", m.ID, "capabilities", m.Capabilities, "provider", provider.Name, "provider_id", provider.ID)
			continue
		}
		models = append(models, built)
	}

	// The context size is what the running server will honour for the model,
	// so it is live: a config edit propagates on the next scan.
	markLiveMeta(models)

	debuglog.Info("discovery: localai discovered models", "models", len(models), "provider", provider.Name, "provider_id", provider.ID)
	return models, nil
}

// buildLocalAIModel files one listing entry. A model whose usecases name an
// endpoint class gets that class, stated explicitly so no name heuristic can
// move it. A chat or completion model is filed as chat with the modalities and
// modifiers the listing reports; structured output is always on: the listing
// does not name the backend, llama.cpp is the one that serves the GGUF files
// LocalAI is mostly run with and LocalAI constrains it with a grammar built
// from the schema, and the operator can unpin it for a backend that does
// not honour response_format. An
// entry with no capabilities at all is a bare model file without a config,
// which LocalAI serves with its chat defaults; it gets the chat capabilities
// but no explicit class, so the central classification can still read an
// embedding or reranker out of its name. One that names only usecases Model
// Hotel has no endpoint for (video, vad, detection, ...) is returned as nil.
func buildLocalAIModel(provider *Provider, m LocalAICapabilitiesModel) *model.Model {
	base := &model.Model{
		ID:           uuid.New(),
		ProviderID:   provider.ID,
		ModelID:      m.ID,
		Name:         m.ID,
		DisplayName:  m.ID,
		Description:  "LocalAI model",
		Capabilities: "{}",
		Params:       "{}",
		OwnedBy:      "localai",
		Enabled:      true,
	}
	if m.ContextSize > 0 {
		cl := m.ContextSize
		base.ContextLength = &cl
	}

	for _, e := range localAIEndpointClasses {
		if slices.Contains(m.Capabilities, e.usecase) {
			base.Modality = e.class
			return base
		}
	}
	if len(m.Capabilities) > 0 && !slices.Contains(m.Capabilities, "chat") && !slices.Contains(m.Capabilities, "completion") {
		return nil
	}

	input := []string{"text"}
	for _, mod := range []string{"image", "audio", "video"} {
		if slices.Contains(m.InputModalities, mod) {
			input = append(input, mod)
		}
	}
	caps := model.Capability{
		Streaming:        true,
		StructuredOutput: true,
		ToolCalling:      slices.Contains(m.Capabilities, "tools"),
		Reasoning:        slices.Contains(m.Capabilities, "thinking"),
		Vision:           slices.Contains(m.Capabilities, "vision") || slices.Contains(input, "image"),
		AudioInput:       slices.Contains(input, "audio"),
		VideoInput:       slices.Contains(input, "video"),
	}
	if caps.Vision && !slices.Contains(input, "image") {
		input = append(input, "image")
	}
	capJSON, _ := json.Marshal(caps)
	if len(m.Capabilities) > 0 {
		base.Modality = "chat"
	}
	base.Capabilities = string(capJSON)
	base.InputModalities = marshalModalityList(input)
	base.OutputModalities = marshalModalityList([]string{"text"})
	return base
}

// isLocalAICapabilitiesListing reports whether body is LocalAI's
// /v1/models/capabilities response: a data array whose every entry carries a
// capabilities member that is a string array or null (LocalAI writes null for
// a model file without a config). Nothing else serves that route, so an
// empty data array (a LocalAI with no models configured) counts too, as long
// as the body is not an error envelope.
func isLocalAICapabilitiesListing(body []byte) bool {
	var listing struct {
		Error json.RawMessage `json:"error"`
		Data  []struct {
			Capabilities json.RawMessage `json:"capabilities"`
		} `json:"data"`
	}
	if json.Unmarshal(body, &listing) != nil || len(listing.Error) > 0 || listing.Data == nil {
		return false
	}
	for _, d := range listing.Data {
		var caps []string
		if len(d.Capabilities) == 0 || json.Unmarshal(d.Capabilities, &caps) != nil {
			return false
		}
	}
	return true
}
