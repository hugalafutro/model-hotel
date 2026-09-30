package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// attemptPassthroughCandidate runs one failover attempt for a multimodal
// request: build and send the upstream request, record the circuit-breaker
// outcome, and either fail over to the next candidate, forward a terminal
// error, or stream the response through.
func (h *Handler) attemptPassthroughCandidate(w http.ResponseWriter, r *http.Request, st *requestState, candidate modelCandidate, attempt, totalCandidates int) candidateOutcome {
	logData := st.logData
	// Per-attempt DNS resolution timing, written by SafeDialer via context.
	var dialMs float64
	failoverCtx, failoverCancel := context.WithDeadline(r.Context(), st.attemptDeadline())
	// Fires on every return path, after the pass-through dispatch has
	// consumed the body.
	defer failoverCancel()
	failoverCtx = context.WithValue(failoverCtx, ctxkeys.CancelOriginKey, "failover_timeout")

	// A Gemini TTS or STT candidate that cannot serve the request as asked
	// (a format it does not produce, an upload it cannot read) is skipped
	// before any request is built, so another candidate in the group can
	// serve it. Nothing was contacted, so the skip is recorded on the trail
	// like a breaker skip and pays no failover backoff.
	if reason := geminiRequestRefusal(st, candidate); reason != "" {
		st.setReqErr(reqError{Kind: KindProviderBadRequest, Attempt: attempt, Provider: candidate.provider.Name, Underlying: reason})
		logData.failoverAttempt = attempt
		logData.appendSkip(candidate.provider.ID, candidate.provider.Name, candidateModelID(candidate), reason)
		debuglog.Info("proxy: gemini candidate skipped", "endpoint", logData.endpointType, "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "reason", reason)
		return outcomeSkipped
	}

	resp, providerType, _, busyAttempt, ok := h.beginAttempt(failoverCtx, st, candidate, attempt, totalCandidates, &dialMs)
	if busyAttempt {
		return outcomeBusy
	}
	if !ok {
		return outcomeFailover
	}

	// MiniMax reports business errors (rate limit, exhausted plan balance, auth
	// failures) inside an HTTP 200 envelope, so the status is normalised before
	// anything is judged from it.
	resp = remapMiniMaxBusinessError(providerType, candidate.provider.Name, resp, logData.fence())
	h.finishAttemptAdmission(st, candidate, resp)
	logData.noteAttemptStatus(resp.StatusCode)

	responseHeaderMs := util.MillisSince(st.startTime)
	hasMoreCandidates := attempt < totalCandidates-1
	isFailoverEligible := h.shouldFailover(r.Context(), resp.StatusCode)

	rl := h.judge429AndRecordBreaker(r.Context(), st, candidate, resp, isFailoverEligible)

	if isFailoverEligible {
		if hasMoreCandidates {
			// The read is capped because on multimodal endpoints the body
			// behind an error status can be an image payload rather than a
			// sentence.
			return h.failOverPastCandidate(st, candidate, resp, attempt, rl, "endpoint", logData.endpointType)
		}
		// The last candidate's one-shot retries, the same as the chat path.
		if outcome, ok := h.deferLastCandidateRetry(st, candidate, resp, attempt, rl); ok {
			return outcome
		}
	}

	if !servedSuccessStatus(resp.StatusCode) {
		// A non-failover-eligible error (e.g. 400) means the provider is alive:
		// credit the circuit before forwarding. RecordAlive, not RecordSuccess:
		// nothing was served, so the 429 behavioural fallback must not count it
		// as a recent serve.
		if !isFailoverEligible && st.circuitBreakerEnabled {
			logData.noteBreaker(breakerAlive)
			h.circuitBreaker.RecordAlive(candidate.provider.ID, candidate.provider.Name, candidateModelID(candidate), resp.StatusCode)
		}
		return h.forwardUpstreamError(w, st, candidate, resp, attempt, isFailoverEligible, responseHeaderMs)
	}

	// Breaker success for 2xx and the gone-strike clear both happen inside
	// servePassthroughResponse at the commit point (the buffered read for
	// JSON, the first body byte for SSE/binary), so a provider that returns
	// 200 headers and then stalls before producing data still accrues breaker
	// failures.
	debuglog.Debug("proxy: upstream responded OK, dispatching passthrough", "endpoint", logData.endpointType, "model", logData.modelID, "provider", logData.providerName, "status", resp.StatusCode, "content_type", fencedDebugText(resp.Header.Get("Content-Type"), logData))
	// The dispatch reads the upstream body under the ATTEMPT's context, the same
	// request the chat path hands dispatchNonStreaming. With the bare client
	// request instead, a read this gateway's own per-attempt deadline ended
	// carries no cancel origin, so resolveCancelOrigin calls it a client
	// disconnect: a provider that answered headers and then stalled would look
	// like a caller who hung up, and the sibling behind it would never be asked.
	attemptReq := r.WithContext(failoverCtx)
	if st.speechFormat != "" {
		return h.serveGeminiSpeechResponse(w, attemptReq, st, candidate, resp, attempt, responseHeaderMs)
	}
	if st.transcriptionFormat != "" {
		return h.serveGeminiTranscriptionResponse(w, attemptReq, st, candidate, resp, attempt, responseHeaderMs)
	}
	return h.servePassthroughResponse(w, attemptReq, st, candidate, resp, attempt, responseHeaderMs, hasMoreCandidates)
}

// passthroughAnswered reports whether a buffered pass-through response is the
// model answering: what clears its gone-strike streak, credits its circuit,
// and lets the metering floor charge for it.
//
// The families that answer in JSON (embeddings, rerank, image generation) are
// judged on content, since a 200 carrying `{"data":[]}` or `{"results":[]}` is
// the provider answering with nothing: crediting it would let a provider that
// always answers so keep its circuit closed, and reset an embeddings model's
// gone-strike count on every empty answer. Audio is judged on bytes, because a
// speech answer is binary and a transcription of silence is legitimately empty
// text, so neither can be told apart from a failure by its body.
//
// A request that asked for nothing (no documents, no input) cannot be told
// apart here either. Every hosted API refuses one with a 400, which is never
// charged; a lenient local server answering it with an empty 200 has its
// circuit charged, as the embeddings family always was, at a rate the per-key
// limits bound.
func passthroughAnswered(endpointType string, body []byte) bool {
	if len(body) == 0 {
		return false
	}
	// Past the buffer cap, body is a prefix: the caller read
	// passthroughJSONBufferCap+1 bytes and streams the remainder. Truncated
	// JSON never parses, so the content check below would report a provider
	// that produced megabytes as having answered with nothing.
	if len(body) > passthroughJSONBufferCap {
		return true
	}
	switch endpointType {
	case endpointTypeEmbeddings, endpointTypeRerank, endpointTypeImage:
		return probeDeliveredContent(endpointType, body)
	}
	return true
}

// errPassthroughErrorEnvelope is the fault a 2xx pass-through body fails over
// with when it is an error envelope instead of an answer. Gateway-authored, so
// it can be reported as it is: the provider's own message is not in it.
var errPassthroughErrorEnvelope = errors.New("upstream answered 2xx with an error envelope instead of a response")

// passthroughErrorEnvelope reports the provider's own message when a buffered
// 2xx JSON pass-through body is an error envelope and nothing else: its
// "error" member carries something (the shared util.ValueCarries rule the chat
// and stream paths use), and every other top-level member either carries
// nothing (envelopeMemberIsEmpty: absent, null, [], "" or {}) or belongs to the
// envelope itself (envelopeMetadataKeys). Content is judged structurally, not by
// util.ValueCarries: that rule reads 0 and false as "no error", which is right
// for an error member and wrong for content ({"results":[{"index":0}]}). LM Studio
// answers every route it does not serve (images, speech, rerank) with HTTP 200
// and {"error":"Unexpected endpoint or method."}, which was served to the
// client as a success and logged as completed.
//
// Any other member that carries something is content, whatever the family
// calls it (data, results, text, audio, a key of a provider's own), so an
// answer that carries one beside an advisory error member is left to the
// ordinary path. probeDeliveredContent is not the test here: it counts a shape
// it does not recognise as delivered, which is right for a dialect it cannot
// read and wrong for an envelope that is plainly only an error.
func passthroughErrorEnvelope(status int, body []byte) (string, bool) {
	if !servedSuccessStatus(status) || len(body) > passthroughJSONBufferCap {
		return "", false
	}
	var members map[string]json.RawMessage
	if json.Unmarshal(body, &members) != nil || !util.ValueCarries(members["error"]) {
		return "", false
	}
	for key, value := range members {
		if key != "error" && !envelopeMetadataKeys[key] && !envelopeMemberIsEmpty(value) {
			return "", false
		}
	}
	return util.ErrorMemberMessage(members["error"]), true
}

// envelopeMetadataKeys are the top-level members an error envelope carries
// beside "error" without that making it an answer: fields such as OpenAI's
// {"object":"error"}, FastAPI's "detail", the status, code, id and usage
// fields servers stamp on every response, and the model, provider and
// fingerprint echoes a relay adds to refusals too. No pass-through family
// carries its answer under any of them.
var envelopeMetadataKeys = map[string]bool{
	"object": true, "type": true, "code": true, "message": true, "status": true,
	"detail": true, "param": true, "created": true, "id": true, "request_id": true,
	"usage": true, "model": true, "provider": true, "system_fingerprint": true,
	"service_tier": true, "version": true, "timestamp": true,
}

// envelopeMemberIsEmpty is jsonValueIsEmpty that also reads {} as empty: an
// answer member that is an empty object carries no answer either.
func envelopeMemberIsEmpty(raw json.RawMessage) bool {
	if jsonValueIsEmpty(raw) {
		return true
	}
	v := bytes.TrimSpace(raw)
	return len(v) >= 2 && v[0] == '{' && v[len(v)-1] == '}' && len(bytes.TrimSpace(v[1:len(v)-1])) == 0
}

// failPassthroughErrorEnvelope settles an attempt whose buffered 2xx JSON body
// was an error envelope (passthroughErrorEnvelope). A streamed pass-through
// (SSE or binary) never reaches it. While a sibling remains it fails over,
// through the same reject the chat path takes for a 2xx that is not a
// completion. On the last candidate the client is answered 502, as the chat
// path answers the same shape (nonCompletionClientStatus): a 2xx carrying the
// gateway's error envelope would read as success to an OpenAI SDK. The row
// keeps the upstream's 2xx and records the provider's message, masked and
// fenced, as the failure.
func (h *Handler) failPassthroughErrorEnvelope(w http.ResponseWriter, r *http.Request, st *requestState, candidate modelCandidate, status int, msg string, attempt int, responseHeaderMs float64, hasMoreCandidates bool) candidateOutcome {
	logData := st.logData
	if hasMoreCandidates && !requestAbandoned(r.Context(), nil) {
		return h.rejectUntranslatableBody(st, candidate, logData, "passthrough", status, errPassthroughErrorEnvelope, attempt, r)
	}
	h.chargeBreaker(st, candidate, status, "response carried an error instead of an answer")
	sanitized := util.SanitizeLogBody(msg, logBodyCap)
	kind, reason := classifyUpstreamError(status, sanitized, candidate.model.ModelID)
	logData.errorKind = kind
	fenced := fencedFrameMessage(logData.fence(), logData.masks(), sanitized)
	debuglog.Warn("proxy: passthrough 2xx carried an error", "endpoint", logData.endpointType, "status", status, "error_kind", kind, "model", logData.modelID, "provider", logData.providerName, "error", fenced)
	h.finalizePassthroughLog(st, status, attempt, responseHeaderMs, 0, 0, "failed", fenced)
	writeOpenAIError(w, upstreamClientMessage(candidate.provider.Name, status, reason), http.StatusBadGateway)
	return outcomeFatal
}
