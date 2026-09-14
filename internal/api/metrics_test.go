package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/metrics"
	"github.com/hugalafutro/model-hotel/internal/quota"
)

// fakeBreakerReader is a CircuitBreakerControl stub for the metrics handler
// test. /metrics only ever reads Status; the reset methods exist because the
// handler holds one breaker for both reads and operator resets, and they panic
// so a metrics path that ever mutated breaker state would fail loudly here
// instead of silently passing.
type fakeBreakerReader struct{ statuses []failover.ProviderStatus }

func (f fakeBreakerReader) Status() []failover.ProviderStatus       { return f.statuses }
func (f fakeBreakerReader) StatusDetail() []failover.ProviderStatus { return f.statuses }

func (f fakeBreakerReader) Reset(uuid.UUID) failover.State {
	panic("metrics must never reset the circuit breaker")
}

func (f fakeBreakerReader) ResetModel(uuid.UUID, string) (failover.State, bool) {
	return failover.StateClosed, false
}
func (f fakeBreakerReader) ResetAll() (int, int) {
	panic("metrics must never reset the circuit breaker")
}

func (f fakeBreakerReader) ReleaseQuotaPins(map[uuid.UUID]struct{}) int {
	panic("metrics must never mutate quota pins")
}

func (f fakeBreakerReader) ReleaseAllQuotaPins() int {
	panic("metrics must never mutate quota pins")
}

func (f fakeBreakerReader) ApplyQuotaPins(map[uuid.UUID]time.Time, map[uuid.UUID]string) int {
	panic("metrics must never mutate quota pins")
}

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
			{ProviderID: "prov-x", ProviderName: "Provider X", State: "open"},
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
	if !strings.Contains(body, `modelhotel_circuit_breaker_state{provider="Provider X",provider_id="prov-x"} 2`) {
		t.Errorf("expected open breaker gauge for prov-x, got:\n%s", body)
	}
}

// TestMetricsAuth_LogLinesFeedTheCrowdSecParser pins the wording of the two
// refusal lines the metrics gate emits. The parser shipped in
// contrib/crowdsec/parsers/s01-parse/model-hotel-logs.yaml matches them by
// literal prefix to feed the model-hotel-admin-bf brute-force scenario, so a
// reworded line here disarms that scenario without failing anything else.
func TestMetricsAuth_LogLinesFeedTheCrowdSecParser(t *testing.T) {
	tests := []struct {
		name   string
		bearer string
		want   string
	}{
		{"no bearer", "", "auth: metrics scrape missing bearer token"},
		{"wrong bearer", "Bearer nope", "auth: metrics scrape with invalid token"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			capt := captureAPILogs(t)
			h := &Handler{cfg: &config.Config{MetricsToken: "s3cret"}}
			guarded := h.metricsAuth(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				t.Error("next must not run for a refused scrape")
			}))

			r := httptest.NewRequest("GET", "/metrics", http.NoBody)
			if tc.bearer != "" {
				r.Header.Set("Authorization", tc.bearer)
			}
			guarded.ServeHTTP(httptest.NewRecorder(), r)

			if capt.msg != tc.want {
				t.Errorf("log message = %q, want %q", capt.msg, tc.want)
			}
		})
	}
}

// TestCollectQuotaWindows_ReadsStoredSnapshots is the scrape-time read behind
// the quota gauges: a provider's latest snapshot, assessed by its type, named
// by the operator's name.
func TestCollectQuotaWindows_ReadsStoredSnapshots(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	reset := time.Now().Add(4 * time.Hour).Truncate(time.Millisecond)
	id := insertQuotaPollProvider(t, h.dbPool.Pool(), "zai-main", "https://api.z.ai", true)
	if err := h.quotaRepo.Upsert(ctx, quota.Snapshot{
		ProviderID: id, Kind: "usage", HTTPStatus: 200, Source: "poll",
		Payload:   json.RawMessage(fmt.Sprintf(`{"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"percentage":100,"nextResetTime":%d}]}}`, reset.UnixMilli())),
		FetchedAt: time.Now(),
	}); err != nil {
		t.Fatalf("seed snapshot: %v", err)
	}
	// A second provider with no snapshot contributes nothing.
	insertQuotaPollProvider(t, h.dbPool.Pool(), "zai-idle", "https://api.z.ai", true)

	got := h.collectQuotaWindows()

	if len(got) == 0 {
		t.Fatal("got no windows from a stored exhausted snapshot")
	}
	for _, w := range got {
		if w.ProviderID != id.String() || w.ProviderName != "zai-main" {
			t.Errorf("got window %+v, want it attributed to zai-main %s", w, id)
		}
		if w.Used < 1 {
			t.Errorf("window %s: got used=%v, want spent (>= 1)", w.Window, w.Used)
		}
	}
}

