package proxy

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestCancelOriginToKind(t *testing.T) {
	cases := map[string]ErrorKind{
		"client_disconnect": KindClientDisconnect,
		"failover_timeout":  KindFailoverTimeout,
		"retry_timeout":     KindRetryTimeout,
		"":                  KindInternal,
		"something_else":    KindInternal,
	}
	for origin, want := range cases {
		if got := cancelOriginToKind(origin); got != want {
			t.Errorf("cancelOriginToKind(%q) = %q, want %q", origin, got, want)
		}
	}
}

func TestErrString(t *testing.T) {
	if got := errString(nil); got != "" {
		t.Errorf("errString(nil) = %q, want empty", got)
	}
	if got := errString(errors.New("boom")); got != "boom" {
		t.Errorf("errString(boom) = %q, want boom", got)
	}
	long := errors.New(strings.Repeat("x", 600))
	got := errString(long)
	if len([]rune(got)) != 501 || !strings.HasSuffix(got, "…") {
		t.Errorf("errString did not truncate long error: len=%d suffix=%q", len([]rune(got)), got[len(got)-3:])
	}
}

// TestReqErrorRender is the golden-string table for per-attempt rendering.
// Wording changes are intentional one-file diffs here.
func TestReqErrorRender(t *testing.T) {
	cases := []struct {
		name string
		err  reqError
		want string
	}{
		{
			name: "client disconnect with underlying preserves real error",
			err:  reqError{Kind: KindClientDisconnect, Attempt: 0, Provider: "groq", Underlying: "connection reset by peer"},
			want: `client disconnected while retrying provider "groq" (attempt 1); last provider error: connection reset by peer`,
		},
		{
			name: "client disconnect without underlying",
			err:  reqError{Kind: KindClientDisconnect, Attempt: 1, Provider: "groq"},
			want: `client disconnected during attempt 2 to provider "groq"`,
		},
		{
			name: "provider error with HTTP detail",
			err:  reqError{Kind: KindProviderError, Attempt: 0, Provider: "groq", Detail: "HTTP 500"},
			want: `provider "groq" returned HTTP 500 on attempt 1`,
		},
		{
			name: "provider error with transport underlying",
			err:  reqError{Kind: KindProviderError, Attempt: 2, Provider: "groq", Underlying: "dial tcp: connection refused"},
			want: `provider "groq" failed on attempt 3: dial tcp: connection refused`,
		},
		{
			name: "provider timeout",
			err:  reqError{Kind: KindProviderTimeout, Attempt: 0, Provider: "groq"},
			want: `provider "groq" did not return a response in time on attempt 1`,
		},
		{
			name: "failover timeout with underlying",
			err:  reqError{Kind: KindFailoverTimeout, Attempt: 1, Provider: "groq", Underlying: "HTTP 503"},
			want: `request timed out while waiting on provider "groq" (attempt 2); last provider error: HTTP 503`,
		},
		{
			name: "retry timeout",
			err:  reqError{Kind: KindRetryTimeout, Attempt: 0, Provider: "groq"},
			want: `self-heal retry timed out on provider "groq" (attempt 1)`,
		},
		{
			name: "internal with underlying",
			err:  reqError{Kind: KindInternal, Attempt: 0, Underlying: "bad url"},
			want: `internal error on attempt 1: bad url`,
		},
		{
			name: "validation",
			err:  reqError{Kind: KindValidation},
			want: `invalid request`,
		},
		{
			name: "auth",
			err:  reqError{Kind: KindAuth},
			want: `authorization failed`,
		},
		{
			name: "no provider name falls back to generic phrase",
			err:  reqError{Kind: KindProviderError, Attempt: 0},
			want: `the provider failed on attempt 1`,
		},
		// The three kinds split out of KindProviderError. Their whole purpose is
		// that a caller can tell them apart, so the wording is the contract: a
		// retired model, a billing or plan refusal, and a rejected payload each
		// need a different operator response, and every one of them used to
		// arrive as the same "returned HTTP 400".
		{
			name: "model gone",
			err:  reqError{Kind: KindProviderModelGone, Attempt: 0, Provider: "google"},
			want: `provider "google" no longer serves this model (attempt 1)`,
		},
		{
			name: "model gone keeps the provider's own words",
			err:  reqError{Kind: KindProviderModelGone, Attempt: 1, Provider: "google", Underlying: "model not found"},
			want: `provider "google" no longer serves this model (attempt 2); last provider error: model not found`,
		},
		{
			name: "not entitled",
			err:  reqError{Kind: KindProviderNotEntitled, Attempt: 0, Provider: "zai"},
			want: `provider "zai" rejected the request for billing or plan reasons on attempt 1`,
		},
		{
			name: "bad request",
			err:  reqError{Kind: KindProviderBadRequest, Attempt: 0, Provider: "groq"},
			want: `provider "groq" rejected the request payload on attempt 1`,
		},
		{
			name: "bad request without a provider name",
			err:  reqError{Kind: KindProviderBadRequest, Attempt: 0},
			want: `the provider rejected the request payload on attempt 1`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.render(); got != tc.want {
				t.Errorf("render() =\n  %q\nwant\n  %q", got, tc.want)
			}
		})
	}
}

