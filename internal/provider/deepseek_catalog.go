package provider

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// DeepSeekModelSpec contains specification and pricing for a DeepSeek model.
//
// Prices are DeepSeek's OFF-PEAK rates, which apply for 17 of every 24 hours.
// Peak hours (01:00-04:00 and 06:00-10:00 UTC) bill at exactly double, and a
// model row holds one figure, so metering under-reports during that window.
// This matches how the other catalogs store a base rate rather than a
// conditional surcharge (openai.json carries gpt-5.x-pro at its standard rate,
// not the >200K-context tier).
type DeepSeekModelSpec struct {
	ModelID string `json:"model_id"`
	// Description reaches the dashboard, and is the only place an operator can
	// find out that the listed price is the off-peak one.
	Description     string `json:"description,omitempty"`
	ContextLength   int    `json:"context_length"`
	MaxOutputTokens int    `json:"max_output_tokens"`
	Reasoning       bool   `json:"reasoning"`
	Vision          bool   `json:"vision,omitempty"`
	// InputModalities is a JSON array literal, e.g. `["text","image"]`. Empty
	// defaults to text-only, which is every DeepSeek model but the vision one.
	//
	// It overlaps with Vision rather than complementing it: NormalizeModels
	// derives "image" from the flag and the flag back from the array, so
	// setting either alone reaches the same end state. Set both, so a reader of
	// the row does not have to know that.
	InputModalities               string  `json:"input_modalities,omitempty"`
	InputPricePerMillionCacheHit  float64 `json:"input_price_per_million_cache_hit,omitempty"`
	InputPricePerMillionCacheMiss float64 `json:"input_price_per_million_cache_miss"`
	OutputPricePerMillion         float64 `json:"output_price_per_million"`
}

// deepseekCatalog is not an ordinary price-override channel. models.dev still
// carries DeepSeek's pre-V4 rates (0.14/0.28) under the V4 model IDs, and knows
// nothing at all about deepseek-v4-flash-vision-exp, so these rows are the only
// correct pricing MH has. deepseek-chat and deepseek-reasoner are absent from
// the live /models listing entirely — they are permanent aliases onto
// deepseek-v4-flash, non-thinking and thinking respectively — so the catalog is
// also the only thing surfacing them.
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
		Vision:      spec.Vision,
	}
	capJSON, _ := json.Marshal(caps)

	inputModalities := spec.InputModalities
	if inputModalities == "" {
		inputModalities = `["text"]`
	}

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
		Description:                  spec.Description,
		Capabilities:                 string(capJSON),
		Params:                       "{}",
		InputModalities:              inputModalities,
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
