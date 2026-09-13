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
// conditional surcharge.
//
// The price fields are overrides, absent from any row whose price models.dev
// already carries correctly; deepseekSpecToModel leaves an absent price unset
// for models.dev to fill.
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
	InputModalities               string   `json:"input_modalities,omitempty"`
	InputPricePerMillionCacheHit  *float64 `json:"input_price_per_million_cache_hit,omitempty"`
	InputPricePerMillionCacheMiss *float64 `json:"input_price_per_million_cache_miss,omitempty"`
	OutputPricePerMillion         *float64 `json:"output_price_per_million,omitempty"`
}

// deepseekCatalog surfaces the Flash family under every id DeepSeek still
// answers to, with the vision flag models.dev leaves off, and prices the rows
// models.dev cannot.
//
// deepseek-flash is DeepSeek V4.1 Flash and the only Flash id DeepSeek still
// documents. deepseek-chat, deepseek-reasoner, deepseek-v4-flash and
// deepseek-v4-flash-vision-exp all resolve to it upstream (verified by the id
// each one echoes back in its response), so every Flash-family row carries
// its vision flag: the API answers a request carrying an image on each of
// those ids. deepseek-chat still selects the non-thinking preset.
//
// models.dev prices deepseek-flash, deepseek-v4-flash and
// deepseek-v4-flash-vision-exp at DeepSeek's published Flash rate, so those
// rows carry no price. deepseek-chat and deepseek-reasoner are absent from
// models.dev and from the live /models listing both, so their rows are the
// only thing surfacing or pricing them, at Flash's rate.
//
// deepseek-v4-pro is the last row on its own price, and models.dev has that
// price wrong (it lists 0.435/0.87 where DeepSeek's pricing page says 0.66/1.98
// off-peak), so the row keeps it. It is also the only id that answers as
// itself rather than as deepseek-flash, and the only one that drops an image
// instead of reading it, so it carries no vision flag. Once that id stops
// echoing itself back, DeepSeek is serving Flash under it at Flash's rate and
// this row meters every request several times over, so it has to be repriced
// or dropped then. Dropping it does not retire the model on its own:
// DiscoverDeepSeek unions the catalog into the live listing, so a catalog row
// can never be recorded as missing.
var deepseekCatalog = loadCatalog[[]DeepSeekModelSpec]("deepseek.json")

// GetDeepSeekModels returns the full DeepSeek model catalog.
func GetDeepSeekModels() []DeepSeekModelSpec {
	return deepseekCatalog
}

// deepseekSpecToModel converts a DeepSeekModelSpec into a model.Model. The
// catalog's cache-miss price maps to the model's standard input price; cache-hit
// is carried separately. A row without prices yields an unpriced model for
// models.dev to fill.
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

	m := &model.Model{
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
		InputPricePerMillion:         copyPrice(spec.InputPricePerMillionCacheMiss),
		InputPricePerMillionCacheHit: copyPrice(spec.InputPricePerMillionCacheHit),
		OutputPricePerMillion:        copyPrice(spec.OutputPricePerMillion),
		OwnedBy:                      "deepseek",
		Enabled:                      true,
	}
	m.StampPriceSources(model.PriceSourceCatalog)
	return m
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
