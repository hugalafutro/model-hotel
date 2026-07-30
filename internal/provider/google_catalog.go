package provider

import (
	"slices"
	"strings"
)

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

// googleRetiredModels lists model IDs Google has shut down but still returns
// from its /models listing. Google's own deprecation page publishes shutdown
// dates as the EARLIEST possible date and it does not always prune the listing
// on time, so neither the listing nor the date can be trusted alone: the
// entries here were each confirmed to answer a real request with
// 404 "This model is no longer available".
//
// IDs are stored without the "models/" prefix, matching the internal model ID
// discovery builds (unlike google.json, which keys on the prefixed name).
var googleRetiredModels = loadCatalog[[]string]("google_retired.json")

// IsRetiredGoogleModel reports whether a Google model ID is known-shutdown and
// must be kept out of discovery results. Without this filter a retired model is
// still listed by Google, so it would be upserted as Enabled and offered to
// callers, then fail every request with a 404. Dropping the model's pricing
// entry alone does not prevent that: an absent pricing entry only skips price
// enrichment.
func IsRetiredGoogleModel(modelID string) bool {
	return slices.Contains(googleRetiredModels, strings.TrimPrefix(modelID, "models/"))
}
