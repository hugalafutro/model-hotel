package api

import (
	"crypto/subtle"
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/metrics"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// MetricsHandler returns the authenticated Prometheus /metrics handler and
// registers the live circuit-breaker-state collector. Authentication: when
// METRICS_TOKEN is set it must match (Bearer header or ?token=); otherwise the
// admin token / passkey session is required. The endpoint is never served
// unauthenticated.
func (h *Handler) MetricsHandler() http.Handler {
	// Register the breaker-state collector once; it reads live state at scrape
	// time so the time-based open→half-open transition is reflected.
	if h.circuitBreaker != nil {
		cb := h.circuitBreaker
		metrics.RegisterBreakerCollector(func() []metrics.BreakerState {
			statuses := cb.Status()
			out := make([]metrics.BreakerState, 0, len(statuses))
			for _, s := range statuses {
				out = append(out, metrics.BreakerState{
					ProviderID: s.ProviderID,
					State:      breakerStateCode(s.State),
				})
			}
			return out
		})
	}
	return h.metricsAuth(metrics.Handler())
}

// breakerStateCode maps the circuit breaker's state string to the gauge's
// numeric encoding (0 closed / 1 half-open / 2 open).
func breakerStateCode(state string) int {
	switch state {
	case "open":
		return metrics.BreakerOpen
	case "half-open":
		return metrics.BreakerHalfOpen
	default:
		return metrics.BreakerClosed
	}
}

// metricsAuth gates the metrics endpoint. A dedicated METRICS_TOKEN (so the
// Prometheus scrape config need not hold the admin token) takes precedence;
// without one, the standard admin auth applies. The token must be presented as
// an Authorization: Bearer header — not a query parameter — so it does not leak
// into reverse-proxy access logs, browser history, or referrers.
func (h *Handler) metricsAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h.cfg != nil && h.cfg.MetricsToken != "" {
			tok, _ := util.ParseBearerToken(r)
			if subtle.ConstantTimeCompare([]byte(tok), []byte(h.cfg.MetricsToken)) == 1 {
				next.ServeHTTP(w, r)
				return
			}
			http.Error(w, "invalid metrics token", http.StatusUnauthorized)
			return
		}
		// No dedicated token configured — fall back to admin auth.
		h.AuthMiddleware(next).ServeHTTP(w, r)
	})
}
