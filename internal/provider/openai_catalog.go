package provider

import (
	"encoding/json"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// OpenAIModelSpec defines pricing and capabilities for an OpenAI model.
type OpenAIModelSpec struct {
	ModelID                      string  `json:"model_id"`
	DisplayName                  string  `json:"display_name"`
	Description                  string  `json:"description"`
	ContextLength                int     `json:"context_length"`
	MaxOutputTokens              int     `json:"max_output_tokens"`
	Modality                     string  `json:"modality"`
	InputModalities              string  `json:"input_modalities"`
	OutputModalities             string  `json:"output_modalities"`
	Streaming                    bool    `json:"streaming"`
	Reasoning                    bool    `json:"reasoning"`
	ToolCalling                  bool    `json:"tool_calling"`
	StructuredOutput             bool    `json:"structured_output"`
	Vision                       bool    `json:"vision"`
	InputPricePerMillion         float64 `json:"input_price_per_million"`
	InputPricePerMillionCacheHit float64 `json:"input_price_per_million_cache_hit,omitempty"`
	OutputPricePerMillion        float64 `json:"output_price_per_million"`
}

var openaiCatalog = loadCatalog[[]OpenAIModelSpec]("openai.json")

// GetOpenAIModels returns the OpenAI model catalog.
func GetOpenAIModels() []OpenAIModelSpec {
	return openaiCatalog
}

// LookupOpenAICatalog finds a model spec in the OpenAI catalog.
func LookupOpenAICatalog(catalog []OpenAIModelSpec, modelID string) *OpenAIModelSpec {
	for i := range catalog {
		if catalog[i].ModelID == modelID {
			return &catalog[i]
		}
	}
	return nil
}

// openaiSpecToModel converts an OpenAIModelSpec into a model.Model.
func openaiSpecToModel(spec *OpenAIModelSpec, providerID uuid.UUID) *model.Model {
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
	inPrice := spec.InputPricePerMillion
	outPrice := spec.OutputPricePerMillion

	m := &model.Model{
		ID:                    uuid.New(),
		ProviderID:            providerID,
		ModelID:               spec.ModelID,
		Name:                  spec.ModelID,
		DisplayName:           spec.DisplayName,
		Description:           spec.Description,
		Capabilities:          string(capJSON),
		Params:                "{}",
		Modality:              spec.Modality,
		InputModalities:       spec.InputModalities,
		OutputModalities:      spec.OutputModalities,
		ContextLength:         &contextLen,
		MaxOutputTokens:       &maxOutput,
		InputPricePerMillion:  &inPrice,
		OutputPricePerMillion: &outPrice,
		OwnedBy:               "openai",
		Enabled:               true,
	}
	if spec.InputPricePerMillionCacheHit > 0 {
		cacheHitPrice := spec.InputPricePerMillionCacheHit
		m.InputPricePerMillionCacheHit = &cacheHitPrice
	}
	return m
}

// openaiCatalogModels converts the whole OpenAI catalog into models, ready to
// union with a live /models listing via mergeLiveAndCatalog.
func openaiCatalogModels(providerID uuid.UUID) []*model.Model {
	specs := GetOpenAIModels()
	models := make([]*model.Model, 0, len(specs))
	for i := range specs {
		models = append(models, openaiSpecToModel(&specs[i], providerID))
	}
	return models
}
