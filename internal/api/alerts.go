package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/hugalafutro/model-hotel/internal/alert"
	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// RegisterAlerts mounts the alerting API routes:
//
//	GET  /alert/events  — the alertable-event catalog that feeds the picker.
//	GET  /alert/status  — reachability of the saved apprise-api configuration.
//	POST /alert/probe   — reachability of an apprise-api URL not yet saved.
//	POST /alert/test    — send a test notification through the saved or an
//	                       explicit configuration.
//	GET  /alert/targets — the saved destinations, decrypted for the admin UI.
func (h *Handler) RegisterAlerts(r chi.Router) {
	r.Route("/alert", func(r chi.Router) {
		r.Get("/events", h.GetAlertEvents)
		r.Get("/status", h.GetAlertStatus)
		r.Post("/probe", h.ProbeAlert)
		r.Post("/test", h.SendAlertTest)
		r.Get("/targets", h.GetAlertTargets)
	})
}

// masterKey returns the configured MASTER_KEY, or "" when there is no config at
// all. Every alert handler that touches encrypted settings routes through this,
// so the nil check lives in one place and an absent key degrades to a decrypt
// failure (reported as undecryptable) rather than a panic.
func (h *Handler) masterKey() string {
	if h.cfg != nil {
		return h.cfg.MasterKey
	}
	return ""
}

// alertDispatcher builds a Dispatcher reading live settings, used by every
// alert handler so the master-key lookup lives in one place.
func (h *Handler) alertDispatcher() *alert.Dispatcher {
	// h.alertClient is nil only for a hand-built Handler (tests), where
	// alert.New falls back to a client of its own.
	return alert.New(
		alert.NewSettingsConfigProvider(h.settingsRepo, h.masterKey()),
		h.alertClient,
	)
}

// GetAlertStatus reports whether the configured apprise-api container is
// reachable, so an unset/wrong URL or a stopped container is visible in the UI
// rather than failing silently when an event later fires.
func (h *Handler) GetAlertStatus(w http.ResponseWriter, r *http.Request) {
	// Bound the probe so a hung apprise-api can't stall the dashboard request.
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()

	status, err := h.alertDispatcher().Probe(ctx)
	if err != nil {
		respondError(w, "failed to probe alert status", err, http.StatusInternalServerError)
		return
	}
	writeJSON(w, status)
}

// ProbeAlert (POST /alert/probe) checks an apprise-api URL the operator typed
// but has not saved; the setup wizard gates its first step on it. Admin-only,
// same netguard client as every outbound call; an admin could already save
// any URL and hit /alert/status, so this adds no capability.
func (h *Handler) ProbeAlert(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIURL string `json:"api_url"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		writeCodedError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
		return
	}
	if strings.TrimSpace(req.APIURL) == "" {
		writeCodedError(w, http.StatusBadRequest, "invalid_body", "api_url is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	writeJSON(w, h.alertDispatcher().ProbeURL(ctx, req.APIURL))
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

// SendAlertTest (POST /alert/test) fires a test notification. With no body it
// uses the saved configuration (the card's Send test). The wizard sends
// {api_url, targets} to test explicit values before either is saved; either
// field may be omitted to fall back to the saved value.
func (h *Handler) SendAlertTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIURL  *string  `json:"api_url"`
		Targets []string `json:"targets"`
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
	if err != nil {
		writeCodedError(w, http.StatusBadRequest, "invalid_body", "read body")
		return
	}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &req); err != nil {
			writeCodedError(w, http.StatusBadRequest, "invalid_body", "invalid JSON body")
			return
		}
	}
	// Each half falls back to what is stored, independently: the wizard tests a
	// destination it has not saved yet through an address it has not saved
	// either, while the card's own button tests the stored pair.
	var cfg alert.Config
	cfg.APIBaseURL = h.settingsRepo.GetWithDefault(r.Context(), alert.KeyAPIBaseURL, "")
	if req.APIURL != nil {
		cfg.APIBaseURL = *req.APIURL
	}
	if len(req.Targets) > 0 {
		cfg.Targets = alert.JoinTargets(req.Targets)
	} else {
		// Only decrypt the saved target when it is actually needed, which is
		// what keeps a corrupt stored target from blocking a test of a fresh
		// one: a request carrying its own targets never reads it.
		stored := h.settingsRepo.GetWithDefault(r.Context(), alert.KeyTargets, "")
		if stored != "" {
			plain, derr := auth.DecryptString(stored, h.masterKey())
			if derr != nil {
				writeCodedError(w, http.StatusBadGateway, alert.ReasonUndecryptable,
					"stored target cannot be decrypted (master key rotated?)")
				return
			}
			cfg.Targets = plain
		}
	}
	if err := h.alertDispatcher().TestSendTo(r.Context(), cfg); err != nil {
		code := alert.ReasonOf(err)
		if code == "" {
			// The only uncoded errors TestSendTo/post can return are marshal/
			// build-request failures, never a delivery outcome.
			code = "send_failed"
		}
		debuglog.Warn("api: test notification failed", "code", code, "error", err)
		writeCodedError(w, http.StatusBadGateway, code, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// GetAlertTargets (GET /alert/targets) returns the stored destinations in
// plaintext for the admin UI's readable list; the only place the decrypted
// list leaves the server. On a normal instance nothing new is revealed: an
// admin can already write any target and trigger delivery to it.
//
// A read-only demo is the exception, and the reason this handler carries a
// guard the other two do not. DEMO_SHOW_TOKEN publishes the admin token on the
// login screen, which config.Load only permits alongside DEMO_READONLY,
// because that pairing is safe exactly while every admin surface is either
// non-mutating or non-secret. This read is the one that is neither, so it is
// refused there rather than handing a visitor the operator's bot tokens.
// readOnlyGuard cannot do it: it passes every GET through by design, so the
// dashboard stays browsable.
func (h *Handler) GetAlertTargets(w http.ResponseWriter, r *http.Request) {
	if h.cfg != nil && h.cfg.DemoReadOnly {
		respondError(w, "this is a read-only demo: stored alert destinations are hidden", nil, http.StatusForbidden)
		return
	}
	stored := h.settingsRepo.GetWithDefault(r.Context(), alert.KeyTargets, "")
	targets := []string{}
	if stored != "" {
		plain, err := auth.DecryptString(stored, h.masterKey())
		if err != nil {
			writeCodedError(w, http.StatusInternalServerError, alert.ReasonUndecryptable,
				"stored target cannot be decrypted (master key rotated?)")
			return
		}
		targets = alert.SplitTargets(plain)
	}
	writeJSON(w, map[string]any{"targets": targets})
}
