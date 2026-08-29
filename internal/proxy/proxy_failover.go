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
	"github.com/hugalafutro/model-hotel/internal/gemini"
	"github.com/hugalafutro/model-hotel/internal/openairesponses"
	"github.com/hugalafutro/model-hotel/internal/paramrewrite"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// failoverErrorClassifyCap bounds how much of a discarded failover error body is
// held in memory long enough to be classified.
//
// The client is served by the next candidate, and the only thing still wanted
// from the body is whether the provider said the model is retired.
// classifyUpstreamError never sees more than util.SanitizeLogBody's own 10 000
// characters, so everything above this cap is read and discarded rather than
// retained.
//
// Shared by the chat loop and the multimodal pass-through loop, which run the
// same drain-and-classify block. Sixteen kibibytes is comfortably above any
// error message and well below the multi-megabyte bodies the image endpoints can
// answer with.
const failoverErrorClassifyCap = 16 << 10

// failAllExhausted handles phase E: every candidate failed (or the overall
// deadline was hit). It logs the exhaustion, records a 502 failure row, and
// writes the failover-vs-single-provider error response. numCandidates is the
// resolved candidate count (for the failRequest attempt index).
func (h *Handler) failAllExhausted(w http.ResponseWriter, st *requestState, numCandidates int) {
	last := st.lastReqErr
	status := last.terminalStatus()
	logMsg := last.terminalLogMessage(st.isFailover, numCandidates)
	clientMsg := last.terminalClientMessage(st.reqModel, st.isFailover)
	if st.isFailover {
		debuglog.Error("proxy: all providers exhausted", "model", st.logData.modelID, "provider", st.logData.providerName, "error", logMsg, "kind", string(last.Kind), "status", status, "candidates", numCandidates, "failover_timeout", st.failoverTimeout)
	} else {
		debuglog.Error("proxy: provider request failed", "model", st.logData.modelID, "provider", st.logData.providerName, "error", logMsg, "kind", string(last.Kind), "status", status, "request_timeout", st.failoverTimeout)
	}
	st.logData.providerID = uuid.Nil
	h.failRequest(st.logData, status, last.Kind, logMsg, numCandidates-1, st.startTime, st.parseMs, st.timings, st.cacheHits, st.proxyOverhead)
	writeOpenAIError(w, clientMsg, status)
}

