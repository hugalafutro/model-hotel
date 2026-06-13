package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/failover"
)

// fakeBreakerReader is a CircuitBreakerReader stub for the metrics handler test.
type fakeBreakerReader struct{ statuses []failover.ProviderStatus }

func (f fakeBreakerReader) Status() []failover.ProviderStatus { return f.statuses }

func TestMetricsAuth_DedicatedToken(t *testing.T) {
	h := &Handler{cfg: &config.Config{MetricsToken: "s3cret"}}
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("metrics"))
	})
	guarded := h.metricsAuth(next)

	cases := []struct {
		name   string
		setup  func(r *http.Request)
		status int
	}{
		{"no token", func(_ *http.Request) {}, http.StatusUnauthorized},
		{"wrong bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") }, http.StatusUnauthorized},
		{"correct bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer s3cret") }, http.StatusOK},
		{"query param rejected (Bearer required)", func(r *http.Request) { r.URL.RawQuery = "token=s3cret" }, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/metrics", http.NoBody)
			tc.setup(r)
			rr := httptest.NewRecorder()
			guarded.ServeHTTP(rr, r)
			if rr.Code != tc.status {
				t.Errorf("status = %d, want %d", rr.Code, tc.status)
			}
		})
	}
}

// TestMetricsAuth_FallsBackToAdmin verifies that with no METRICS_TOKEN the
// endpoint is still protected (never unauthenticated): a request with no
// credentials is rejected by the admin auth fallback.
func TestMetricsAuth_FallsBackToAdmin(t *testing.T) {
	h := &Handler{cfg: &config.Config{}} // no MetricsToken
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	guarded := h.metricsAuth(next)

	r := httptest.NewRequest("GET", "/metrics", http.NoBody) // no Authorization header
	rr := httptest.NewRecorder()
	guarded.ServeHTTP(rr, r)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401 (must never be unauthenticated)", rr.Code)
	}
}

func TestBreakerStateCode(t *testing.T) {
	cases := map[string]int{"open": 2, "half-open": 1, "closed": 0, "unknown": 0}
	for state, want := range cases {
		if got := breakerStateCode(state); got != want {
			t.Errorf("breakerStateCode(%q) = %d, want %d", state, got, want)
		}
	}
}

// TestMetricsHandler_ServesBreakerGauge exercises MetricsHandler end-to-end:
// it registers the scrape-time breaker collector from the handler's circuit
// breaker and serves the authenticated /metrics scrape, asserting the breaker
// gauge reflects the reader's state (open -> 2).
func TestMetricsHandler_ServesBreakerGauge(t *testing.T) {
	h := &Handler{
		cfg: &config.Config{MetricsToken: "tok"},
		circuitBreaker: fakeBreakerReader{statuses: []failover.ProviderStatus{
			{ProviderID: "prov-x", State: "open"},
		}},
	}
	srv := h.MetricsHandler()

	r := httptest.NewRequest("GET", "/metrics", http.NoBody)
	r.Header.Set("Authorization", "Bearer tok")
	rr := httptest.NewRecorder()
	srv.ServeHTTP(rr, r)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `modelhotel_circuit_breaker_state{provider_id="prov-x"} 2`) {
		t.Errorf("expected open breaker gauge for prov-x, got:\n%s", body)
	}
}
