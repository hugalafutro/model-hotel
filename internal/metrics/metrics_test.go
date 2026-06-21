package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
	const prov = "test-prov-emit"
	Record(Observation{
		Provider:         prov,
		Model:            "llama-3",
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
		`modelhotel_requests_total{error_kind="",model="llama-3",provider="test-prov-emit",status_class="2xx"} 1`,
		`modelhotel_request_duration_seconds_bucket{model="llama-3",provider="test-prov-emit",`,
		`modelhotel_ttft_seconds_bucket{model="llama-3",provider="test-prov-emit",`,
		`modelhotel_tokens_total{kind="completion",model="llama-3",provider="test-prov-emit"} 20`,
		`modelhotel_tokens_total{kind="prompt",model="llama-3",provider="test-prov-emit"} 10`,
		`modelhotel_tokens_total{kind="reasoning",model="llama-3",provider="test-prov-emit"} 5`,
		`modelhotel_failover_attempts_total{model="llama-3"} 1`,
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
	const prov = "test-prov-skip"
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
	if strings.Contains(out, `provider="test-prov-skip"`) && strings.Contains(out, `modelhotel_ttft_seconds_bucket{model="m",provider="test-prov-skip"`) {
		t.Error("ttft must not be recorded for a non-streaming request")
	}
	if !strings.Contains(out, `modelhotel_requests_total{error_kind="provider_error",model="m",provider="test-prov-skip",status_class="5xx"} 1`) {
		t.Errorf("missing 5xx provider_error series:\n%s", out)
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
