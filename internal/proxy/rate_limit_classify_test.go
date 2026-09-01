package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func hdr(pairs ...string) http.Header {
	h := make(http.Header)
	for i := 0; i+1 < len(pairs); i += 2 {
		h.Set(pairs[i], pairs[i+1])
	}
	return h
}

// The bodies below are the real payloads from the 2026-08-31 hotel/glm53 run
// (and the provider docs the design names), not paraphrases: the classifier is
// a substring table and only earns trust against the exact sentences.
func TestClassifyRateLimit(t *testing.T) {
	const maxWait = 60 * time.Second
	cases := []struct {
		name       string
		status     int
		hdr        http.Header
		body       string
		wantClass  rateLimitClass
		wantRetry  time.Duration
		wantPin    time.Duration
		wantEntitl bool
	}{
		{
			name:      "Neuralwatt concurrent_budget_exceeded (verbatim 2026-08-31 14:52 UTC)",
			status:    429,
			body:      `{"error":{"type":"rate_limit_error","code":"concurrent_budget_exceeded","message":"Concurrency budget exceeded for basic tier"}}`,
			wantClass: rateLimitSaturated,
			wantRetry: defaultSaturatedRetryAfter,
		},
		{
			name:      "Ollama session usage limit",
			status:    429,
			body:      `{"error":"you have reached your session usage limit, upgrade for higher limits"}`,
			wantClass: rateLimitExhausted,
			wantPin:   pinHintWindow,
		},
		{
			name:      "Ollama weekly usage limit outranks the session pin",
			status:    429,
			body:      `{"error":"you have reached your weekly usage limit, upgrade for higher limits"}`,
			wantClass: rateLimitExhausted,
			wantPin:   pinHintWeekly,
		},
		{
			name:       "Z.ai coding plan code 1113",
			status:     429,
			body:       `{"error":{"code":"1113","message":"Insufficient balance or no resource package. Please recharge."}}`,
			wantClass:  rateLimitExhausted,
			wantPin:    pinHintUntilPaid,
			wantEntitl: true,
		},
		{
			// Verbatim from prod, 2026-09-01 16:42 UTC, glm-5.3 on the Coding Plan:
			// a weekly/monthly cap with a dated reset. Time fixes it (not
			// entitled), and the pin is the weekly one.
			name:      "Z.ai coding plan code 1310 weekly/monthly limit exhausted",
			status:    429,
			body:      `{"error":{"code":"1310","message":"Weekly/Monthly Limit Exhausted. Your limit will reset at 2026-09-03 18:01:05"}}`,
			wantClass: rateLimitExhausted,
			wantPin:   pinHintWeekly,
		},
		{
			// Holds the line the entry above could have crossed: "exhausted"
			// beside a concurrency limit is a busy provider, and it must reach
			// the saturated entries rather than open a circuit with a 2h pin.
			name:      "concurrency limit exhausted stays saturated (no reset named)",
			status:    429,
			body:      `{"error":{"message":"Concurrency limit exhausted, retry shortly"}}`,
			wantClass: rateLimitSaturated,
			wantRetry: defaultSaturatedRetryAfter,
		},
		{
			name:       "OpenAI insufficient_quota",
			status:     429,
			body:       `{"error":{"message":"You exceeded your current quota, please check your plan and billing details.","type":"insufficient_quota"}}`,
			wantClass:  rateLimitExhausted,
			wantPin:    pinHintUntilPaid,
			wantEntitl: true,
		},
		{
			name:       "Anthropic credit balance too low",
			status:     429,
			body:       `{"type":"error","error":{"type":"rate_limit_error","message":"Your credit balance is too low to access the Anthropic API."}}`,
			wantClass:  rateLimitExhausted,
			wantPin:    pinHintUntilPaid,
			wantEntitl: true,
		},
		{
			name:       "MiniMax 1008 remap (insufficient balance by code alone)",
			status:     429,
			body:       `{"base_resp":{"status_code":1008,"status_msg":"balance not enough"}}`,
			wantClass:  rateLimitExhausted,
			wantPin:    pinHintUntilPaid,
			wantEntitl: true,
		},
		{
			// A spent token window, not a billing failure: time fixes it, so it
			// gets the generic window pin and never reads as an entitlement.
			name:      "MiniMax 1039 remap (token plan window spent)",
			status:    429,
			body:      `{"base_resp":{"status_code": 1039,"status_msg":"token limit of package"}}`,
			wantClass: rateLimitExhausted,
			wantPin:   pinHintWindow,
		},
		{
			name:      "MiniMax 1002 (plain rate limit) is not exhaustion",
			status:    429,
			body:      `{"base_resp":{"status_code":1002,"status_msg":"rate limit exceeded"}}`,
			wantClass: rateLimitSaturated,
			wantRetry: defaultSaturatedRetryAfter,
		},
		{
			name:      "OpenAI TPM saturation with Retry-After",
			status:    429,
			hdr:       hdr("Retry-After", "7"),
			body:      `{"error":{"message":"Rate limit reached for gpt-x: 30000 tokens per minute. Please try again in 6.9s.","type":"tokens"}}`,
			wantClass: rateLimitSaturated,
			wantRetry: 7 * time.Second,
		},
		{
			name:      "Retry-After alone at the ceiling stays saturation",
			status:    429,
			hdr:       hdr("Retry-After", "60"),
			body:      `{}`,
			wantClass: rateLimitSaturated,
			wantRetry: 60 * time.Second,
		},
		{
			name:      "Retry-After alone above the ceiling means the window",
			status:    429,
			hdr:       hdr("Retry-After", "61"),
			body:      `{}`,
			wantClass: rateLimitExhausted,
			wantPin:   61 * time.Second,
		},
		{
			name:      "a dated Retry-After beyond the ceiling overrides the phrase's pin",
			status:    429,
			hdr:       hdr("Retry-After", "7200"),
			body:      `{"error":"you have reached your session usage limit, upgrade for higher limits"}`,
			wantClass: rateLimitExhausted,
			wantPin:   7200 * time.Second,
		},
		{
			name:      "x-ratelimit-reset-tokens in OpenAI duration form",
			status:    429,
			hdr:       hdr("X-RateLimit-Reset-Tokens", "6m0s"),
			body:      `{}`,
			wantClass: rateLimitExhausted,
			wantPin:   6 * time.Minute,
		},
		{
			name:      "x-ratelimit-reset-requests in bare seconds",
			status:    429,
			hdr:       hdr("X-RateLimit-Reset-Requests", "30"),
			body:      `{}`,
			wantClass: rateLimitSaturated,
			wantRetry: 30 * time.Second,
		},
		{
			name:      "garbage Retry-After is no signal",
			status:    429,
			hdr:       hdr("Retry-After", "soon"),
			body:      `{}`,
			wantClass: rateLimitUnknown,
		},
		{
			name:      "an unfamiliar body with no headers stays unknown",
			status:    429,
			body:      `{"error":{"message":"request denied","code":"E_DENIED"}}`,
			wantClass: rateLimitUnknown,
		},
		{
			name:      "a non-429 never classifies",
			status:    500,
			body:      `{"error":"you have reached your session usage limit"}`,
			wantClass: rateLimitUnknown,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyRateLimit(tc.status, tc.hdr, tc.body, maxWait)
			if got.class != tc.wantClass {
				t.Fatalf("class = %v, want %v", got.class, tc.wantClass)
			}
			if got.retryAfter != tc.wantRetry {
				t.Errorf("retryAfter = %v, want %v", got.retryAfter, tc.wantRetry)
			}
			if got.pinHint != tc.wantPin {
				t.Errorf("pinHint = %v, want %v", got.pinHint, tc.wantPin)
			}
			if got.entitled != tc.wantEntitl {
				t.Errorf("entitled = %v, want %v", got.entitled, tc.wantEntitl)
			}
		})
	}
}

