package provider

// ZAICodingModelSpec describes a ZAI Coding model specification.
type ZAICodingModelSpec struct {
	ModelID          string `json:"model_id"`
	ContextLength    int    `json:"context_length"`
	MaxOutputTokens  int    `json:"max_output_tokens"`
	Modality         string `json:"modality"`
	Reasoning        bool   `json:"reasoning"`
	ToolCalling      bool   `json:"tool_calling"`
	StructuredOutput bool   `json:"structured_output"`
}

var zaiCodingCatalog = loadCatalog[[]ZAICodingModelSpec]("zai.json")

// GetZAICodingModels returns the ZAI Coding model catalog.
func GetZAICodingModels() []ZAICodingModelSpec {
	return zaiCodingCatalog
}
