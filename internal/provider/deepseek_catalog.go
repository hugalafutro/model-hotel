package provider

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// DeepSeekModelSpec contains specification and pricing for a DeepSeek model.
type DeepSeekModelSpec struct {
	ModelID                       string  `json:"model_id"`
	ContextLength                 int     `json:"context_length"`
	MaxOutputTokens               int     `json:"max_output_tokens"`
	Reasoning                     bool    `json:"reasoning"`
	InputPricePerMillionCacheHit  float64 `json:"input_price_per_million_cache_hit,omitempty"`
	InputPricePerMillionCacheMiss float64 `json:"input_price_per_million_cache_miss"`
	OutputPricePerMillion         float64 `json:"output_price_per_million"`
}

var deepseekCatalog = loadCatalog[[]DeepSeekModelSpec]("deepseek.json")

// GetDeepSeekModels returns the full DeepSeek model catalog.
func GetDeepSeekModels() []DeepSeekModelSpec {
	return deepseekCatalog
}

// GetDeepSeekModelSpec returns the spec for a specific DeepSeek model ID.
func GetDeepSeekModelSpec(modelID string) *DeepSeekModelSpec {
	for _, spec := range deepseekCatalog {
		if spec.ModelID == modelID {
			return &spec
		}
	}
	return nil
}

// deepseekSpecToModel converts a DeepSeekModelSpec into a model.Model. The
// catalog's cache-miss price maps to the model's standard input price; cache-hit
// is carried separately.
func deepseekSpecToModel(spec *DeepSeekModelSpec, providerID uuid.UUID) *model.Model {
	caps := model.Capability{
		Streaming:   true,
		Reasoning:   spec.Reasoning,
		ToolCalling: true,
	}
	capJSON, _ := json.Marshal(caps)

	contextLen := spec.ContextLength
	maxOutput := spec.MaxOutputTokens
	inPriceCacheHit := spec.InputPricePerMillionCacheHit
	inPriceCacheMiss := spec.InputPricePerMillionCacheMiss
	outPrice := spec.OutputPricePerMillion

	return &model.Model{
		ID:                           uuid.New(),
		ProviderID:                   providerID,
		ModelID:                      spec.ModelID,
		Name:                         spec.ModelID,
		DisplayName:                  spec.ModelID,
		Capabilities:                 string(capJSON),
		Params:                       "{}",
		InputModalities:              `["text"]`,
		OutputModalities:             `["text"]`,
		ContextLength:                &contextLen,
		MaxOutputTokens:              &maxOutput,
		InputPricePerMillion:         &inPriceCacheMiss,
		InputPricePerMillionCacheHit: &inPriceCacheHit,
		OutputPricePerMillion:        &outPrice,
		OwnedBy:                      "deepseek",
		Enabled:                      true,
	}
}

// deepseekCatalogModels converts the whole DeepSeek catalog into models, ready
// to union with a live /models listing via mergeLiveAndCatalog.
func deepseekCatalogModels(providerID uuid.UUID) []*model.Model {
	specs := GetDeepSeekModels()
	models := make([]*model.Model, 0, len(specs))
	for i := range specs {
		models = append(models, deepseekSpecToModel(&specs[i], providerID))
	}
	return models
}