// TestCollectQuotaWindows_NoReposReportsNothing: a handler wired without the
// quota or provider repository (a test harness, a partial boot) must scrape
// clean rather than panic on a nil repository.
func TestCollectQuotaWindows_NoReposReportsNothing(t *testing.T) {
	if got := (&Handler{}).collectQuotaWindows(); got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

// TestCollectQuotaWindows_SkipsUnconfirmedAndForeignKinds: a row whose latest
// poll failed keeps its last good payload, a row of a kind this provider type
// no longer polls can linger after a type change, and a disabled provider's
// row only ages since the poller skips it; none belongs on a gauge that
// claims to show the current reading.
func TestCollectQuotaWindows_SkipsUnconfirmedAndForeignKinds(t *testing.T) {
	h := newTestHandler(t)
	ctx := context.Background()
	payload := json.RawMessage(fmt.Sprintf(`{"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"percentage":40,"nextResetTime":%d}]}}`, time.Now().Add(time.Hour).UnixMilli()))
	failed := insertQuotaPollProvider(t, h.dbPool.Pool(), "zai-failed", "https://api.z.ai", true)
	foreign := insertQuotaPollProvider(t, h.dbPool.Pool(), "zai-foreign", "https://api.z.ai", true)
	disabled := insertQuotaPollProvider(t, h.dbPool.Pool(), "zai-disabled", "https://api.z.ai", false)
	for _, s := range []quota.Snapshot{
		{ProviderID: failed, Kind: "usage", HTTPStatus: 200, Source: "poll", Payload: payload, FetchedAt: time.Now()},
		{ProviderID: foreign, Kind: "balance", HTTPStatus: 200, Source: "poll", Payload: payload, FetchedAt: time.Now()},
		{ProviderID: disabled, Kind: "usage", HTTPStatus: 200, Source: "poll", Payload: payload, FetchedAt: time.Now()},
	} {
		if err := h.quotaRepo.Upsert(ctx, s); err != nil {
			t.Fatalf("seed snapshot: %v", err)
		}
	}
	if err := h.quotaRepo.RecordFailure(ctx, failed, "usage", "upstream 503"); err != nil {
		t.Fatalf("record failure: %v", err)
	}

	if got := h.collectQuotaWindows(); len(got) != 0 {
		t.Errorf("got %+v, want nothing: one row is unconfirmed, one is a kind zai-coding does not poll, one belongs to a disabled provider the poller skips", got)
	}
}

// TestCollectBreakerStates_UntouchedEnabledProvidersReadClosed: the breaker
// tracks a provider only once a request has routed to it, and an untracked
// provider is served like a closed one, so the gauge must say closed for it
// rather than leave the lane blank; a disabled provider stays off the gauge.
func TestCollectBreakerStates_UntouchedEnabledProvidersReadClosed(t *testing.T) {
	h := newTestHandler(t)
	touched := insertQuotaPollProvider(t, h.dbPool.Pool(), "touched", "https://api.example.com", true)
	untouched := insertQuotaPollProvider(t, h.dbPool.Pool(), "untouched", "https://api.example.com", true)
	insertQuotaPollProvider(t, h.dbPool.Pool(), "switched-off", "https://api.example.com", false)
	h.circuitBreaker = fakeBreakerReader{statuses: []failover.ProviderStatus{
		{ProviderID: touched.String(), ProviderName: "touched", State: "open"},
	}}

	got := h.collectBreakerStates()

	byID := make(map[string]metrics.BreakerState, len(got))
	for _, s := range got {
		byID[s.ProviderID] = s
	}
	if s, ok := byID[touched.String()]; !ok || s.State != metrics.BreakerOpen {
		t.Errorf("touched: got %+v, want the tracked open state", s)
	}
	if s, ok := byID[untouched.String()]; !ok || s.State != metrics.BreakerClosed || s.ProviderName != "untouched" {
		t.Errorf("untouched: got %+v, want closed under its own name", s)
	}
	if len(got) != 2 {
		t.Errorf("got %d states, want exactly the two enabled providers: %+v", len(got), got)
	}
}
