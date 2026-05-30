package provider

// CoherePricingEntry provides pricing and max output data not available from the API.
type CoherePricingEntry struct {
	ModelID               string  `json:"model_id"`
	DisplayName           string  `json:"display_name"`
	Description           string  `json:"description"`
	MaxOutputTokens       int     `json:"max_output_tokens"`
	InputPricePerMillion  float64 `json:"input_price_per_million"`
	OutputPricePerMillion float64 `json:"output_price_per_million"`
}

var coherePricingCatalog = loadCatalog[[]CoherePricingEntry]("cohere.json")

// GetCoherePricingCatalog returns the Cohere pricing catalog.
func GetCoherePricingCatalog() []CoherePricingEntry {
	return coherePricingCatalog
}

// LookupCoherePricing finds a pricing entry by model ID.
func LookupCoherePricing(catalog []CoherePricingEntry, modelID string) *CoherePricingEntry {
	for i := range catalog {
		if catalog[i].ModelID == modelID {
			return &catalog[i]
		}
	}
	return nil
}
