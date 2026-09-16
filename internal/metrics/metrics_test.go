package metrics

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// labelSeq numbers the label values below so each test invocation gets its own
// series.
//
// The registry is process-wide and counters only accumulate, so every exact-count
// assertion in this file is a claim about how many times its test has ever run.
// With fixed labels `go test -count=2` failed all of them, which matters because
// re-running under -count is how a flake is hunted here: the whole package looked
// broken the moment anyone did it.
//
// A counter rather than a random suffix, because it keeps a failed assertion
// readable and cannot collide. It is shared across tests, so the numbers a given
// test sees are not contiguous; nothing depends on that.
var labelSeq atomic.Uint64

func uniqueLabel(prefix string) string {
	return prefix + "-" + strconv.FormatUint(labelSeq.Add(1), 10)
}

func scrape(t *testing.T) string {
	t.Helper()
	req := httptest.NewRequest("GET", "/metrics", http.NoBody)
	rr := httptest.NewRecorder()
	Handler().ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("scrape returned status %d", rr.Code)
	}
	b, _ := io.ReadAll(rr.Body)
	return string(b)
}

func TestStatusClass(t *testing.T) {
	cases := map[int]string{
		200: "2xx", 204: "2xx", 301: "3xx", 404: "4xx",
		499: "499", 500: "5xx", 502: "5xx", 0: "unknown",
	}
	for code, want := range cases {
		if got := statusClass(code); got != want {
			t.Errorf("statusClass(%d) = %q, want %q", code, got, want)
		}
	}
}

// TestRecordEmitsMetrics records one observation under a unique provider label
// (so the assertions are isolated from any other test's counters) and verifies
// the exposition output parses and carries every expected series.
func TestRecordEmitsMetrics(t *testing.T) {
	prov := uniqueLabel("test-prov-emit")
	// The model label has to be unique too: the failover counter is keyed by
	// model alone, so a shared id would accumulate across runs even under a
	// fresh provider.
	mdl := uniqueLabel("llama-3")
	fallback := uniqueLabel("test-prov-fallback")
	Record(Observation{
		Provider:           prov,
		Model:              mdl,
		StatusCode:         200,
		DurationSeconds:    0.5,
		Streaming:          true,
		TTFTSeconds:        0.1,
		PromptTokens:       10,
		CompletionTokens:   20,
		ReasoningTokens:    5,
		PromptCachedTokens: 4,
		CostUSD:            0.0025,
		Priced:             true,
		// Two attempts after the first: a hedge to the fallback that lost, and
		// the fallback again, which served. One increment each, per provider.
		FailoverProviders: []string{fallback, fallback},
	})

	out := scrape(t)
	wantSubstrings := []string{
		fmt.Sprintf(`modelhotel_requests_total{error_kind="",model=%q,provider=%q,status_class="2xx"} 1`, mdl, prov),
		fmt.Sprintf(`modelhotel_request_duration_seconds_bucket{model=%q,provider=%q,`, mdl, prov),
		fmt.Sprintf(`modelhotel_ttft_seconds_bucket{model=%q,provider=%q,`, mdl, prov),
		fmt.Sprintf(`modelhotel_tokens_total{kind="completion",model=%q,provider=%q} 20`, mdl, prov),
		fmt.Sprintf(`modelhotel_tokens_total{kind="prompt",model=%q,provider=%q} 10`, mdl, prov),
		fmt.Sprintf(`modelhotel_tokens_total{kind="reasoning",model=%q,provider=%q} 5`, mdl, prov),
		fmt.Sprintf(`modelhotel_tokens_total{kind="prompt_cached",model=%q,provider=%q} 4`, mdl, prov),
		fmt.Sprintf(`modelhotel_cost_usd_total{model=%q,provider=%q} 0.0025`, mdl, prov),
		fmt.Sprintf(`modelhotel_failover_attempts_total{model=%q,provider=%q} 2`, mdl, fallback),
		`go_goroutines`, // Go runtime collector is registered
	}
	for _, w := range wantSubstrings {
		if !strings.Contains(out, w) {
			t.Errorf("scrape output missing %q", w)
		}
	}
	if strings.Contains(out, fmt.Sprintf(`modelhotel_failover_attempts_total{model=%q,provider=%q}`, mdl, prov)) {
		t.Errorf("the first attempt's provider was counted as a failover attempt")
	}

	// An unpriced request leaves no cost series at all: a sum that read it as
	// $0 would look like a total when it is a floor.
	unpriced := uniqueLabel("test-prov-unpriced")
	Record(Observation{Provider: unpriced, Model: mdl, StatusCode: 200, PromptTokens: 3, CompletionTokens: 4})
	if strings.Contains(scrape(t), fmt.Sprintf(`modelhotel_cost_usd_total{model=%q,provider=%q}`, mdl, unpriced)) {
		t.Errorf("an unpriced request must not create a cost series")
	}
	// A free model is priced at zero and does get a series: it is known to
	// cost nothing, which is not the same as not knowing.
	free := uniqueLabel("test-prov-free")
	Record(Observation{Provider: free, Model: mdl, StatusCode: 200, PromptTokens: 3, CompletionTokens: 4, Priced: true})
	if !strings.Contains(scrape(t), fmt.Sprintf("modelhotel_cost_usd_total{model=%q,provider=%q} 0\n", mdl, free)) {
		t.Errorf("a free model's request must create a zero cost series")
	}
	// A negative price (a bad catalog import) must not panic the request path.
	bad := uniqueLabel("test-prov-negative")
	Record(Observation{Provider: bad, Model: mdl, StatusCode: 200, Priced: true, CostUSD: -1})
	if strings.Contains(scrape(t), fmt.Sprintf(`modelhotel_cost_usd_total{model=%q,provider=%q}`, mdl, bad)) {
		t.Errorf("a negative cost must be dropped, not counted")
	}
}