func TestReqErrorTerminalStatus(t *testing.T) {
	cases := map[ErrorKind]int{
		KindClientDisconnect: 499,
		KindFailoverTimeout:  http.StatusGatewayTimeout,
		KindRetryTimeout:     http.StatusGatewayTimeout,
		KindProviderError:    http.StatusBadGateway,
		KindProviderTimeout:  http.StatusBadGateway,
		KindInternal:         http.StatusBadGateway,
		KindValidation:       http.StatusBadRequest,
	}
	for kind, want := range cases {
		if got := (reqError{Kind: kind}).terminalStatus(); got != want {
			t.Errorf("terminalStatus(%q) = %d, want %d", kind, got, want)
		}
	}
}

// A validation refusal is this gateway's own: the reason (a part index and
// type, never request content) reaches the client, and the log line does not
// dress it as a provider's error.
func TestReqErrorValidationMessages(t *testing.T) {
	e := reqError{Kind: KindValidation, Attempt: 0, Provider: "Anthropic", Underlying: "anthropicegress: content part cannot be sent to Anthropic Messages: part 1 file carries no inline data or URL"}
	if got := e.terminalClientMessage("hotel/claude", true); got != "invalid request for model hotel/claude: "+e.Underlying {
		t.Errorf("client message = %q", got)
	}
	if got := e.render(); got != "invalid request: "+e.Underlying {
		t.Errorf("render = %q", got)
	}
	if got := e.terminalLogMessage(true, 3); got != "invalid request: "+e.Underlying {
		t.Errorf("log message = %q, want the refusal without an all-providers-failed wrap", got)
	}
	if got := (reqError{Kind: KindValidation}).terminalClientMessage("m", false); got != "invalid request for model m" {
		t.Errorf("client message without a reason = %q", got)
	}
}