// attemptCandidate runs one failover attempt against a single candidate (phase
// D3–D11): build and send the upstream request, handle the 400 auto-retry,
// record the circuit-breaker outcome, and either fail over to the next
// candidate, forward a terminal error, or dispatch the 200 response.
//
// It owns the per-attempt request contexts: failoverCtx is cancelled via a
// deferred call (and the retry context, once created, via a second deferred
// call), so no exit path can leak a context. The deferred cancels fire as the
// method returns — i.e. AFTER the streaming/non-streaming dispatch has consumed
// the body — preserving the "cancel only after the body is consumed" ordering.
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
	resp, providerType, targetURL, ok := h.beginAttempt(failoverCtx, st, candidate, attempt, totalCandidates, &dialMs)
	if !ok {
		return outcomeFailover
	}

	// Auto-retry learnable 400s (see retryLearnable400 for which are learnable in
	// which dialect). A 400 nothing can learn from is left exactly as it arrived,
	// to fail over or be forwarded to the client.
	if resp.StatusCode == 400 {
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
				st.setReqErr(res.lastReqErr)
				return outcomeFailover
			}
		}
	}

	// MiniMax reports business errors (rate limit, exhausted plan balance,
	// auth failures) inside an HTTP 200 envelope; remap them to an effective
	// status so the breaker/failover/error paths below — all keyed on status
	// codes — see the failure.
	resp = remapMiniMaxBusinessError(providerType, candidate.provider.Name, resp)

	responseHeaderMs := float64(time.Since(st.startTime).Microseconds()) / 1000.0

	hasMoreCandidates := attempt < totalCandidates-1
	isFailoverEligible := h.shouldFailover(r.Context(), resp.StatusCode)

	h.recordBreakerOutcome(st, candidate, resp.StatusCode, isFailoverEligible)

	shouldFailoverNow := isFailoverEligible && hasMoreCandidates
	debuglog.Debug("proxy: failover decision", "status", resp.StatusCode, "is_failover_eligible", isFailoverEligible, "has_more_candidates", hasMoreCandidates, "should_failover_now", shouldFailoverNow, "attempt", attempt+1)

	if shouldFailoverNow {
		// Read only what can be classified, then drain the rest straight to
		// Discard so the connection stays reusable without the body being held in
		// memory. Everything above the cap was being retained to be thrown away —
		// once per concurrent failing request, on the request path, at whatever
		// size the provider chose to send.
		drained, _ := io.ReadAll(io.LimitReader(resp.Body, failoverErrorClassifyCap))
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		// The body is being discarded anyway, so classify it on the way out.
		// A retired model usually answers 404, which is failover-eligible, so
		// without this the "model gone" signal would be lost precisely when
		// there is another candidate to fall back to.
		//
		// The whole candidate goes through, not just the model: the retirement is
		// adjudicated by a real request to this provider, so it needs the provider
		// and the decrypted key already in hand here. The endpoint family comes
		// off the log entry and decides whether the refusal can be adjudicated at
		// all.
		if kind, _ := classifyUpstreamError(resp.StatusCode, util.SanitizeLogBody(string(drained), 10000), candidate.model.ModelID); kind == KindProviderModelGone {
			h.noteModelGone(candidate, logData.endpointType)
		}
		st.setReqErr(reqError{Kind: KindProviderError, Attempt: attempt, Provider: candidate.provider.Name, Detail: fmt.Sprintf("HTTP %d", resp.StatusCode)})
		debuglog.Info("proxy: failover triggered", "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "status", resp.StatusCode)
		logData.failoverAttempt = attempt
		return outcomeFailover
	}

	if resp.StatusCode != http.StatusOK {
		return h.forwardUpstreamError(w, st, candidate, resp, attempt, isFailoverEligible, responseHeaderMs)
	}

	debuglog.Debug("proxy: upstream responded OK, dispatching to handler", "stream", st.isStreaming, "native_anthropic", st.anthropicNativeAttempt, "responses_api", st.responsesAttempt, "model", logData.modelID, "provider", logData.providerName, "provider_id", candidate.provider.ID, "status", resp.StatusCode)
	if st.responsesAttempt {
		// Translate the /v1/responses answer back to the chat-completions
		// shape on the UPSTREAM side, so the streaming pipeline (TTFT probe,
		// stall watchdog, transforms, metering) and the non-streaming handler
		// below run unchanged on what they already understand.
		if st.isStreaming {
			resp.Body = openairesponses.NewStreamAdapter(resp.Body, st.reqModel)
		} else if err := translateResponsesResponseBody(resp, st.reqModel); err != nil {
			// A 200 whose body cannot be read or is not a Responses object is
			// a provider fault; fail over like any other malformed upstream.
			return h.rejectUntranslatableBody(st, candidate, logData, "responses api", err, attempt)
		}
	}
	if st.geminiAttempt {
		// Same upstream-side trick for the gemini egress adapter.
		if st.isStreaming {
			resp.Body = gemini.NewStreamAdapter(resp.Body, st.reqModel)
		} else if err := translateEgressResponseBody(resp, st.reqModel, gemini.BuildChatCompletion); err != nil {
			return h.rejectUntranslatableBody(st, candidate, logData, "gemini", err, attempt)
		}
	}
	if st.anthropicEgressAttempt {
		// Same upstream-side trick for the anthropic egress adapter.
		if st.isStreaming {
			resp.Body = anthropicegress.NewStreamAdapter(resp.Body, st.reqModel)
		} else if err := translateEgressResponseBody(resp, st.reqModel, anthropicegress.BuildChatCompletion); err != nil {
			return h.rejectUntranslatableBody(st, candidate, logData, "anthropic egress", err, attempt)
		}
	}
	if st.isStreaming {
		// Streaming cannot be judged yet: the provider can send 200 headers and
		// only then report the model gone in an SSE error, so that verdict is
		// deferred to dispatchStreaming once the stream has ended.
		return h.dispatchStreaming(w, r, st, candidate, resp, attempt, responseHeaderMs, streamCancelOrigin)
	}

	// A non-streaming answer clears any gone-strike streak the model had, judged
	// AFTER the handler on what the handler decoded rather than on the 200 that
	// preceded it. Both halves of that placement are load-bearing:
	//
	//   - Below the dialect translations, because either of them can read the
	//     body, fail, and send the attempt to FAILOVER. A provider that answered
	//     200 with something that is not a Responses object or a Gemini answer has
	//     not served the model.
	//   - Below the handler, because a status is not an answer. `200
	//     {"choices":[]}` decodes and is forwarded as a normal completion, and is
	//     what an aggregator in front of a retired model returns between its
	//     gone-shaped 404s — resetting the count so three never land
	//     consecutively and the model is never nominated.
	//
	// producedOutput is where that line is drawn.
	if st.anthropicNativeAttempt {
		outcome := h.handleNativeNonStreaming(w, r, st, resp, attempt, responseHeaderMs)
		h.recordAnswerOutcome(st, candidate, logData)
		if producedOutput(logData) {
			h.noteModelServed(candidate.model, logData.endpointType)
		}
		return outcome
	}

	h.handleNonStreamingResponse(w, r, logData, resp, st.startTime, st.proxyOverhead, st.parseMs, st.timings.failoverLookupMs, st.timings.modelLookupMs, st.timings.providerLookupMs, st.timings.keyDecryptMs, st.timings.dialMs, st.timings.settingsReadMs, responseHeaderMs, st.vkHash, attempt)
	h.recordAnswerOutcome(st, candidate, logData)
	if producedOutput(logData) {
		h.noteModelServed(candidate.model, logData.endpointType)
	}
	return outcomeServed
}