// The failover observability counters: one increment per event, labels as the
// design names them, and an empty label collapsed to "unknown" rather than an
// empty series.
func TestRecordFailoverObservabilityCounters(t *testing.T) {
	prov := uniqueLabel("test-prov-obs")
	mdl := uniqueLabel("glm-5.3")
	group := uniqueLabel("group")

	RecordUpstreamRateLimit(prov, mdl, "saturated")
	RecordUpstreamRateLimit(prov, mdl, "saturated")
	RecordUpstreamRateLimit(prov, mdl, "exhausted")
	RecordBreakerOpen(prov, mdl, "upstream status 429 (saturated)")
	RecordBreakerOpen(prov, mdl, "")
	RecordFailoverExhausted(group, "all_busy")
	RecordFailoverExhausted(group, "no_available_provider")

	out := scrape(t)
	for _, w := range []string{
		fmt.Sprintf(`modelhotel_upstream_rate_limit_total{class="saturated",model=%q,provider=%q} 2`, mdl, prov),
		fmt.Sprintf(`modelhotel_upstream_rate_limit_total{class="exhausted",model=%q,provider=%q} 1`, mdl, prov),
		fmt.Sprintf(`modelhotel_circuit_breaker_opens_total{cause="upstream status 429 (saturated)",model=%q,provider=%q} 1`, mdl, prov),
		fmt.Sprintf(`modelhotel_circuit_breaker_opens_total{cause="unknown",model=%q,provider=%q} 1`, mdl, prov),
		fmt.Sprintf(`modelhotel_failover_exhausted_total{group=%q,reason="all_busy"} 1`, group),
		fmt.Sprintf(`modelhotel_failover_exhausted_total{group=%q,reason="no_available_provider"} 1`, group),
	} {
		if !strings.Contains(out, w) {
			t.Errorf("scrape output missing %q", w)
		}
	}
}

// TestRecordSkipsZeroTokensAndNonStreamingTTFT verifies we don't emit token or
// TTFT series for values that don't apply.
func TestRecordSkipsZeroTokensAndNonStreamingTTFT(t *testing.T) {
	prov := uniqueLabel("test-prov-skip")
	Record(Observation{
		Provider:        prov,
		Model:           "m",
		StatusCode:      502,
		ErrorKind:       "provider_error",
		DurationSeconds: 0.2,
		Streaming:       false,
		TTFTSeconds:     0.3, // ignored because not streaming
	})
	out := scrape(t)
	if strings.Contains(out, fmt.Sprintf(`modelhotel_ttft_seconds_bucket{model="m",provider=%q`, prov)) {
		t.Error("ttft must not be recorded for a non-streaming request")
	}
	if !strings.Contains(out, fmt.Sprintf(`modelhotel_requests_total{error_kind="provider_error",model="m",provider=%q,status_class="5xx"} 1`, prov)) {
		t.Errorf("missing 5xx provider_error series:\n%s", out)
	}
}

// TestRecordResponsesReroute verifies the OpenAI Responses re-route counter
// tracks learned and preemptive attempts as separate series.
func TestRecordResponsesReroute(t *testing.T) {
	prov := uniqueLabel("test-prov-responses")
	RecordResponsesReroute(prov, "gpt-5.6-sol", "learned")
	RecordResponsesReroute(prov, "gpt-5.6-sol", "preemptive")
	RecordResponsesReroute(prov, "gpt-5.6-sol", "preemptive")
	out := scrape(t)
	wantSubstrings := []string{
		fmt.Sprintf(`modelhotel_responses_reroute_total{mode="learned",model="gpt-5.6-sol",provider=%q} 1`, prov),
		fmt.Sprintf(`modelhotel_responses_reroute_total{mode="preemptive",model="gpt-5.6-sol",provider=%q} 2`, prov),
	}
	for _, w := range wantSubstrings {
		if !strings.Contains(out, w) {
			t.Errorf("scrape output missing %q", w)
		}
	}
}