// A validation refusal recorded after a provider-side failure leaves that
// failure as the terminal verdict: a retry may serve the request through the
// candidate that had a route for it. The reverse order, and two refusals,
// record normally.
func TestSetReqErr_ValidationDoesNotDisplaceProviderFailure(t *testing.T) {
	st := &requestState{logData: &requestLogData{}}
	st.setReqErr(reqError{Kind: KindProviderError, Attempt: 0, Provider: "OpenAI", Underlying: "HTTP 502"})
	st.setReqErr(reqError{Kind: KindValidation, Attempt: 1, Provider: "Anthropic", Underlying: "refused"})
	if st.lastReqErr.Kind != KindProviderError || st.lastReqErr.Attempt != 0 {
		t.Fatalf("lastReqErr = %+v, want the provider failure kept", st.lastReqErr)
	}
	st.setReqErr(reqError{Kind: KindProviderTimeout, Attempt: 2, Provider: "Groq"})
	if st.lastReqErr.Kind != KindProviderTimeout {
		t.Fatalf("lastReqErr = %+v, want the later provider failure", st.lastReqErr)
	}

	st = &requestState{logData: &requestLogData{}}
	st.setReqErr(reqError{Kind: KindValidation, Attempt: 0, Provider: "Anthropic", Underlying: "refused"})
	st.setReqErr(reqError{Kind: KindValidation, Attempt: 1, Provider: "Anthropic Messages", Underlying: "refused again"})
	if st.lastReqErr.Attempt != 1 {
		t.Fatalf("lastReqErr = %+v, want the second refusal", st.lastReqErr)
	}
	st.setReqErr(reqError{Kind: KindProviderError, Attempt: 2, Provider: "OpenAI", Underlying: "HTTP 500"})
	if st.lastReqErr.Kind != KindProviderError {
		t.Fatalf("lastReqErr = %+v, want the provider failure to replace a refusal", st.lastReqErr)
	}

	// The same candidate's earlier busy skip says nothing about a route: its
	// refusal, once the slot freed and the body was built, is the verdict.
	st = &requestState{logData: &requestLogData{}}
	st.setReqErr(reqError{Kind: KindProviderSaturated, Attempt: 0, Provider: "Anthropic", Model: "claude-a", Detail: busySkipDetail})
	st.setReqErr(reqError{Kind: KindValidation, Attempt: 1, Provider: "Anthropic", Model: "claude-a", Underlying: "refused"})
	if st.lastReqErr.Kind != KindValidation {
		t.Fatalf("lastReqErr = %+v, want the same candidate's refusal", st.lastReqErr)
	}
	// One provider, two models: the model with a route for the body (a GPT on
	// OpenCode) failed provider-side, the Claude model beside it refused the
	// body. The failure stands.
	st = &requestState{logData: &requestLogData{}}
	st.setReqErr(reqError{Kind: KindProviderError, Attempt: 0, Provider: "OpenCode Go", Model: "gpt-6-luna", Underlying: "HTTP 502"})
	st.setReqErr(reqError{Kind: KindValidation, Attempt: 1, Provider: "OpenCode Go", Model: "claude-haiku-5-5", Underlying: "refused"})
	if st.lastReqErr.Kind != KindProviderError {
		t.Fatalf("lastReqErr = %+v, want the other model's provider failure kept", st.lastReqErr)
	}
	// Busy skips recorded after a routed candidate's failure displace it from
	// the running verdict; a refusal that would then become the verdict yields
	// to that failure, on the same-candidate replace as on a fresh record.
	st = &requestState{logData: &requestLogData{}}
	st.rateLimit = rateLimitVerdict{classified: true, class: rateLimitSaturated, retryAfter: 7 * time.Second}
	st.setReqErr(reqError{Kind: KindProviderError, Attempt: 0, Provider: "OpenAI", Model: "gpt", Underlying: "HTTP 502"})
	st.rateLimit = rateLimitVerdict{}
	st.setReqErr(reqError{Kind: KindProviderSaturated, Attempt: 1, Provider: "Anthropic", Model: "claude-a", Detail: busySkipDetail})
	st.setReqErr(reqError{Kind: KindProviderSaturated, Attempt: 2, Provider: "Anthropic Messages", Model: "claude-b", Detail: busySkipDetail})
	st.setReqErr(reqError{Kind: KindValidation, Attempt: 3, Provider: "Anthropic", Model: "claude-a", Underlying: "refused"})
	if st.lastReqErr.Kind != KindProviderSaturated || st.lastReqErr.Model != "claude-b" {
		t.Fatalf("lastReqErr = %+v, want the other candidate's skip kept over the refusal", st.lastReqErr)
	}
	st.setReqErr(reqError{Kind: KindValidation, Attempt: 4, Provider: "Anthropic Messages", Model: "claude-b", Underlying: "refused"})
	if st.lastReqErr.Kind != KindProviderError || st.lastReqErr.Provider != "OpenAI" || st.terminalRateLimit.retryAfter != 7*time.Second {
		t.Fatalf("lastReqErr = %+v (retry-after %v), want the routed failure back as the verdict with its verdict", st.lastReqErr, st.terminalRateLimit.retryAfter)
	}
	if st.lastErr != st.routedFailure.render() {
		t.Errorf("lastErr = %q, want the routed failure rendered", st.lastErr)
	}

	// A 429 the provider answered is a routed failure too, unlike the busy skip
	// recorded without a request: it comes back as the verdict with its own
	// Retry-After when the refusal would otherwise stand.
	st = &requestState{logData: &requestLogData{}}
	st.rateLimit = rateLimitVerdict{classified: true, class: rateLimitSaturated, retryAfter: 9 * time.Second}
	st.setReqErr(reqError{Kind: KindProviderSaturated, Attempt: 0, Provider: "OpenAI", Detail: "HTTP 429"})
	st.rateLimit = rateLimitVerdict{}
	st.setReqErr(reqError{Kind: KindProviderSaturated, Attempt: 1, Provider: "Anthropic", Model: "claude-a", Detail: busySkipDetail})
	st.setReqErr(reqError{Kind: KindValidation, Attempt: 2, Provider: "Anthropic", Model: "claude-a", Underlying: "refused"})
	if st.lastReqErr.Provider != "OpenAI" || st.lastReqErr.Kind != KindProviderSaturated || st.terminalRateLimit.retryAfter != 9*time.Second {
		t.Fatalf("lastReqErr = %+v (retry-after %v), want the provider's 429 back with its Retry-After", st.lastReqErr, st.terminalRateLimit.retryAfter)
	}
	// An attempt that timed out on its provider is a routed failure as well.
	st = &requestState{logData: &requestLogData{}}
	st.setReqErr(reqError{Kind: KindFailoverTimeout, Attempt: 0, Provider: "OpenAI", Underlying: "context deadline exceeded"})
	st.setReqErr(reqError{Kind: KindProviderSaturated, Attempt: 1, Provider: "Anthropic", Model: "claude-a", Detail: busySkipDetail})
	st.setReqErr(reqError{Kind: KindValidation, Attempt: 2, Provider: "Anthropic", Model: "claude-a", Underlying: "refused"})
	if st.lastReqErr.Kind != KindFailoverTimeout || st.lastReqErr.Provider != "OpenAI" {
		t.Fatalf("lastReqErr = %+v, want the timed-out attempt back as the verdict", st.lastReqErr)
	}
	// Busy skips alone are no routed failure: two refusals end as a refusal.
	st = &requestState{logData: &requestLogData{}}
	st.setReqErr(reqError{Kind: KindProviderSaturated, Attempt: 0, Provider: "Anthropic", Model: "claude-a", Detail: busySkipDetail})
	st.setReqErr(reqError{Kind: KindValidation, Attempt: 1, Provider: "Anthropic", Model: "claude-a", Underlying: "refused"})
	if st.lastReqErr.Kind != KindValidation {
		t.Fatalf("lastReqErr = %+v, want the refusal when only a busy skip preceded it", st.lastReqErr)
	}

	// This gateway's own internal error is not a route either.
	st = &requestState{logData: &requestLogData{}}
	st.setReqErr(reqError{Kind: KindInternal, Attempt: 0, Provider: "OpenAI", Underlying: "boom"})
	st.setReqErr(reqError{Kind: KindValidation, Attempt: 1, Provider: "Anthropic", Underlying: "refused"})
	if st.lastReqErr.Kind != KindValidation {
		t.Fatalf("lastReqErr = %+v, want the refusal over an internal error", st.lastReqErr)
	}
}

