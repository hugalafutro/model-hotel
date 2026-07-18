package provider

// vertexExpressCandidates is the shipped list of Gemini model IDs probed
// during vertex-express discovery. Google exposes no model-listing route that
// accepts express API keys (the publishers listing is OAuth-only,
// live-verified 2026-07-18), so discovery validates each candidate with a
// cheap :countTokens call and keeps the ones the key can actually reach.
// Not-yet-eligible entries (e.g. gemini-3-pro-preview) stay in the list: they
// light up automatically once Google enables them for express mode.
var vertexExpressCandidates = loadCatalog[[]string]("vertex_express.json")

// GetVertexExpressCandidates returns the candidate model IDs.
func GetVertexExpressCandidates() []string {
	return vertexExpressCandidates
}
