package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/anthropicegress"
	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/gemini"
	"github.com/hugalafutro/model-hotel/internal/openairesponses"
	"github.com/hugalafutro/model-hotel/internal/paramrewrite"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// failoverErrorClassifyCap bounds how much of a discarded failover error body is
// held in memory long enough to be classified. The client is served by the next
// candidate, and the only thing still wanted from the body is whether the
// provider said the model is retired, so everything above the cap is read and
// discarded rather than retained.
//
// Sixteen kibibytes is comfortably above any error message and well below the
// multi-megabyte bodies the image endpoints can answer with.
const failoverErrorClassifyCap = 16 << 10

// attemptCandidate runs one failover attempt against a single candidate (phase
// D3–D11): build and send the upstream request, handle the 400 auto-retry,
// record the circuit-breaker outcome, and either fail over to the next
// candidate, forward a terminal error, or dispatch the 200 response.
//
// It owns the per-attempt request contexts: failoverCtx is cancelled via a
// deferred call (and the retry context, once created, via a second deferred
// call), so no exit path can leak a context. The deferred cancels fire as the
// method returns, after the streaming or non-streaming dispatch has consumed
// the body, keeping the "cancel only after the body is consumed" ordering.
//
// Accumulating state (dial time, proxy overhead, lastErr, failoverAttempt) is
// written back to st so the loop's deadline/backoff checks and the exhaustion
// path see the running totals.
func (h *Handler) attemptCandidate(w http.ResponseWriter, r *http.Request, st *requestState, candidate modelCandidate, attempt, totalCandidates int) candidateOutcome {
	logData := st.logData
	// Per-attempt DNS resolution timing. SafeDialer's DialContext writes into
	// this via context, avoiding cross-request races on a shared field.
	var dialMs float64
	streamCancelOrigin := "failover_timeout"
	failoverCtx, failoverCancel := context.WithTimeout(r.Context(), st.failoverTimeout)
	// Own the request context: this fires on every return path, after any
	// dispatch below has consumed the body (dispatch is the final statement on
	// the served paths). Idempotent, so the retry helper may also call it.
	defer failoverCancel()
	// retryCancel is set only when the 400 auto-retry issues a live request whose
	// body has not yet been consumed; cancel it (after body consumption) on return.
	var retryCancel context.CancelFunc
	defer func() {
		if retryCancel != nil {
			retryCancel()
		}
	}()
	failoverCtx = context.WithValue(failoverCtx, ctxkeys.CancelOriginKey, "failover_timeout")

	// The response is owned by the dispatch at the end of this function, which
	// closes it on every outcome. On a learnable 400 that ownership passes
	// through retryLearnable400: the learner consumes and closes the body it
	// reads, and either hands back a retry response to be closed in its place or
	// returns this one untouched. bodyclose cannot follow a handover through a
	// function boundary, so without this it reads the original as leaked.
	//nolint:bodyclose // closed by the dispatch below, or by retryLearnable400's handover
	resp, providerType, targetURL, busyAttempt, ok := h.beginAttempt(failoverCtx, st, candidate, attempt, totalCandidates, &dialMs)
	if busyAttempt {
		return outcomeBusy
	}
	if !ok {
		return outcomeFailover
	}

	// Auto-retry learnable 400s. A 400 nothing can learn from is left exactly as
	// it arrived, to fail over or be forwarded to the client. OpenAI's
	// Responses-only refusal ("not a chat model") is a 404, so a
	// chat-completions 404 from an OpenAI provider takes the same path.
	if isLearnableRefusal(resp.StatusCode, providerType, candidate.provider.BaseURL, st) {
		res, handled := h.retryLearnable400(r, st, candidate, providerType, targetURL, resp, attempt, &dialMs, failoverCancel, streamCancelOrigin)
		if handled {
			resp = res.resp
			streamCancelOrigin = res.streamCancelOrigin
			retryCancel = res.retryCancel
			if res.retried || res.cont {
				// Accumulate the retries' dial time into the total. The cont path is
				// included because a transport failure on one round must not discard
				// what the rounds before it already spent dialling; dialMs is zero
				// when no round got that far, so the fold is a no-op there.
				st.timings.dialMs += dialMs
				dialMs = 0
				st.proxyOverhead = st.timings.proxyOverheadMs(st.parseMs)
			}
			if res.cont {
				st.attemptSlot.settle(false)
				st.setReqErr(res.lastReqErr)
				logData.closeAttemptRecord(0, res.lastReqErr.Kind, res.lastReqErr.Underlying, "", 0)
				return outcomeFailover
			}
		}
	}

	// MiniMax reports business errors (rate limit, exhausted plan balance,
	// auth failures) inside an HTTP 200 envelope, so they are remapped to an
	// effective status and the breaker, failover and error paths below, all
	// keyed on status codes, see the failure. The in-flight slot rides the body
	// only from here, with the remapped status deciding the clean flag.
	resp = remapMiniMaxBusinessError(providerType, candidate.provider.Name, resp)
	h.finishAttemptAdmission(st, candidate, resp)
	logData.noteAttemptStatus(resp.StatusCode)

	responseHeaderMs := float64(time.Since(st.startTime).Microseconds()) / 1000.0

	hasMoreCandidates := attempt < totalCandidates-1
	isFailoverEligible := h.shouldFailover(r.Context(), resp.StatusCode)

	rl := h.judge429AndRecordBreaker(r.Context(), st, candidate, resp, isFailoverEligible)

	shouldFailoverNow := isFailoverEligible && hasMoreCandidates
	debuglog.Debug("proxy: failover decision", "status", resp.StatusCode, "is_failover_eligible", isFailoverEligible, "has_more_candidates", hasMoreCandidates, "should_failover_now", shouldFailoverNow, "attempt", attempt+1)

	if shouldFailoverNow {
		// Read only what can be classified, then drain the rest straight to
		// Discard so the connection stays reusable without the body being held
		// in memory at whatever size the provider chose to send.
		drained, _ := io.ReadAll(io.LimitReader(resp.Body, failoverErrorClassifyCap))
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		// The body is being discarded anyway, so classify it on the way out.
		// A retired model usually answers 404, which is failover-eligible, so
		// without this the "model gone" signal would be lost whenever there is
		// another candidate to fall back to.
		//
		// The whole candidate goes through, not just the model: the retirement
		// is adjudicated by a real request to this provider, so it needs the
		// provider and the decrypted key. The endpoint family comes off the log
		// entry and decides whether the refusal can be adjudicated at all.
		drainedMsg := util.SanitizeLogBody(string(drained), 10000)
		kind, _ := classifyUpstreamError(resp.StatusCode, drainedMsg, candidate.model.ModelID)
		if kind == KindProviderModelGone {
			h.noteModelGone(candidate, logData.endpointType)
		}
		st.setReqErr(failoverReqErr(rl, attempt, candidate.provider.Name, resp.StatusCode))
		debuglog.Info("proxy: failover triggered", "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "status", resp.StatusCode, "rate_limit_class", rl.class.String())
		logData.failoverAttempt = attempt
		logData.closeAttemptRecord(resp.StatusCode, st.lastReqErr.Kind, drainedMsg, rl.phrase, 0)
		return outcomeFailover
	}

	// The last candidate's one-shot retries: a saturated 429 or a transient
	// 5xx gets one more try instead of a terminal error.
	if isFailoverEligible && !hasMoreCandidates {
		if outcome, ok := h.deferLastCandidateRetry(st, candidate, resp, attempt, rl); ok {
			return outcome
		}
	}

	// The whole 2xx range, not a bare 200: a relay or aggregator may answer a
	// chat completion 201 or 202, and routing those to forwardUpstreamError
	// would serve the client a complete answer while writing the row as
	// state="failed" and metering nothing, neither tokens against the virtual
	// key nor a TPM debit. servedSuccessStatus is the one definition, asked here
	// and at every other site that judges an upstream status (the breaker, the
	// hedge race, the retirement probe, the Anthropic ingress writer), so a 201
	// cannot be a success to one and a failure to another.
	if !servedSuccessStatus(resp.StatusCode) {
		return h.forwardUpstreamError(w, st, candidate, resp, attempt, isFailoverEligible, responseHeaderMs)
	}

	debuglog.Debug("proxy: upstream responded OK, dispatching to handler", "stream", st.isStreaming, "native_anthropic", st.anthropicNativeAttempt, "responses_api", st.responsesAttempt, "model", logData.modelID, "provider", logData.providerName, "provider_id", candidate.provider.ID, "status", resp.StatusCode)
	if st.responsesAttempt {
		// Translate the /v1/responses answer back to the chat-completions
		// shape on the upstream side, so the streaming pipeline (TTFT probe,
		// stall watchdog, transforms, metering) and the non-streaming handler
		// below run on what they already understand.
		if st.isStreaming {
			resp.Body = openairesponses.NewStreamAdapter(resp.Body, st.reqModel)
		} else if err := translateResponsesResponseBody(resp, st.reqModel); err != nil {
			// A 200 whose body cannot be read or is not a Responses object is
			// a provider fault; fail over like any other malformed upstream.
			return h.rejectUntranslatableBody(st, candidate, logData, "responses api", resp.StatusCode, err, attempt, r)
		}
	}
	if st.geminiAttempt {
		// Same upstream-side trick for the gemini egress adapter.
		if st.isStreaming {
			resp.Body = gemini.NewStreamAdapter(resp.Body, st.reqModel)
		} else if err := translateEgressResponseBody(resp, st.reqModel, gemini.BuildChatCompletion); err != nil {
			return h.rejectUntranslatableBody(st, candidate, logData, "gemini", resp.StatusCode, err, attempt, r)
		}
	}
	if st.anthropicEgressAttempt {
		// Same upstream-side trick for the anthropic egress adapter.
		if st.isStreaming {
			resp.Body = anthropicegress.NewStreamAdapter(resp.Body, st.reqModel)
		} else if err := translateEgressResponseBody(resp, st.reqModel, anthropicegress.BuildChatCompletion); err != nil {
			return h.rejectUntranslatableBody(st, candidate, logData, "anthropic egress", resp.StatusCode, err, attempt, r)
		}
	}
	if st.isStreaming {
		// Streaming cannot be judged yet: the provider can send 200 headers and
		// only then report the model gone in an SSE error, so that verdict is
		// deferred to dispatchStreaming once the stream has ended.
		return h.dispatchStreaming(w, r, st, candidate, resp, attempt, responseHeaderMs, streamCancelOrigin)
	}

	// A non-streaming answer clears any gone-strike streak the model had, judged
	// after the handler on what the handler decoded rather than on the 200 that
	// preceded it. Both halves of that placement are load-bearing:
	//
	//   - Below the dialect translations, because either of them can read the
	//     body, fail, and send the attempt to failover. A provider that answered
	//     200 with something that is not a Responses object or a Gemini answer
	//     has not served the model.
	//   - Below the handler, because a status is not an answer. `200
	//     {"choices":[]}` decodes and is forwarded as a normal completion, and
	//     is what an aggregator in front of a retired model returns between its
	//     gone-shaped 404s, resetting the count so three never land
	//     consecutively and the model is never nominated.
	//
	// producedOutput is where that line is drawn. The breaker verdict is
	// deferred to the handler's terminal write so the attempt trail's record
	// carries it; judgeAnswerNow is the fallback for a handler exit that wrote
	// nothing.
	h.deferAnswerJudgement(st, candidate, logData, resp.StatusCode)
	if st.anthropicNativeAttempt {
		outcome := h.handleNativeNonStreaming(w, r.WithContext(failoverCtx), st, resp, attempt, responseHeaderMs)
		judgeAnswerNow(logData)
		if producedOutput(logData) {
			h.noteModelServed(candidate.model, logData.endpointType)
		}
		return outcome
	}

	// The handler reads the upstream body under failoverCtx, so that is the
	// context it judges an interrupted read by. With the bare client request
	// instead, this gateway's own request_timeout looks like the provider dying.
	h.handleNonStreamingResponse(w, r.WithContext(failoverCtx), logData, resp, st.startTime, st.proxyOverhead, st.parseMs, st.timings.failoverLookupMs, st.timings.modelLookupMs, st.timings.providerLookupMs, st.timings.keyDecryptMs, st.timings.dialMs, st.timings.settingsReadMs, responseHeaderMs, st.vkHash, attempt)
	judgeAnswerNow(logData)
	if producedOutput(logData) {
		h.noteModelServed(candidate.model, logData.endpointType)
	}
	return outcomeServed
}

// classifyProbeError maps any TTFT probe failure to the error recorded for the
// attempt and whether the provider is charged for it. It is the single entry
// point both the sequential and the hedged path use, so the two cannot drift.
//
// An error envelope in the first frame, and a stream that ends at that frame,
// are split out from the zero-token cases classifyProbeFailure handles: the
// provider did answer, so this is its failure and never the client's.
func classifyProbeError(probeErr error, providerName string, masker credentialMasker, clientGone bool, elapsed, stallTimeout, ttftTimeout time.Duration, attempt int) (re reqError, recordFailure bool) {
	// The provider answered, but with nothing the caller can use: either it
	// reported an error, or it ended the stream without a single chunk. Neither
	// is ever the client's doing, so both are charged whatever the downstream
	// connection was up to.
	answered := func(underlying string) (reqError, bool) {
		return reqError{
			Kind:       KindProviderError,
			Attempt:    attempt,
			Provider:   providerName,
			Underlying: underlying,
		}, true
	}
	var frameErr *upstreamFrameError
	if errors.As(probeErr, &frameErr) {
		// This text is durable: on the last candidate it becomes the request
		// log's error_message, which the virtual key's owner can read. Every
		// other path that moves provider error text into that row masks the
		// credential first, and a provider is free to quote the key back inside
		// its error. Same treatment here.
		return answered(util.SanitizeLogBody(string(masker.mask([]byte(frameErr.msg))), 500))
	}
	var emptyErr *emptyStreamError
	if errors.As(probeErr, &emptyErr) {
		// Charged, exactly like an error frame: a zero-token answer is not a
		// valid one in almost any real use, so a provider that keeps producing
		// them belongs out of rotation. Silence is partly a function of the
		// prompt, so a caller can darken a provider for every tenant by
		// coercing a model into saying nothing; that risk is accepted.
		//
		// Gateway-authored text, so nothing to mask.
		return answered(emptyErr.Error())
	}
	return classifyProbeFailure(providerName, errString(probeErr), clientGone, elapsed, stallTimeout, ttftTimeout, attempt)
}

// classifyProbeFailure decides how a zero-token TTFT probe failure is recorded.
// The provider is at fault (provider_timeout, charged to the breaker and
// eligible for failover) when the gateway's own TTFT timer fired, or when the
// downstream connection closed only after the provider had already stayed
// silent past the stall timeout. A faster downstream close with zero tokens is
// a genuine client disconnect and is not charged. A close during a provider
// stall carries a hint naming an upstream reverse proxy, load balancer or CDN
// idle-read timeout, since nothing is sent downstream during the probe and a
// silent connection looks idle.
func classifyProbeFailure(providerName, underlying string, clientGone bool, elapsed, stallTimeout, ttftTimeout time.Duration, attempt int) (re reqError, recordFailure bool) {
	if clientGone && elapsed < stallTimeout {
		// Fast downstream close with zero tokens: a genuine client cancel.
		return reqError{Kind: KindClientDisconnect, Attempt: attempt, Provider: providerName, Underlying: underlying}, false
	}
	re = reqError{Kind: KindProviderTimeout, Attempt: attempt, Provider: providerName, Underlying: underlying}
	if clientGone {
		re.Hint = fmt.Sprintf("%s produced no first token before the connection was closed after %.0fs, under the %s first-token timeout; if the caller did not cancel, an upstream reverse proxy, load balancer, or CDN likely closed the idle connection: raise its read timeout above %s or set ttft_timeout below it", providerName, elapsed.Seconds(), ttftTimeout, ttftTimeout)
	}
	return re, true
}

// dispatchStreaming serves a streaming 200 response (phase H): read the TTFT and
// stall timeouts, build the streamOptions, run the TTFT probe (on success commit
// the breaker and stash the pre-read buffer; on failure close the body, classify
// the zero-token stall via classifyProbeFailure, record a breaker failure unless
// it was a fast client cancel, and fail over), then hand off to
// handleStreamingResponse. Returns outcomeServed on a served stream or
// outcomeFailover when the probe fails.
func (h *Handler) dispatchStreaming(w http.ResponseWriter, r *http.Request, st *requestState, candidate modelCandidate, resp *http.Response, attempt int, responseHeaderMs float64, streamCancelOrigin string) candidateOutcome {
	logData := st.logData
	ttftTimeout := h.settingsRepo.GetDuration(r.Context(), "ttft_timeout", 60*time.Second)
	stallTimeout := h.settingsRepo.GetDuration(r.Context(), "stream_stall_timeout", 30*time.Second)

	opts := streamOptions{
		responseHeaderMs:   responseHeaderMs,
		streamStallTimeout: stallTimeout,
		providerID:         candidate.provider.ID,
		providerName:       candidate.provider.Name,
		model:              candidateModelID(candidate),
		circuitBreakerOn:   st.circuitBreakerEnabled,
		proxyOverheadMs:    st.proxyOverhead,
		parseMs:            st.parseMs,
		failoverLookupMs:   st.timings.failoverLookupMs,
		modelLookupMs:      st.timings.modelLookupMs,
		providerLookupMs:   st.timings.providerLookupMs,
		keyDecryptMs:       st.timings.keyDecryptMs,
		dialMs:             st.timings.dialMs,
		settingsReadMs:     st.timings.settingsReadMs,
		vkHash:             st.vkHash,
		attempt:            attempt,
		cancelOrigin:       streamCancelOrigin,
		rawPassthrough:     st.anthropicNativeAttempt,
		masker:             logData.masker,
	}

	if ttftTimeout > 0 {
		// TTFT probe: read until first real data chunk.
		probeBuf, trueTtftMs, probeErr := h.probeFirstToken(r.Context(), resp.Body, ttftTimeout, st.startTime)
		if probeErr != nil {
			// Timeout or read error, so fail over. probeFirstToken may or may
			// not have closed the body (only on DeadlineExceeded); close it
			// unconditionally to release the connection.
			_ = resp.Body.Close()
			// Reaching here means zero "data:" tokens arrived from the provider.
			// classifyProbeFailure decides whether that is a provider stall
			// (recorded against the breaker, failover-eligible) or a genuinely
			// fast client cancel that must not penalize the provider.
			clientGone := r.Context().Err() != nil
			elapsed := time.Since(st.startTime)
			re, recordFailure := classifyProbeError(probeErr, candidate.provider.Name, newCredentialMasker(candidate.apiKey), clientGone, elapsed, stallTimeout, ttftTimeout, attempt)
			if recordFailure && st.circuitBreakerEnabled {
				logData.noteBreaker(breakerCharge)
				h.circuitBreaker.RecordFailure(candidate.provider.ID, candidate.provider.Name, candidateModelID(candidate), failover.Cause{Status: resp.StatusCode, Reason: "TTFT probe failed"})
			}
			st.setReqErr(re)
			logData.failoverAttempt = attempt
			logData.responseHeaderMs = responseHeaderMs
			logData.closeAttemptRecord(resp.StatusCode, re.Kind, re.Underlying, "", 0)
			// "kind", not "provider_stalled": a probe can fail because the
			// provider reported its own error rather than because it went
			// silent, and calling that a stall sends the operator hunting the
			// wrong thing. charged says what the breaker was told either way.
			debuglog.Warn("proxy: TTFT probe failed", "attempt", attempt+1, "provider", candidate.provider.Name, "client_gone", clientGone, "elapsed", elapsed, "kind", string(re.Kind), "charged", recordFailure, "error", logData.content.maskOne(re.Underlying))
			return outcomeFailover
		}
		// First token confirmed. No breaker success is recorded here: a first
		// token is not a served stream, and recording one would zero
		// consecutiveFails on every request, so the finalizer's own failure
		// charges could never reach the threshold. finalizeStream owns the
		// verdict for a streaming 200.
		opts.preReadBuf = probeBuf
		opts.trueTtftMs = trueTtftMs
	}

	h.handleStreamingResponse(w, r, logData, resp, st.startTime, opts)

	// The model is judged only once the stream has ended. deriveStreamError
	// classifies any in-stream SSE error into logData.errorKind, so a provider
	// that returns 200 headers and then reports the model gone mid-stream is
	// caught here rather than credited with a success.
	//
	// A stream that failed for any other reason (transient provider error,
	// client disconnect, stall) is not evidence either way and leaves the streak
	// alone: clearing it there would let a retired model stay routable
	// indefinitely, since its own failures would keep resetting the count.
	h.noteStreamOutcome(logData, candidate)
	return outcomeServed
}

// beginAttempt performs the per-attempt prologue shared by the chat and
// pass-through attempt fns: stamp the candidate's provider identity onto the
// log entry, pass the in-flight admission gate, emit the routing logs, touch
// the provider's last-used timestamp, build the upstream request on
// failoverCtx, and execute it. providerType and targetURL are returned for
// chat's 400 auto-retry path. busy=true means the provider's learned
// in-flight window is full and no request was made (the caller should treat
// the candidate as skipped); otherwise ok=false means the caller should fail
// over to the next candidate (st.lastErr is already set by this helper or
// doUpstream).
func (h *Handler) beginAttempt(failoverCtx context.Context, st *requestState, candidate modelCandidate, attempt, totalCandidates int, dialMs *float64) (resp *http.Response, providerType, targetURL string, busy, ok bool) {
	logData := st.logData
	logData.providerID = candidate.provider.ID
	logData.providerName = candidate.provider.Name
	logData.masker = newCredentialMasker(candidate.apiKey)
	// The attempt trail's record for this candidate opens here, before
	// admission: a busy skip is an attempt the operator wants to see too.
	logData.openAttemptRecord(attempt, candidate, false, time.Now(), st.circuitBreakerEnabled)
	// The 429 verdict is per attempt: reset it here so a terminal path can
	// never read a stale class off an earlier candidate's rate limit.
	st.rateLimit = rateLimitVerdict{}
	if st.isFailover {
		logData.resolvedModelID = candidate.model.ModelID
	}
	// Admission before anything is built or sent: a provider at its learned
	// window is skipped exactly as a breaker-open one is, without a round
	// trip. On admission a slot is held; every exit below settles it.
	if !h.admitCandidate(st, candidate) {
		st.setReqErr(reqError{Kind: KindProviderSaturated, Attempt: attempt, Provider: candidate.provider.Name, Detail: "at in-flight limit"})
		logData.failoverAttempt = attempt
		logData.noteBreaker(breakerNoop)
		logData.closeAttemptRecord(0, KindProviderSaturated, "at in-flight limit", "", 0)
		return nil, "", "", true, false
	}
	if attempt == 0 {
		debuglog.Info("proxy: routing to provider", "endpoint", logData.endpointType, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidate.model.ModelID, "total_candidates", totalCandidates)
	} else {
		debuglog.Info("proxy: failover attempt", "endpoint", logData.endpointType, "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidate.model.ModelID)
	}
	debuglog.Debug("proxy: candidate details", "provider_id", candidate.provider.ID, "provider", candidate.provider.Name, "model_id", candidate.model.ModelID, "provider_type", provider.TypeOf(candidate.provider), "attempt", attempt+1, "total_candidates", totalCandidates)
	h.touchProviderLastUsed(candidate.provider.ID)

	proxyReq, providerType, targetURL, err := h.buildCandidateRequest(failoverCtx, st, candidate)
	if err != nil {
		st.attemptSlot.settle(false)
		st.setReqErr(reqError{Kind: KindInternal, Attempt: attempt, Provider: candidate.provider.Name, Underlying: errString(err)})
		logData.closeAttemptRecord(0, KindInternal, errString(err), "", 0)
		return nil, providerType, targetURL, false, false
	}

	resp, upstreamOK := h.doUpstream(failoverCtx, proxyReq, st, candidate, attempt, dialMs)
	if !upstreamOK {
		st.attemptSlot.settle(false)
		// doUpstream set st.lastReqErr; no response was seen, so no status.
		logData.closeAttemptRecord(0, st.lastReqErr.Kind, st.lastReqErr.Underlying, "", 0)
		return nil, providerType, targetURL, false, false
	}
	// The held slot is not handed to the body here: the caller does that via
	// finishAttemptAdmission after the MiniMax status remap, so a business
	// error dressed as a 200 never counts as a clean completion.
	return resp, providerType, targetURL, false, true
}

// touchProviderLastUsed updates the provider's last-used timestamp in a
// fire-and-forget goroutine with its own timeout, so the request path is
// never blocked by a slow DB write.
func (h *Handler) touchProviderLastUsed(pid uuid.UUID) {
	go func(pid uuid.UUID) {
		defer func() {
			if r := recover(); r != nil {
				debuglog.Error("proxy: panic in TouchLastUsed (provider)", "error", r)
			}
		}()
		tctx, tcancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer tcancel()
		if err := h.providerRepo.TouchLastUsed(tctx, pid); err != nil {
			debuglog.Debug("proxy: failed to touch provider last-used", "error", err)
		}
	}(pid)
}

// buildCandidateRequest builds the upstream HTTP request for a single failover
// candidate (phase C): detect the provider type, build the target URL, rewrite
// the request body when needed, create the request on the provided context, and
// set the auth + content-type headers. The caller owns ctx cancellation; this
// helper never cancels it. providerType and targetURL are returned so the caller
// can thread them into the 400 auto-retry path.
//
// Chat requests (st.makeUpstreamBody == nil) go through the chat-specific
// rewrite (paramrewrite.BuildUpstreamBody: model rename, stream_options, param stripping).
// Multimodal requests provide st.makeUpstreamBody, which owns the body rewrite
// and its Content-Type (JSON model rename, or multipart reconstruction).
func (h *Handler) buildCandidateRequest(ctx context.Context, st *requestState, candidate modelCandidate) (*http.Request, string, string, error) {
	logData := st.logData
	providerType := provider.TypeOf(candidate.provider)
	debuglog.Debug("proxy: provider type", "provider_type", providerType, "base_url", util.SanitizeBaseURL(candidate.provider.BaseURL))

	// Native Anthropic passthrough: an Anthropic-in request resolved to an
	// Anthropic-family provider forwards the original Messages body to the
	// provider's native /v1/messages, so thinking blocks, cache_control and
	// fine-grained tool streaming survive. Every non-Anthropic candidate in the
	// same failover group still goes through translation, so a single
	// hotel/claude-* request can fail over from native to translated. The flag
	// is read by the response dispatch and writer.
	st.anthropicNativeAttempt = st.anthropicIn && isAnthropicFamily(providerType)
	// Per-attempt flags: a failover group can mix an OpenAI candidate that
	// needs /v1/responses (or a vertex-express one) with providers that
	// don't, so all dialect flags reset on every candidate.
	st.responsesAttempt = false
	st.geminiAttempt = false
	st.anthropicEgressAttempt = false
	st.speechFormat = ""
	st.transcriptionFormat = ""
	st.passthroughUsage = nil
	// The Messages self-heal is per candidate too. Within one attempt it cannot
	// fire twice, since attemptCandidate consults it from a single branch rather
	// than a loop, so carrying the flag forward would only deny the next
	// candidate a self-heal it has not used yet. Each candidate is a different
	// model behind a different endpoint, with its own facts to learn.
	st.messagesRetried = false
	st.lastMessagesBody = nil
	if st.anthropicNativeAttempt {
		return h.buildNativeAnthropicRequest(ctx, st, candidate, providerType)
	}
	if isGeminiSpeechAttempt(st, providerType, candidate.model.OutputModalities) { // Gemini TTS via generateContent
		return h.buildGeminiSpeechRequest(ctx, st, candidate, providerType)
	}
	if isGeminiTranscriptionAttempt(st, providerType, candidate.model.InputModalities) { // Gemini STT via generateContent
		return h.buildGeminiTranscriptionRequest(ctx, st, candidate, providerType)
	}

	// OpenAI Responses re-route: a model learned (from a prior 400) to reject
	// tools+reasoning over chat-completions is served via /v1/responses, with
	// the request translated out and the response translated back by the
	// dispatch.
	if h.shouldUseResponsesAttempt(st, candidate, providerType) {
		st.responsesAttempt = true
		return h.buildResponsesRequest(ctx, st, candidate, providerType)
	}

	// Gemini egress adapter: chat requests to a vertex-express provider, a
	// Zen Gemini model or a Google AI Studio image model are translated to
	// generateContent on the way out and back on the response side.
	if isGeminiEgressAttempt(st, providerType, candidate.model.ModelID, candidate.model.OutputModalities) {
		st.geminiAttempt = true
		return h.buildGeminiRequest(ctx, st, candidate, providerType)
	}

	// Anthropic egress adapter: a chat request is translated to Anthropic's
	// native Messages shape on the way out and back on the response side. Every
	// chat request to an anthropic-messages provider takes this route; for
	// anthropic, only one carrying a document does, and text and image requests
	// stay on the cheaper compat endpoint below.
	if isAnthropicEgressAttempt(st, providerType) {
		st.anthropicEgressAttempt = true
		return h.buildAnthropicEgressRequest(ctx, st, candidate, providerType)
	}

	endpoint := st.endpointPath
	if endpoint == "" {
		endpoint = "/chat/completions"
	}
	targetURL := util.BuildProviderTargetURL(candidate.provider.BaseURL, providerType, endpoint)
	debuglog.Debug("proxy: built target URL", "target_url", targetURL)

	upstreamBody := st.bodyBytes
	contentType := "application/json"
	if st.makeUpstreamBody != nil {
		var err error
		upstreamBody, contentType, err = st.makeUpstreamBody(candidate.model.ModelID)
		if err != nil {
			return nil, providerType, targetURL, err
		}
		// The body builder knows the model but not the provider, and an image
		// request carries parameters not every image API accepts: xAI answers
		// 400 to the "size" the OpenAI SDKs send by default. Adapted here, the
		// one place both are known.
		if logData.endpointType == endpointTypeImage && contentType == "application/json" {
			var droppedSize, ratio string
			upstreamBody, droppedSize, ratio = paramrewrite.RewriteImageRequest(upstreamBody, providerType, candidate.model.ModelID)
			if droppedSize != "" {
				debuglog.Debug("proxy: image size rewritten for the provider", "provider_type", providerType, "resolved_model", candidate.model.ModelID, "dropped_size", droppedSize, "aspect_ratio", ratio)
			}
		}
	} else {
		needsRewrite := st.reqModel != candidate.model.ModelID || isAnthropicFamily(providerType) || paramrewrite.NeedsProviderInjection(providerType) || st.isStreaming ||
			paramrewrite.HasLearnedRewrites(&h.deprecationCache, &h.paramRenameCache, learnedScopeFor(candidate), candidate.model.ModelID)
		debuglog.Debug("proxy: request rewrite check", "needs_rewrite", needsRewrite, "request_model", logData.modelID, "provider", logData.providerName, "resolved_model", candidate.model.ModelID, "provider_type", providerType)
		if needsRewrite {
			upstreamBody = paramrewrite.BuildUpstreamBody(st.bodyBytes, providerType, candidate.model.ModelID, st.reqModel, st.isStreaming, &h.deprecationCache, &h.paramRenameCache, nil, learnedScopeFor(candidate))
		}
		// Log the actual model name in the upstream body for debugging rewrite
		// issues. Chat-only: multipart bodies must never reach debug logs.
		if upstreamModel, _, _ := strings.Cut(string(upstreamBody), ","); strings.Contains(upstreamModel, `"model"`) {
			debuglog.Debug("proxy: upstream body model", "upstream_model_snippet", upstreamModel)
		}
	}

	proxyReq, err := newRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(upstreamBody))
	if err != nil {
		return nil, providerType, targetURL, err
	}

	util.SetProviderAuthHeaders(proxyReq, providerType, candidate.apiKey)
	proxyReq.Header.Set("Content-Type", contentType)
	debuglog.Debug("proxy: sending upstream request", "method", proxyReq.Method, "url", targetURL, "content_length", len(upstreamBody), "has_api_key", candidate.apiKey != "")
	return proxyReq, providerType, targetURL, nil
}

// resolveCancelOrigin names the cause behind a context error on an upstream
// attempt. A deadline reads the origin the derived context was created with
// (failover vs retry). A cancellation is the client hanging up, unless the
// hedging orchestrator abandoned this attempt because a faster candidate won:
// that cancellation is the gateway's own, and reporting it as a client
// disconnect describes a request the client is still receiving.
func resolveCancelOrigin(ctx context.Context, err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		if s, ok := ctx.Value(ctxkeys.CancelOriginKey).(string); ok && s != "" {
			return s
		}
		return "client_disconnect"
	}
	if sup, ok := ctx.Value(ctxkeys.HedgeSupersededKey).(*atomic.Bool); ok && sup.Load() {
		return "hedge_superseded"
	}
	return "client_disconnect"
}

