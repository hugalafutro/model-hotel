package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// forwardableErrorStatus reports whether a status is in the payload class a
// client may see the provider's body for: a 4xx that judged the caller's own
// request. Deliberately static where shouldFailover is dynamic: its 429
// verdict follows the failover_on_rate_limit setting, but a quota body is the
// operator's account state whichever way that toggle points, and what can
// reach a client must not be a side effect of a routing knob. The denied 4xx
// are the auth, billing, quota and routing classes whose bodies can carry
// operator account detail; 1xx/3xx are not payload errors and 5xx bodies are
// the provider talking about itself, so none of those forward either.
func forwardableErrorStatus(status int) bool {
	if status < 400 || status >= 500 {
		return false
	}
	switch status {
	case http.StatusUnauthorized, http.StatusPaymentRequired, http.StatusForbidden,
		http.StatusNotFound, http.StatusProxyAuthRequired, http.StatusTooManyRequests, 499:
		return false
	}
	return true
}

// forwardableErrorBodyCap bounds a payload-class error body that may be
// forwarded to the client. Reading it whole keeps forwarded JSON intact, but
// "whole" needs a ceiling: the multimodal endpoints have no upstream pre-cap,
// so a broken or hostile custom endpoint could answer an error status with
// anything. A megabyte is far past any real error document; a body over it is
// answered with the synthesised envelope instead.
const forwardableErrorBodyCap = 1 << 20

// forwardUpstreamError handles a non-200 upstream response that is NOT being
// failed over (phase G): log + meter the failure via failRequest, then answer the
// client. isFailoverEligible carries the caller's shouldFailover verdict; what
// the client may see is decided by it together with the static
// forwardableErrorStatus class. A payload-class refusal (a plain 400 and its
// kin) judged this caller's own request, so the upstream's error object is
// forwarded with the provider's credential masked (credentialMasker: exact
// key, then key-shaped tokens). Everything else - auth, billing,
// rate limit, not-found, server faults, whether classed by eligibility or by
// status - gets a synthesised envelope with the classified reason, because
// those bodies can quote the operator's provider credentials or account
// details; the body stays in the request log either way, with only this
// gateway's own key redacted. Drains/closes
// resp.Body exactly once and always returns outcomeFatal.
func (h *Handler) forwardUpstreamError(w http.ResponseWriter, st *requestState, candidate modelCandidate, resp *http.Response, attempt int, isFailoverEligible bool, responseHeaderMs float64) candidateOutcome {
	logData := st.logData
	masker := logData.masker
	mayForwardError := !isFailoverEligible && forwardableErrorStatus(resp.StatusCode)
	// How much of the body is worth holding depends on what happens to it
	// below. A 2xx is forwarded whole - truncating a success would corrupt it.
	// A forwardable error is read under its own cap, and one that overflows it
	// is demoted to the envelope rather than forwarded truncated, so a client
	// never receives invalid JSON where the provider sent something complete.
	// A discarded body is read under the same cap as the two drain sites,
	// since all that is left to take from it is a classification and the first
	// 10 000 bytes of request log.
	var body []byte
	oversized := false
	switch {
	case resp.StatusCode/100 == 2:
		body, _ = io.ReadAll(resp.Body)
	case mayForwardError:
		body, _ = io.ReadAll(io.LimitReader(resp.Body, forwardableErrorBodyCap+1))
		if len(body) > forwardableErrorBodyCap {
			oversized = true
			body = body[:forwardableErrorBodyCap]
		}
		_, _ = io.Copy(io.Discard, resp.Body)
	default:
		body, _ = io.ReadAll(io.LimitReader(resp.Body, failoverErrorClassifyCap))
		_, _ = io.Copy(io.Discard, resp.Body)
	}
	_ = resp.Body.Close()
	errMsg := util.SanitizeLogBody(string(body), 10000)
	// Classify for the request log and metrics only — routing is unaffected,
	// the caller already decided it from the status code.
	kind, reason := classifyUpstreamError(resp.StatusCode, errMsg, candidate.model.ModelID)
	if kind == KindProviderModelGone {
		// Same as the drain path above: the candidate carries what the
		// pre-retirement probe needs, and logData.endpointType is the family
		// that decides whether this model can be adjudicated at all.
		h.noteModelGone(candidate, logData.endpointType)
	}
	debuglog.Warn("proxy: upstream non-200", "status", resp.StatusCode, "error_kind", kind, "model", logData.modelID, "provider", logData.providerName, "provider_id", candidate.provider.ID)
	debuglog.Debug("proxy: upstream error response", "status", resp.StatusCode, "model", logData.modelID, "provider", logData.providerName, "provider_id", candidate.provider.ID, "body_length", len(body), "attempt", attempt+1)
	logData.responseHeaderMs = responseHeaderMs
	h.failRequest(logData, resp.StatusCode, kind, string(masker.mask([]byte(errMsg))), attempt, st.startTime, st.parseMs, st.timings, st.cacheHits, st.proxyOverhead)

	// A 2xx that reached this function (any success status other than a bare
	// 200) is not an error at all and is forwarded whatever its body is,
	// including a non-JSON or empty one. That is what lets a 204 answer with
	// no body: an envelope written under a No Content status would be a body
	// this gateway invented for a request the provider considered successful.
	// Checked before eligibility so a success can never be rewritten.
	if resp.StatusCode/100 == 2 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		//nolint:gosec // G705 false positive: provider JSON body, not HTML; Content-Type is application/json
		_, _ = w.Write(masker.maskExact(body))
		return outcomeFatal
	}

	if !mayForwardError {
		// Auth, billing, rate-limit, not-found and server-fault classes -
		// whether ruled out by the caller's eligibility verdict or by the
		// static status class. Their bodies are the ones that can quote the
		// operator's provider credentials ("Incorrect API key provided:
		// sk-...") or account details a virtual-key holder must not see, so the
		// body (own key redacted) stays in the DB request log via failRequest and the caller gets
		// the classified reason: enough to tell "this model is gone" from "top
		// up your account" from "try again shortly".
		writeOpenAIError(w, upstreamClientMessage(candidate.provider.Name, resp.StatusCode, reason), resp.StatusCode)
		return outcomeFatal
	}

	// Payload-class refusal: the provider judged this caller's own request, so
	// the caller is entitled to the detail, whether or not this was the last
	// candidate.
	switch {
	case carriesErrorObject(body) && !oversized:
		// Forward the upstream response so clients can react to semantic errors
		// (e.g. context_length_exceeded). The upstream's own error object
		// carries detail this gateway cannot reconstruct — code, type, param,
		// provider-specific fields — so it is passed through byte for byte
		// apart from masking key-shaped tokens, since even a payload error is
		// provider-authored free text.
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		//nolint:gosec // G705 false positive: provider JSON error body, not HTML; Content-Type is application/json
		_, _ = w.Write(masker.mask(body))
	case json.Valid(body):
		// A non-2xx whose JSON body carries no error object. OpenCode Zen and
		// OpenCode Go answer some failed requests with a complete
		// chat.completion envelope under an HTTP 400, which is valid JSON with
		// nothing for a client to read `.error.message` off. There is no
		// upstream error detail to preserve here, so the classified reason is
		// synthesised into an envelope instead; the body itself stays in the
		// request log via failRequest above.
		writeOpenAIError(w, upstreamClientMessage(candidate.provider.Name, resp.StatusCode, reason), resp.StatusCode)
	default:
		// Body is not JSON (e.g. HTML from a CDN). Wrap in an
		// OpenAI-compatible envelope so JSON-parsing clients don't crash.
		//
		// The sanitized body rides inside the message here, where the case above
		// hands back only the classified reason. The asymmetry is deliberate: the
		// full body reaches the request log either way via failRequest, and how
		// much of a provider's response this gateway echoes to callers is one
		// decision for all three cases rather than something to widen here.
		writeOpenAIError(w, string(masker.mask([]byte(errMsg))), resp.StatusCode)
	}
	return outcomeFatal
}