// The table is data with provenance: every phrase names the provider it was
// observed on and when, so entries can be retired with their providers instead
// of rotting anonymously.
func TestRateLimitPhrases_EveryEntryHasProvenance(t *testing.T) {
	for _, p := range rateLimitPhrases {
		if p.provider == "" {
			t.Errorf("phrase %q has no recorded provider", p.phrase)
		}
		if _, err := time.Parse("2006-01-02", p.observed); err != nil {
			t.Errorf("phrase %q has no parseable observation date (%q): %v", p.phrase, p.observed, err)
		}
		if p.phrase != strings.ToLower(p.phrase) || p.also != strings.ToLower(p.also) {
			t.Errorf("phrase %q/%q is not lowercase; matching is on a lowercased body", p.phrase, p.also)
		}
		if p.class == rateLimitUnknown {
			t.Errorf("phrase %q carries no class", p.phrase)
		}
		if p.entitled && p.class != rateLimitExhausted {
			t.Errorf("phrase %q is entitled but not exhausted; entitlement is a kind of exhaustion", p.phrase)
		}
	}
}

// One table, two consumers: every phrase classifyUpstreamError labels
// provider_not_entitled must classify exhausted here, or the breaker and the
// error kind would disagree about what a balance error means.
func TestRateLimitPhrases_EntitledParityWithClassifier(t *testing.T) {
	entitled := entitledRateLimitPhrases()
	if len(entitled) == 0 {
		t.Fatal("no entitled phrases derived from the table")
	}
	for _, phrase := range entitled {
		v := classifyRateLimit(429, nil, `{"error":{"message":"`+phrase+`"}}`, 0)
		if v.class != rateLimitExhausted || !v.entitled {
			t.Errorf("entitled phrase %q: class=%v entitled=%v, want exhausted+entitled", phrase, v.class, v.entitled)
		}
		kind, _ := classifyUpstreamError(429, phrase, "some-model")
		if kind != KindProviderNotEntitled {
			t.Errorf("classifyUpstreamError(%q) = %v, want provider_not_entitled from the shared table", phrase, kind)
		}
	}
}