// classifyProbeFailure decides how a zero-token TTFT probe failure is recorded.
// Reaching a probe failure means the provider produced no "data:" token within
// the window. The provider is at fault — provider_timeout, recorded against the
// breaker and eligible for failover — when either our own TTFT timer fired
// (clientGone == false) or the downstream connection was closed only after the
// provider had already stayed silent past the stall timeout. A faster downstream
// close with zero tokens is treated as a genuine client disconnect and is not
// charged to the provider. When the connection was closed downstream while the
// provider was stalling, the error carries a hint that an upstream reverse proxy,
// load balancer, or CDN idle-read timeout is the likely cause (Model Hotel sends
// nothing downstream during the probe, so a silent connection looks idle).
// classifyProbeError maps any TTFT probe failure to the error recorded for the
// attempt and whether the provider is charged for it. It is the single entry
// point both the sequential and the hedged path use, so the two cannot drift.
//
// An error envelope in the first frame, and a stream that ends at that frame,
// are split out from the zero-token cases below: the provider did answer, so
// this is its failure and never the client's.
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
		// Charged, exactly like an error frame. A zero-token answer is not a
		// valid one in almost any real use, so a provider that keeps producing
		// them belongs out of rotation.
		//
		// This was once left uncharged on the reasoning that silence is a
		// function of the PROMPT, which the caller controls, so one virtual key
		// could darken a provider for every tenant. That risk is real and
		// accepted: a caller deliberately coercing a model into saying nothing
		// is not a case worth keeping a provider in rotation for.
		//
		// Gateway-authored text, so nothing to mask.
		return answered(emptyErr.Error())
	}
	return classifyProbeFailure(providerName, errString(probeErr), clientGone, elapsed, stallTimeout, ttftTimeout, attempt)
}

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
			// Timeout or read error — failover. probeFirstToken may
			// or may not have closed the body (only on DeadlineExceeded);
			// close it unconditionally to release the connection.
			_ = resp.Body.Close()
			// Reaching here means zero "data:" tokens arrived from the provider.
			// classifyProbeFailure decides whether that is a provider stall
			// (recorded against the breaker, failover-eligible) or a genuinely
			// fast client cancel that must not penalize the provider.
			clientGone := r.Context().Err() != nil
			elapsed := time.Since(st.startTime)
			re, recordFailure := classifyProbeError(probeErr, candidate.provider.Name, newCredentialMasker(candidate.apiKey), clientGone, elapsed, stallTimeout, ttftTimeout, attempt)
			if recordFailure && st.circuitBreakerEnabled {
				h.circuitBreaker.RecordFailure(candidate.provider.ID, candidate.provider.Name)
			}
			st.setReqErr(re)
			logData.failoverAttempt = attempt
			logData.responseHeaderMs = responseHeaderMs
			// "kind", not "provider_stalled": a probe can now fail because the
			// provider reported its own error rather than because it went
			// silent, and calling that a stall sends the operator hunting the
			// wrong thing. charged says what the breaker was told either way.
			debuglog.Warn("proxy: TTFT probe failed", "attempt", attempt+1, "provider", candidate.provider.Name, "client_gone", clientGone, "elapsed", elapsed, "kind", string(re.Kind), "charged", recordFailure, "error", re.Underlying)
			return outcomeFailover
		}
		// First token confirmed. No breaker success is
		// recorded here: a first token is not a served stream, and recording one
		// zeroes consecutiveFails on every request, which left the finalizer's
		// own failure charges unable to ever reach the threshold. finalizeStream
		// owns the verdict for a streaming 200 — see judgeStreamForBreaker.
		opts.preReadBuf = probeBuf
		opts.trueTtftMs = trueTtftMs
	}

	h.handleStreamingResponse(w, r, logData, resp, st.startTime, opts)

	// Judge the model only now. deriveStreamError classifies any in-stream SSE
	// error into logData.errorKind, so a provider that returns 200 headers and
	// then reports the model gone mid-stream is caught here rather than being
	// credited with a success. Without this the streak could never accumulate on
	// that path: the 200 would have cleared it before the error even arrived.
	//
	// The three cases are deliberately distinct. A stream that failed for any
	// OTHER reason (transient provider error, client disconnect, stall) is not
	// evidence either way, so it must leave the streak alone: clearing it there
	// would let a retired model stay routable indefinitely, since its own
	// failures would keep resetting the count.
	h.noteStreamOutcome(logData, candidate)
	return outcomeServed
}

