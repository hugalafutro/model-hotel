package provider

// openaiCatalog carries the gpt-5.x pro rows. Their input and output prices
// come from models.dev; the rows state only the cache-hit price, which
// models.dev does not record for the pro tiers because they get no cache
// discount. That figure equals the list input price, so it has to move when
// OpenAI moves that price: the dashboard shows it, nothing meters by it. The
// OpenCode spec shape describes the rows exactly, so they decode into that
// type rather than a second copy of it.
var openaiCatalog = loadCatalog[[]OpenCodeModelSpec]("openai.json")

// GetOpenAIModels returns the OpenAI model catalog.
func GetOpenAIModels() []OpenCodeModelSpec {
	return openaiCatalog
}
