package proxy

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// modelGoneVerbs are the phrasings a provider uses to say a model is gone. They
// are matched only in the immediate neighbourhood of the requested model's own
// id — see modelGoneAbout for why that constraint, not the wording, is what
// makes this safe.
var modelGoneVerbs = []string{
	"is no longer available",
	"is not supported",
	"does not exist",
	"is not found for api version",
	"model not found",
	"unknown model",
}

// modelPhraseWindow is how many characters may separate the model id from the
// phrase asserting it is gone. Wide enough for the real payloads, which put them
// adjacent or a few words apart, and far too narrow to bridge an unrelated
// sentence elsewhere in the body.
const modelPhraseWindow = 80

// modelCapabilityRefusal matches the shape that names a model before a rejection
// phrase and yet is NOT a retirement: the provider still serves the model, it
// just will not do THIS with it. "Model X is not supported for this operation"
// and "... for this endpoint" both read like a retirement, and three of them
// would disable a live model.
//
// It is a veto applied after a positive match rather than part of the pattern,
// because RE2 has no negative lookahead. It must not be a blanket "any trailing
// text disqualifies" rule either: real retirement messages continue past the
// phrase too ("... does not exist or you do not have access to it"), and Zen's
// "not supported on the full model list" is a retirement whose qualifier simply
// is not a capability.
var modelCapabilityRefusal = regexp.MustCompile(
	`(is not supported|is no longer available|is not available) (for|with|on|in) (this |your |that |the )?` +
		`(operation|endpoint|method|route|api|api version|request|request type|mode|task|region|plan|tier|account|subscription)`)

// isModelIDChar reports whether b can appear inside a model identifier.
//
// It defines what counts as a neighbouring character for namesModelID, and the
// membership is chosen from how providers actually spell ids:
//
//   - Letters, digits, '.', '-' and '_' are the ordinary body of an id. A
//     neighbouring one means the match is part of a LONGER id, which is a
//     different model.
//   - ':' and '@' pin a variant apart from its base ("llama3:8b",
//     "text-bison@001"), so they are id characters too.
//   - '/' is deliberately absent. Providers path-qualify the same model
//     ("models/gemini-2.0-flash", "openai/gpt-4"), so a slash neighbour still
//     refers to this model and must not disqualify the match.
func isModelIDChar(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '.', b == '-', b == '_', b == ':', b == '@':
		return true
	default:
		return false
	}
}

// namesModelID reports whether id appears in body[lo:hi] as a WHOLE identifier
// rather than as part of a longer one.
//
// A plain substring test is not enough, and the failure is not exotic: model
// families are named by extension, so "gpt-4" sits inside "gpt-4.1" and
// "gemini-3-flash" inside "gemini-3-flash-lite". An error about the newer model
// would then read as proof the older one is retired, and three of them disable a
// model that is serving perfectly well — the exact outcome this classifier
// exists to avoid causing.
//
// Boundaries are checked against the FULL body, not the window, because the
// window is a fixed-width cut: an id sliced by hi would otherwise look like it
// ended cleanly there when the real text continues into a longer id.
func namesModelID(body string, lo, hi int, id string) bool {
	for off := lo; off+len(id) <= hi; {
		at := strings.Index(body[off:hi], id)
		if at < 0 {
			return false
		}
		pos := off + at
		startsClean := pos == 0 || !isModelIDChar(body[pos-1])
		end := pos + len(id)
		endsClean := end == len(body) || !isModelIDChar(body[end])
		if startsClean && endsClean {
			return true
		}
		// Advance by one rather than by len(id): ids can overlap themselves.
		off = pos + 1
	}
	return false
}

// modelGoneAbout reports whether the body is the provider asserting that THIS
// model — the one the request asked for — is gone.
//
// Requiring the model's own id next to the phrase is the constraint that makes
// this safe, and it replaces four rounds of trying to get the wording alone
// right. Matching prose without it meant any body that merely mentioned a
// different missing model, or echoed request content containing "unknown
// model", was read as proof the requested model had been retired — and three
// such responses disable it. No amount of phrase-tuning fixes that, because the
// text being matched was never required to be about the model in question.
//
// modelID is the upstream model id for the attempt. When it is empty nothing can
// be verified, so nothing is claimed: the caller gets the generic provider
// error rather than a retirement it cannot substantiate.
func modelGoneAbout(body, modelID string) bool {
	id := strings.ToLower(strings.TrimSpace(modelID))
	if id == "" || body == "" {
		return false
	}
	// Providers report Google models with the "models/" prefix the id omits, so
	// compare on the last path segment; it is the distinctive part either way.
	if i := strings.LastIndex(id, "/"); i >= 0 && i+1 < len(id) {
		id = id[i+1:]
	}

	for _, verb := range modelGoneVerbs {
		for off := 0; off < len(body); {
			at := strings.Index(body[off:], verb)
			if at < 0 {
				break
			}
			pos := off + at
			lo := max(0, pos-modelPhraseWindow)
			hi := min(len(body), pos+len(verb)+modelPhraseWindow)
			if namesModelID(body, lo, hi, id) {
				return true
			}
			off = pos + len(verb)
		}
	}
	return false
}

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
// modelID is the upstream model id this attempt asked for. It is required for
// the retirement verdict: the body has to be the provider talking about THAT
// model, not merely text that reads like a retirement.
//
// body must already be sanitized (util.SanitizeLogBody); matching is done on a
// lowercased copy and every phrase below was observed on a real provider
// response.
func classifyUpstreamError(status int, body, modelID string) (ErrorKind, string) {
	b := strings.ToLower(body)

	// Model retired or never served by this provider. The verdict requires the
	// requested model's own id beside the phrase (modelGoneAbout), and then that
	// the phrase is not merely refusing one capability (the veto).
	if modelGoneAbout(b, modelID) && !modelCapabilityRefusal.MatchString(b) {
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
