package proxy

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/metrics"
)

// metricValue reads one series off a fresh scrape: the sample whose line starts
// with `series ` (name plus its full, alphabetically ordered label set). 0 when
// the series has never been touched. The registry is process-wide and counters
// only accumulate, so the tests below assert deltas, never absolute counts.
func metricValue(t *testing.T, series string) float64 {
	t.Helper()
	rec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", http.NoBody))
	body, _ := io.ReadAll(rec.Body)
	for _, line := range strings.Split(string(body), "\n") {
		if rest, ok := strings.CutPrefix(line, series+" "); ok {
			v, err := strconv.ParseFloat(strings.TrimSpace(rest), 64)
			if err != nil {
				t.Fatalf("unparsable sample %q: %v", line, err)
			}
			return v
		}
	}
	return 0
}

// Every upstream 429 is counted once, by the class the classifier assigned it.
func TestMetrics_UpstreamRateLimitCountedByClass(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(ollamaExhaustedBody))
	}))
	defer upstream.Close()
	env := newTestProxyEnvWithUpstream(t, upstream)
	series := `modelhotel_upstream_rate_limit_total{class="exhausted",model="` + env.ModelName + `",provider="` + env.ProviderName + `"}`
	before := metricValue(t, series)

	if w := chatRequest(t, env); w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body: %s", w.Code, w.Body.String())
	}
	if got := metricValue(t, series) - before; got != 1 {
		t.Errorf("exhausted 429s counted = %v, want 1", got)
	}
}

// The three exhaustion reasons, one increment each, keyed by the group's
// display model without the hotel/ prefix.
func TestMetrics_FailoverExhaustedByReason(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Upstream.Close()
	h := env.Handler
	group := "g-" + strconv.FormatInt(time.Now().UnixNano(), 36)

	newState := func(last reqError) (*requestState, *http.Request) {
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader("{}"))
		logData, _ := h.newPendingRequestLog(req, endpointTypeChat, "hotel/"+group, false)
		st := &requestState{startTime: time.Now(), reqModel: "hotel/" + group, isFailover: true, logData: logData}
		st.setReqErr(last)
		return st, req
	}
	series := func(reason string) string {
		return `modelhotel_failover_exhausted_total{group="` + group + `",reason="` + reason + `"}`
	}

	st, _ := newState(rateLimitReqErr(rateLimitVerdict{class: rateLimitSaturated, retryAfter: time.Second}, 1, "busy"))
	st.rateLimit = rateLimitVerdict{class: rateLimitSaturated, retryAfter: time.Second}
	h.failAllExhausted(httptest.NewRecorder(), st, 2)
	if got := metricValue(t, series("all_busy")); got != 1 {
		t.Errorf("all_busy = %v, want 1", got)
	}

	st, _ = newState(reqError{Kind: KindProviderError, Attempt: 1, Provider: "broken", Detail: "upstream status 503"})
	h.failAllExhausted(httptest.NewRecorder(), st, 2)
	if got := metricValue(t, series("all_failed")); got != 1 {
		t.Errorf("all_failed = %v, want 1", got)
	}

	st, req := newState(reqError{})
	h.failNoAvailableProvider(httptest.NewRecorder(), req, st, group, resolveTimings{}, resolveCacheHits{}, breakerSkipSummary{skips: 1, earliestRetry: time.Now().Add(time.Minute)})
	if got := metricValue(t, series("no_available_provider")); got != 1 {
		t.Errorf("no_available_provider = %v, want 1", got)
	}

	// A single-provider request that fails is not a failover exhaustion.
	st, _ = newState(reqError{Kind: KindProviderError, Attempt: 0, Provider: "solo"})
	st.isFailover = false
	h.failAllExhausted(httptest.NewRecorder(), st, 1)
	if got := metricValue(t, series("all_failed")); got != 1 {
		t.Errorf("all_failed after a single-provider failure = %v, want still 1", got)
	}
}

// The per-provider failover counter reads the trail: every attempt after the
// first, hedged launches included, and never a breaker skip.
func TestFailoverProviders_FromTheTrail(t *testing.T) {
	l := &requestLogData{attempts: []attemptRecord{
		{Attempt: -1, Provider: "skipped"},
		{Attempt: 0, Provider: "first"},
		{Attempt: 1, Provider: "hedge", Hedged: true},
		{Attempt: 2, Provider: "third"},
	}}
	got := l.failoverProviders()
	if len(got) != 2 || got[0] != "hedge" || got[1] != "third" {
		t.Errorf("failoverProviders = %v, want [hedge third]", got)
	}
	if (&requestLogData{}).failoverProviders() != nil {
		t.Error("an empty trail names a provider")
	}
}
