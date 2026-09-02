package proxy

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// modelScopedErrorCodes name the model in the field itself, so a body carrying
// one is talking about the model the request asked for.
//
// An allowlist rather than a "model_*" prefix match: "model_overloaded" and
// "model_rate_limited" are transient faults about a model that still exists.
var modelScopedErrorCodes = map[string]bool{
	"model_not_found":     true,
	"model_not_supported": true,
}

// genericNotFoundTypes say something is missing without saying what, so they
// only count as a retirement alongside the requested model's own id.
//
// not_found_error is Anthropic's, forwarded verbatim by aggregators, and covers
// any absent entity (a conversation, a file, a batch), so on its own it is far
// too weak to disable a model on.
var genericNotFoundTypes = map[string]bool{
	"not_found_error": true,
}

// structuredError is what a provider said about why it refused, in fields
// rather than prose.
//
// The three fields must come from ONE object: a code or type is a claim about
// whatever its own object describes, and pairing it with a message from
// elsewhere invents a statement no provider made, so the identity check that
// bounds a generic not-found type reads the wrong text. Both extractors enforce
// this and neither may relax it: the parse takes the error object whole or the
// top level whole, and the scan works inside one delimited region.
type structuredError struct {
	code    string
	typ     string
	message string
}

// The scan fallback's patterns: a quoted string field by name, anchored on the
// key so a value that merely looks like one cannot be picked up.
//
// The value may contain escaped quotes: providers quote the model name inside a
// message they are already sending as JSON, so `"message":"model \"gpt-4\" not
// found"` is an ordinary shape and a pattern stopping at the first quote loses
// the id. The escape handling matches jsonObjectBody's, so the region walker
// and the field readers agree about where a string ends.
var (
	scanCode    = regexp.MustCompile(`"code"\s*:\s*"((?:[^"\\]|\\.)*)"`)
	scanType    = regexp.MustCompile(`"type"\s*:\s*"((?:[^"\\]|\\.)*)"`)
	scanMessage = regexp.MustCompile(`"message"\s*:\s*"((?:[^"\\]|\\.)*)"`)
	// errorObjectStart finds where the error object opens, so the scan reads
	// the fields belonging to it rather than the first ones in the document.
	errorObjectStart = regexp.MustCompile(`"error"\s*:\s*\{`)
)

// jsonObjectBody returns the contents of the object whose opening brace ends at
// start, up to its matching close brace, or to the end of the input when the
// object never closes.
//
// The unclosed case is normal rather than an error path: this only runs on a
// body no parser would accept, usually one SanitizeLogBody cut mid-object, and
// a truncated object has no siblings after it. Where the object does close,
// stopping at the brace keeps a sibling's message from being read as this
// error's.
//
// Braces inside strings are skipped, since a provider message may contain one
// ("expected { at position 4"), and a quote delimits only when unescaped.
// Anything beyond that belongs to the parser this function backs up.
func jsonObjectBody(body string, start int) string {
	depth := 1
	inString, escaped := false, false
	for i := start; i < len(body); i++ {
		c := body[i]
		switch {
		case escaped:
			escaped = false
		case c == '\\' && inString:
			escaped = true
		case c == '"':
			inString = !inString
		case inString:
			// Braces inside a string are text, not structure.
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return body[start:i]
			}
		}
	}
	return body[start:]
}

// scanStructuredError extracts the three fields from a body no parser would
// accept, scoped to the error object when one can be found.
//
// The scoping matters: a payload opening
// `{"type":"error","error":{"type":"not_found_error",…}}` yields the ENVELOPE's
// type first on a whole-document scan, which is not a retirement signal and
// shadows the one that is.
//
// With no error object the whole document is scanned, so a provider reporting
// its fields at the top level still works; the callers bound how loose that is.
func scanStructuredError(body string) structuredError {
	// One region for all three fields, never a field-by-field fallback: a type
	// from inside the error object paired with a message from outside it is two
	// unrelated statements, and the identity check meant to bound the type then
	// reads the wrong text. Fields that do not travel together are not evidence
	// about each other.
	region := body
	if at := errorObjectStart.FindStringIndex(body); at != nil {
		region = jsonObjectBody(body, at[1])
	}
	first := func(re *regexp.Regexp) string {
		if m := re.FindStringSubmatch(region); m != nil {
			return m[1]
		}
		return ""
	}
	return structuredError{code: first(scanCode), typ: first(scanType), message: first(scanMessage)}
}