// The Retry-After the exhaustion path sends belongs to the verdict it renders:
// a 429 verdict recorded with a provider failure survives a later attempt that
// reset the per-attempt verdict and did not replace the error.
func TestSetReqErr_KeepsTheRateLimitVerdictOfTheRecordedError(t *testing.T) {
	st := &requestState{logData: &requestLogData{}}
	st.rateLimit = rateLimitVerdict{classified: true, class: rateLimitSaturated, retryAfter: 42 * time.Second}
	st.setReqErr(reqError{Kind: KindProviderSaturated, Attempt: 0, Provider: "OpenAI"})
	st.rateLimit = rateLimitVerdict{}
	st.setReqErr(reqError{Kind: KindValidation, Attempt: 1, Provider: "Anthropic", Underlying: "refused"})
	if st.terminalRateLimit.retryAfter != 42*time.Second {
		t.Fatalf("terminalRateLimit = %+v, want the 42s verdict of the kept error", st.terminalRateLimit)
	}
}

func TestReqErrorTerminalLogMessage(t *testing.T) {
	// Provider error across multiple candidates wraps with the "all N" prefix.
	provErr := reqError{Kind: KindProviderError, Attempt: 2, Provider: "groq", Detail: "HTTP 502"}
	if got := provErr.terminalLogMessage(true, 3); got != `all 3 providers failed; last error: provider "groq" returned HTTP 502 on attempt 3` {
		t.Errorf("multi-candidate provider error wrap = %q", got)
	}
	// Single provider: no "all N" wrap.
	if got := provErr.terminalLogMessage(false, 1); got != `provider "groq" returned HTTP 502 on attempt 3` {
		t.Errorf("single-candidate provider error = %q", got)
	}
	// Client disconnect is reported directly, never as "all providers failed",
	// even with multiple candidates.
	disc := reqError{Kind: KindClientDisconnect, Attempt: 0, Provider: "groq", Underlying: "EOF"}
	got := disc.terminalLogMessage(true, 3)
	if strings.Contains(got, "all 3 providers failed") {
		t.Errorf("client disconnect must not be reported as all-providers-failed: %q", got)
	}
	if !strings.Contains(got, "EOF") {
		t.Errorf("client disconnect terminal message dropped the underlying error: %q", got)
	}
}

func TestReqErrorTerminalClientMessage(t *testing.T) {
	if got := (reqError{Kind: KindClientDisconnect}).terminalClientMessage("m", true); got != "client disconnected" {
		t.Errorf("client disconnect client msg = %q", got)
	}
	if got := (reqError{Kind: KindFailoverTimeout}).terminalClientMessage("gpt-4", true); got != "request timed out for model gpt-4" {
		t.Errorf("timeout client msg = %q", got)
	}
	if got := (reqError{Kind: KindProviderError}).terminalClientMessage("gpt-4", true); got != "all providers failed for model gpt-4" {
		t.Errorf("failover provider error client msg = %q", got)
	}
	if got := (reqError{Kind: KindProviderError}).terminalClientMessage("gpt-4", false); got != "provider request failed for model gpt-4" {
		t.Errorf("single provider error client msg = %q", got)
	}
}
