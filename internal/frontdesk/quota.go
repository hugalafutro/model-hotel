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

// writeQuotaUnreachable answers a quota proxy request that could not be served
// because Front Desk failed to get an answer out of a primary that does exist.
// It must never be an empty 200: Bellhop keeps its last-good badges only when a
// read fails, and an empty 200 is indistinguishable from a fleet that genuinely
// has no quota-capable providers, so degrading a transient failure into one
// silently wipes the badges off the dashboard and the home-screen widget.
func writeQuotaUnreachable(w http.ResponseWriter, msg string) {
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": msg})
}

// quotaPrimary resolves the member both quota handlers proxy to. When it returns
// ok=false it has already written the response, so the caller just returns:
// either a 200 carrying none (the caller's "nothing to report" payload) for the
// one steady state with no primary to ask -- none designated -- or an error
// status for every failure to reach a primary that is designated.
func (s *Server) quotaPrimary(w http.ResponseWriter, r *http.Request, none any) (*Member, string, bool) {
	cfg, err := s.store.GetAutoSync(r.Context())
	if err != nil {
		// Front Desk's own store is unreadable. That is our failure, not the
		// primary's, so it maps to 500 rather than 502 -- but it is still an
		// error, because answering "no quota" here would wipe the device's
		// badges over a transient local hiccup.
		writeError(w, err)
		return nil, "", false
	}
	if cfg.PrimaryID == "" {
		// Standalone / not set up yet: there is no one to ask, and there never
		// was. A steady state, so an empty payload is the truthful answer.
		writeJSON(w, http.StatusOK, none)
		return nil, "", false
	}
	primary, token, err := s.memberTokenOrErr(r.Context(), cfg.PrimaryID)
	if err != nil {
		// A designated primary we cannot use: no stored admin token
		// (ErrValidation), an undecryptable one, a store failure, or no member
		// row at all (ErrNotFound). The dangling-id case is an anomaly rather
		// than a steady state -- DeleteMemberIfNotPrimary refuses to remove the
		// designated primary, and DeleteMember clears the pointer -- so it means
		// a cleanup failed, not that the fleet stopped having a primary. Either
		// way we could not ask, which is not the same as "nothing to report".
		writeQuotaUnreachable(w, "could not reach the fleet primary")
		return nil, "", false
	}
	return primary, token, true
}

// handleQuota proxies the designated primary member's quota snapshot export to a
// paired device (monitor tier). It mirrors DistributeQuotaOnce's read side: the
// primary is the source of truth, Front Desk keeps no copy. With no primary to
// ask the answer is an empty set; a primary that cannot be reached, does not
// answer 200, or answers something undecodable is a 502, which leaves the device
// on its last-good badges (stale beats blank on the Android side).
func (s *Server) handleQuota(w http.ResponseWriter, r *http.Request) {
	empty := map[string]any{"quota": []quotaWire{}}

	primary, token, ok := s.quotaPrimary(w, r, empty)
	if !ok {
		return
	}
	status, body, err := s.callMember(r.Context(), http.MethodGet, primary.URL, memberQuotaSnapshotsPath, token, nil)
	if err != nil || status != http.StatusOK {
		writeQuotaUnreachable(w, "could not read the primary's quota snapshots")
		return
	}
	var parsed struct {
		Snapshots []quotaWire `json:"snapshots"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		// A 200 that isn't the export shape (a proxy's error page, say) means we
		// still don't know the primary's quota, so it fails like an unreachable
		// primary rather than reading as "no snapshots".
		writeQuotaUnreachable(w, "could not decode the primary's quota snapshots")
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
// monitor-tier: any paired device may trigger a refresh. It splits outcomes the
// same way: no primary to ask is a 200 no-op, a primary that could not be asked
// is a 502 so the device can say the refresh failed instead of showing a
// successful refresh that changed nothing.
func (s *Server) handleQuotaRefresh(w http.ResponseWriter, r *http.Request) {
	// The success path writes the member's body verbatim ({results, refreshed,
	// failed, skipped}); the no-op carries the same keys with an empty result
	// list so both paths answer this route with one shape.
	noop := map[string]any{"results": []any{}, "refreshed": 0, "failed": 0, "skipped": 0}

	primary, token, ok := s.quotaPrimary(w, r, noop)
	if !ok {
		return
	}
	status, body, err := s.callMember(r.Context(), http.MethodPost, primary.URL, memberRefreshQuotasPath, token, nil)
	if err != nil || status != http.StatusOK {
		writeQuotaUnreachable(w, "the primary could not refresh its quotas")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}
