package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strconv"

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

// forwardUpstreamError handles a FAILED upstream response that is NOT being
// failed over (phase G): log + meter the failure via failRequest, then answer the
// client.
//
// Callers must only ever hand it a non-2xx. It answers by writing an error, and
// a success routed here would be served to the client while failRequest wrote
// the row as a failure and metered nothing — the quota bypass that made the
// caller-side status test a RANGE rather than an equality. Both call sites now
// route on that range. isFailoverEligible carries the caller's shouldFailover verdict; what
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
	// below. A forwardable error is read under its own cap, and one that
	// overflows it is demoted to the envelope rather than forwarded truncated,
	// so a client never receives invalid JSON where the provider sent something
	// complete. A discarded body is read under the same cap as the two drain
	// sites, since all that is left to take from it is a classification and the
	// first 10 000 bytes of request log.
	//
	// Every branch is bounded. The unbounded read that used to sit here was for
	// the 2xx case, on the reasoning that truncating a success corrupts it —
	// true, and the reason a success does not belong in this function at all.
	var body []byte
	oversized := false
	switch {
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
	kind, reason = rateLimitTerminalKind(kind, reason, resp.StatusCode, st.rateLimit)
	// A saturated terminal 429 tells the client when to come back; SDKs
	// honour Retry-After on 429 natively.
	if resp.StatusCode == http.StatusTooManyRequests && st.rateLimit.class == rateLimitSaturated {
		w.Header().Set("Retry-After", strconv.Itoa(retryAfterSeconds(st.rateLimit.retryAfter)))
	}
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

// credentialMinLen is the shared threshold, not a second opinion on it: two
// copies of one rule is how the scrub itself came to differ between packages.
const credentialMinLen = util.CredentialMinLen

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

// maskKeyShapedTokens scrubs credential-looking substrings from upstream error
// text before it reaches a client or the request log. Auth-class errors never
// forward at all, and credentialMasker has already removed this gateway's own
// key by exact value; this is the third layer, for a provider quoting some
// other credential inside an otherwise forwardable payload error.
//
// The rule itself lives in internal/util, shared with the dashboard's model
// test and provider discovery, which decrypt the same credential and write the
// same bodies to the same tables. Two copies is how one of them came to have
// only a UUID scrub.
func maskKeyShapedTokens(body []byte) []byte {
	return util.MaskKeyShapedTokens(body)
}

// carriesErrorObject reports whether an upstream body is a JSON object with an
// "error" member that actually carries something, which is what decides between
// forwarding that body verbatim and synthesising an envelope over it.
//
// util.ErrorMemberCarries holds the rule and documents it: emptiness, not shape,
// applied at every depth, with `false` and `0` counting as empty like every
// other zero value. A body that is not a JSON object (an array, a bare string,
// HTML) can carry no member and reports false.
func carriesErrorObject(body []byte) bool {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return false
	}
	return util.ErrorMemberCarries(envelope["error"])
}
