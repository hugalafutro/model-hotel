package provider

// openaiCatalog carries the gpt-5.x specs the live /models listing omits. The
// OpenCode spec shape describes it exactly, so it decodes into that type
// rather than a second copy of it.
var openaiCatalog = loadCatalog[[]OpenCodeModelSpec]("openai.json")

// GetOpenAIModels returns the OpenAI model catalog.
func GetOpenAIModels() []OpenCodeModelSpec {
	return openaiCatalog
}
