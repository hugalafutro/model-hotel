package provider

// modelsDevCanonical names the models.dev provider entry that carries a Model
// Hotel provider type's own official metadata and pricing, and whether that
// entry is the ONLY models.dev source the type may use.
type modelsDevCanonical struct {
	ID string
	// Exclusive stops the lookup from falling back to the cross-provider index
	// when the canonical entry misses. Set for single-vendor provider types:
	// their API serves only their own models, so another models.dev provider's
	// data for the same bare ID is by definition secondhand (OpenCode Go lists
	// "glm-5.3" with a guessed price before Z.ai publishes one, and that guess
	// must not become the metered price on a Z.ai provider). Aggregator and
	// catch-all types stay non-exclusive: their listings genuinely span many
	// vendors, so the cross-provider index is legitimate gap coverage.
	Exclusive bool
}

// modelsDevProviderForType maps Model Hotel provider types (as returned by
// provider_type) to their canonical models.dev entry. Enrichment consults
// that entry's models first, so a reseller's price for the same bare model ID
// (models.dev lists "glm-5.2" under 26 different providers) can never shadow
// the official one.
//
// Coding-plan provider types map to the pay-per-token provider (zai-coding →
// "zai", kimi-code → "moonshotai"), not to the "-coding-plan" models.dev
// entries: those price every model at $0 (subscription), while Model Hotel
// meters the shadow cost a request would have had at list price.
//
// "ollama-cloud" is deliberately absent: models.dev's ollama-cloud entry
// carries no cost data at all (subscription shape), so mapping it would return
// canonical specs whose empty prices block the cross-provider index that is
// Ollama Cloud's only pricing source.
var modelsDevProviderForType = map[string]modelsDevCanonical{
	// Single-vendor types: canonical entry or nothing.
	"anthropic":      {ID: "anthropic", Exclusive: true},
	"deepseek":       {ID: "deepseek", Exclusive: true},
	"xai":            {ID: "xai", Exclusive: true},
	"google":         {ID: "google", Exclusive: true},
	"vertex-express": {ID: "google-vertex", Exclusive: true},
	"cohere":         {ID: "cohere", Exclusive: true},
	"minimax":        {ID: "minimax", Exclusive: true},
	"kimi-code":      {ID: "moonshotai", Exclusive: true},
	"zai-coding":     {ID: "zai", Exclusive: true},
	// Aggregators and the unknown-host catch-all ("openai"): canonical first,
	// cross-provider index as gap coverage (Bedrock/Azure host many vendors'
	// models, custom OpenAI-compatible hosts serve arbitrary ones).
	"openai":       {ID: "openai"},
	"nanogpt":      {ID: "nano-gpt"},
	"openrouter":   {ID: "openrouter"},
	"opencode-go":  {ID: "opencode-go"},
	"opencode-zen": {ID: "opencode"},
	"bedrock":      {ID: "amazon-bedrock"},
	"azure":        {ID: "azure"},
	"neuralwatt":   {ID: "neuralwatt"},
}

// modelsDevIDAliases maps a provider type's own model IDs onto the IDs its
// canonical models.dev entry uses, for the one case where they differ. Kimi
// Code lists subscription route names (k3, k3-256k, kimi-for-coding-highspeed)
// while models.dev's "moonshotai" entry prices the underlying models by their
// API names, so without the alias every Kimi Code model metered at zero.
// kimi-for-coding is deliberately absent: Kimi documents it as the K2.8
// preview, which models.dev does not list, so it stays unpriced rather than
// being charged as a different model.
var modelsDevIDAliases = map[string]map[string]string{
	"kimi-code": {
		"k3":                        "kimi-k3",
		"k3-256k":                   "kimi-k3",
		"kimi-for-coding-highspeed": "kimi-k2.7-code-highspeed",
	},
}

// canonicalModelsDevID returns the models.dev ID to look a model up by on its
// canonical provider entry: the alias when the type declares one, the model's
// own ID otherwise.
func canonicalModelsDevID(providerType, modelID string) string {
	if alias, ok := modelsDevIDAliases[providerType][modelID]; ok {
		return alias
	}
	return modelID
}