// The new error kinds come from the attempt's verdict via
// rateLimitTerminalKind, never from classifyUpstreamError itself: the latter
// cannot read the master switch, and rate_limit_classify_enabled off must
// restore today's labels bit for bit (no verdict is stamped then, so the
// refinement is inert).
func TestRateLimitTerminalKind(t *testing.T) {
	// classifyUpstreamError stays generic for rate-limit vocabulary...
	kind, _ := classifyUpstreamError(429, `{"error":{"code":"concurrent_budget_exceeded"}}`, "m")
	if kind != KindProviderError {
		t.Fatalf("classifyUpstreamError(saturated body) = %v, want the generic provider_error (the verdict refines it)", kind)
	}
	// ...and the verdict-driven refinement sharpens only that generic kind.
	kind, reason := rateLimitTerminalKind(KindProviderError, "r", 429, rateLimitVerdict{class: rateLimitSaturated})
	if kind != KindProviderSaturated || !strings.Contains(reason, "capacity") {
		t.Errorf("saturated refinement = (%v, %q)", kind, reason)
	}
	kind, _ = rateLimitTerminalKind(KindProviderError, "r", 429, rateLimitVerdict{class: rateLimitExhausted})
	if kind != KindProviderQuotaExhausted {
		t.Errorf("exhausted refinement = %v, want provider_quota_exhausted", kind)
	}
	// No verdict (master switch off, or nothing recognised): labels unchanged.
	if kind, reason := rateLimitTerminalKind(KindProviderError, "r", 429, rateLimitVerdict{}); kind != KindProviderError || reason != "r" {
		t.Errorf("no-verdict refinement changed the labels: (%v, %q)", kind, reason)
	}
	// A sharper body-derived kind is never blurred.
	if kind, _ := rateLimitTerminalKind(KindProviderNotEntitled, "r", 429, rateLimitVerdict{class: rateLimitSaturated}); kind != KindProviderNotEntitled {
		t.Errorf("provider_not_entitled was blurred to %v", kind)
	}
	// Non-429 statuses are untouched whatever the verdict claims.
	if kind, _ := rateLimitTerminalKind(KindProviderError, "r", 500, rateLimitVerdict{class: rateLimitSaturated}); kind != KindProviderError {
		t.Errorf("a 500 was refined to %v", kind)
	}
}

func TestParseRetryAfter_HTTPDate(t *testing.T) {
	when := time.Now().Add(90 * time.Second).UTC().Format(http.TimeFormat)
	d, ok := parseRetryAfter(when)
	if !ok || d <= 80*time.Second || d > 91*time.Second {
		t.Errorf("parseRetryAfter(%q) = (%v, %v), want ~90s", when, d, ok)
	}
	if _, ok := parseRetryAfter(time.Now().Add(-time.Minute).UTC().Format(http.TimeFormat)); ok {
		t.Error("a Retry-After date in the past is not a wait")
	}
}

// The structured attempt errors and their terminal renderings tell "busy"
// from "spent" from "failed" all the way to the client.
func TestRateLimitReqErr_Rendering(t *testing.T) {
	sat := rateLimitReqErr(rateLimitVerdict{class: rateLimitSaturated, retryAfter: time.Second}, 1, "np")
	if sat.Kind != KindProviderSaturated {
		t.Fatalf("saturated kind = %v", sat.Kind)
	}
	if msg := sat.render(); !strings.Contains(msg, "busy") {
		t.Errorf("saturated render %q does not say busy", msg)
	}
	if msg := sat.terminalClientMessage("m", false); !strings.Contains(msg, "provider busy for model m") {
		t.Errorf("single-provider saturated client message = %q", msg)
	}
	if msg := sat.terminalLogMessage(true, 3); !strings.Contains(msg, "all 3 providers busy") {
		t.Errorf("saturated terminal log = %q, want the busy wording", msg)
	}

	exh := rateLimitReqErr(rateLimitVerdict{class: rateLimitExhausted}, 0, "np")
	if exh.Kind != KindProviderQuotaExhausted {
		t.Fatalf("exhausted kind = %v", exh.Kind)
	}
	if msg := exh.render(); !strings.Contains(msg, "quota") {
		t.Errorf("exhausted render %q does not name the quota", msg)
	}
	ent := rateLimitReqErr(rateLimitVerdict{class: rateLimitExhausted, entitled: true}, 0, "np")
	if ent.Kind != KindProviderNotEntitled {
		t.Errorf("entitled exhaustion kind = %v, want provider_not_entitled: a person fixes it", ent.Kind)
	}
}

