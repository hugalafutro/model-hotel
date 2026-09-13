package provider

import (
	"slices"
	"strings"
)

// googleRetiredModels lists model IDs Google has shut down but still returns
// from its /models listing. Google's own deprecation page publishes shutdown
// dates as the EARLIEST possible date and it does not always prune the listing
// on time, so neither the listing nor the date can be trusted alone: the
// entries here were each confirmed to answer a real request with
// 404 "This model is no longer available".
//
// IDs are stored without the "models/" prefix, matching the internal model ID
// discovery builds.
var googleRetiredModels = loadCatalog[[]string]("google_retired.json")

// IsRetiredGoogleModel reports whether a Google model ID is known-shutdown and
// must be kept out of discovery results. Without this filter a retired model is
// still listed by Google, so it would be upserted as Enabled and offered to
// callers, then fail every request with a 404.
func IsRetiredGoogleModel(modelID string) bool {
	return slices.Contains(googleRetiredModels, strings.TrimPrefix(modelID, "models/"))
}