// TestRecordRetirementProbe verifies the pre-retirement probe counter keeps the
// three verdicts as separate series, which is the whole point of the metric: the
// operator question is the RATIO between them, so a refused that could not be
// told from a served would answer nothing.
func TestRecordRetirementProbe(t *testing.T) {
	prov := uniqueLabel("test-prov-retirement")
	RecordRetirementProbe(prov, "gemini-2.0-flash", "refused")
	RecordRetirementProbe(prov, "gemini-2.0-flash", "inconclusive")
	RecordRetirementProbe(prov, "gemini-2.0-flash", "inconclusive")
	RecordRetirementProbe(prov, "claude-sonnet-4", "served")
	// An empty provider still has to produce a usable series rather than a
	// blank label, exactly as the request counter does. The model carries the
	// run's own suffix because the provider label — the usual isolator — is the
	// very thing under test here.
	orphan := uniqueLabel("orphan-model")
	RecordRetirementProbe("", orphan, "inconclusive")

	out := scrape(t)
	wantSubstrings := []string{
		fmt.Sprintf(`modelhotel_retirement_probes_total{model="gemini-2.0-flash",provider=%q,verdict="refused"} 1`, prov),
		fmt.Sprintf(`modelhotel_retirement_probes_total{model="gemini-2.0-flash",provider=%q,verdict="inconclusive"} 2`, prov),
		fmt.Sprintf(`modelhotel_retirement_probes_total{model="claude-sonnet-4",provider=%q,verdict="served"} 1`, prov),
		fmt.Sprintf(`modelhotel_retirement_probes_total{model=%q,provider="unknown",verdict="inconclusive"} 1`, orphan),
	}
	for _, w := range wantSubstrings {
		if !strings.Contains(out, w) {
			t.Errorf("scrape output missing %q", w)
		}
	}
}

func TestBreakerCollector(t *testing.T) {
	RegisterBreakerCollector(func() []BreakerState {
		return []BreakerState{
			{ProviderID: "prov-open", ProviderName: "Open Provider", State: BreakerOpen},
			{ProviderID: "prov-closed", State: BreakerClosed},
		}
	})
	out := scrape(t)
	if !strings.Contains(out, `modelhotel_circuit_breaker_state{provider="Open Provider",provider_id="prov-open"} 2`) {
		t.Errorf("missing open breaker gauge:\n%s", out)
	}
	// A state with no name (a breaker that has not been told one) is labelled
	// unknown, never dropped.
	if !strings.Contains(out, `modelhotel_circuit_breaker_state{provider="unknown",provider_id="prov-closed"} 0`) {
		t.Errorf("missing closed breaker gauge:\n%s", out)
	}
}

func TestQuotaCollector(t *testing.T) {
	reset := time.Unix(1_800_000_000, 0)
	RegisterQuotaCollector(func() []QuotaWindow {
		return []QuotaWindow{
			{ProviderID: "prov-zai", ProviderName: "Z.ai", Window: "5h", Used: 0.42, ResetsAt: reset, Reserve: 0.2},
			{ProviderID: "prov-zai", ProviderName: "Z.ai", Window: "weekly", Used: 0.1, Reserve: 0.2},
			{ProviderID: "prov-nw", ProviderName: "NeuralWatt", Window: "credits", Used: 1.5},
		}
	})
	out := scrape(t)
	for _, want := range []string{
		`modelhotel_provider_quota_used_ratio{provider="Z.ai",provider_id="prov-zai",window="5h"} 0.42`,
		`modelhotel_provider_quota_resets_at_seconds{provider="Z.ai",provider_id="prov-zai",window="5h"} 1.8e+09`,
		`modelhotel_provider_quota_used_ratio{provider="NeuralWatt",provider_id="prov-nw",window="credits"} 1.5`,
		`modelhotel_provider_quota_reserve_ratio{provider="Z.ai",provider_id="prov-zai"} 0.2`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in:\n%s", want, out)
		}
	}
	// A window nothing dates has no reset series rather than a zero one, which
	// a countdown panel would read as a rollover in 1970.
	if strings.Contains(out, `modelhotel_provider_quota_resets_at_seconds{provider="NeuralWatt"`) {
		t.Errorf("undated window must not emit a reset:\n%s", out)
	}
	// One reserve series per provider, not per window, and none without a reserve.
	if strings.Count(out, `modelhotel_provider_quota_reserve_ratio{`) != 1 {
		t.Errorf("want exactly one reserve series:\n%s", out)
	}
}

