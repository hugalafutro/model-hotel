package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/alert"
)

// RegisterAlerts mounts the alerting API routes:
//
//	GET  /alert/events — the alertable-event catalog that feeds the picker.
//	POST /alert/test   — send a test notification through the configured target.
func (h *Handler) RegisterAlerts(r chi.Router) {
	r.Route("/alert", func(r chi.Router) {
		r.Get("/events", h.GetAlertEvents)
		r.Get("/status", h.GetAlertStatus)
		r.Post("/test", h.SendAlertTest)
	})
}

// GetAlertStatus reports whether the configured apprise-api container is
// reachable, so an unset/wrong URL or a stopped container is visible in the UI
// rather than failing silently when an event later fires.
func (h *Handler) GetAlertStatus(w http.ResponseWriter, r *http.Request) {
	masterKey := ""
	if h.cfg != nil {
		masterKey = h.cfg.MasterKey
	}
	// Bound the probe so a hung apprise-api can't stall the dashboard request.
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()

	dispatcher := alert.New(alert.NewSettingsConfigProvider(h.settingsRepo, masterKey), nil)
	status, err := dispatcher.Probe(ctx)
	if err != nil {
		respondError(w, "failed to probe alert status", err, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		respondError(w, "failed to encode response", err, http.StatusInternalServerError)
	}
}

// GetAlertEvents returns the static catalog of operator-subscribable events.
// The dashboard renders its event picker from this, so a new Go-side event
// surfaces in the UI without any frontend change.
func (h *Handler) GetAlertEvents(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(alert.Catalog()); err != nil {
		respondError(w, "failed to encode alert events", err, http.StatusInternalServerError)
	}
}

// SendAlertTest fires a test notification through the live alert configuration,
// surfacing success or failure so the operator can verify the whole chain
// (Model Hotel → apprise-api → service) before relying on it.
func (h *Handler) SendAlertTest(w http.ResponseWriter, r *http.Request) {
	masterKey := ""
	if h.cfg != nil {
		masterKey = h.cfg.MasterKey
	}
	dispatcher := alert.New(alert.NewSettingsConfigProvider(h.settingsRepo, masterKey), nil)
	if err := dispatcher.TestSend(r.Context()); err != nil {
		respondError(w, "test notification failed", err, http.StatusBadGateway)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]bool{"ok": true}); err != nil {
		respondError(w, "failed to encode response", err, http.StatusInternalServerError)
	}
}