// parseStructuredError pulls the error code, type and message out of a body,
// and never fails: an absent field means no signal, which every caller treats as
// "nothing established".
//
// Two passes, because the input cannot be trusted to be a document: the body
// has been through util.SanitizeLogBody, which truncates at 10 000 bytes and
// can cut JSON mid-structure, and plenty of providers answer with HTML or plain
// text. The parse runs first for its precision, reading error.code and
// error.type from the error OBJECT rather than from anywhere the strings
// appear; the key scan is the fallback.
//
// The callers bound the looser scan: a code counts only if it is in a small
// allowlist, and a type only alongside the model's own id, so a stray
// "type":"function" in an echoed tool definition matches neither.
func parseStructuredError(body string) structuredError {
	var envelope struct {
		Error struct {
			Code    any    `json:"code"`
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
		Code    any    `json:"code"`
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	if json.Unmarshal([]byte(body), &envelope) == nil {
		// A code may be a string ("model_not_found") or a number; only the
		// string form carries a name worth matching.
		codeOf := func(v any) string {
			s, _ := v.(string)
			return s
		}
		nested := structuredError{
			code:    codeOf(envelope.Error.Code),
			typ:     envelope.Error.Type,
			message: envelope.Error.Message,
		}
		// One level or the other, never a mixture, for the reason
		// scanStructuredError gives. The error object wins whenever it says
		// anything at all; a provider reporting at the top level still works,
		// because then the error object said nothing.
		if nested != (structuredError{}) {
			return nested
		}
		return structuredError{
			code:    codeOf(envelope.Code),
			typ:     envelope.Type,
			message: envelope.Message,
		}
	}

	return scanStructuredError(body)
}

// normalizeModelID reduces an upstream model id to the form the body is matched
// against: lowercased, trimmed, and cut to the last path segment, because
// providers report Google models with the "models/" prefix the id omits and the
// distinctive part is the tail either way.
func normalizeModelID(modelID string) string {
	id := strings.ToLower(strings.TrimSpace(modelID))
	if i := strings.LastIndex(id, "/"); i >= 0 && i+1 < len(id) {
		id = id[i+1:]
	}
	return id
}

// structuredModelGone reports whether the body's error FIELDS say the requested
// model is gone, for the providers that answer with a code rather than a
// sentence.
//
// Prose matching cannot reach these payloads: OpenCode Zen forwards Anthropic's
// error verbatim, where the only retirement signal is error.type while the
// model id lives in error.message, and modelGoneAbout requires the two adjacent
// with no clause break between them.
//
// Two tiers, differing in whether the field already names its subject:
//
//   - A model-scoped code is about the model by construction, so no prose
//     anchor is wanted.
//   - A generic not-found type could as easily be a missing conversation, so it
//     needs the requested model named in the error's own message. Not merely
//     somewhere in the body: a provider echoing the request back would name the
//     model in it, and a not_found_error about something else would then read
//     as this model's retirement.
//
// An empty model id claims nothing: a retirement that cannot be attributed is
// not asserted.
func structuredModelGone(body, modelID string) bool {
	id := normalizeModelID(modelID)
	if id == "" || body == "" {
		return false
	}
	e := parseStructuredError(body)
	if modelScopedErrorCodes[e.code] {
		return true
	}
	return genericNotFoundTypes[e.typ] && namesModelAllowingVersion(e.message, id)
}

// modelGoneAbout reports whether the body is the provider asserting that THIS
// model, the one the request asked for, is gone.
//
// The model's own id must sit next to the gone-phrase. Without that, a body
// merely mentioning some other missing model, or echoing request content
// containing "unknown model", counts as proof the requested model is retired,
// and three such responses disable it.
//
// modelID is the upstream model id for the attempt. An empty one verifies
// nothing, so nothing is claimed and the caller gets the generic provider
// error.
func modelGoneAbout(body, modelID string) bool {
	id := normalizeModelID(modelID)
	if id == "" || body == "" {
		return false
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
			// The capability veto is applied per phrase, so a body that both
			// retires this model and refuses some other model's capability
			// still classifies on the retirement.
			if phraseIsAbout(body, pos, pos+len(verb), lo, hi, id) && !refusesCapabilityAt(body, pos) {
				return true
			}
			off = pos + len(verb)
		}
	}
	return false
}

// classifyUpstreamError turns an upstream non-2xx response into a stable
// ErrorKind plus a short, gateway-authored reason for the client. It keeps
// apart the three failures that need different operator responses: a model the
// provider has retired, a model the account is not entitled to, and a transient
// upstream fault.
//
// For ROUTING this is observability only. Failover eligibility is purely
// status-code driven (isFailoverEligible, plus the MiniMax 1008 -> 429 remap
// that funnels balance errors into the rate-limit path so failover moves on),
// and a new kind here must never change where a request is routed.
//
// The circuit breaker decides from the status alone for every status but 429
// (breakerRecordAction); a 429's charge follows the classified claim, saturated
// being a no-op and exhausted opening at once, through classifyRateLimit and
// recordBreakerOutcome, which share the rateLimitPhrases table with this
// function.
//
// The returned reason is always gateway-authored static text. The upstream body
// is never echoed to the caller: it can quote the request back, and the
// no-request-content-in-logs rule extends to what clients are handed.
//
// modelID is the upstream model id this attempt asked for. It is required for
// the retirement verdict: the body has to be the provider talking about THAT
// model, not merely text that reads like a retirement.
//
// body must already be sanitized (util.SanitizeLogBody); matching runs on a
// lowercased copy, and every phrase below was observed on a real provider
// response.
func classifyUpstreamError(status int, body, modelID string) (ErrorKind, string) {
	b := strings.ToLower(body)

	// Model retired or never served by this provider, said in fields rather than
	// in a sentence. Ahead of the prose path because it is the more specific
	// claim.
	//
	// The capability veto does not apply here: it exists to stop "is not
	// supported for this endpoint" reading as a retirement, which is a property
	// of prose, and a structured model_not_supported has no qualifying clause to
	// misread.
	if structuredModelGone(b, modelID) {
		return KindProviderModelGone, "the provider no longer serves this model"
	}

	// Model retired or never served by this provider, said in prose.
	// modelGoneAbout requires the requested model's own id beside the phrase and
	// that the phrase is not merely refusing one capability. Both checks are per
	// phrase, so an unrelated sentence elsewhere in the body can neither create
	// the verdict nor suppress it.
	if modelGoneAbout(b, modelID) {
		return KindProviderModelGone, "the provider no longer serves this model"
	}

	// The account cannot pay for this model: Z.ai's coding-plan endpoint answers
	// 429 code 1113 "Insufficient balance or no resource package. Please
	// recharge." for models outside the subscription, which is otherwise
	// indistinguishable from ordinary rate limiting and looks retryable when it
	// can only succeed once someone pays.
	//
	// 402 is unambiguous and needs no body match at all. The phrases are each a
	// full sentence fragment from a real provider, never a bare word like
	// "billing": a transient fault naming a billing subsystem
	// ("billing_engine_timeout" on a 500) must not read as a permanent
	// entitlement failure.
	if status == http.StatusPaymentRequired {
		return KindProviderNotEntitled, "the provider rejected this request for billing or plan reasons"
	}
	// The entitlement phrases come from the shared rate-limit table
	// (rateLimitPhrases), so the breaker's exhaustion verdict and this label
	// can never disagree about what a balance error looks like.
	for _, p := range entitledRateLimitPhrases() {
		if strings.Contains(b, p) {
			return KindProviderNotEntitled, "the provider rejected this request for billing or plan reasons"
		}
	}

	// A 429's remaining claims (saturated vs quota-exhausted) are not classified
	// here: this function cannot read settings, and the
	// rate_limit_classify_enabled master switch has to restore the unclassified
	// labels bit for bit when it is off. rateLimitTerminalKind refines the kind
	// from the attempt's verdict at the terminal forward instead.

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
