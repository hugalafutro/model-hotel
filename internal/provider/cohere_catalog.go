package provider

// CoherePricingEntry provides pricing and max output data not available from
// the API. Every figure is optional: a row states only what the API and
// models.dev lack. A rerank model bills per search unit, so its row carries
// SearchPricePerThousand and no per-token price.
type CoherePricingEntry struct {
	ModelID                string   `json:"model_id"`
	DisplayName            string   `json:"display_name"`
	Description            string   `json:"description"`
	MaxOutputTokens        *int     `json:"max_output_tokens,omitempty"`
	InputPricePerMillion   *float64 `json:"input_price_per_million,omitempty"`
	OutputPricePerMillion  *float64 `json:"output_price_per_million,omitempty"`
	SearchPricePerThousand *float64 `json:"search_price_per_thousand,omitempty"`
}

var coherePricingCatalog = loadCatalog[[]CoherePricingEntry]("cohere.json")

// GetCoherePricingCatalog returns the Cohere pricing catalog.
func GetCoherePricingCatalog() []CoherePricingEntry {
	return coherePricingCatalog
}

// LookupCoherePricing finds a pricing entry by model ID.
func LookupCoherePricing(catalog []CoherePricingEntry, modelID string) *CoherePricingEntry {
	return lookupByModelID(catalog, modelID, func(e *CoherePricingEntry) string { return e.ModelID })
}
