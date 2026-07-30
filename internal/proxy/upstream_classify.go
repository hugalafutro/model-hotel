package proxy

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// modelGonePattern matches a retired-model phrase only when the body names a
// model ahead of it on the same line. Anchoring on "model" first is what keeps
// a capability or parameter rejection ("... is not supported for this model")
// from being read as a dead model and auto-disabling a healthy one. Matched
// against a lowercased body.
var modelGonePattern = regexp.MustCompile(
	`model[^\n]{0,120}?(is no longer available|is not supported|does not exist|is not found for api version)`)

// modelCapabilityRefusal matches the other shape that names a model before a
// rejection phrase: the provider still serves the model, it just will not do
// THIS with it. "Model X is not supported for this operation" and "... for this
// endpoint" both satisfy modelGonePattern — the model is named first, so the
// trailing-model guard does not catch them — yet neither says the model is
// retired, and three of them would disable a live model.
//
// A trailing qualifier is therefore a veto, checked after modelGonePattern
// matches. It cannot be folded into that pattern as a negative lookahead
// because RE2 has none, and it must not be a blanket "any trailing text"
// rule: real retirement messages continue past the phrase too
// ("...does not exist or you do not have access to it").
//
// Note that Zen's "not supported on the full model list" is deliberately not
// vetoed — "full model list" is not a capability, and that phrase is matched
// as a standalone retirement signal anyway.
var modelCapabilityRefusal = regexp.MustCompile(
	`(is not supported|is no longer available|is not available) (for|with|on|in) (this |your |that |the )?` +
		`(operation|endpoint|method|route|api|api version|request|request type|mode|task|region|plan|tier|account|subscription)`)

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

	// Model retired or never served by this provider. Only these phrases name a
	// model inherently, so only these are safe to match anywhere in the body.
	for _, p := range []string{
		"model not found",
		"unknown model",
		"not supported on the full model list",
	} {
		if strings.Contains(b, p) {
			return KindProviderModelGone, "the provider no longer serves this model"
		}
	}

	// Everything else is a generic failure phrase that only means "this model is
	// gone" when it is talking about the model. Matching them loose is how a
	// healthy model gets auto-disabled: "Parameter 'temperature' is not
	// supported", "this operation is not supported in your region", "the
	// requested conversation does not exist" would each have counted as proof
	// the model no longer exists, and three of them retire it from routing.
	//
	// modelGonePattern therefore requires the word "model" to appear BEFORE the
	// phrase, within one line, which is how every real payload reads:
	//
	//	This model models/gemini-2.0-flash is no longer available   (Google)
	//	Model gemini-3-pro is not supported                         (OpenCode Zen)
	//	The model `gpt-4.5-preview` does not exist                  (OpenAI)
	//	models/gemini-embedding-001 is not found for API version    (Google)
	//
	// while "... is not supported for this model" — the parameter-error shape,
	// where "model" trails the phrase — deliberately does not match.
	// The veto runs second: a body can name a model ahead of a rejection phrase
	// and still only be refusing one capability ("Model X is not supported for
	// this operation"), which must not retire a model that is serving fine.
	if modelGonePattern.MatchString(b) && !modelCapabilityRefusal.MatchString(b) {
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
