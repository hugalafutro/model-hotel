package proxy

import (
	"errors"
	"net/http"
	"strings"
	"testing"
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
			want: `retry without unsupported parameters timed out on provider "groq" (attempt 1)`,
		},
		{
			name: "internal with underlying",
			err:  reqError{Kind: KindInternal, Attempt: 0, Underlying: "bad url"},
			want: `internal error on attempt 1: bad url`,
		},
		{
			name: "no provider name falls back to generic phrase",
			err:  reqError{Kind: KindProviderError, Attempt: 0},
			want: `the provider failed on attempt 1`,
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
	}
	for kind, want := range cases {
		if got := (reqError{Kind: kind}).terminalStatus(); got != want {
			t.Errorf("terminalStatus(%q) = %d, want %d", kind, got, want)
		}
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
