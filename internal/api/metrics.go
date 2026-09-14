package api

import (
	"context"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/adminauth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/metrics"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/quota"
)

// MetricsHandler returns the authenticated Prometheus /metrics handler and
// registers the live circuit-breaker-state collector. Authentication: when
// METRICS_TOKEN is set it must match, presented as an Authorization: Bearer
// header; otherwise the admin token / passkey session is required. The endpoint
// is never served unauthenticated.
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
					ProviderID:   s.ProviderID,
					ProviderName: s.ProviderName,
					State:        breakerStateCode(s.State),
				})
			}
			return out
		})
	}
	metrics.RegisterQuotaCollector(h.collectQuotaWindows)
	return h.metricsAuth(metrics.Handler())
}

// quotaScrapeTimeout bounds the two reads a scrape makes for the quota gauges.
// A scrape that waits on a slow database would hold Prometheus past its own
// deadline and fail the whole page, breaker gauge included, so the quota
// series drop out for that scrape instead.
const quotaScrapeTimeout = 3 * time.Second

// collectQuotaWindows reads every provider's latest stored quota snapshot and
// turns it into the windows the quota gauges report. It runs at scrape time so
// the gauges follow the poller's last write without a second cache to keep in
// step; both tables are small. A read failure reports nothing for this scrape.
func (h *Handler) collectQuotaWindows() []metrics.QuotaWindow {
	if h.quotaRepo == nil || h.providerRepo == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), quotaScrapeTimeout)
	defer cancel()
	providers, err := h.providerRepo.List(ctx)
	if err != nil {
		debuglog.Warn("metrics: quota gauges skipped, provider list failed", "error", err)
		return nil
	}
	snaps, err := h.quotaRepo.List(ctx)
	if err != nil {
		debuglog.Warn("metrics: quota gauges skipped, snapshot list failed", "error", err)
		return nil
	}
	byID := make(map[uuid.UUID]*provider.Provider, len(providers))
	for _, p := range providers {
		byID[p.ID] = p
	}
	var out []metrics.QuotaWindow
	// One series per provider and window: the poller keeps one row per provider
	// and kind, and every supported type polls one kind, but a duplicate label
	// set would fail the whole scrape, so the guard is cheap insurance.
	seen := make(map[string]struct{})
	for _, s := range snaps {
		p, ok := byID[s.ProviderID]
		if !ok {
			continue
		}
		for _, w := range quota.Windows(provider.TypeOf(p), s) {
			key := s.ProviderID.String() + "\x00" + w.Name
			if _, dup := seen[key]; dup {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, metrics.QuotaWindow{
				ProviderID:   s.ProviderID.String(),
				ProviderName: p.Name,
				Window:       w.Name,
				Used:         w.Used,
				ResetsAt:     w.ResetsAt,
			})
		}
	}
	return out
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
			adminauth.BearerTokenGate(h.cfg.MetricsToken, "metrics", "auth: metrics scrape", next).ServeHTTP(w, r)
			return
		}
		// No dedicated token configured — fall back to ADMIN auth, which is
		// AuthMiddleware plus requireAdmin. AuthMiddleware alone only proves the
		// caller is authenticated: it admits any resolved identity, including a
		// non-admin multi-user session. The exported counters carry provider and
		// model labels but no owner label, so they are fleet-wide totals across
		// every virtual-key owner and belong to nobody in particular — the same
		// cross-tenant aggregate /api/stats scopes with an owner filter.
		h.AuthMiddleware(requireAdmin(next)).ServeHTTP(w, r)
	})
}
