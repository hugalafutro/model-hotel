package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/quota"
)

// QuotaSnapshotWire is a quota snapshot keyed by provider NAME (not UUID) so a
// receiving member maps it onto its own provider IDs, matching the name-keyed
// contract config-sync already uses.
type QuotaSnapshotWire struct {
	ProviderName string          `json:"provider_name"`
	Kind         string          `json:"kind"`
	Payload      json.RawMessage `json:"payload"`
	HTTPStatus   int             `json:"http_status"`
	FetchedAt    time.Time       `json:"fetched_at"`
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
	for _, p := range provs {
		idToName[p.ID] = p.Name
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
			continue // provider deleted since the snapshot was stored; skip it
		}
		wire = append(wire, QuotaSnapshotWire{
			ProviderName: name,
			Kind:         s.Kind,
			Payload:      s.Payload,
			HTTPStatus:   s.HTTPStatus,
			FetchedAt:    s.FetchedAt,
		})
	}
	writeJSON(w, map[string]any{"snapshots": wire})
}

// ReceiveSnapshots stores fleet-distributed snapshots, mapping each by provider
// name onto this member's own provider IDs and writing with UpsertIfNewer so an
// older fleet write never clobbers a fresher local (e.g. manual) snapshot. A
// name with no local provider is skipped. Written with source='fleet'.
func (h *QuotaFleetHandler) ReceiveSnapshots(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Snapshots []QuotaSnapshotWire `json:"snapshots"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
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
