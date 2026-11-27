package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/jsonfault"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// SystemOneModel is one row of a System One model listing, the API of vLLM
// Semantic Router's decision models. Two servers answer it. A model runtime
// worker (vllm-srun) lists what it loaded on GET /v1/models, each row with the
// surfaces it serves, the question types it answers, the modalities it reads
// and its limits. A Router or Engine frontend lists its System One models on
// GET /v1/systemone/models, each row marked api "systemone" and carrying the
// question types and modalities its configuration knows. Everything but the id
// is kept raw and read leniently (listingStrings, listingString,
// wholePositive): a member in a shape of its own must not cost the listing its
// other rows.
type SystemOneModel struct {
	ID            string          `json:"id"`
	API           json.RawMessage `json:"api"`
	Surfaces      json.RawMessage `json:"surfaces"`
	QuestionTypes json.RawMessage `json:"question_types"`
	Modalities    json.RawMessage `json:"modalities"`
	Limits        json.RawMessage `json:"limits"`
}

// SystemOneListing is the listing either server answers with: the OpenAI list
// envelope around System One rows, and on a worker the API contract version.
type SystemOneListing struct {
	Object     string           `json:"object"`
	APIVersion json.RawMessage  `json:"api_version"`
	Data       []SystemOneModel `json:"data"`
}

// discoverSystemOne reads a System One server's models. The frontend's native
// listing is asked first: a Router answers /v1/models with its chat models,
// which are not what this provider type serves, and an Engine frontend has no
// /v1/models at all. A worker has no native listing route (it answers 404), so
// its own /v1/models is read instead. Any other failure of the first read
// fails the scan rather than falling through to a listing of something else.
func (d *DiscoveryService) discoverSystemOne(ctx context.Context, provider *Provider, apiKey string) ([]*model.Model, error) {
	baseURL := util.SanitizeBaseURL(provider.BaseURL)
	source := "frontend"
	body, err := d.fetchURL(ctx, "GET", baseURL+"/systemone/models", bearerHeader(apiKey))
	if he := httpErrorFrom(err); he != nil && he.StatusCode == http.StatusNotFound {
		source = "worker"
		body, err = d.fetchURL(ctx, "GET", baseURL+"/models", bearerHeader(apiKey))
	}
	if err != nil {
		debuglog.Error("discovery: systemone fetch models failed", "provider", provider.Name, "provider_id", provider.ID, "source", source, "error", err)
		return nil, fmt.Errorf("systemone: failed to fetch models for provider %s: %w", provider.Name, err)
	}

	var listing SystemOneListing
	if err := json.Unmarshal(body, &listing); err != nil {
		desc := jsonfault.Describe(err, len(body))
		debuglog.Error("discovery: systemone json decode failed", "provider", provider.Name, "provider_id", provider.ID, "source", source, "error", desc)
		return nil, fmt.Errorf("systemone: failed to decode response for provider %s: %s", provider.Name, desc)
	}

	models := make([]*model.Model, 0, len(listing.Data))
	for _, entry := range listing.Data {
		if entry.ID == "" {
			continue
		}
		models = append(models, buildSystemOneModel(provider, entry))
	}
	debuglog.Info("discovery: systemone discovered models", "models", len(models), "provider", provider.Name, "provider_id", provider.ID, "source", source)
	return models, nil
}

// buildSystemOneModel files one listed model by what its row says it serves.
// A row that answers questions, serves the decisions surface or is marked as a
// System One model is a decision model: it reads a state and answers typed
// questions with probabilities, never text, so its output is "decisions" and
// the class derives from that; the question types it names go into the
// description and, as params.question_types, onto the row. A worker can also hold an embedding or a
// reranking model, named by its surfaces. A row that says none of it is left
// to the name heuristics, as custom would be.
func buildSystemOneModel(provider *Provider, entry SystemOneModel) *model.Model {
	questionTypes := listingStrings(entry.QuestionTypes)
	surfaces := listingStrings(entry.Surfaces)
	m := newServedModel(provider, entry.ID, "systemone", "")
	switch {
	case len(questionTypes) > 0 || slices.Contains(surfaces, "decisions") || listingString(entry.API) == "systemone":
		m.Description = "System One decision model"
		if len(questionTypes) > 0 {
			m.Description += " (" + strings.Join(questionTypes, ", ") + ")"
			// Kept on the row as well: the Test button asks a question of a
			// type the model answers, and reads the list from here.
			params, _ := json.Marshal(map[string]any{"question_types": questionTypes})
			m.Params = string(params)
		}
		input := listingStrings(entry.Modalities)
		if len(input) == 0 {
			input = []string{"text"}
		}
		m.InputModalities = marshalModalityList(input)
		m.OutputModalities = marshalModalityList([]string{"decisions"})
	case slices.Contains(surfaces, "embeddings"):
		m.Description = "System One embedding model"
		m.Modality = "embedding"
	case slices.Contains(surfaces, "rerank"):
		m.Description = "System One reranking model"
		m.Modality = "rerank"
	}
	var limits struct {
		MaxInputTokens json.RawMessage `json:"max_input_tokens"`
	}
	if json.Unmarshal(entry.Limits, &limits) == nil {
		if n := wholePositive(limits.MaxInputTokens); n > 0 {
			m.ContextLength = &n
			m.MarkLiveMetaFromCurrent()
		}
	}
	return m
}

// listingStrings reads a raw member as a list of strings, keeping the entries
// that are strings and nothing for any other shape.
func listingStrings(raw json.RawMessage) []string {
	var entries []json.RawMessage
	if len(raw) == 0 || json.Unmarshal(raw, &entries) != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if s := listingString(e); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// isSystemOneWorkerListing reports whether body is a model runtime worker's
// /v1/models answer: the list envelope stamped with the API contract version,
// which no OpenAI-compatible server adds. The version is returned for the
// add dialog.
func isSystemOneWorkerListing(body []byte) (string, bool) {
	var listing SystemOneListing
	if json.Unmarshal(body, &listing) != nil || listing.Object != "list" {
		return "", false
	}
	version := listingString(listing.APIVersion)
	return version, version != ""
}

// isSystemOneFrontendListing reports whether body is a Router or Engine
// frontend's /v1/systemone/models answer: a list whose rows are marked api
// "systemone". A frontend that publishes no model yet answers an empty list,
// which names nothing and so confirms nothing.
func isSystemOneFrontendListing(body []byte) bool {
	var listing SystemOneListing
	if json.Unmarshal(body, &listing) != nil || listing.Object != "list" {
		return false
	}
	return slices.ContainsFunc(listing.Data, func(m SystemOneModel) bool {
		return listingString(m.API) == "systemone"
	})
}
