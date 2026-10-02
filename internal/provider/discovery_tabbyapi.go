package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/jsonfault"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// TabbyAPIModelCard is TabbyAPI's GET /v1/model: the one chat model the
// process has loaded, with the parameters it was loaded with (context, cache,
// vision projector) and the chat template it renders with. The /v1/models
// listing names the same model but carries no parameters. The parameters'
// members are kept raw and read leniently (wholePositive, listingString,
// listingBool): one in a shape of its own must not cost the card its other
// fields.
type TabbyAPIModelCard struct {
	ID         string                   `json:"id"`
	Parameters *TabbyAPIModelParameters `json:"-"`
}

// TabbyAPIModelParameters is the card's parameters block.
type TabbyAPIModelParameters struct {
	MaxSeqLen             json.RawMessage `json:"max_seq_len"`
	UseVision             json.RawMessage `json:"use_vision"`
	PromptTemplateContent json.RawMessage `json:"prompt_template_content"`
}

// decodeTabbyAPICard reads a card route's 200 body: an object with an id
// string, and a parameters member that is an object, null or absent. Anything
// else is not TabbyAPI's card.
func decodeTabbyAPICard(body []byte) (*TabbyAPIModelCard, bool) {
	var raw struct {
		ID         json.RawMessage `json:"id"`
		Parameters json.RawMessage `json:"parameters"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return nil, false
	}
	card := &TabbyAPIModelCard{ID: listingString(raw.ID)}
	if card.ID == "" {
		return nil, false
	}
	if len(raw.Parameters) > 0 && string(raw.Parameters) != "null" {
		var p TabbyAPIModelParameters
		if json.Unmarshal(raw.Parameters, &p) != nil {
			return nil, false
		}
		card.Parameters = &p
	}
	return card, true
}

// isTabbyAPIDetail reports whether body is TabbyAPI's own error answer, the
// FastAPI shape {"detail": "..."}: "No models are currently loaded." on a card
// route while the container is empty, "Not Found" on a route it lacks. A
// proxy's error page is neither.
func isTabbyAPIDetail(body []byte) bool {
	var e struct {
		Detail json.RawMessage `json:"detail"`
	}
	return json.Unmarshal(body, &e) == nil && listingString(e.Detail) != ""
}

// listingBool reads a raw member as a boolean, false for any other shape.
func listingBool(raw json.RawMessage) bool {
	var b bool
	return len(raw) > 0 && json.Unmarshal(raw, &b) == nil && b
}

var (
	// A chat template that renders a tools block is one TabbyAPI calls tools
	// through: its own detection reads the template for the same thing.
	tabbyAPITemplateTools = regexp.MustCompile(`\btools\b`)
	// A template that renders a think block is one for a reasoning model,
	// whose thinking TabbyAPI parses out as reasoning_content.
	tabbyAPITemplateThink = regexp.MustCompile(`<think>`)
)

// discoverTabbyAPI reads the OpenAI listing for the names, /v1/model for what
// the loaded chat model was loaded with, and /v1/model/embedding for the
// embedding model the second container holds. A key without admin rights
// lists the loaded chat model alone; an admin key lists the whole model
// directory, the embedding model's folder among it, where nothing says what
// an unloaded entry can do. Either card route answers TabbyAPI's own 503
// while its container is empty, which is no card and not a fault.
func (d *DiscoveryService) discoverTabbyAPI(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	bodyBytes, err := d.fetchURL(ctx, "GET", baseURL+"/models", bearerHeader(apiKey))
	if err != nil {
		return nil, fmt.Errorf("tabbyapi: failed to fetch models for provider %s: %w", provider.Name, err)
	}
	var listing OpenAIModelsResponse
	if err := json.Unmarshal(bodyBytes, &listing); err != nil {
		return nil, fmt.Errorf("tabbyapi: failed to decode response for provider %s: %s", provider.Name, jsonfault.Describe(err, len(bodyBytes)))
	}

	loaded, err := d.fetchTabbyAPICard(ctx, provider, baseURL+"/model", apiKey, "chat")
	if err != nil {
		return nil, err
	}
	embedding, err := d.fetchTabbyAPICard(ctx, provider, baseURL+"/model/embedding", apiKey, "embedding")
	if err != nil {
		return nil, err
	}

	// Each card applies to the listed entry of its id. A listing that names
	// neither (a dummy-names listing, or a key that lists another directory)
	// still gets the loaded models from the cards: they are what the server
	// serves.
	models := make([]*model.Model, 0, len(listing.Data)+2)
	loadedListed, embeddingListed := false, false
	for _, entry := range listing.Data {
		if embedding != nil && entry.ID == embedding.ID {
			models = append(models, buildTabbyAPIEmbeddingModel(provider, entry.ID))
			embeddingListed = true
			continue
		}
		var card *TabbyAPIModelCard
		if loaded != nil && entry.ID == loaded.ID {
			card = loaded
			loadedListed = true
		}
		models = append(models, buildTabbyAPIModel(provider, entry, card))
	}
	if loaded != nil && !loadedListed {
		models = append(models, buildTabbyAPIModel(provider, OpenAIModel{ID: loaded.ID}, loaded))
	}
	if embedding != nil && !embeddingListed {
		models = append(models, buildTabbyAPIEmbeddingModel(provider, embedding.ID))
	}
	debuglog.Info("discovery: tabbyapi discovered models", "models", len(models), "provider", provider.Name, "provider_id", provider.ID,
		"loaded", loaded != nil && loaded.Parameters != nil, "embedding", embedding != nil)
	return models, nil
}

// fetchTabbyAPICard reads one of TabbyAPI's loaded-model routes with a
// single GET on the discovery client's own deadline (not the retrying
// fetchURL: the route's answer for an empty container is a status it would
// retry). No card, and no fault, is TabbyAPI's own detail answer: 503 (400
// before April 2025) while the container is empty, 404 from an older build
// without the route. Anything else fails the scan: another status, a 503 or
// 404 that is a proxy's page, a 200 whose body is not a card, or a transport
// fault. A scan that went on would file the loaded model as streaming only
// and overwrite the capabilities the last good scan stored.
func (d *DiscoveryService) fetchTabbyAPICard(ctx context.Context, provider *Provider, url, apiKey, which string) (*TabbyAPIModelCard, error) {
	bodyBytes, status, err := d.getOnce(ctx, url, apiKey, 0)
	if err != nil {
		return nil, fmt.Errorf("tabbyapi: failed to fetch the %s model card for provider %s: %w", which, provider.Name, err)
	}
	switch status {
	case http.StatusOK:
		card, ok := decodeTabbyAPICard(bodyBytes)
		if !ok {
			return nil, fmt.Errorf("tabbyapi: the %s model card for provider %s is not TabbyAPI's (%d bytes)", which, provider.Name, len(bodyBytes))
		}
		return card, nil
	case http.StatusBadRequest, http.StatusServiceUnavailable, http.StatusNotFound:
		if isTabbyAPIDetail(bodyBytes) {
			return nil, nil
		}
	}
	return nil, fmt.Errorf("tabbyapi: failed to fetch the %s model card for provider %s: HTTP %d", which, provider.Name, status)
}

// buildTabbyAPIModel files one listed chat model. The loaded model's card
// says what it was loaded with: image input when a vision projector is in,
// tool calling and reasoning when its chat template renders a tools block or
// a think block (what TabbyAPI itself reads the template for), and structured
// output, which TabbyAPI gives any loaded model through its grammar filter.
// Any other listed name is a model in the directory that is not loaded (or a
// configured dummy name), so it is filed as a plain chat model with
// streaming, which is what custom would say; the listing's context
// (meta.n_ctx, which TabbyAPI sets to the loaded max_seq_len) is read for
// both, with the card's max_seq_len as the fallback for the loaded one.
func buildTabbyAPIModel(provider *Provider, entry OpenAIModel, card *TabbyAPIModelCard) *model.Model {
	m := newServedModel(provider, entry.ID, "tabbyapi", "TabbyAPI model")
	m.Modality = "chat"
	m.InputModalities = marshalModalityList([]string{"text"})
	m.OutputModalities = marshalModalityList([]string{"text"})
	applyListingExtras(m, entry)
	caps := model.Capability{Streaming: true, StructuredOutput: card != nil}
	if card != nil && card.Parameters != nil {
		p := card.Parameters
		template := listingString(p.PromptTemplateContent)
		caps.Vision = listingBool(p.UseVision)
		caps.ToolCalling = tabbyAPITemplateTools.MatchString(template)
		caps.Reasoning = tabbyAPITemplateThink.MatchString(template)
		if caps.Vision {
			m.InputModalities = marshalModalityList([]string{"text", "image"})
		}
		if n := wholePositive(p.MaxSeqLen); m.ContextLength == nil && n > 0 {
			m.ContextLength = &n
			m.MarkLiveMetaFromCurrent()
		}
	}
	capJSON, _ := json.Marshal(caps)
	m.Capabilities = string(capJSON)
	return m
}

// buildTabbyAPIEmbeddingModel files the embedding model TabbyAPI's second
// container holds, served on /v1/embeddings.
func buildTabbyAPIEmbeddingModel(provider *Provider, id string) *model.Model {
	m := newServedModel(provider, id, "tabbyapi", "TabbyAPI embedding model")
	m.Modality = "embedding"
	return m
}

// isTabbyAPIServiceInfo reports whether body is TabbyAPI's
// /.well-known/serviceinfo answer: the software block naming TabbyAPI. The
// route needs no key and answers with or without a model loaded, which its
// model routes do not.
func isTabbyAPIServiceInfo(body []byte) bool {
	var info struct {
		Software struct {
			Name string `json:"name"`
		} `json:"software"`
	}
	return json.Unmarshal(body, &info) == nil && info.Software.Name == "TabbyAPI"
}
