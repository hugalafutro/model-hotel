package proxy

import (
	"encoding/json"
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
//
// Anchored, and applied to ONE phrase rather than to the whole body. A response
// can say two things at once — "Model gemini-2.0-flash does not exist.
// Separately, tool-only-model is not supported for this endpoint." — and vetoing
// on a match anywhere let the second sentence suppress the first. The model
// would then never accrue strikes and would stay routable while the provider
// was plainly saying it is gone. The veto now only cancels the phrase it
// actually qualifies.
// The qualifier is allowed to be several words: providers write "on your
// current plan" and "for this specific operation" as readily as the bare forms,
// and matching only the bare ones let the wordier phrasings through as
// retirements — retiring a model that is still served for other requests. The
// run of filler words cannot cross punctuation, so it stays inside one clause,
// and it still has to END on a whole capability noun. That is what keeps genuine
// retirements out: Zen's "is not supported on the full model list" walks the
// same filler run and lands on "model", which is not a capability, so it remains
// a retirement.
//
// The trailing \b is load-bearing rather than tidiness. Without it "mode"
// matches the front of "model", and that one prefix turns Zen's real retirement
// payload into a capability refusal — the widened filler run is what lets the
// pattern reach that far into the sentence in the first place.
var modelCapabilityRefusal = regexp.MustCompile(
	`^(is not supported|is no longer available|is not available) (for|with|on|in) ` +
		`((this|that|the|your|our|any) )?([a-z]+ ){0,3}` +
		`(operation|endpoint|method|route|api|api version|request|request type|mode|task|region|plan|tier|account|subscription)s?\b`)

// refusesCapabilityAt reports whether the phrase starting at pos is a capability
// refusal rather than a retirement. The pattern is anchored, so this tests that
// one position and no other.
func refusesCapabilityAt(body string, pos int) bool {
	return modelCapabilityRefusal.MatchString(body[pos:])
}

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
func isWholeIDAt(body string, pos, end int) bool {
	startsClean := pos == 0 || !isModelIDChar(body[pos-1])
	endsClean := end == len(body) || !isModelIDChar(body[end])
	return startsClean && endsClean
}

// versionSuffix matches the tail a provider adds when it resolves an alias to a
// dated snapshot: dash-separated runs of digits, at most three of them.
//
// The run count is bounded and the digits are counted separately (see
// minVersionSuffixDigits) rather than being spelled `(-\d{4,}){1,3}`, because
// a segmented date does not have four digits in every run: "-2024-04-09" is
// "-2024", "-04", "-09". Requiring four per run rejected exactly that case.
var versionSuffix = regexp.MustCompile(`^(-\d+){1,3}`)

// minVersionSuffixDigits is what separates a date from a size or a variant
// number, and it is the whole safety of the alias rule.
//
// "-20250514" and "-0613" and "-2024-04-09" are dates; "-70" (llama-3-70) and
// "-32" are not, and neither is ".1" (gpt-4.1), which the pattern rejects on
// shape before this is consulted. Four is the smallest threshold that admits a
// bare year and excludes the two-digit variant numbers providers actually use.
// It is the one number here I would expect to revisit, and it should be revisited
// from a real payload rather than from speculation.
const minVersionSuffixDigits = 4

// idEndAllowingVersion reports where an occurrence of the requested id at
// [pos, end) really ends, allowing the provider to have named a dated snapshot
// of it, and whether that occurrence names the requested model at all.
//
// The problem it solves is asymmetric, which is why it is a separate predicate
// from isWholeIDAt rather than a loosening of it. We ask for "claude-sonnet-4";
// the provider resolves the alias and answers about "claude-sonnet-4-20250514".
// Whole-identifier matching rejects that, correctly by its own rule, and that
// same rejection is what stops an error about "gpt-4.1" from retiring "gpt-4".
// So the boundary rule cannot be relaxed: what is added is one narrow shape on
// top of it.
//
// The start must still be clean. Only the tail may differ, and only by digits:
// an id is never a suffix of another id's dated form, so nothing that ends in
// letters ("-lite", "-32k") or in a decimal (".1") can reach this at all.
func idEndAllowingVersion(body string, pos, end int) (int, bool) {
	// The whole-identifier rule first and by name, because the extension below
	// sits on top of it rather than instead of it.
	if isWholeIDAt(body, pos, end) {
		return end, true
	}
	// Only the tail may differ. A dirty START means the match is inside a longer
	// id, which no suffix can rescue.
	if pos != 0 && isModelIDChar(body[pos-1]) {
		return 0, false
	}
	suffix := versionSuffix.FindString(body[end:])
	if suffix == "" {
		return 0, false
	}
	digits := 0
	for i := range len(suffix) {
		if suffix[i] >= '0' && suffix[i] <= '9' {
			digits++
		}
	}
	if digits < minVersionSuffixDigits {
		return 0, false
	}
	extended := end + len(suffix)
	// Whatever follows the snapshot must not continue the identifier:
	// "gpt-4-0613-preview" is a variant of a snapshot, not the model we asked
	// for.
	if extended != len(body) && isModelIDChar(body[extended]) {
		return 0, false
	}
	return extended, true
}

// namesModelAllowingVersion reports whether body names the requested model
// anywhere in it, as a whole identifier or as a dated snapshot of one.
//
// No proximity, deliberately, and it is only used where proximity cannot apply:
// a structured error puts the type in one JSON field and the id in another, so
// there is no adjacency to measure. The callers that DO have prose to work with
// keep using phraseIsAbout, which is stricter.
func namesModelAllowingVersion(body, id string) bool {
	if id == "" || body == "" {
		return false
	}
	for off := 0; off+len(id) <= len(body); {
		at := strings.Index(body[off:], id)
		if at < 0 {
			return false
		}
		pos := off + at
		if _, ok := idEndAllowingVersion(body, pos, pos+len(id)); ok {
			return true
		}
		off = pos + 1
	}
	return false
}

// maxAttributionGap bounds the text allowed between the model id and the phrase
// that retires it. Real payloads put them adjacent or a few words apart ("The
// model `gpt-4` has been deprecated and does not exist"); anything longer is a
// different clause that happens to be nearby.
const maxAttributionGap = 40

// isClauseBreak reports whether b ends the clause a phrase belongs to.
//
// A comma counts. "healthy-model was routed, but retired-model does not exist"
// is two claims, and only the second one is a retirement.
func isClauseBreak(b byte) bool {
	switch b {
	case '.', ',', ';', '!', '?', '\n', '\r', '{', '}', '[', ']':
		return true
	default:
		return false
	}
}

// looksLikeAModelID reports whether s contains a token shaped like a model id.
// Digits and hyphens are the tell: "gpt-4", "retired-model" and "llama3" all
// qualify, while ordinary words and vendor prefixes like "openai/" do not.
func looksLikeAModelID(s string) bool {
	tokenHasDigit, tokenHasDash := false, false
	for i := 0; i <= len(s); i++ {
		if i < len(s) && (isModelIDChar(s[i]) || s[i] == '/') {
			switch {
			case s[i] >= '0' && s[i] <= '9':
				tokenHasDigit = true
			case s[i] == '-':
				tokenHasDash = true
			}
			continue
		}
		if tokenHasDigit || tokenHasDash {
			return true
		}
		tokenHasDigit, tokenHasDash = false, false
	}
	return false
}

// gapBindsPhrase reports whether the text between a model id and a retirement
// phrase is short and plain enough that the phrase is about THAT id.
//
// Proximity alone is not attribution, which is the trap this closes: an 80
// character window around "is no longer available" catches any id that happens
// to be nearby, so a response naming the requested model in one clause and
// retiring a different model in the next retires the wrong one — and three of
// those disable a model that is serving fine. The subject has to be adjacent to
// its predicate, with no clause boundary and no competing id in between.
func gapBindsPhrase(gap string) bool {
	if len(gap) > maxAttributionGap {
		return false
	}
	for i := range len(gap) {
		if isClauseBreak(gap[i]) {
			return false
		}
	}
	return !looksLikeAModelID(gap)
}

// phraseIsAbout reports whether the phrase occupying [verbPos, verbEnd) is the
// provider talking about id, searching the surrounding window for an occurrence
// bound tightly enough to be its subject or object.
//
// Both sides are allowed because provider wording splits along grammatical
// lines: predicates take the id before them ("gpt-4 does not exist"), while the
// noun-phrase verbs take it after ("unknown model openai/gpt-4").
func phraseIsAbout(body string, verbPos, verbEnd, lo, hi int, id string) bool {
	for off := lo; off+len(id) <= hi; {
		at := strings.Index(body[off:hi], id)
		if at < 0 {
			return false
		}
		pos := off + at
		// The occurrence may be a dated snapshot of the id we asked for, and
		// then it is the snapshot that has to clear the phrase: the gap below
		// starts after the suffix, not after the alias. Prose carries this shape
		// as readily as a JSON field does — "model `gpt-4-0613` does not exist"
		// is the same claim as an error.message saying so — and reading only one
		// of the two would leave the other silently unhandled.
		if occEnd, ok := idEndAllowingVersion(body, pos, pos+len(id)); ok {
			var gap string
			switch {
			case occEnd <= verbPos:
				gap = body[occEnd:verbPos]
			case pos >= verbEnd:
				gap = body[verbEnd:pos]
			default:
				// Overlapping the phrase itself; treat as bound.
				return true
			}
			if gapBindsPhrase(gap) {
				return true
			}
		}
		// Advance by one rather than by len(id): ids can overlap themselves.
		off = pos + 1
	}
	return false
}

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
type structuredError struct {
	code    string
	typ     string
	message string
}

// The scan fallback's patterns: a quoted string field by name, anchored on the
// key so a value that merely looks like one cannot be picked up.
var (
	scanCode    = regexp.MustCompile(`"code"\s*:\s*"([^"]*)"`)
	scanType    = regexp.MustCompile(`"type"\s*:\s*"([^"]*)"`)
	scanMessage = regexp.MustCompile(`"message"\s*:\s*"([^"]*)"`)
	// errorObjectStart finds where the error object opens, so the scan reads
	// the fields belonging to it rather than the first ones in the document.
	errorObjectStart = regexp.MustCompile(`"error"\s*:\s*\{`)
)

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
	scoped := body
	if at := errorObjectStart.FindStringIndex(body); at != nil {
		scoped = body[at[1]:]
	}
	first := func(re *regexp.Regexp) string {
		if m := re.FindStringSubmatch(scoped); m != nil {
			return m[1]
		}
		if m := re.FindStringSubmatch(body); m != nil {
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
		return structuredError{
			code:    firstNonEmpty(codeOf(envelope.Error.Code), codeOf(envelope.Code)),
			typ:     firstNonEmpty(envelope.Error.Type, envelope.Type),
			message: firstNonEmpty(envelope.Error.Message, envelope.Message),
		}
	}

	return scanStructuredError(body)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
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