// beginAttempt performs the per-attempt prologue shared by the chat and
// pass-through attempt fns: stamp the candidate's provider identity onto the
// log entry, emit the routing logs, touch the provider's last-used timestamp,
// build the upstream request on failoverCtx, and execute it. providerType and
// targetURL are returned for chat's 400 auto-retry path. Returns ok=false
// when the caller should fail over to the next candidate (st.lastErr is
// already set by this helper or doUpstream).
func (h *Handler) beginAttempt(failoverCtx context.Context, st *requestState, candidate modelCandidate, attempt, totalCandidates int, dialMs *float64) (resp *http.Response, providerType, targetURL string, ok bool) {
	logData := st.logData
	logData.providerID = candidate.provider.ID
	logData.providerName = candidate.provider.Name
	logData.masker = newCredentialMasker(candidate.apiKey)
	if st.isFailover {
		logData.resolvedModelID = candidate.model.ModelID
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
		st.setReqErr(reqError{Kind: KindInternal, Attempt: attempt, Provider: candidate.provider.Name, Underlying: errString(err)})
		return nil, providerType, targetURL, false
	}

	resp, upstreamOK := h.doUpstream(failoverCtx, proxyReq, st, candidate, attempt, dialMs)
	if !upstreamOK {
		return nil, providerType, targetURL, false
	}
	return resp, providerType, targetURL, true
}

