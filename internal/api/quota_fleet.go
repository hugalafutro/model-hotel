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