// keyShapedToken matches credential-looking substrings a provider may quote
// inside an error body: prefixed secret keys (sk- also covers sk-ant-, sk-or-
// and sk-proj-; hf_, fw_, r8_, gsk_, xai- cover HuggingFace, Fireworks,
// Replicate, Groq and xAI), Google API keys (AIza...), AWS access key ids
// (AKIA...), bare JWTs (the MiniMax API key format), and bearer tokens. The
// minimum tail lengths keep prose like "sk-abc" out of scope; matches without
// a digit are prose too and are dropped by maskKeyShapedTokens. A prefix list
// necessarily trails the provider roster - it is the third layer, behind the
// status-class gate above and credentialMasker's exact match of this
// gateway's own key.
var keyShapedToken = regexp.MustCompile(`\b(?:sk|gsk|xai|hf|fw|r8)[-_][A-Za-z0-9_-]{16,}|\bAIza[0-9A-Za-z_-]{30,}|\bAKIA[0-9A-Z]{16}\b|\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{5,}|(?i:\bbearer\s+)[A-Za-z0-9._~+/=-]{16,}`)

// credentialMinLen is the shortest provider key the exact-value mask will
// redact. Keyless local providers carry an empty key and a handful of test or
// placeholder setups use tiny ones; rewriting every occurrence of a few
// characters would shred the body without protecting anything.
const credentialMinLen = 8

// credentialMasker scrubs a provider's credential out of client-bound text.
// The exact decrypted key is the primary control: it is an exact byte match,
// so it covers every key shape, including custom or self-hosted gateways
// whose format the prefix regex can never anticipate, but not a JSON-escaped
// rendering of the key (an encoder turning "&" into "\u0026" or "/" into
// "\/" defeats it; real keys rarely carry such bytes). maskKeyShapedTokens
// runs after it as the third layer, behind the status-class gate, for
// credentials a relay quotes that are not ours. Build one per candidate with
// newCredentialMasker and pass it to every client-facing emit of provider
// error text: the buffered paths in forwardUpstreamError and the in-stream
// error frames on the translated (handleDataChunk), native Anthropic
// (emitRawData) and pass-through (sseErrorMaskWriter) streaming paths. The
// zero value masks by shape only. The exact layer alone (maskExact) also runs
// on every other provider body a client receives; the request log's error
// message gets both layers, since it is error text readable by non-admin
// users with the logs grant. Content bodies never meet the regex.
type credentialMasker struct {
	secret []byte
}

