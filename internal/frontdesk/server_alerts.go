package frontdesk

import (
	"context"
	"net/http"

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
				st.Detail = "no notification target configured"
			default:
				if _, derr := auth.DecryptString(set.AlertAppriseTargets, s.masterKey); derr != nil {
					st.Healthy = false
					st.Detail = "stored target cannot be decrypted (master key rotated?)"
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, st)
}

// alertTest sends a test notification to the configured target(s). A delivery or
// configuration failure is reported as 502 with the reason, so the UI can show it.
func (s *Server) alertTest(w http.ResponseWriter, r *http.Request) {
	if err := s.alertDisp.TestSend(r.Context()); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
