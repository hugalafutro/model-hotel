package frontdesk

import (
	"encoding/json"
	"net/http"
)

// quotaWire is one provider's quota snapshot as proxied to Bellhop. It mirrors
// the member export (internal/api QuotaSnapshotWire) but is defined locally so the
// control-plane package does not depend on the data-plane api package.
type quotaWire struct {
	ProviderName string          `json:"provider_name"`
	Type         string          `json:"type"`
	Kind         string          `json:"kind"`
	Payload      json.RawMessage `json:"payload"`
	HTTPStatus   int             `json:"http_status"`
	FetchedAt    string          `json:"fetched_at"`
}

// handleQuota proxies the designated primary member's quota snapshot export to a
// paired device (monitor tier). It mirrors DistributeQuotaOnce's read side: the
// primary is the source of truth, Front Desk keeps no copy. No primary designated
// (standalone / not set up) or an unreachable primary yields an empty set, and the
// device shows its last-good (stale-beats-blank on the Android side).
func (s *Server) handleQuota(w http.ResponseWriter, r *http.Request) {
	empty := map[string]any{"quota": []quotaWire{}}

	cfg, err := s.store.GetAutoSync(r.Context())
	if err != nil || cfg.PrimaryID == "" {
		writeJSON(w, http.StatusOK, empty)
		return
	}
	primary, token, err := s.memberTokenOrErr(r.Context(), cfg.PrimaryID)
	if err != nil {
		writeJSON(w, http.StatusOK, empty)
		return
	}
	status, body, err := s.callMember(r.Context(), http.MethodGet, primary.URL, memberQuotaSnapshotsPath, token, nil)
	if err != nil || status != http.StatusOK {
		writeJSON(w, http.StatusOK, empty)
		return
	}
	var parsed struct {
		Snapshots []quotaWire `json:"snapshots"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		writeJSON(w, http.StatusOK, empty)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"quota": parsed.Snapshots})
}

// memberRefreshQuotasPath is the primary member's manual quota-refresh route
// (internal/api Handler.RefreshAllQuotas), which re-polls quota-capable
// providers' upstream endpoints synchronously.
const memberRefreshQuotasPath = "/api/providers/refresh-quotas"

// handleQuotaRefresh forces the designated primary member to re-poll its
// quota-capable providers' upstream endpoints (the pull-to-refresh path),
// mirroring handleQuota's read side but as a write-through proxy. It is
// monitor-tier: any paired device may trigger a refresh. No primary
// designated or an unreachable primary yields a 200 no-op.
func (s *Server) handleQuotaRefresh(w http.ResponseWriter, r *http.Request) {
	// The success path writes the member's body verbatim ({results, refreshed,
	// failed, skipped}); the no-op carries the same keys with an empty result
	// list so both paths answer this route with one shape.
	noop := map[string]any{"results": []any{}, "refreshed": 0, "failed": 0, "skipped": 0}

	cfg, err := s.store.GetAutoSync(r.Context())
	if err != nil || cfg.PrimaryID == "" {
		writeJSON(w, http.StatusOK, noop)
		return
	}
	primary, token, err := s.memberTokenOrErr(r.Context(), cfg.PrimaryID)
	if err != nil {
		writeJSON(w, http.StatusOK, noop)
		return
	}
	status, body, err := s.callMember(r.Context(), http.MethodPost, primary.URL, memberRefreshQuotasPath, token, nil)
	if err != nil || status != http.StatusOK {
		writeJSON(w, http.StatusOK, noop)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
