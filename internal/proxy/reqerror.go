package proxy

import (
	"fmt"
	"net/http"
)

// ErrorKind classifies why a proxied request failed. It is the machine-readable
// contract for the request log and dashboard; the human-facing message is
// rendered FROM it and must never be parsed to recover the kind. See
// plans/logging-and-errors-overhaul.md.
type ErrorKind string

const (
	// KindClientDisconnect means the calling client hung up before we responded.
	KindClientDisconnect ErrorKind = "client_disconnect"
	// KindProviderError is an upstream non-2xx response or a transport failure.
	KindProviderError ErrorKind = "provider_error"
	// KindProviderTimeout means the TTFT probe or stall watchdog fired — the
	// provider accepted the connection but did not produce output in time.
	KindProviderTimeout ErrorKind = "provider_timeout"
	// KindFailoverTimeout means the overall failover deadline expired.
	KindFailoverTimeout ErrorKind = "failover_timeout"
	// KindRetryTimeout means the param-strip retry's deadline expired.
	KindRetryTimeout ErrorKind = "retry_timeout"
	// KindInternal is a gateway-internal failure (e.g. could not build the request).
	KindInternal ErrorKind = "internal"
)

// reqError is the structured description of a single failed failover attempt,
// threaded through the loop as requestState.lastReqErr. The exhaustion path
// (failAllExhausted) renders it — possibly wrapped — into the terminal request
// log message, the client response, and the HTTP status code.
//
// Attempt is 0-based internally and always rendered 1-based for humans.
// Underlying preserves the real provider/transport error even when the
// attempt's terminal cause is a context cancellation, so the original failure
// is never silently dropped (the motivating bug). Detail carries a short
// structured fragment such as "HTTP 500".
type reqError struct {
	Kind       ErrorKind
	Attempt    int
	Provider   string
	Underlying string
	Detail     string
}

// cancelOriginToKind maps an internal cancel-origin identifier (the value
// stored under ctxkeys.CancelOriginKey, also fed to humanReadableCancelOrigin)
// to its error kind.
func cancelOriginToKind(origin string) ErrorKind {
	switch origin {
	case "client_disconnect":
		return KindClientDisconnect
	case "failover_timeout":
		return KindFailoverTimeout
	case "retry_timeout":
		return KindRetryTimeout
	default:
		return KindInternal
	}
}

// providerLabel renders the provider name for prose, falling back to a generic
// phrase when the failure is not attributable to a named provider.
func (e reqError) providerLabel() string {
	if e.Provider == "" {
		return "the provider"
	}
	return fmt.Sprintf("provider %q", e.Provider)
}

// withUnderlying appends the preserved real error when one exists, so a
// higher-level cause (disconnect, timeout) never hides the provider error that
// triggered it.
func (e reqError) withUnderlying(msg string) string {
	if e.Underlying == "" {
		return msg
	}
	return msg + "; last provider error: " + e.Underlying
}

// render produces a causally-ordered, human-readable description of this single
// attempt's failure (1-based attempt number, lowercase OpenAI-style fragment).
func (e reqError) render() string {
	n := e.Attempt + 1
	switch e.Kind {
	case KindClientDisconnect:
		if e.Underlying != "" {
			return fmt.Sprintf("client disconnected while retrying %s (attempt %d); last provider error: %s", e.providerLabel(), n, e.Underlying)
		}
		return fmt.Sprintf("client disconnected during attempt %d to %s", n, e.providerLabel())
	case KindProviderTimeout:
		return e.withUnderlying(fmt.Sprintf("%s did not return a response in time on attempt %d", e.providerLabel(), n))
	case KindFailoverTimeout:
		return e.withUnderlying(fmt.Sprintf("request timed out while waiting on %s (attempt %d)", e.providerLabel(), n))
	case KindRetryTimeout:
		return e.withUnderlying(fmt.Sprintf("retry without unsupported parameters timed out on %s (attempt %d)", e.providerLabel(), n))
	case KindInternal:
		if e.Underlying != "" {
			return fmt.Sprintf("internal error on attempt %d: %s", n, e.Underlying)
		}
		return fmt.Sprintf("internal error on attempt %d", n)
	default: // KindProviderError and any unclassified failure
		if e.Detail != "" {
			return fmt.Sprintf("%s returned %s on attempt %d", e.providerLabel(), e.Detail, n)
		}
		if e.Underlying != "" {
			return fmt.Sprintf("%s failed on attempt %d: %s", e.providerLabel(), n, e.Underlying)
		}
		return fmt.Sprintf("%s failed on attempt %d", e.providerLabel(), n)
	}
}

// terminalStatus is the HTTP status recorded (and written) when a request
// exhausts with this error as its last cause, per the truth-in-status-codes
// rule: a client hangup is 499, an exceeded timeout is 504, and a genuine
// provider/transport failure is 502.
func (e reqError) terminalStatus() int {
	switch e.Kind {
	case KindClientDisconnect:
		return statusClientClosedRequest
	case KindFailoverTimeout, KindRetryTimeout:
		return http.StatusGatewayTimeout
	default:
		return http.StatusBadGateway
	}
}

// terminalLogMessage renders the request-log error_message for an exhausted
// request. For genuine provider failures across multiple candidates it wraps
// the last attempt's description ("all N providers failed; last error: …"); for
// a terminal client disconnect or timeout it reports that cause directly, since
// "all providers failed" would misattribute the failure.
func (e reqError) terminalLogMessage(isFailover bool, numCandidates int) string {
	last := e.render()
	switch e.Kind {
	case KindClientDisconnect, KindFailoverTimeout, KindRetryTimeout:
		return last
	default:
		if isFailover && numCandidates > 1 {
			return fmt.Sprintf("all %d providers failed; last error: %s", numCandidates, last)
		}
		return last
	}
}

// terminalClientMessage renders the message sent to the API client. It is
// intentionally coarser than the log message (no internal attempt numbers) but
// agrees on the cause, so the dashboard and the client tell the same story.
func (e reqError) terminalClientMessage(reqModel string, isFailover bool) string {
	switch e.Kind {
	case KindClientDisconnect:
		return "client disconnected"
	case KindFailoverTimeout, KindRetryTimeout:
		return fmt.Sprintf("request timed out for model %s", reqModel)
	default:
		if isFailover {
			return fmt.Sprintf("all providers failed for model %s", reqModel)
		}
		return fmt.Sprintf("provider request failed for model %s", reqModel)
	}
}

// errString renders an error as a bounded string for the Underlying field,
// returning "" for a nil error. Transport/context errors are short, but the cap
// guards against an unexpectedly long provider error leaking unbounded text
// into the request log.
func errString(err error) string {
	if err == nil {
		return ""
	}
	const maxLen = 500
	s := err.Error()
	if len(s) > maxLen {
		return s[:maxLen] + "…"
	}
	return s
}

// statusClientClosedRequest is nginx's non-standard 499 "Client Closed Request".
// Go's net/http has no constant for it. Used (in the request log and on the
// wire) whenever the terminal cause is the client going away.
const statusClientClosedRequest = 499
