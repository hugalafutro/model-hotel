package frontdesk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/alert"
	"github.com/hugalafutro/model-hotel/internal/auth"
)

// RunAlerts runs the outbound Apprise dispatcher until ctx is cancelled. Started
// as a goroutine in cmd/frontdesk; best-effort, never blocks request serving.
func (s *Server) RunAlerts(ctx context.Context) { s.alertDisp.Run(ctx) }

// alertEvents serves the Front Desk alert catalog so the UI renders its picker.
func (s *Server) alertEvents(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, fdCatalog)
}

// alertStatus reports whether the configured apprise-api is reachable. A
// reachable host is not enough: if the stored target cannot be decrypted (master
// key rotated, ciphertext corrupted) every dispatch fails silently, so that is
// surfaced as unhealthy with a reason rather than a falsely green pill.
func (s *Server) alertStatus(w http.ResponseWriter, r *http.Request) {
	st, err := s.alertDisp.Probe(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	if st.Configured {
		if set, gerr := s.store.GetSettings(r.Context()); gerr == nil {
			switch set.AlertAppriseTargets {
			case "":
				// A reachable apprise-api with no target still cannot deliver, so it
				// must not show a green pill.
				st.Healthy = false
				st.Reason = alert.ReasonNotConfigured
				st.Detail = "no notification target configured"
			default:
				if _, derr := auth.DecryptString(set.AlertAppriseTargets, s.masterKey); derr != nil {
					st.Healthy = false
					st.Reason = alert.ReasonUndecryptable
					st.Detail = "stored target cannot be decrypted (master key rotated?)"
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, st)
}

// alertProbe (POST /api/alert/probe, admin) checks an apprise-api URL the
// operator has typed but not yet saved. The setup wizard gates its first step
// on this answer. An admin can already save any URL and hit /alert/status, so
// probing an explicit one adds no capability.
func (s *Server) alertProbe(w http.ResponseWriter, r *http.Request) {
	var req struct {
		APIURL string `json:"api_url"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if strings.TrimSpace(req.APIURL) == "" {
		writeCodedError(w, http.StatusBadRequest, "missing_api_url", "api_url is required")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	writeJSON(w, http.StatusOK, s.alertDisp.ProbeURL(ctx, req.APIURL))
}

// alertTest (POST /api/alert/test, admin) sends a test notification. With no
// body it uses the saved configuration (the card's Send test). The wizard sends
// {api_url, targets} to test one destination through a URL before either is
// saved; either field may be omitted to fall back to the saved value. Failure is
// 502 with a reason code so the UI can say what to fix.
func (s *Server) alertTest(w http.ResponseWriter, r *http.Request) {
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
	var cfg alert.Config
	if req.APIURL != nil && len(req.Targets) > 0 {
		// Fully explicit: nothing is read from settings, so a corrupt stored
		// target cannot block testing a fresh one.
		cfg = alert.Config{APIBaseURL: *req.APIURL, Targets: alert.JoinTargets(req.Targets)}
	} else {
		// A store error (transient DB failure) and a decrypt failure (bad
		// ciphertext / rotated MASTER_KEY) are different failure kinds: the
		// former is a generic 500 via writeError, the latter a 502 with the
		// fixed undecryptable message, never the raw decrypt error text.
		set, gerr := s.store.GetSettings(r.Context())
		if gerr != nil {
			writeError(w, gerr)
			return
		}
		cfg.APIBaseURL = set.AlertAppriseAPIURL
		if set.AlertAppriseTargets != "" {
			plain, derr := auth.DecryptString(set.AlertAppriseTargets, s.masterKey)
			if derr != nil {
				writeCodedError(w, http.StatusBadGateway, alert.ReasonUndecryptable,
					"stored target cannot be decrypted (master key rotated?)")
				return
			}
			cfg.Targets = plain
		}
		if req.APIURL != nil {
			cfg.APIBaseURL = *req.APIURL
		}
		if len(req.Targets) > 0 {
			cfg.Targets = alert.JoinTargets(req.Targets)
		}
	}
	if err := s.alertDisp.TestSendTo(r.Context(), cfg); err != nil {
		code := alert.ReasonOf(err)
		if code == "" {
			// The only uncoded errors TestSendTo/post can return are marshal/
			// build-request failures, never a delivery outcome.
			code = "send_failed"
		}
		writeCodedError(w, http.StatusBadGateway, code, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// alertTargets (GET /api/alert/targets, admin) returns the stored destinations
// in plaintext, one Apprise URL per entry. This is the one place the decrypted
// list leaves the server; it feeds the readable destinations list in Settings.
// Admins can already write any target and trigger delivery to it, so nothing
// is revealed that an admin could not obtain, and it is what a readable list
// requires.
func (s *Server) alertTargets(w http.ResponseWriter, r *http.Request) {
	set, err := s.store.GetSettings(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	targets := []string{}
	if set.AlertAppriseTargets != "" {
		plain, derr := auth.DecryptString(set.AlertAppriseTargets, s.masterKey)
		if derr != nil {
			writeCodedError(w, http.StatusInternalServerError, alert.ReasonUndecryptable,
				"stored target cannot be decrypted (master key rotated?)")
			return
		}
		targets = alert.SplitTargets(plain)
	}
	writeJSON(w, http.StatusOK, map[string]any{"targets": targets})
}

// alertEventState is one catalog event plus whether Front Desk currently alerts
// on it. It is the wire shape for the operator-facing selection endpoints so
// Bellhop can render (and flip) the picker from a single call, without pulling
// the whole admin-only settings row.
type alertEventState struct {
	alert.EventDef
	Enabled bool `json:"enabled"`
}

// alertSelection folds the stored alert_events CSV over the catalog so every
// event carries its current on/off state, in catalog order.
func (s *Server) alertSelection(ctx context.Context) ([]alertEventState, error) {
	set, err := s.store.GetSettings(ctx)
	if err != nil {
		return nil, err
	}
	return selectionFrom(alert.ParseEnabled(set.AlertEvents)), nil
}

// selectionFrom projects an enabled-event set onto the catalog, in catalog order,
// so both the read handler and a just-applied toggle render the same shape without
// a second settings read.
func selectionFrom(enabled map[string]bool) []alertEventState {
	out := make([]alertEventState, len(fdCatalog))
	for i, def := range fdCatalog {
		out[i] = alertEventState{EventDef: def, Enabled: enabled[def.Type]}
	}
	return out
}

// getAlertSelection (GET /api/alert/selection) returns the catalog with each
// event's current alert-on state. Any paired device may read it (monitor role
// included) so Bellhop can show what Front Desk alerts on; flipping it needs the
// operator role (putAlertSelection).
func (s *Server) getAlertSelection(w http.ResponseWriter, r *http.Request) {
	sel, err := s.alertSelection(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": sel})
}

// putAlertSelection (POST /api/alert/selection, operator-only) flips a single
// event on or off and echoes back the refreshed selection. A per-event toggle
// (rather than a full-set replace) keeps a Bellhop built against an older or
// newer catalog from clobbering events it does not know about. Only the
// alert_events column is rewritten, so the encrypted Apprise target and OIDC
// secret sharing that row are never round-tripped.
func (s *Server) putAlertSelection(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type    string `json:"type"`
		Enabled bool   `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if !fdCatalogHas(req.Type) {
		writeCodedError(w, http.StatusBadRequest, "unknown_alert_event",
			fmt.Sprintf("no such alert event: %q", req.Type))
		return
	}
	// Serialize with putSettings' read-merge-write so a concurrent full-settings
	// save cannot lose this toggle (and this toggle cannot lose that save).
	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()

	set, err := s.store.GetSettings(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	enabled := alert.ParseEnabled(set.AlertEvents)
	enabled[req.Type] = req.Enabled
	if err := s.store.SetAlertEvents(r.Context(), enabledCSV(enabled)); err != nil {
		writeError(w, err)
		return
	}
	s.emit(r.Context(), Event{
		Type: "settings.changed", Severity: "info", Source: "frontdesk",
		Message: fmt.Sprintf("Alerting for %s %s by %s", req.Type, enabledWord(req.Enabled), actorFromContext(r.Context())),
	})
	writeJSON(w, http.StatusOK, map[string]any{"events": selectionFrom(enabled)})
}
