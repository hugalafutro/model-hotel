package proxy

import (
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// musicEndpointPath is KoboldCpp's music route. It lives at the server's
// origin, not under /v1: BuildProviderTargetURL drops the /v1 the provider's
// base URL carries for it.
const musicEndpointPath = "/api/extra/music/generate"

// musicCandidates keeps the candidates a music request can be sent to: those
// on a KoboldCpp provider, the only type with a music route. Any other
// candidate is dropped before an attempt, so the client's caption and lyrics
// never travel to a provider that has no such endpoint. When nothing is left
// the request is answered with a 400 naming the constraint, and ok is false.
// Requests on every other endpoint pass through unchanged.
func (h *Handler) musicCandidates(w http.ResponseWriter, st *requestState, candidates []modelCandidate) ([]modelCandidate, bool) {
	if st.endpointPath != musicEndpointPath {
		return candidates, true
	}
	kept := candidates[:0:0]
	for _, c := range candidates {
		if provider.TypeOf(c.provider) == "koboldcpp" {
			kept = append(kept, c)
			continue
		}
		debuglog.Debug("proxy: music candidate skipped, provider type has no music route", "provider", c.provider.Name, "provider_type", provider.TypeOf(c.provider))
	}
	if len(kept) > 0 {
		return kept, true
	}
	const reason = "music generation is served only by KoboldCpp providers; the model resolves to none"
	h.failRequest(st.logData, http.StatusBadRequest, KindValidation, reason, 0, st.startTime, st.parseMs, st.timings, st.cacheHits, st.proxyOverhead)
	writeOpenAIError(w, reason, http.StatusBadRequest)
	return nil, false
}
