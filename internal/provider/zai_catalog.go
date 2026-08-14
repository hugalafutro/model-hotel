package provider

// ZAICodingModelSpec describes a ZAI Coding model specification.
//
// The price fields are OVERRIDES, not a full price list: leave them absent for
// any model whose official price models.dev's canonical "zai" entry already
// carries (a duplicated price silently pins a stale value when Z.ai changes
// theirs). Set them only for models that entry lacks, using the official
// pricing page (https://docs.z.ai/guides/overview/pricing) as the source.
type ZAICodingModelSpec struct {
	ModelID                      string   `json:"model_id"`
	ContextLength                int      `json:"context_length"`
	MaxOutputTokens              int      `json:"max_output_tokens"`
	Modality                     string   `json:"modality"`
	Reasoning                    bool     `json:"reasoning"`
	ToolCalling                  bool     `json:"tool_calling"`
	StructuredOutput             bool     `json:"structured_output"`
	InputPricePerMillion         *float64 `json:"input_price_per_million,omitempty"`
	InputPricePerMillionCacheHit *float64 `json:"input_price_per_million_cache_hit,omitempty"`
	OutputPricePerMillion        *float64 `json:"output_price_per_million,omitempty"`
}

var zaiCodingCatalog = loadCatalog[[]ZAICodingModelSpec]("zai.json")

// GetZAICodingModels returns the ZAI Coding model catalog.
func GetZAICodingModels() []ZAICodingModelSpec {
	return zaiCodingCatalog
}