// TestQuotaCollector_DropsCorruptWindows: the figures are a provider's own
// HTTP response, so a negative share, an absurd one or a reset centuries out
// stays off the gauge, while genuine overage above 1 passes.
func TestQuotaCollector_DropsCorruptWindows(t *testing.T) {
	for _, tc := range []struct {
		name string
		used float64
		ok   bool
	}{
		{"overage", 3.5, true},
		{"negative", -0.1, false},
		{"absurd", 1e300, false},
		{"nan", math.NaN(), false},
		{"inf", math.Inf(1), false},
	} {
		if got := reportableQuotaUsed(tc.used); got != tc.ok {
			t.Errorf("%s: reportable=%v, want %v", tc.name, got, tc.ok)
		}
	}
	now := time.Now()
	if reportableQuotaReset(time.Time{}, now) || reportableQuotaReset(now.Add(11*365*24*time.Hour), now) || reportableQuotaReset(now.Add(-11*365*24*time.Hour), now) {
		t.Error("an undated reset or one outside the ten-year horizon must not reach the gauge")
	}
	if !reportableQuotaReset(now.Add(48*time.Hour), now) {
		t.Error("a reset two days out is fit for the gauge")
	}
}

func TestRegisterQuotaCollector_NilIsNoop(t *testing.T) {
	RegisterQuotaCollector(nil)
}

// TestLatencyBucketsReachGenerationLengths pins the histogram range: a request
// that runs for minutes must land in a finite bucket, or every quantile above
// the median clamps to the top edge.
func TestLatencyBucketsReachGenerationLengths(t *testing.T) {
	Record(Observation{Provider: "p-long", Model: "m", StatusCode: 200, DurationSeconds: 150, TTFTSeconds: 45, Streaming: true})
	out := scrape(t)
	for _, want := range []string{
		`modelhotel_request_duration_seconds_bucket{model="m",provider="p-long",le="180"} 1`,
		`modelhotel_request_duration_seconds_bucket{model="m",provider="p-long",le="120"} 0`,
		`modelhotel_request_duration_seconds_bucket{model="m",provider="p-long",le="0.005"} 0`,
		`modelhotel_ttft_seconds_bucket{model="m",provider="p-long",le="60"} 1`,
		`modelhotel_ttft_seconds_bucket{model="m",provider="p-long",le="30"} 0`,
		`modelhotel_ttft_seconds_bucket{model="m",provider="p-long",le="0.005"} 0`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in:\n%s", want, out)
		}
	}
}

// TestLabelOrUnknown verifies the empty-label fallback used for the provider and
// model metric labels: an empty value becomes "unknown" so a series is never
// emitted with a blank label, while a real value passes through untouched.
func TestLabelOrUnknown(t *testing.T) {
	if got := labelOrUnknown(""); got != "unknown" {
		t.Errorf(`labelOrUnknown("") = %q, want "unknown"`, got)
	}
	if got := labelOrUnknown("openai"); got != "openai" {
		t.Errorf(`labelOrUnknown("openai") = %q, want "openai"`, got)
	}
}

// TestRegisterBreakerCollector_NilIsNoop guards the documented nil-collector
// contract: passing nil must be ignored (no registration, no panic) so callers
// without a breaker source can pass through unconditionally.
func TestRegisterBreakerCollector_NilIsNoop(t *testing.T) {
	RegisterBreakerCollector(nil)
}

// TestInflightCollector pins the scrape-time gauges for the adaptive in-flight
// limiter: the learned allowance (0 = uncapped) and the live count, one series
// per provider.
func TestInflightCollector(t *testing.T) {
	RegisterInflightCollector(func() []InflightState {
		return []InflightState{
			{ProviderID: "prov-capped", Limit: 3, Inflight: 2},
			{ProviderID: "prov-uncapped", Limit: 0, Inflight: 1},
		}
	})
	out := scrape(t)
	for _, want := range []string{
		`modelhotel_provider_inflight_limit{provider_id="prov-capped"} 3`,
		`modelhotel_provider_inflight{provider_id="prov-capped"} 2`,
		`modelhotel_provider_inflight_limit{provider_id="prov-uncapped"} 0`,
		`modelhotel_provider_inflight{provider_id="prov-uncapped"} 1`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("scrape output missing %q:\n%s", want, out)
		}
	}
}

// TestRegisterInflightCollector_NilIsNoop guards the same nil contract the
// breaker collector has: nil registers nothing and does not panic.
func TestRegisterInflightCollector_NilIsNoop(t *testing.T) {
	RegisterInflightCollector(nil)
}
