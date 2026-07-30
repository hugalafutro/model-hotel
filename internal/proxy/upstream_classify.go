package proxy

import (
	"net/http"
	"strconv"
	"strings"
)

// classifyUpstreamError turns an upstream non-2xx response into a stable
// ErrorKind plus a short, gateway-authored reason for the client.
//
// Why this exists: every upstream failure used to be recorded as
// KindProviderError and reported to the caller as the bare "upstream provider
// returned HTTP 400". Three failures that need completely different operator
// responses were indistinguishable from each other on the wire and in metrics:
//
//   - a model the provider has retired (fix the catalog / stop routing to it)
//   - a model the account is not entitled to (top up, or change plan)
//   - a transient upstream fault (do nothing, it will pass)
//
// A catalog audit had to reconstruct that distinction by hand out of
// request_logs.error_message. Classifying it here makes it a queryable
// error_kind and a Prometheus label instead.
//
// IMPORTANT: this is observability only. Failover eligibility, circuit-breaker
// trips and quota handling stay purely status-code driven (see
// isFailoverEligible and the MiniMax 1008 -> 429 remap, which deliberately
// funnels balance errors into the rate-limit path so failover moves on).
// Returning a new kind here must never change where a request is routed.
//
// The returned reason is always gateway-authored static text. The upstream body
// is never echoed to the caller: it can quote the request back at us, and the
// no-request-content-in-logs rule extends to what we hand to clients.
//
// body must already be sanitized (util.SanitizeLogBody); matching is done on a
// lowercased copy and every pattern below was observed on a real provider
// response, cited per group.
func classifyUpstreamError(status int, body string) (ErrorKind, string) {
	b := strings.ToLower(body)

	// Model retired or never served by this provider. Google keeps shut-down
	// models in its /models listing and only fails at generation time
	// ("This model ... is no longer available"); OpenCode Zen answers
	// "Model X is not supported"; xAI says "Model not found".
	//
	// These phrases are specific enough to stand alone.
	for _, p := range []string{
		"is no longer available",
		"is not supported",
		"not supported on the full model list",
		"model not found",
		"unknown model",
		"is not found for api version",
	} {
		if strings.Contains(b, p) {
			return KindProviderModelGone, "the provider no longer serves this model"
		}
	}

	// "does not exist" is the OpenAI-family phrasing ("The model `x` does not
	// exist") but is far too generic on its own — a provider erroring about some
	// other entity ("the requested conversation does not exist") would otherwise
	// accrue gone-strikes against a perfectly live model. Require the body to be
	// talking about a model before treating it as one.
	if strings.Contains(b, "does not exist") && strings.Contains(b, "model") {
		return KindProviderModelGone, "the provider no longer serves this model"
	}

	// Account cannot pay for this model: Z.ai's coding-plan endpoint answers
	// 429 code 1113 "Insufficient balance or no resource package. Please
	// recharge." for models outside the subscription. Without this it is
	// indistinguishable from ordinary rate limiting, so it looks retryable
	// when it will never succeed until someone pays.
	//
	// 402 is the unambiguous signal and needs no body match at all. The phrases
	// are each a full sentence fragment from a real provider; a bare "billing"
	// was deliberately removed after review, because a transient fault naming a
	// billing subsystem ("billing_engine_timeout" on a 500) would have been
	// recorded as a permanent entitlement failure and sent an operator chasing a
	// provider_not_entitled spike that was really a provider outage.
	if status == http.StatusPaymentRequired {
		return KindProviderNotEntitled, "the provider rejected this request for billing or plan reasons"
	}
	for _, p := range []string{
		"insufficient balance",
		"no resource package",
		"please recharge",
		"exceeded your current quota",
	} {
		if strings.Contains(b, p) {
			return KindProviderNotEntitled, "the provider rejected this request for billing or plan reasons"
		}
	}

	// The provider understood us and refused the payload. Seen when a request
	// is sent in the wrong dialect: OpenCode Zen routes its Gemini models to
	// Google's native API, which rejects an OpenAI-shaped body with
	// "Invalid JSON request body: Missing key at [\"contents\"]".
	if status == 400 {
		for _, p := range []string{
			"invalid json request body",
			"invalid_request_error",
			"missing key",
			"unsupported parameter",
			"invalid argument",
		} {
			if strings.Contains(b, p) {
				return KindProviderBadRequest, "the provider rejected the request payload"
			}
		}
	}

	// Everything else, including "Upstream request failed" (an aggregator's own
	// backend fault) and any 5xx, stays the transient default.
	return KindProviderError, "the provider failed to serve this request"
}

// upstreamClientMessage renders the message sent to an API client for an
// upstream failure. It names the provider and status so a caller can act, and
// carries the classified reason, but never the provider's own body.
func upstreamClientMessage(providerName string, status int, reason string) string {
	if providerName == "" {
		return reason
	}
	return reason + " (provider " + providerName + ", upstream HTTP " + strconv.Itoa(status) + ")"
}
