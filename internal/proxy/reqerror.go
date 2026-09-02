package proxy

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

// ErrorKind classifies why a proxied request failed. It is the machine-readable
// contract for the request log and dashboard; the human-facing message is
// rendered FROM it and must never be parsed to recover the kind.
type ErrorKind string

const (
	// KindClientDisconnect means the calling client hung up before we responded.
	KindClientDisconnect ErrorKind = "client_disconnect"
	// KindProviderError is an upstream non-2xx response or a transport failure.
	// It stays the default for anything classifyUpstreamError cannot place,
	// including transient aggregator faults and every 5xx.
	KindProviderError ErrorKind = "provider_error"
	// KindProviderModelGone means the provider no longer serves the model. It is
	// a permanent condition an operator must fix (drop it from the catalog or
	// stop routing to it), unlike KindProviderError which usually passes on its
	// own. Google notably keeps retired models in its /models listing and only
	// fails at generation time, so discovery cannot see this coming.
	KindProviderModelGone ErrorKind = "provider_model_gone"
	// KindProviderNotEntitled means the account cannot pay for the model
	// (empty balance, or a model outside the subscription). Providers report it
	// as a 429, which makes it look like ordinary rate limiting even though
	// retrying can never succeed until someone tops up or changes plan.
	KindProviderNotEntitled ErrorKind = "provider_not_entitled"
	// KindProviderBadRequest means the provider understood the request and
	// refused the payload, normally a gateway bug (the wrong dialect for the
	// upstream route) rather than a provider fault.
	KindProviderBadRequest ErrorKind = "provider_bad_request"
	// KindProviderSaturated means the provider is alive and refusing on
	// capacity (concurrency slots, RPM, TPM). Retry in seconds. Distinct from
	// KindProviderQuotaExhausted, where retrying cannot succeed until a window
	// resets, and from KindProviderNotEntitled, where a person has to pay.
	KindProviderSaturated ErrorKind = "provider_saturated"
	// KindProviderQuotaExhausted means a usage window is spent (a session,
	// daily or weekly cap; a 5h coding-plan window). Retry after the window
	// resets. The difference from KindProviderNotEntitled is who fixes it:
	// time, versus a person topping up or changing plan.
	KindProviderQuotaExhausted ErrorKind = "provider_quota_exhausted"
	// KindProviderTimeout means the TTFT probe or stall watchdog fired: the
	// provider accepted the connection but did not produce output in time.
	KindProviderTimeout ErrorKind = "provider_timeout"
	// KindFailoverTimeout means the overall failover deadline expired.
	KindFailoverTimeout ErrorKind = "failover_timeout"
	// KindRetryTimeout means the param-strip retry's deadline expired.
	KindRetryTimeout ErrorKind = "retry_timeout"
	// KindHedgeSuperseded means the gateway itself abandoned this attempt because
	// another hedged candidate won the race. It is not a provider failure and
	// not a client hangup, since the client is still connected and served by the
	// winner, so it must never be confused with KindClientDisconnect.
	KindHedgeSuperseded ErrorKind = "hedge_superseded"
	// KindInternal is a gateway-internal failure (e.g. could not build the request).
	KindInternal ErrorKind = "internal"
	// KindValidation is a bad request from the client (malformed body, missing
	// model, unknown model, invalid model format).
	KindValidation ErrorKind = "validation"
	// KindAuth is an authorization failure (virtual key lacks access).
	KindAuth ErrorKind = "auth"
)

// reqError is the structured description of a single failed failover attempt,
// threaded through the loop as requestState.lastReqErr. The exhaustion path
// (failAllExhausted) renders it, possibly wrapped, into the terminal request
// log message, the client response, and the HTTP status code.
//
// Attempt is 0-based internally and always rendered 1-based for humans.
// Underlying preserves the real provider or transport error even when the
// attempt's terminal cause is a context cancellation, so the original failure
// is never silently dropped. Detail carries a short structured fragment such as
// "HTTP 500".
type reqError struct {
	Kind       ErrorKind
	Attempt    int
	Provider   string
	Underlying string
	Detail     string
	// Hint is an optional, actionable diagnosis appended to the rendered
	// message (e.g. a reverse-proxy idle-timeout hint when a zero-token stall's
	// connection was closed downstream before our own TTFT timer fired). It is
	// human-facing guidance, not a machine contract.
	Hint string
}

