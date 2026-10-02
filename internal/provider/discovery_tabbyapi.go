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
// listing names the same model but carries no parameters. The parameters are
// kept raw and read leniently (wholePositive, listingString, listingBool): a
// field in a shape of its own must not cost the card its other fields.
type TabbyAPIModelCard struct {
	ID         string `json:"id"`
	Parameters *struct {
		MaxSeqLen             json.RawMessage `json:"max_seq_len"`
		UseVision             json.RawMessage `json:"use_vision"`
		PromptTemplateContent json.RawMessage `json:"prompt_template_content"`
	} `json:"parameters"`
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
// while its container is empty, which is not a fault.
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
// single GET (probeLocal, not the retrying fetchURL: the route's 503 for an
// empty container is its normal answer, not a transient to retry). That 503,
// and an older TabbyAPI's 404 for a route it lacks, are no card, not a fault.
// Any other status or a transport fault fails the scan: a scan that went on
// would file the loaded model as streaming only and overwrite the
// capabilities the last good scan stored. A 200 whose body is not a model
// card (a proxy or another server answering with its own) is logged and
// counts as no card.
func (d *DiscoveryService) fetchTabbyAPICard(ctx context.Context, provider *Provider, url, apiKey, which string) (*TabbyAPIModelCard, error) {
	bodyBytes, status, err := d.probeLocal(ctx, url, apiKey)
	switch {
	case err != nil:
		return nil, fmt.Errorf("tabbyapi: failed to fetch the %s model card for provider %s: %w", which, provider.Name, err)
	case status == http.StatusServiceUnavailable || status == http.StatusNotFound:
		return nil, nil
	case status != http.StatusOK:
		return nil, fmt.Errorf("tabbyapi: failed to fetch the %s model card for provider %s: HTTP %d", which, provider.Name, status)
	}
	var card TabbyAPIModelCard
	if err := json.Unmarshal(bodyBytes, &card); err != nil {
		debuglog.Warn("discovery: tabbyapi "+which+" model card unreadable, skipped",
			"provider", provider.Name, "provider_id", provider.ID, "error", jsonfault.Describe(err, len(bodyBytes)))
		return nil, nil
	}
	if card.ID == "" {
		debuglog.Warn("discovery: tabbyapi "+which+" model card is not TabbyAPI's, skipped",
			"provider", provider.Name, "provider_id", provider.ID, "bytes", len(bodyBytes))
		return nil, nil
	}
	return &card, nil
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
	caps := model.Capability{Streaming: true}
	if card != nil && card.Parameters != nil {
		p := card.Parameters
		template := listingString(p.PromptTemplateContent)
		caps.StructuredOutput = true
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
