package provider

// GoogleModelPricing holds pricing data for a Google AI Studio model.
// Pricing is not available from the API and must be maintained from docs.
type GoogleModelPricing struct {
	ModelID                      string  `json:"model_id"`
	DisplayName                  string  `json:"display_name"`
	InputPricePerMillion         float64 `json:"input_price_per_million"`
	InputPricePerMillionCacheHit float64 `json:"input_price_per_million_cache_hit,omitempty"`
	OutputPricePerMillion        float64 `json:"output_price_per_million"`
	// Whether this model has a free tier (affects keyless discovery)
	FreeTier bool `json:"free_tier"`
}

var googlePricingCatalog = loadCatalog[[]GoogleModelPricing]("google.json")

// LookupGooglePricing finds pricing for a model in the Google catalog.
func LookupGooglePricing(catalog []GoogleModelPricing, modelID string) *GoogleModelPricing {
	for i := range catalog {
		if catalog[i].ModelID == modelID {
			return &catalog[i]
		}
	}
	return nil
}

// GetGooglePricingCatalog returns the Google model pricing catalog.
func GetGooglePricingCatalog() []GoogleModelPricing {
	return googlePricingCatalog
}
