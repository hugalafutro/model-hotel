package api

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

const (
	// healthPingTimeout bounds a single DB reachability probe.
	healthPingTimeout = 1 * time.Second
	// healthCacheTTL keeps the unauthenticated endpoint cheap under the combined
	// polling of a load balancer, the Front Desk control plane, and stray noise.
	healthCacheTTL = 2 * time.Second
)

// healthPinger is the subset of the database pool the health check needs.
// *pgxpool.Pool satisfies it; tests inject a fake.
type healthPinger interface {
	Ping(ctx context.Context) error
}

// HealthHandler answers GET /health. It reports 200 OK with body "OK" when the
// database is reachable and 503 with body "DEGRADED" when it is not, so a load
// balancer stops routing to an instance whose Postgres is down. The result is
// cached for healthCacheTTL and only one probe runs at a time.
type HealthHandler struct {
	pinger      healthPinger
	pingTimeout time.Duration
	cacheTTL    time.Duration
	now         func() time.Time

	mu        sync.Mutex
	healthy   bool
	hasResult bool
	checkedAt time.Time
}

// NewHealthHandler builds a HealthHandler probing the given pool.
func NewHealthHandler(pinger healthPinger) *HealthHandler {
	return &HealthHandler{
		pinger:      pinger,
		pingTimeout: healthPingTimeout,
		cacheTTL:    healthCacheTTL,
		now:         time.Now,
	}
}

// ServeHTTP implements http.Handler.
func (h *HealthHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	healthy := h.check(r.Context())

	w.Header().Set("Content-Type", "text/plain")
	if healthy {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
		return
	}
	w.WriteHeader(http.StatusServiceUnavailable)
	_, _ = w.Write([]byte("DEGRADED"))
}

// check returns the cached health state, refreshing it with a single bounded
// DB probe when the cache has expired. Transitions are logged once.
func (h *HealthHandler) check(ctx context.Context) bool {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.hasResult && h.now().Sub(h.checkedAt) < h.cacheTTL {
		return h.healthy
	}

	pingCtx, cancel := context.WithTimeout(ctx, h.pingTimeout)
	defer cancel()
	err := h.pinger.Ping(pingCtx)

	healthy := err == nil
	if h.hasResult && healthy != h.healthy {
		if healthy {
			debuglog.Info("api: health check recovered, database reachable")
		} else {
			debuglog.Warn("api: health check failing, database unreachable", "error", err)
		}
	} else if !h.hasResult && !healthy {
		debuglog.Warn("api: health check failing, database unreachable", "error", err)
	}

	h.healthy = healthy
	h.hasResult = true
	h.checkedAt = h.now()
	return healthy
}
