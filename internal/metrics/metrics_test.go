package metrics

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
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
	Record(Observation{
		Provider:         prov,
		Model:            mdl,
		StatusCode:       200,
		DurationSeconds:  0.5,
		Streaming:        true,
		TTFTSeconds:      0.1,
		PromptTokens:     10,
		CompletionTokens: 20,
		ReasoningTokens:  5,
		FailoverAttempt:  1,
	})

	out := scrape(t)
	wantSubstrings := []string{
		fmt.Sprintf(`modelhotel_requests_total{error_kind="",model=%q,provider=%q,status_class="2xx"} 1`, mdl, prov),
		fmt.Sprintf(`modelhotel_request_duration_seconds_bucket{model=%q,provider=%q,`, mdl, prov),
		fmt.Sprintf(`modelhotel_ttft_seconds_bucket{model=%q,provider=%q,`, mdl, prov),
		fmt.Sprintf(`modelhotel_tokens_total{kind="completion",model=%q,provider=%q} 20`, mdl, prov),
		fmt.Sprintf(`modelhotel_tokens_total{kind="prompt",model=%q,provider=%q} 10`, mdl, prov),
		fmt.Sprintf(`modelhotel_tokens_total{kind="reasoning",model=%q,provider=%q} 5`, mdl, prov),
		fmt.Sprintf(`modelhotel_failover_attempts_total{model=%q} 1`, mdl),
		`go_goroutines`, // Go runtime collector is registered
	}
	for _, w := range wantSubstrings {
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
			{ProviderID: "prov-open", State: BreakerOpen},
			{ProviderID: "prov-closed", State: BreakerClosed},
		}
	})
	out := scrape(t)
	if !strings.Contains(out, `modelhotel_circuit_breaker_state{provider_id="prov-open"} 2`) {
		t.Errorf("missing open breaker gauge:\n%s", out)
	}
	if !strings.Contains(out, `modelhotel_circuit_breaker_state{provider_id="prov-closed"} 0`) {
		t.Errorf("missing closed breaker gauge:\n%s", out)
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
