package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/quota"
)

// QuotaSnapshotWire is a quota snapshot keyed by provider NAME (not UUID) so a
// receiving member maps it onto its own provider IDs, matching the name-keyed
// contract config-sync already uses.
type QuotaSnapshotWire struct {
	ProviderName string          `json:"provider_name"`
	Type         string          `json:"type"` // the provider's stored provider_type; chooses the badge
	Kind         string          `json:"kind"`
	Payload      json.RawMessage `json:"payload"`
	HTTPStatus   int             `json:"http_status"`
	FetchedAt    time.Time       `json:"fetched_at"`
	// LastError carries the sending node's failure marker so a receiving member
	// classifies the snapshot exactly as the sender would. RecordFailure keeps
	// the last good payload, http_status and fetched_at and sets only
	// last_error, so without this field a row whose latest refresh failed
	// arrives looking fresh and healthy and counts as affirmative recovery
	// evidence — releasing the quota pin on a provider whose window is still
	// spent. Omitted when empty, so an export from a node that has nothing to
	// report stays byte-identical to the pre-existing shape.
	LastError string `json:"last_error,omitempty"`
}

// QuotaFleetHandler serves and receives fleet quota snapshots. It mounts on the
// same fleet-authed router as config-sync (see ConfigSyncHandler.Register), so
// it inherits that router's fleet auth. Quota snapshots carry no key material,
// so unlike config import there is no MASTER_KEY canary.
type QuotaFleetHandler struct {
	quotaRepo    *quota.Repository
	providerRepo ProviderStore
}

// NewQuotaFleetHandler builds a QuotaFleetHandler.
func NewQuotaFleetHandler(quotaRepo *quota.Repository, providerRepo ProviderStore) *QuotaFleetHandler {
	return &QuotaFleetHandler{quotaRepo: quotaRepo, providerRepo: providerRepo}
}

// Register mounts the fleet quota routes on the given (fleet-authed) router.
func (h *QuotaFleetHandler) Register(r chi.Router) {
	r.Get("/config/quota-snapshots", h.ExportSnapshots)
	r.Post("/config/quota-snapshots", h.ReceiveSnapshots)
}

// ExportSnapshots serves this node's quota snapshots keyed by provider name so a
// consumer maps them onto its own provider IDs.
func (h *QuotaFleetHandler) ExportSnapshots(w http.ResponseWriter, r *http.Request) {
	snaps, err := h.quotaRepo.List(r.Context())
	if err != nil {
		respondError(w, "failed to list quota snapshots", err, http.StatusInternalServerError)
		return
	}
	provs, err := h.providerRepo.List(r.Context())
	if err != nil {
		respondError(w, "failed to list providers", err, http.StatusInternalServerError)
		return
	}

	idToName := make(map[uuid.UUID]string, len(provs))
	idToType := make(map[uuid.UUID]string, len(provs))
	for _, p := range provs {
		if !p.Enabled {
			// A disabled provider's snapshot is frozen (the poller skips it) and
			// its badge must disappear everywhere a disable happens, not just on
			// the dashboard: this export feeds Front Desk's /api/quota proxy, which
			// is what the FD dashboard and Bellhop render. Dropping it here is also
			// harmless for the member-to-member distribution the same wire serves:
			// members skip disabled providers in their own polls, and quota advice
			// is meaningless for a provider that is not routed. Config-sync keeps
			// enabled state fleet-wide, so the primary's view is the fleet's view.
			continue
		}
		idToName[p.ID] = p.Name
		idToType[p.ID] = provider.TypeOf(p)
	}

	wire := make([]QuotaSnapshotWire, 0, len(snaps))
	for _, s := range snaps {
		if s.HTTPStatus == 0 {
			// Failure placeholder from RecordFailure (no successful fetch yet): it
			// carries no real payload but a fresh fetched_at. Distributing it as
			// source='fleet' would suppress a member's own (potentially successful)
			// poll, so it is never worse than standalone only if we drop it here. A
			// real fetch always has a non-zero HTTP status (200/204/424, ...).
			continue
		}
		name, ok := idToName[s.ProviderID]
		if !ok {
			continue // provider deleted or disabled since the snapshot was stored
		}
		wire = append(wire, QuotaSnapshotWire{
			ProviderName: name,
			Type:         idToType[s.ProviderID],
			Kind:         s.Kind,
			Payload:      s.Payload,
			HTTPStatus:   s.HTTPStatus,
			FetchedAt:    s.FetchedAt,
			LastError:    s.LastError,
		})
	}
	writeJSON(w, map[string]any{"snapshots": wire})
}

// maxQuotaSnapshotsBody bounds a fleet quota distribution. This body is not
// client-written: Front Desk reads the primary's snapshot export and relays it
// verbatim, so the producer's own read ceiling (internal/frontdesk's
// maxMemberRespBody, 1 MiB) is what actually caps what can arrive here. Keeping
// this strictly above it means that if Front Desk ever needs a bigger ceiling
// for quota, as it already did for one config-sync endpoint, members do not
// start answering 413 to a payload Front Desk considers legal. The failure
// would be near-silent: the distributor logs a non-200 at debug level and each
// member quietly falls back to polling upstream itself.
const maxQuotaSnapshotsBody = 4 << 20

// ReceiveSnapshots stores fleet-distributed snapshots, mapping each by provider
// name onto this member's own provider IDs and writing with UpsertIfNewer so an
// older fleet write never clobbers a fresher local (e.g. manual) snapshot. A
// name with no local provider is skipped. Written with source='fleet'.
func (h *QuotaFleetHandler) ReceiveSnapshots(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Snapshots []QuotaSnapshotWire `json:"snapshots"`
	}
	if !decodeJSONLimit(w, r, maxQuotaSnapshotsBody, &in) {
		return
	}

	provs, err := h.providerRepo.List(r.Context())
	if err != nil {
		respondError(w, "failed to list providers", err, http.StatusInternalServerError)
		return
	}
	nameToID := make(map[string]uuid.UUID, len(provs))
	for _, p := range provs {
		nameToID[p.Name] = p.ID
	}

	applied, skipped := 0, 0
	for _, s := range in.Snapshots {
		pid, ok := nameToID[s.ProviderName]
		if !ok {
			skipped++ // provider not present on this member
			continue
		}
		wrote, err := h.quotaRepo.UpsertIfNewer(r.Context(), quota.Snapshot{
			ProviderID: pid,
			Kind:       s.Kind,
			Payload:    s.Payload,
			HTTPStatus: s.HTTPStatus,
			Source:     "fleet",
			FetchedAt:  s.FetchedAt,
			// Carried through verbatim: a member must reach the same verdict on
			// this row as the node that sent it. An older primary sends no field
			// at all, which lands here as empty and behaves exactly as it did
			// before the field existed.
			LastError: s.LastError,
		})
		if err != nil {
			respondError(w, "failed to store snapshot", err, http.StatusInternalServerError)
			return
		}
		if wrote {
			applied++
		} else {
			skipped++
		}
	}
	writeJSON(w, map[string]any{"applied": applied, "skipped": skipped})
}