// cancelKind classifies a failure that is a context error rather than the
// provider misbehaving, and reports whether it was one. It is the package's
// single spelling of that rule: a narrower one checking only
// errors.Is(err, context.Canceled) misses context.DeadlineExceeded, which is
// what this gateway's own request_timeout produces mid-body-read, and a slow but
// healthy provider is then charged with a breaker failure.
//
// Two inputs, one question: the error itself, and the attempt's context going
// down underneath a read that reported something else.
//
// Whoever cancelled, it was not the provider: every kind this returns is
// excluded by providerAtFault, so a caller that classifies with this and then
// gates on providerAtFault needs no separate client-gone guard.
func cancelKind(ctx context.Context, err error) (ErrorKind, bool) {
	interrupted := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
	if !interrupted && ctx.Err() == nil {
		return "", false
	}
	if !interrupted {
		// The context went down underneath a read that reported something else.
		// The cancellation is still the reason there is no answer, and it is
		// still not the provider's doing.
		err = ctx.Err()
	}
	return cancelOriginToKind(resolveCancelOrigin(ctx, err)), true
}

// cancelOriginToKind maps an internal cancel-origin identifier (the value stored
// under ctxkeys.CancelOriginKey, also fed to humanReadableCancelOrigin) to its
// error kind.
func cancelOriginToKind(origin string) ErrorKind {
	switch origin {
	case "client_disconnect":
		return KindClientDisconnect
	case "failover_timeout":
		return KindFailoverTimeout
	case "retry_timeout":
		return KindRetryTimeout
	case "hedge_superseded":
		return KindHedgeSuperseded
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
		base := fmt.Sprintf("%s did not return a response in time on attempt %d", e.providerLabel(), n)
		if e.Hint != "" {
			base += "; " + e.Hint
		}
		return e.withUnderlying(base)
	case KindFailoverTimeout:
		return e.withUnderlying(fmt.Sprintf("request timed out while waiting on %s (attempt %d)", e.providerLabel(), n))
	case KindRetryTimeout:
		return e.withUnderlying(fmt.Sprintf("retry without unsupported parameters timed out on %s (attempt %d)", e.providerLabel(), n))
	case KindInternal:
		if e.Underlying != "" {
			return fmt.Sprintf("internal error on attempt %d: %s", n, e.Underlying)
		}
		return fmt.Sprintf("internal error on attempt %d", n)
	case KindValidation:
		return "invalid request"
	case KindAuth:
		return "authorization failed"
	case KindProviderModelGone:
		return e.withUnderlying(fmt.Sprintf("%s no longer serves this model (attempt %d)", e.providerLabel(), n))
	case KindProviderNotEntitled:
		return e.withUnderlying(fmt.Sprintf("%s rejected the request for billing or plan reasons on attempt %d", e.providerLabel(), n))
	case KindProviderSaturated:
		return e.withUnderlying(fmt.Sprintf("%s is busy (rate limited at capacity) on attempt %d", e.providerLabel(), n))
	case KindProviderQuotaExhausted:
		return e.withUnderlying(fmt.Sprintf("%s has spent its usage quota on attempt %d", e.providerLabel(), n))
	case KindProviderBadRequest:
		return e.withUnderlying(fmt.Sprintf("%s rejected the request payload on attempt %d", e.providerLabel(), n))
	case KindHedgeSuperseded:
		return e.withUnderlying(fmt.Sprintf("attempt %d to %s was superseded by a faster hedged attempt", n, e.providerLabel()))
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
	case KindClientDisconnect, KindFailoverTimeout, KindRetryTimeout, KindHedgeSuperseded:
		return last
	case KindProviderSaturated:
		// Busy, not broken: every provider is alive and at capacity, and "all
		// providers failed" sends the operator hunting an outage that is not
		// happening.
		if isFailover && numCandidates > 1 {
			return fmt.Sprintf("all %d providers busy; last error: %s", numCandidates, last)
		}
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
	case KindHedgeSuperseded:
		// Not reachable while a superseded attempt is always replaced by the
		// winner, but it must never render as "all providers failed".
		return fmt.Sprintf("request superseded for model %s", reqModel)
	case KindProviderSaturated:
		// Alive and at capacity: telling the caller everything "failed" makes
		// an immediate retry look pointless when it is what will work.
		if isFailover {
			return fmt.Sprintf("all providers busy for model %s, retry shortly", reqModel)
		}
		return fmt.Sprintf("provider busy for model %s, retry shortly", reqModel)
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
	// Truncate on rune boundaries, not bytes, so a multi-byte rune straddling
	// the cap is never split into invalid UTF-8 in the stored error_message.
	r := []rune(err.Error())
	if len(r) > maxLen {
		return string(r[:maxLen]) + "…"
	}
	return string(r)
}

// statusClientClosedRequest is nginx's non-standard 499 "Client Closed Request",
// which Go's net/http has no constant for. It goes in the request log and on the
// wire whenever the terminal cause is the client going away.
const statusClientClosedRequest = 499