// The saturation retry never waits past the request's own budget or a client
// that already left.
func TestRetrySaturatedCandidate_BudgetGuards(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Upstream.Close()
	h := env.Handler
	attempted := false
	fn := func(http.ResponseWriter, *http.Request, *requestState, modelCandidate, int, int) candidateOutcome {
		attempted = true
		return outcomeServed
	}

	// Overall deadline already spent: no wait, no retry.
	st := &requestState{overallDeadline: time.Now().Add(-time.Second), rateLimit: rateLimitVerdict{class: rateLimitSaturated, retryAfter: time.Second}, logData: &requestLogData{}}
	if h.retrySaturatedCandidate(httptest.NewRecorder(), httptest.NewRequest("POST", "/", http.NoBody), st, modelCandidateForBreaker(uuid.New()), 1, fn) {
		t.Error("an exhausted budget reported the request as answered")
	}
	if attempted {
		t.Error("the retry ran with no time budget left")
	}

	// Client gone during the wait: the request ends there as a 499 client
	// disconnect, never as a 429 "all providers busy" the caller did not see.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequest("POST", "/", http.NoBody).WithContext(ctx)
	logData, _ := h.newPendingRequestLog(req, endpointTypeChat, "m", false)
	st = &requestState{startTime: time.Now(), overallDeadline: time.Now().Add(time.Hour), rateLimit: rateLimitVerdict{class: rateLimitSaturated, retryAfter: 30 * time.Second}, logData: logData}
	w := httptest.NewRecorder()
	start := time.Now()
	if !h.retrySaturatedCandidate(w, req, st, modelCandidateForBreaker(uuid.New()), 1, fn) {
		t.Error("a disconnect during the wait must be terminal, not fall through to the exhaustion path")
	}
	if attempted {
		t.Error("the retry ran after the client left")
	}
	if w.Code != statusClientClosedRequest {
		t.Errorf("status = %d, want 499 for a client disconnect", w.Code)
	}
	if st.lastReqErr.Kind != KindClientDisconnect {
		t.Errorf("terminal kind = %v, want client_disconnect", st.lastReqErr.Kind)
	}
	if time.Since(start) > 5*time.Second {
		t.Error("the wait did not stop when the client left")
	}
}

func TestRetryAfterSeconds(t *testing.T) {
	for _, tc := range []struct {
		in   time.Duration
		want int
	}{
		{0, 2},                       // class default
		{-time.Second, 2},            // class default
		{1500 * time.Millisecond, 2}, // rounds up, never "retry now"
		{2 * time.Second, 2},
		{61 * time.Second, 61},
	} {
		if got := retryAfterSeconds(tc.in); got != tc.want {
			t.Errorf("retryAfterSeconds(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestParseResetValue(t *testing.T) {
	for _, tc := range []struct {
		in     string
		want   time.Duration
		wantOK bool
	}{
		{"", 0, false},
		{"30", 30 * time.Second, true},
		{"6m0s", 6 * time.Minute, true},
		{"1s", time.Second, true},
		{"0", 0, false},
		{"-5", 0, false},
		{"nonsense", 0, false},
	} {
		got, ok := parseResetValue(tc.in)
		if ok != tc.wantOK || (ok && got != tc.want) {
			t.Errorf("parseResetValue(%q) = (%v, %v), want (%v, %v)", tc.in, got, ok, tc.want, tc.wantOK)
		}
	}
	// Unix epochs are relative to now; assert the shape rather than the value.
	future := time.Now().Add(90 * time.Second).Unix()
	if got, ok := parseResetValue(strconv.FormatInt(future, 10)); !ok || got <= 80*time.Second || got > 91*time.Second {
		t.Errorf("epoch-seconds reset parsed to (%v, %v), want ~90s", got, ok)
	}
}
