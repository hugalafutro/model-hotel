package proxy

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
)

// modelScopedErrorCodes name the model in the field itself, so a body carrying
// one is talking about the model the request asked for by construction.
//
// A deliberately small allowlist rather than a "model_*" pattern. A provider is
// free to invent "model_overloaded" or "model_rate_limited", and those are
// transient faults about a model that very much still exists — matching them by
// prefix would retire it. The list grows from payloads observed in /api/logs,
// which is the rule this whole change came from.
var modelScopedErrorCodes = map[string]bool{
	"model_not_found":     true,
	"model_not_supported": true,
}

// genericNotFoundTypes say something is missing without saying what, so they
// only count as a retirement alongside the requested model's own id.
//
// not_found_error is Anthropic's, forwarded verbatim by aggregators. It is used
// for any absent entity — a conversation, a file, a batch — so on its own it is
// far too weak to disable a model on.
var genericNotFoundTypes = map[string]bool{
	"not_found_error": true,
}

// structuredError is what a provider said about WHY it refused, in fields rather
// than prose.
//
// The three fields must come from ONE object. That is the invariant this type
// exists to carry, and it has been broken three separate ways while this was
// being written: a message read from anywhere in the body, then from a different
// level of the same document, then from a sibling object of the right one. Each
// is the same mistake — a code or type is a claim about whatever its own object
// is describing, and pairing it with a message from elsewhere invents a
// statement no provider made. The identity check that bounds a generic
// not-found type then reads the wrong text and retires a model nobody said
// anything about.
//
// Both extractors enforce it and neither may relax it: the parse takes the error
// object whole or the top level whole, and the scan works inside one delimited
// region.
type structuredError struct {
	code    string
	typ     string
	message string
}

// The scan fallback's patterns: a quoted string field by name, anchored on the
// key so a value that merely looks like one cannot be picked up.
//
// The value may contain escaped quotes, which is not a nicety: providers quote
// the model name inside the message they are already sending as JSON, so
// `"message":"model \"gpt-4\" not found"` is an ordinary shape, and a pattern
// that stopped at the first quote captured `model \` and lost the id. The escape
// handling matches jsonObjectBody's, so the region walker and the field readers
// agree about where a string ends.
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
// The unclosed case is not an error path, it is the point: this only runs on a
// body no parser would accept, which usually means SanitizeLogBody cut it
// mid-object. A truncated object has no siblings after it, so running to the end
// is exactly right there — and where the object DOES close, stopping at the brace
// is what keeps a sibling's message from being read as this error's.
//
// Braces inside strings are skipped, because a provider message is free to
// contain one ("expected { at position 4"), and a quote is only a delimiter when
// it is not escaped. Anything more than that — numbers, nesting rules, escape
// semantics beyond \" — belongs to the parser this function is the fallback for.
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
// The scoping is the whole point rather than tidiness. The payload this change
// exists for opens `{"type":"error","error":{"type":"not_found_error",…}}`, so a
// scan of the whole document finds the ENVELOPE's type first and reads "error" —
// which is not a retirement signal, and shadows the one that is. A truncated
// body is the normal case for a large error, so the fallback has to handle the
// real shape rather than a tidier one.
//
// Falling back to the whole document when no error object is found keeps a
// provider that reports its fields at the top level working; it is looser, and
// what the callers do with the result is what bounds it.
func scanStructuredError(body string) structuredError {
	// One region for all three fields, never a field-by-field fallback. Reading
	// the type from inside the error object and the message from outside it
	// pairs a real signal with an unrelated sentence, and the identity check
	// that is meant to bound the type then answers about the wrong text: a body
	// whose top-level message merely mentions the model would retire it. Fields
	// that do not travel together are not evidence about each other.
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
// Two passes, because the input cannot be trusted to be a document. The body
// reaching the classifier has been through util.SanitizeLogBody, which truncates
// at 10 000 bytes and will happily cut JSON mid-structure, and plenty of
// providers answer with HTML or plain text. So the parse is tried first, for its
// precision — it reads error.code and error.type from the error OBJECT, not from
// anywhere the strings happen to appear — and a key scan is the fallback.
//
// The fallback is the looser of the two and is scoped by what the callers do
// with it rather than by the scan itself: a code only counts if it is in a small
// allowlist, and a type only counts alongside the model's own id. A stray
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
		// scanStructuredError gives: a type from the error object paired with a
		// message from outside it is two unrelated statements, and the identity
		// check that bounds the type would then be reading the wrong text. The
		// error object wins whenever it said anything at all; a provider that
		// reports at the top level still works because then it said nothing.
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

// structuredModelGone reports whether the body's ERROR FIELDS say the requested
// model is gone, for the providers that answer with a code rather than a
// sentence.
//
// It exists because prose matching cannot reach these payloads at all. Zen
// forwards Anthropic's error verbatim, and the only retirement signal in it is
// error.type; the model id lives in error.message. modelGoneAbout requires the
// two to be adjacent with no clause break between them — a rule that is right
// for prose and structurally impossible to satisfy across two JSON fields, which
// are separated by the comma isClauseBreak treats as a boundary by design.
//
// Two tiers, and the difference between them is whether the field already names
// its subject:
//
//   - A model-scoped code is about the model by construction. There is no prose
//     to anchor to and none is wanted.
//   - A generic not-found type could as easily be a missing conversation, so it
//     needs the requested model named in the error's own message. Not merely
//     somewhere in the body: a provider echoing the request back would name the
//     model in it, and a not_found_error about something else entirely would
//     then read as this model's retirement.
//
// An empty model id claims nothing, for the same reason modelGoneAbout claims
// nothing: a retirement that cannot be attributed is not one this gateway will
// assert.
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
// IMPORTANT: this was observability only, and for ROUTING it still is —
// failover eligibility and quota handling stay purely status-code driven (see
// isFailoverEligible and the MiniMax 1008 -> 429 remap, which deliberately
// funnels balance errors into the rate-limit path so failover moves on), and
// returning a new kind here must never change where a request is routed.
//
// The CIRCUIT BREAKER is in that list too: it decides from the status alone
// (see breakerRecordAction), and the phrases below label the row without
// changing what it charges. Circuits are keyed per resolved upstream model, so
// the refusal that made the phrase lists load-bearing — a plan excluding one
// model, answered 429 — now darkens that model and leaves its siblings alone
// without anything having to read the sentence.
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

	// Model retired or never served by this provider, said in fields rather than
	// in a sentence. Ahead of the prose path because it is the more specific
	// claim: a provider that names a model-scoped code has already told us what
	// its message can only imply.
	//
	// The capability veto deliberately does not apply here. It exists to stop
	// "is not supported for this endpoint" reading as a retirement, which is a
	// property of prose; a structured model_not_supported is the provider
	// answering the question directly, and there is no qualifying clause to
	// misread.
	if structuredModelGone(b, modelID) {
		return KindProviderModelGone, "the provider no longer serves this model"
	}

	// Model retired or never served by this provider. modelGoneAbout requires
	// the requested model's own id beside the phrase AND that the phrase is not
	// merely refusing one capability — both checks are per phrase, so an
	// unrelated sentence elsewhere in the body can neither create the verdict
	// nor suppress it.
	if modelGoneAbout(b, modelID) {
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