// touchProviderLastUsed updates the provider's last-used timestamp in a
// fire-and-forget goroutine with its own timeout, so the request path is
// never blocked by a slow DB write.
func (h *Handler) touchProviderLastUsed(pid uuid.UUID) {
	//nolint:gosec // intentional: failover goroutine needs independent lifecycle
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
	// Anthropic-family provider forwards the ORIGINAL Messages body to the
	// provider's native /v1/messages (max fidelity: thinking blocks,
	// cache_control, fine-grained tool streaming survive). Every non-Anthropic
	// candidate in the same failover group still goes through translation, so a
	// single hotel/claude-* request can fail over from native to translated
	// seamlessly. The flag is read by the response dispatch + writer.
	st.anthropicNativeAttempt = st.anthropicIn && isAnthropicFamily(providerType)
	// Per-attempt flags: a failover group can mix an OpenAI candidate that
	// needs /v1/responses (or a vertex-express one) with providers that
	// don't, so all dialect flags reset on every candidate.
	st.responsesAttempt = false
	st.geminiAttempt = false
	st.anthropicEgressAttempt = false
	// The Messages self-heal is per candidate too. Within one attempt it cannot
	// fire twice — attemptCandidate consults it from a single branch, not a loop
	// — so carrying the flag forward would only deny the NEXT candidate a
	// self-heal it has not used yet, which is precisely the multi-provider case
	// a failover group exists for. Each candidate is a different model behind a
	// different endpoint, with its own facts to learn.
	st.messagesRetried = false
	st.lastMessagesBody = nil
	if st.anthropicNativeAttempt {
		return h.buildNativeAnthropicRequest(ctx, st, candidate, providerType)
	}

	// OpenAI Responses re-route: a model learned (from a prior 400) to reject
	// tools+reasoning over chat-completions is served via /v1/responses, with
	// the request translated out and the response translated back by the
	// dispatch (see openai_responses.go).
	if h.shouldUseResponsesAttempt(st, candidate, providerType) {
		st.responsesAttempt = true
		return h.buildResponsesRequest(ctx, st, candidate, providerType)
	}

	// Vertex express egress adapter: chat requests to a vertex-express
	// provider are translated to Gemini generateContent on the way out and
	// back on the response side (see gemini_egress.go).
	if isGeminiEgressAttempt(st, providerType, candidate.model.ModelID) {
		st.geminiAttempt = true
		return h.buildGeminiRequest(ctx, st, candidate, providerType)
	}

	// Anthropic egress adapter: a chat request is translated to Anthropic's
	// native Messages shape on the way out and back on the response side (see
	// anthropic_egress.go). Every chat request to an anthropic-messages
	// provider takes this route; for anthropic, only one carrying a document
	// does, and text and image requests stay on the cheaper compat endpoint
	// below.
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
	} else {
		needsRewrite := st.reqModel != candidate.model.ModelID || isAnthropicFamily(providerType) || paramrewrite.NeedsProviderInjection(providerType) || st.isStreaming
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
// (failover vs retry). A cancellation is the client hanging up UNLESS the
// hedging orchestrator abandoned this attempt because a faster candidate won —
// that cancellation is ours, and reporting it as a client disconnect describes
// a request the client is still happily receiving.
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
// (phase D): inject the per-request dial-timing pointer, run the request —
// retrying up to maxTransientRetries times against the same provider on
// transient network errors (see isRetryableUpstreamError) — fold each try's
// dial sample into the running timings, and recompute proxy overhead. Retries
// share the per-attempt failover timeout, replay the body via GetBody, and
// back off briefly between tries. On final failure it classifies the cause —
// client disconnect vs failover/retry timeout vs provider error — and records a
// breaker failure only for real provider errors, never for context cancellation.
// Returns (resp, true) on a usable response; (nil, false) after setting
// st.lastErr on a failover-worthy failure. The caller retains ownership of ctx
// cancellation.
func (h *Handler) doUpstream(ctx context.Context, req *http.Request, st *requestState, candidate modelCandidate, attempt int, dialMs *float64) (*http.Response, bool) {
	logData := st.logData
	// Reuse the shared upstream Transport instead of creating a new one
	// per request. A fresh Transport spawns persistent readLoop/writeLoop
	// goroutines per connection that only die after IdleConnTimeout, so
	// creating one per request causes unbounded goroutine growth.
	// Inject per-request dial timing pointer so SafeDialer writes
	// DNS resolution time into this request's own variable, avoiding
	// cross-request race conditions on a shared atomic.
	dialCtx := context.WithValue(ctx, ctxkeys.DialMsKey, dialMs)

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
	// the retry loop, so that if a client disconnect or timeout later overwrites
	// `err` with a context error (below), the original cause is not lost — it is
	// carried into the structured error as Underlying. This is the fix for the
	// "provider request failed: client disconnected" bug where the real provider
	// error was silently dropped.
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
		// both channels are ready Go picks a branch at random — the timer
		// branch must not leave the transport error in err.
		if ctxErr := dialCtx.Err(); ctxErr != nil {
			err = ctxErr
			break
		}
	}
	st.proxyOverhead = st.timings.proxyOverheadMs(st.parseMs)
	if err != nil {
		// Determine the origin of context cancellation for actionable errors.
		// "context canceled" is opaque — we need to know whether the client
		// disconnected, the hedging orchestrator abandoned this attempt for a
		// faster one, or a deadline expired. resolveCancelOrigin owns that
		// classification.
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
		// Client-initiated cancellations and deadline exceeded are not
		// provider failures. If the caller disconnected (Canceled) or
		// the request timed out (DeadlineExceeded), we must not penalize
		// the circuit breaker for that. Real provider errors record exactly
		// one breaker failure per candidate attempt — here, after any
		// transient retries are exhausted — so a blip that self-heals on
		// retry never counts against the provider.
		if !isContextErr {
			if st.circuitBreakerEnabled {
				h.circuitBreaker.RecordFailure(candidate.provider.ID, candidate.provider.Name)
			}
		}
		return nil, false
	}

	// Log upstream response metadata for debugging.
	debuglog.Debug("proxy: upstream response received", "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidate.model.ModelID, "status", resp.StatusCode, "content_type", resp.Header.Get("Content-Type"), "x_request_id", resp.Header.Get("X-Request-Id"), "x_ratelimit_remaining", resp.Header.Get("X-RateLimit-Remaining"), "attempt", attempt+1)
	return resp, true
}
