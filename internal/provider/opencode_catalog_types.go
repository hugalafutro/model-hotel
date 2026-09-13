package provider

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// OpenCodeModelSpec describes a model's capabilities and pricing. It is the
// shape every embedded per-model catalog uses: OpenCode Go, xAI and OpenAI.
//
// The price fields are overrides: a row leaves them absent for any model whose
// price models.dev already carries, and the converter then leaves the model
// unpriced for enrichment to fill. A row only ever states a price models.dev
// lacks or gets wrong, because the catalog wins over models.dev at merge time
// and a duplicated price silently pins a stale value.
type OpenCodeModelSpec struct {
	ModelID                      string   `json:"model_id"`
	DisplayName                  string   `json:"display_name"`
	Description                  string   `json:"description,omitempty"`
	ContextLength                int      `json:"context_length"`
	MaxOutputTokens              int      `json:"max_output_tokens"`
	Modality                     string   `json:"modality"`
	InputModalities              string   `json:"input_modalities"`
	OutputModalities             string   `json:"output_modalities"`
	Streaming                    bool     `json:"streaming"`
	Reasoning                    bool     `json:"reasoning"`
	ToolCalling                  bool     `json:"tool_calling"`
	StructuredOutput             bool     `json:"structured_output"`
	Vision                       bool     `json:"vision"`
	InputPricePerMillion         *float64 `json:"input_price_per_million,omitempty"`
	InputPricePerMillionCacheHit *float64 `json:"input_price_per_million_cache_hit,omitempty"`
	OutputPricePerMillion        *float64 `json:"output_price_per_million,omitempty"`
}

// copyPrice returns a fresh pointer to the same figure, or nil for nil. A model
// must own its price pointers: aliasing a catalog row's field would let a write
// through one discovered model edit the embedded catalog for every provider,
// for the life of the process.
func copyPrice(p *float64) *float64 {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// opencodeCatalogModels converts a whole OpenCode-style catalog into models,
// ready to union with a live listing via mergeLiveAndCatalog.
func opencodeCatalogModels(catalog []OpenCodeModelSpec, providerID uuid.UUID, ownedBy string) []*model.Model {
	models := make([]*model.Model, 0, len(catalog))
	for i := range catalog {
		models = append(models, OpenCodeCatalogToModel(&catalog[i], providerID, ownedBy))
	}
	return models
}

// LookupOpenCodeCatalog finds a spec by model ID in a catalog slice.
// Returns nil if not found.
func LookupOpenCodeCatalog(catalog []OpenCodeModelSpec, modelID string) *OpenCodeModelSpec {
	return lookupByModelID(catalog, modelID, func(e *OpenCodeModelSpec) string { return e.ModelID })
}

// OpenCodeCatalogToModel converts an OpenCodeModelSpec into a model.Model
// suitable for upsert into the database.
// ownedBy sets the OwnedBy field — pass the provider name (e.g. "opencode", "xai").
func OpenCodeCatalogToModel(spec *OpenCodeModelSpec, providerID uuid.UUID, ownedBy string) *model.Model {
	caps := model.Capability{
		Streaming:        spec.Streaming,
		Reasoning:        spec.Reasoning,
		ToolCalling:      spec.ToolCalling,
		StructuredOutput: spec.StructuredOutput,
		Vision:           spec.Vision,
	}
	capJSON, _ := json.Marshal(caps)

	contextLen := spec.ContextLength
	maxOutput := spec.MaxOutputTokens

	return &model.Model{
		ID:                           uuid.New(),
		ProviderID:                   providerID,
		ModelID:                      spec.ModelID,
		Name:                         spec.ModelID,
		DisplayName:                  spec.DisplayName,
		Description:                  spec.Description,
		Capabilities:                 string(capJSON),
		Params:                       "{}",
		Modality:                     spec.Modality,
		InputModalities:              spec.InputModalities,
		OutputModalities:             spec.OutputModalities,
		ContextLength:                &contextLen,
		MaxOutputTokens:              &maxOutput,
		InputPricePerMillion:         copyPrice(spec.InputPricePerMillion),
		InputPricePerMillionCacheHit: copyPrice(spec.InputPricePerMillionCacheHit),
		OutputPricePerMillion:        copyPrice(spec.OutputPricePerMillion),
		OwnedBy:                      ownedBy,
		Enabled:                      true,
	}
}
