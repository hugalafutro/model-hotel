package provider

// GoogleModelPricing holds pricing data for a Google AI Studio model.
// Pricing is not available from the API and must be maintained from docs.
type GoogleModelPricing struct {
	ModelID                       string  `json:"model_id"`
	DisplayName                   string  `json:"display_name"`
	InputPricePerMillion          float64 `json:"input_price_per_million"`
	InputPricePerMillionCacheHit  float64 `json:"input_price_per_million_cache_hit,omitempty"`
	OutputPricePerMillion         float64 `json:"output_price_per_million"`
	// Whether this model has a free tier (affects keyless discovery)
	FreeTier bool `json:"free_tier"`
}

var googlePricingCatalog = []GoogleModelPricing{
	// === Gemini 3.1 family ===
	{ModelID: "models/gemini-3.1-pro-preview", DisplayName: "Gemini 3.1 Pro Preview", InputPricePerMillion: 2.00, InputPricePerMillionCacheHit: 0.20, OutputPricePerMillion: 12.00, FreeTier: false},
	{ModelID: "models/gemini-3.1-flash-lite-preview", DisplayName: "Gemini 3.1 Flash-Lite Preview", InputPricePerMillion: 0.25, InputPricePerMillionCacheHit: 0.025, OutputPricePerMillion: 1.50, FreeTier: true},
	{ModelID: "models/gemini-3.1-flash-image-preview", DisplayName: "Nano Banana 2", InputPricePerMillion: 0.50, OutputPricePerMillion: 3.00, FreeTier: false},

	// === Gemini 3 family ===
	{ModelID: "models/gemini-3-flash-preview", DisplayName: "Gemini 3 Flash Preview", InputPricePerMillion: 0.50, InputPricePerMillionCacheHit: 0.05, OutputPricePerMillion: 3.00, FreeTier: true},
	{ModelID: "models/gemini-3-pro-image-preview", DisplayName: "Nano Banana Pro", InputPricePerMillion: 2.00, OutputPricePerMillion: 12.00, FreeTier: false},
	{ModelID: "models/gemini-3.1-flash-tts-preview", DisplayName: "Gemini 3.1 Flash TTS Preview", InputPricePerMillion: 1.00, OutputPricePerMillion: 20.00, FreeTier: false},
	{ModelID: "models/gemini-3.1-flash-live-preview", DisplayName: "Gemini 3.1 Flash Live Preview", InputPricePerMillion: 0.75, OutputPricePerMillion: 4.50, FreeTier: false},

	// === Gemini 2.5 family ===
	{ModelID: "models/gemini-2.5-pro", DisplayName: "Gemini 2.5 Pro", InputPricePerMillion: 1.25, InputPricePerMillionCacheHit: 0.125, OutputPricePerMillion: 10.00, FreeTier: true},
	{ModelID: "models/gemini-2.5-flash", DisplayName: "Gemini 2.5 Flash", InputPricePerMillion: 0.30, InputPricePerMillionCacheHit: 0.03, OutputPricePerMillion: 2.50, FreeTier: true},
	{ModelID: "models/gemini-2.5-flash-lite", DisplayName: "Gemini 2.5 Flash-Lite", InputPricePerMillion: 0.10, InputPricePerMillionCacheHit: 0.01, OutputPricePerMillion: 0.40, FreeTier: true},
	{ModelID: "models/gemini-2.5-flash-image", DisplayName: "Nano Banana", InputPricePerMillion: 0.30, OutputPricePerMillion: 2.50, FreeTier: false},
	{ModelID: "models/gemini-2.5-flash-native-audio-preview-12-2025", DisplayName: "Gemini 2.5 Flash Live", InputPricePerMillion: 0.50, OutputPricePerMillion: 2.00, FreeTier: true},
	{ModelID: "models/gemini-2.5-flash-preview-tts", DisplayName: "Gemini 2.5 Flash TTS", InputPricePerMillion: 0.50, OutputPricePerMillion: 10.00, FreeTier: false},
	{ModelID: "models/gemini-2.5-pro-preview-tts", DisplayName: "Gemini 2.5 Pro TTS", InputPricePerMillion: 1.00, OutputPricePerMillion: 20.00, FreeTier: false},

	// === Deprecated ===
	{ModelID: "models/gemini-2.0-flash", DisplayName: "Gemini 2.0 Flash (deprecated)", InputPricePerMillion: 0.10, OutputPricePerMillion: 0.40, FreeTier: true},
	{ModelID: "models/gemini-2.0-flash-lite", DisplayName: "Gemini 2.0 Flash-Lite (deprecated)", InputPricePerMillion: 0.075, OutputPricePerMillion: 0.30, FreeTier: true},

	// === Specialized ===
	{ModelID: "models/gemini-2.5-computer-use-preview-10-2025", DisplayName: "Gemini 2.5 Computer Use", InputPricePerMillion: 1.25, OutputPricePerMillion: 10.00, FreeTier: false},
	{ModelID: "models/gemini-embedding-2", DisplayName: "Gemini Embedding 2", InputPricePerMillion: 0.20, FreeTier: true},
	{ModelID: "models/gemini-embedding-001", DisplayName: "Gemini Embedding 001", InputPricePerMillion: 0.15, FreeTier: true},
}

func LookupGooglePricing(catalog []GoogleModelPricing, modelID string) *GoogleModelPricing {
	for i := range catalog {
		if catalog[i].ModelID == modelID {
			return &catalog[i]
		}
	}
	return nil
}

func GetGooglePricingCatalog() []GoogleModelPricing {
	return googlePricingCatalog
}
