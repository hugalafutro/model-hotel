package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
)

// DismissDiscoveryClaimsRequest carries the models to dismiss on one provider.
//
// Dismiss-only, deliberately: there is no un-dismiss direction. A dismissal
// self-heals, because models.Upsert clears discovery_dismissed_at on any
// sighting, so the next discovery run undoes it for any model that came back.
// That is the only reversal the feature needs, and it needs no endpoint.
//
// A traffic-retired model gets there by a different route, since Upsert
// deliberately preserves its dismissal (it is sighted on every scan, so clearing
// on a sighting would make it impossible to silence). For those, the operator
// enabling the model clears the dismissal in the same statement as the enable
// (models.SetEnabled and models.Update), so a model retired again afterwards
// raises a fresh claim. That has to be atomic rather than left to the next
// sighting: traffic reaches a re-enabled model in seconds and a scan is about an
// hour away, so a second retirement would otherwise arrive first and re-arm the
// preserve-the-dismissal rule around a stamp nothing could clear. Still no
// endpoint needed.
type DismissDiscoveryClaimsRequest struct {
	ProviderID string   `json:"provider_id"`
	ModelIDs   []string `json:"model_ids"`
}

// DismissDiscoveryClaims stamps the operator dismissal for models on one
// provider. setModelsDismissed only touches rows that are currently
// enabled=false and not manually disabled, so a suspect (still enabled) or
// healthy model cannot be pre-dismissed; those affect zero rows and fall
// through the 404 path below like any other unmatched model ID.
//
// Deliberately NOT added to httpx.IsReadOnlyExemptPost: unlike the discovery-change
// ack it sits beside, this suppresses a real discrepancy from every operator's
// view, which is a genuine state change.
func (h *Handler) DismissDiscoveryClaims(w http.ResponseWriter, r *http.Request) {
	var req DismissDiscoveryClaimsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	providerID, err := uuid.Parse(req.ProviderID)
	if err != nil {
		http.Error(w, "invalid provider ID", http.StatusBadRequest)
		return
	}
	if len(req.ModelIDs) == 0 {
		http.Error(w, "model_ids must not be empty", http.StatusBadRequest)
		return
	}

	dismissed, err := setModelsDismissed(r.Context(), h.dbPool.Pool(), providerID, req.ModelIDs)
	if err != nil {
		respondError(w, "failed to dismiss discovery claims", err, http.StatusInternalServerError)
		return
	}
	if len(dismissed) == 0 {
		http.Error(w, "no matching models", http.StatusNotFound)
		return
	}

	// `dismissed` names the models actually stamped, so a partial result is fully
	// informative: the caller marks exactly those and leaves the rest alone.
	// `updated` is kept for compatibility and is simply its length.
	writeJSON(w, map[string]any{"dismissed": dismissed, "updated": len(dismissed)})
}

// UnpinDiscoveryClaimsRequest carries the models to unpin on one provider. It is
// shaped exactly like DismissDiscoveryClaimsRequest because it is the same kind
// of operation from the modal's side: a bulk verdict on a provider's rows.
//
// Unpin-only, like dismiss, and for a matching reason: the pin direction is not
// an endpoint. A pin is armed by the operator enabling the model
// (models.SetEnabled and models.Update stamp manually_enabled_at), and cleared
// automatically by the next sighting, since a listed model needs no exemption
// from the listing. This endpoint covers the one case neither of those reaches:
// the operator changing their mind about a model the provider still does not
// list, where there is nothing to enable and no sighting coming.
type UnpinDiscoveryClaimsRequest struct {
	ProviderID string   `json:"provider_id"`
	ModelIDs   []string `json:"model_ids"`
}

// UnpinDiscoveryClaims drops the operator pin from models on one provider,
// returning them to discovery's listing-based auto-disable with a clean
// miss-streak. setModelsUnpinned only touches rows that actually carry a pin, so
// an already-unpinned model affects zero rows and falls through the 404 path
// below like any other unmatched model ID.
//
// No model cache invalidation: the pin and the miss-streak live only in the
// database. model.Model carries neither, so no cached entry can go stale on this
// write, and the dismiss endpoint beside it invalidates nothing for the same
// reason. What does change on the next scan — the model being disabled — flows
// through the same path any auto-disable does.
//
// Deliberately NOT added to httpx.IsReadOnlyExemptPost: it hands a model back to
// automatic management, which is a genuine state change.
func (h *Handler) UnpinDiscoveryClaims(w http.ResponseWriter, r *http.Request) {
	var req UnpinDiscoveryClaimsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	providerID, err := uuid.Parse(req.ProviderID)
	if err != nil {
		http.Error(w, "invalid provider ID", http.StatusBadRequest)
		return
	}
	if len(req.ModelIDs) == 0 {
		http.Error(w, "model_ids must not be empty", http.StatusBadRequest)
		return
	}

	unpinned, err := setModelsUnpinned(r.Context(), h.dbPool.Pool(), providerID, req.ModelIDs)
	if err != nil {
		respondError(w, "failed to unpin discovery claims", err, http.StatusInternalServerError)
		return
	}
	if len(unpinned) == 0 {
		http.Error(w, "no matching models", http.StatusNotFound)
		return
	}

	// `unpinned` names the rows actually cleared, so a partial result is fully
	// informative: the caller updates exactly those and leaves the rest alone.
	writeJSON(w, map[string]any{"unpinned": unpinned})
}
