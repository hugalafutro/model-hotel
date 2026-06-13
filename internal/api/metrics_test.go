package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/config"
)

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