func newCredentialMasker(apiKey string) credentialMasker {
	if len(apiKey) < credentialMinLen {
		return credentialMasker{}
	}
	return credentialMasker{secret: []byte(apiKey)}
}

// mask returns body with every occurrence of the exact credential, then any
// key-shaped token, replaced by "[redacted]". The exact pass runs first so the
// regex cannot split the key and leave a recognisable remainder. For error
// frames and bodies only: the regex layer can match prose.
func (m credentialMasker) mask(body []byte) []byte {
	return maskKeyShapedTokens(m.maskExact(body))
}

// maskExact replaces only the exact credential. It cannot false-positive, so
// it is safe on every provider body bound for a client (content chunks,
// success bodies) and on the request log, where it closes the read a
// VK-owning dashboard user has on their own failed requests.
func (m credentialMasker) maskExact(body []byte) []byte {
	if len(m.secret) > 0 && bytes.Contains(body, m.secret) {
		return bytes.ReplaceAll(body, m.secret, []byte("[redacted]"))
	}
	return body
}

// maskKeyShapedTokens scrubs credential-looking substrings from upstream error
// text before it reaches a client or the request log. Auth-class errors never
// forward at all, and credentialMasker has already removed this gateway's own
// key by exact value; this is the third layer, for a provider quoting some
// other credential inside an otherwise forwardable payload error. A match with no
// digit in it is an identifier or prose ("sk_business_unit_identifier",
// "Bearer authentication-required") rather than a credential, and stays -
// real keys carry digits. The replacement carries no JSON metacharacters, so
// a valid body stays valid.
// exactMaskWriter applies credentialMasker.maskExact to a byte stream whose
// writes may split the key: it holds back the last len(key)-1 bytes of each
// write until the next one arrives, so a key straddling two writes is still
// seen whole. Flush releases the held tail at end of stream. Used on the two
// raw forwarding paths (an oversized pass-through JSON remainder and an
// oversized SSE event) where per-chunk masking has boundaries.
type exactMaskWriter struct {
	w    io.Writer
	cred credentialMasker
	tail []byte
}

func newExactMaskWriter(w io.Writer, cred credentialMasker) *exactMaskWriter {
	return &exactMaskWriter{w: w, cred: cred}
}

func (e *exactMaskWriter) Write(p []byte) (int, error) {
	if len(e.cred.secret) == 0 {
		return e.w.Write(p)
	}
	buf := e.cred.maskExact(slices.Concat(e.tail, p))
	keep := min(len(e.cred.secret)-1, len(buf))
	out := buf[:len(buf)-keep]
	e.tail = append([]byte(nil), buf[len(buf)-keep:]...)
	if len(out) > 0 {
		if _, err := e.w.Write(out); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

// Flush writes the held tail. Call once the stream (or the raw stretch of
// it) has ended.
func (e *exactMaskWriter) Flush() error {
	if len(e.tail) == 0 {
		return nil
	}
	out := e.tail
	e.tail = nil
	_, err := e.w.Write(out)
	return err
}

func maskKeyShapedTokens(body []byte) []byte {
	return keyShapedToken.ReplaceAllFunc(body, func(m []byte) []byte {
		if !bytes.ContainsAny(m, "0123456789") {
			return m
		}
		return []byte("[redacted]")
	})
}

// carriesErrorObject reports whether an upstream body is a JSON object with an
// "error" member that actually carries something, which is what decides between
// forwarding that body verbatim and synthesising an envelope over it.
//
// The test is emptiness, not shape. What a provider puts inside its error is not
// this gateway's to judge, so any populated value counts: an object with fields,
// Ollama's bare string ("model not found"), a list, even a number. What does not
// count is a member that leaves a client with nothing to read - `null`, `{}`,
// `""`, `[]` - because a body carrying one of those is, from the caller's side,
// the same body with no error member at all, which is exactly the case this
// function exists to catch. A body that is not a JSON object (an array, a bare
// string, HTML) can carry no member and reports false.
func carriesErrorObject(body []byte) bool {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return false
	}
	raw, present := envelope["error"]
	if !present {
		return false
	}
	return carriesErrorMember(raw)
}

// carriesErrorMember is the same judgement applied to an already-extracted
// "error" value, for callers that hold the member rather than the whole body
// (the SSE observers, which have it off a parsed chunk). One definition of what
// counts as an error, so the two cannot drift.
func carriesErrorMember(raw json.RawMessage) bool {
	var content any
	if err := json.Unmarshal(raw, &content); err != nil {
		return false
	}
	switch v := content.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(v) != ""
	case map[string]any:
		return len(v) > 0
	case []any:
		return len(v) > 0
	case bool:
		// "error": false is how several OpenAI-compatible gateways spell "no
		// error". Reading it as one marks a perfectly good stream failed.
		return v
	default:
		// A number: peculiar, but the provider put a value there and a client
		// can render it.
		return true
	}
}
