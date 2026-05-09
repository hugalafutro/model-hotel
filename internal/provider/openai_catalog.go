package provider

// OpenAIModelSpec defines pricing and capabilities for an OpenAI model.
type OpenAIModelSpec struct {
	ModelID                      string
	DisplayName                  string
	Description                  string
	ContextLength                int
	MaxOutputTokens              int
	Modality                     string
	InputModalities              string
	OutputModalities             string
	Streaming                    bool
	Reasoning                    bool
	ToolCalling                  bool
	StructuredOutput             bool
	Vision                       bool
	InputPricePerMillion         float64
	InputPricePerMillionCacheHit float64
	OutputPricePerMillion        float64
}

var openaiCatalog = []OpenAIModelSpec{
	// GPT-5.5 family
	{
		ModelID: "gpt-5.5", DisplayName: "GPT 5.5",
		Description:   "GPT 5.5. Price increases to $10/$45 above 272K tokens.",
		ContextLength: 272000, MaxOutputTokens: 32768,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 5.00, InputPricePerMillionCacheHit: 0.50, OutputPricePerMillion: 30.00,
	},
	{
		ModelID: "gpt-5.5-pro", DisplayName: "GPT 5.5 Pro",
		Description:   "GPT 5.5 Pro.",
		ContextLength: 272000, MaxOutputTokens: 32768,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 30.00, InputPricePerMillionCacheHit: 30.00, OutputPricePerMillion: 180.00,
	},

	// GPT-5.4 family
	{
		ModelID: "gpt-5.4", DisplayName: "GPT 5.4",
		Description:   "GPT 5.4. Price increases to $5/$22.50 above 272K tokens.",
		ContextLength: 272000, MaxOutputTokens: 32768,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 2.50, InputPricePerMillionCacheHit: 0.25, OutputPricePerMillion: 15.00,
	},
	{
		ModelID: "gpt-5.4-pro", DisplayName: "GPT 5.4 Pro",
		Description:   "GPT 5.4 Pro.",
		ContextLength: 272000, MaxOutputTokens: 32768,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 30.00, InputPricePerMillionCacheHit: 30.00, OutputPricePerMillion: 180.00,
	},
	{
		ModelID: "gpt-5.4-mini", DisplayName: "GPT 5.4 Mini",
		Description:   "GPT 5.4 Mini.",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 0.75, InputPricePerMillionCacheHit: 0.075, OutputPricePerMillion: 4.50,
	},
	{
		ModelID: "gpt-5.4-nano", DisplayName: "GPT 5.4 Nano",
		Description:   "GPT 5.4 Nano.",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 0.20, InputPricePerMillionCacheHit: 0.02, OutputPricePerMillion: 1.25,
	},

	// GPT-5.3 family
	{
		ModelID: "gpt-5.3-codex-spark", DisplayName: "GPT 5.3 Codex Spark",
		Description:   "GPT 5.3 Codex Spark.",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 1.75, InputPricePerMillionCacheHit: 0.175, OutputPricePerMillion: 14.00,
	},
	{
		ModelID: "gpt-5.3-codex", DisplayName: "GPT 5.3 Codex",
		Description:   "GPT 5.3 Codex.",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 1.75, InputPricePerMillionCacheHit: 0.175, OutputPricePerMillion: 14.00,
	},

	// GPT-5.2 family
	{
		ModelID: "gpt-5.2", DisplayName: "GPT 5.2",
		Description:   "GPT 5.2.",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 1.75, InputPricePerMillionCacheHit: 0.175, OutputPricePerMillion: 14.00,
	},

	// GPT-5.1 family
	{
		ModelID: "gpt-5.1", DisplayName: "GPT 5.1",
		Description:   "GPT 5.1.",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 1.07, InputPricePerMillionCacheHit: 0.107, OutputPricePerMillion: 8.50,
	},

	// GPT-5 family
	{
		ModelID: "gpt-5", DisplayName: "GPT 5",
		Description:   "GPT 5.",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, Reasoning: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 1.07, InputPricePerMillionCacheHit: 0.107, OutputPricePerMillion: 8.50,
	},
	{
		ModelID: "gpt-5-nano", DisplayName: "GPT 5 Nano",
		Description:   "GPT 5 Nano (free tier).",
		ContextLength: 200000, MaxOutputTokens: 16384,
		Modality: "text", InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Streaming: true, ToolCalling: true, StructuredOutput: true,
		InputPricePerMillion: 0, InputPricePerMillionCacheHit: 0, OutputPricePerMillion: 0,
	},
}

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