// doUpstream executes the built request against the shared upstream transport
// (phase D): inject the per-request dial-timing pointer, run the request,
// retrying up to maxTransientRetries times against the same provider on
// transient network errors, fold each try's dial sample into the running
// timings, and recompute proxy overhead. Retries share the per-attempt failover
// timeout, replay the body via GetBody, and back off briefly between tries. On
// final failure it classifies the cause (client disconnect, failover or retry
// timeout, provider error) and records a breaker failure only for real provider
// errors, never for context cancellation.
// Returns (resp, true) on a usable response; (nil, false) after setting
// st.lastErr on a failover-worthy failure. The caller retains ownership of ctx
// cancellation.
func (h *Handler) doUpstream(ctx context.Context, req *http.Request, st *requestState, candidate modelCandidate, attempt int, dialMs *float64) (*http.Response, bool) {
	logData := st.logData
	// Reuse the shared upstream Transport instead of creating a new one
	// per request. A fresh Transport spawns persistent readLoop/writeLoop
	// goroutines per connection that only die after IdleConnTimeout, so
	// creating one per request causes unbounded goroutine growth.
	// Hand the request its own dial-timing slot for SafeDialer to write DNS
	// and TCP time into. A slot rather than the caller's *dialMs, because the
	// transport's dial goroutine can outlive Do (see dialTiming); the time is
	// swapped out into *dialMs once Do has returned.
	dialCtx, dialTimer := withDialTiming(ctx)

	var checkRedirect func(req *http.Request, via []*http.Request) error
	if h.safeDialer != nil {
		checkRedirect = h.safeDialer.CheckRedirect
	}
	upstreamClient := &http.Client{
		Transport:     h.upstreamTransport,
		CheckRedirect: checkRedirect,
	}

	var resp *http.Response
	var err error
	// lastTransportErr preserves the real provider/transport error that drove
	// the retry loop, so when a client disconnect or timeout later overwrites
	// `err` with a context error the original cause is still carried into the
	// structured error as Underlying.
	var lastTransportErr error
	for try := 0; ; try++ {
		// Track whether any request bytes reached the wire on this try, so
		// isRetryableUpstreamError can tell provably-safe pre-write failures
		// from ambiguous post-write ones. WroteHeaders may fire on a transport
		// goroutine, hence the atomic.
		var wroteRequest atomic.Bool
		tryCtx := httptrace.WithClientTrace(dialCtx, &httptrace.ClientTrace{
			WroteHeaders: func() { wroteRequest.Store(true) },
		})
		tryReq := req.WithContext(tryCtx)
		if try > 0 {
			// The previous try consumed (and the transport closed) the body.
			// GetBody is always set: buildCandidateRequest builds the request
			// from a bytes.Reader.
			body, gbErr := req.GetBody()
			if gbErr != nil {
				break
			}
			tryReq.Body = body
		}
		//nolint:gosec // provider URL is admin-configured, not arbitrary user input
		resp, err = upstreamClient.Do(tryReq)
		*dialMs += dialTimer.take()
		st.timings.dialMs += *dialMs
		*dialMs = 0
		if err == nil || try == maxTransientRetries || !isRetryableUpstreamError(err, wroteRequest.Load()) {
			break
		}
		// Retryable transport error: remember it before backing off, in case the
		// context is cancelled during the backoff and overwrites `err` below.
		lastTransportErr = err
		backoff := failoverBackoff(100*time.Millisecond, 500*time.Millisecond, try+1)
		debuglog.Warn("proxy: transient upstream error, retrying same provider", "attempt", attempt+1, "try", try+1, "backoff", backoff, "request_written", wroteRequest.Load(), "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "error", err)
		select {
		case <-time.After(backoff):
		case <-dialCtx.Done():
		}
		// Client disconnect or failover timeout during backoff: stop retrying
		// and surface the context error so the classification below does not
		// penalize the circuit breaker. Checked outside the select because when
		// both channels are ready Go picks a branch at random, and the timer
		// branch must not leave the transport error in err.
		if ctxErr := dialCtx.Err(); ctxErr != nil {
			err = ctxErr
			break
		}
	}
	st.proxyOverhead = st.timings.proxyOverheadMs(st.parseMs)
	if err != nil {
		// "context canceled" is opaque, so the origin decides the error the
		// caller sees: a client disconnect, the hedging orchestrator abandoning
		// this attempt for a faster one, or an expired deadline.
		// resolveCancelOrigin owns that classification.
		isContextErr := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
		if isContextErr {
			cancelOrigin := resolveCancelOrigin(dialCtx, err)
			// The context error is the terminal cause, but the provider error
			// that drove the retries (lastTransportErr) is preserved as
			// Underlying so it survives into the request log and response.
			st.setReqErr(reqError{
				Kind:       cancelOriginToKind(cancelOrigin),
				Attempt:    attempt,
				Provider:   candidate.provider.Name,
				Underlying: errString(lastTransportErr),
			})
			debuglog.Info("proxy: context cancelled during request to provider", "provider", logData.providerName, "provider_id", candidate.provider.ID, "model", logData.modelID, "origin", cancelOrigin, "error", err, "underlying", errString(lastTransportErr))
		} else {
			st.setReqErr(reqError{
				Kind:       KindProviderError,
				Attempt:    attempt,
				Provider:   candidate.provider.Name,
				Underlying: errString(err),
			})
			debuglog.Warn("proxy: upstream request failed", "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "error", err)
		}
		// Client-initiated cancellations and deadline exceeded are not provider
		// failures, so the circuit breaker is not charged for them. Real
		// provider errors record exactly one breaker failure per candidate
		// attempt, here, after any transient retries are exhausted, so a blip
		// that self-heals on retry never counts against the provider.
		if !isContextErr {
			if st.circuitBreakerEnabled {
				// No status: the request never completed, so there is none.
				st.logData.noteBreaker(breakerCharge)
				h.circuitBreaker.RecordFailure(candidate.provider.ID, candidate.provider.Name, candidateModelID(candidate), failover.Cause{Reason: "upstream request failed"})
			}
		}
		return nil, false
	}

	// Log upstream response metadata for debugging.
	debuglog.Debug("proxy: upstream response received", "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidate.model.ModelID, "status", resp.StatusCode, "content_type", resp.Header.Get("Content-Type"), "x_request_id", resp.Header.Get("X-Request-Id"), "x_ratelimit_remaining", resp.Header.Get("X-RateLimit-Remaining"), "attempt", attempt+1)
	return resp, true
}

// isLearnableRefusal reports an upstream status the attempt loop hands to
// retryLearnable400: every 400, and a chat-completions 404 from OpenAI's own
// host, which is how OpenAI refuses a Responses-only model. The 404 arm
// applies to the chat endpoint in the chat dialect only, the same shape the
// Responses reroute itself is limited to; a 404 on embeddings, a translated
// dialect, or a relay of unknown make (typed "openai" too) is left as it
// arrived. The hedged path reads the same verdict to learn without retrying.
func isLearnableRefusal(status int, providerType, baseURL string, st *requestState) bool {
	if status == 400 {
		return true
	}
	return status == 404 && providerType == "openai" && isOpenAIHost(baseURL) &&
		st.endpointPath == "" && st.makeUpstreamBody == nil && st.sentChatCompletionsBody()
}
