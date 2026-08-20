package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"regexp"
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

// responsesLearnBodyCap bounds a 400 that has to be parsed rather than scanned.
//
// openairesponses.RequiresResponsesAPI json.Unmarshals the whole error document,
// so the classifier's window is the wrong size for it: a body cut off mid-JSON
// does not parse, and the /v1/responses requirement would silently stop being
// learned. A megabyte is far past any real 400 and still bounded.
const responsesLearnBodyCap = 1 << 20

// paramRetryResult is the outcome of the 400 param-stripping auto-retry
// (retryWithStrippedParams). It tells the failover loop how to proceed:
//   - resp: the response to continue handling with — the last retry's response
//     once any retry was issued, otherwise the original 400. Every 400 that
//     leaves here carries a restored body, so normal non-200 handling can read
//     it whether it is the original or the one a retry earned.
//   - retryCancel: the retry context's cancel func, non-nil only when a retry
//     response is live and its body has NOT yet been consumed. The caller must
//     call it after consuming the body.
//   - streamCancelOrigin: "retry_timeout" once a retry was issued, otherwise
//     the caller's original value, unchanged.
//   - retried: true once a retry request was issued and answered, whatever it
//     answered with — the caller must fold the retries' dial time into the
//     running totals.
//   - lastReqErr: set only when cont is true; the structured cause the caller
//     records via st.setReqErr before failing over.
//   - cont: true => the caller should `continue` to the next candidate (a retry
//     request could not be created or failed).
type paramRetryResult struct {
	resp               *http.Response
	retryCancel        context.CancelFunc
	streamCancelOrigin string
	retried            bool
	lastReqErr         reqError
	cont               bool
}

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

	resp, providerType, targetURL, ok := h.beginAttempt(failoverCtx, st, candidate, attempt, totalCandidates, &dialMs)
	if !ok {
		return outcomeFailover
	}

	// Auto-retry param-rejection 400s: parse the error, learn which params
	// are rejected for this model, strip them, and retry once.
	// Works universally — any LLM API mentioning "temperature" or "top_p"
	// in a 400 error can only mean the sampling parameter.
	//
	// Skipped on every dialect attempt (see sentChatCompletionsBody): this
	// self-heal rebuilds the OpenAI-shaped st.bodyBytes via
	// paramrewrite.BuildUpstreamBody and re-POSTs it to the same targetURL, which
	// for a native /v1/messages, /v1/responses or generateContent route is a
	// malformed request — and those endpoints' 400s name their own fields, not
	// OpenAI's. A dialect 400 fails over or is forwarded as-is.
	//
	// Ordering, for the chat-completions case: retryWithResponses runs BEFORE the
	// param retry because a Responses-required 400 names reasoning_effort, which
	// the param parser would otherwise learn as a strip (useless on
	// reason-by-default models).
	if resp.StatusCode == 400 && st.sentChatCompletionsBody() {
		res, handled := h.retryWithResponses(r, st, candidate, providerType, resp, attempt, &dialMs, failoverCancel, streamCancelOrigin)
		if !handled {
			res = h.retryWithStrippedParams(r, st, candidate, providerType, targetURL, resp, attempt, &dialMs, failoverCancel, streamCancelOrigin)
		}
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
			debuglog.Warn("proxy: responses api translation failed", "error", err, "model", logData.modelID, "provider", logData.providerName)
			st.setReqErr(reqError{Kind: KindProviderError, Attempt: attempt, Provider: candidate.provider.Name, Underlying: errString(err)})
			logData.failoverAttempt = attempt
			return outcomeFailover
		}
	}
	if st.geminiAttempt {
		// Same upstream-side trick for the gemini egress adapter.
		if st.isStreaming {
			resp.Body = gemini.NewStreamAdapter(resp.Body, st.reqModel)
		} else if err := translateEgressResponseBody(resp, st.reqModel, gemini.BuildChatCompletion); err != nil {
			debuglog.Warn("proxy: gemini translation failed", "error", err, "model", logData.modelID, "provider", logData.providerName)
			st.setReqErr(reqError{Kind: KindProviderError, Attempt: attempt, Provider: candidate.provider.Name, Underlying: errString(err)})
			logData.failoverAttempt = attempt
			return outcomeFailover
		}
	}
	if st.anthropicEgressAttempt {
		// Same upstream-side trick for the anthropic egress adapter.
		if st.isStreaming {
			resp.Body = anthropicegress.NewStreamAdapter(resp.Body, st.reqModel)
		} else if err := translateEgressResponseBody(resp, st.reqModel, anthropicegress.BuildChatCompletion); err != nil {
			debuglog.Warn("proxy: anthropic egress translation failed", "error", err, "model", logData.modelID, "provider", logData.providerName)
			st.setReqErr(reqError{Kind: KindProviderError, Attempt: attempt, Provider: candidate.provider.Name, Underlying: errString(err)})
			logData.failoverAttempt = attempt
			return outcomeFailover
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
		if producedOutput(logData) {
			h.noteModelServed(candidate.model, logData.endpointType)
		}
		return outcome
	}

	h.handleNonStreamingResponse(w, r, logData, resp, st.startTime, st.proxyOverhead, st.parseMs, st.timings.failoverLookupMs, st.timings.modelLookupMs, st.timings.providerLookupMs, st.timings.keyDecryptMs, st.timings.dialMs, st.timings.settingsReadMs, responseHeaderMs, st.vkHash, attempt)
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
			re, recordFailure := classifyProbeFailure(candidate.provider.Name, errString(probeErr), clientGone, elapsed, stallTimeout, ttftTimeout, attempt)
			if recordFailure && st.circuitBreakerEnabled {
				h.circuitBreaker.RecordFailure(candidate.provider.ID, candidate.provider.Name)
			}
			st.setReqErr(re)
			logData.failoverAttempt = attempt
			logData.responseHeaderMs = responseHeaderMs
			debuglog.Warn("proxy: TTFT probe failed", "attempt", attempt+1, "provider", candidate.provider.Name, "client_gone", clientGone, "elapsed", elapsed, "provider_stalled", recordFailure, "error", probeErr)
			return outcomeFailover
		}
		// First token confirmed (or [DONE] received).
		if st.circuitBreakerEnabled {
			h.circuitBreaker.RecordSuccess(candidate.provider.ID, candidate.provider.Name)
		}
		opts.preReadBuf = probeBuf
		opts.trueTtftMs = trueTtftMs
	} else if st.circuitBreakerEnabled {
		// Disabled — immediate commit (backward compat).
		h.circuitBreaker.RecordSuccess(candidate.provider.ID, candidate.provider.Name)
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
// forwarded with key-shaped tokens masked. Everything else - auth, billing,
// rate limit, not-found, server faults, whether classed by eligibility or by
// status - gets a synthesised envelope with the classified reason, because
// those bodies can quote the operator's provider credentials or account
// details; the body stays in the request log either way. Drains/closes
// resp.Body exactly once and always returns outcomeFatal.
func (h *Handler) forwardUpstreamError(w http.ResponseWriter, st *requestState, candidate modelCandidate, resp *http.Response, attempt int, isFailoverEligible bool, responseHeaderMs float64) candidateOutcome {
	logData := st.logData
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
	h.failRequest(logData, resp.StatusCode, kind, errMsg, attempt, st.startTime, st.parseMs, st.timings, st.cacheHits, st.proxyOverhead)

	// A 2xx that reached this function (any success status other than a bare
	// 200) is not an error at all and is forwarded whatever its body is,
	// including a non-JSON or empty one. That is what lets a 204 answer with
	// no body: an envelope written under a No Content status would be a body
	// this gateway invented for a request the provider considered successful.
	// Checked before eligibility so a success can never be rewritten.
	if resp.StatusCode/100 == 2 {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
		return outcomeFatal
	}

	if !mayForwardError {
		// Auth, billing, rate-limit, not-found and server-fault classes -
		// whether ruled out by the caller's eligibility verdict or by the
		// static status class. Their bodies are the ones that can quote the
		// operator's provider credentials ("Incorrect API key provided:
		// sk-...") or account details a virtual-key holder must not see, so the
		// body stays in the DB request log via failRequest and the caller gets
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
		_, _ = w.Write(maskKeyShapedTokens(body))
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
		writeOpenAIError(w, string(maskKeyShapedTokens([]byte(errMsg))), resp.StatusCode)
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
// necessarily trails the provider roster - it is the second layer, not the
// control, which is the status-class gate above.
var keyShapedToken = regexp.MustCompile(`\b(?:sk|gsk|xai|hf|fw|r8)[-_][A-Za-z0-9_-]{16,}|\bAIza[0-9A-Za-z_-]{30,}|\bAKIA[0-9A-Z]{16}\b|\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{5,}|(?i:\bbearer\s+)[A-Za-z0-9._~+/=-]{16,}`)

// maskKeyShapedTokens scrubs credential-looking substrings from an upstream
// body before it is forwarded to a client. Auth-class errors never forward at
// all; this is the second layer, for a provider quoting a credential inside an
// otherwise forwardable payload error. A match with no digit in it is an
// identifier or prose ("sk_business_unit_identifier", "Bearer
// authentication-required") rather than a credential, and stays - real keys
// carry digits. The replacement carries no JSON metacharacters, so a valid
// body stays valid. This covers the buffered error paths; in-stream SSE error
// frames ride the streaming pipeline untouched. Client-side only: the request
// log keeps the original body for the operator.
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
	default:
		// A number or a bool: peculiar, but the provider put a value there and
		// a client can render it.
		return true
	}
}

// recordBreakerOutcome records the circuit-breaker result for a completed
// upstream attempt (phase D8). It is a no-op when the breaker is disabled.
//
// For a failover-eligible status it applies the breakerRecordAction mapping
// (failure / no-op / success). For a non-eligible status it records a success,
// except for a streaming 200 — there the success is deferred until the TTFT
// probe confirms a first token, so it must not be recorded here.
func (h *Handler) recordBreakerOutcome(st *requestState, candidate modelCandidate, statusCode int, isFailoverEligible bool) {
	if !st.circuitBreakerEnabled {
		return
	}
	if isFailoverEligible {
		// Determine breaker action from status code.
		// See breakerRecordAction for the full status→action mapping.
		switch breakerRecordAction(statusCode) {
		case breakerActionFailure:
			// The hedged probe path reaches this with no other log of the
			// upstream status, so without this line a breaker opening on
			// repeated 5xx has no recorded cause anywhere.
			debuglog.Warn("proxy: recording circuit breaker failure", "reason", "upstream status", "status", statusCode, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidateModelID(candidate))
			h.circuitBreaker.RecordFailure(candidate.provider.ID, candidate.provider.Name)
		case breakerActionNoOp:
			// Model-specific client error (404/499): provider is alive
			// but rejecting this request. No-op for the breaker — neither
			// failure nor success. Recording success would erase real 5xx
			// failure history (resetting consecutiveFails in Closed state)
			// and could prematurely close a half-open circuit based on a
			// model-specific error that says nothing about provider health.
		case breakerActionSuccess:
			// Not reached for failover-eligible codes: shouldFailover only
			// returns true for {5xx,429,401,403,402,404,499}, all of which map
			// to failure or no-op above. Retained so the switch stays exhaustive
			// over breakerAction — if the shouldFailover/breakerRecordAction
			// mappings ever diverge, a success is recorded rather than dropped.
			h.circuitBreaker.RecordSuccess(candidate.provider.ID, candidate.provider.Name)
		}
		return
	}
	if !st.isStreaming || statusCode != http.StatusOK {
		h.circuitBreaker.RecordSuccess(candidate.provider.ID, candidate.provider.Name)
	}
}

// learnedScopeFor scopes the learned reject/rename caches to one provider. The
// provider id, not its type: many distinct custom endpoints share the "openai"
// type, and a type-scoped entry would let one of them disable a param for all
// the others serving the same model id.
func learnedScopeFor(candidate modelCandidate) string {
	if candidate.provider == nil {
		return ""
	}
	return candidate.provider.ID.String()
}

// candidateModelID reads a candidate's upstream model id for diagnostics.
// recordBreakerOutcome is reached on paths that only carry the provider, so the
// model must never be assumed present.
func candidateModelID(candidate modelCandidate) string {
	if candidate.model == nil {
		return ""
	}
	return candidate.model.ModelID
}

// learnRejectedParams caches the params and renames a 400 body names, so later
// requests to the same provider+model are built without them. It is the
// learning half of retryWithStrippedParams, split out for callers that cannot
// retry in place (a hedged probe, which must not spend a second round-trip
// inside one race slot).
func (h *Handler) learnRejectedParams(candidate modelCandidate, body []byte) {
	rejected := paramrewrite.ParseProviderParamError(body)
	renames := paramrewrite.ParseProviderParamRename(body)
	if rejected == nil && renames == nil {
		return
	}
	cacheKey := paramrewrite.LearnedCacheKey(learnedScopeFor(candidate), candidate.model.ModelID)
	if rejected != nil {
		paramrewrite.MergeLearnedParamCache(&h.deprecationCache, cacheKey, rejected)
	}
	if renames != nil {
		paramrewrite.MergeLearnedParamCache(&h.paramRenameCache, cacheKey, renames)
	}
	debuglog.Info("proxy: learned rejected params from upstream 400", "provider", candidate.provider.Name, "model", candidate.model.ModelID, "rejected", fmt.Sprintf("%v", rejected), "renames", fmt.Sprintf("%v", renames))
}

// paramRetryRounds caps how many times one candidate is re-issued with newly
// learned params stripped.
//
// Upstreams name a single offending param per 400, so a request carrying two of
// them needs a second round to get through. Past that the provider is objecting
// to something this self-heal cannot fix, and the 400 belongs to the client.
const paramRetryRounds = 2

// issueParamRetry rebuilds the upstream body with strip applied on top of the
// learned caches and re-POSTs it to targetURL.
//
// The returned cancel func belongs to the retry's context: non-nil exactly when
// a response is returned, whose body the caller must consume before calling it.
// On failure the context is cancelled here (there is no body to read) and the
// structured cause is returned for the failover loop to record.
func (h *Handler) issueParamRetry(
	r *http.Request,
	st *requestState,
	candidate modelCandidate,
	providerType, targetURL string,
	strip map[string]bool,
	attempt int,
	dialMs *float64,
) (*http.Response, context.CancelFunc, *reqError) {
	// Rebuild the request body using the shared rewrite path. This ensures
	// stream_options injection, provider injection, universal/learned param
	// stripping, and learned renaming are all applied on retry, preventing drift
	// from the initial attempt path. The renames just cached are picked up from
	// paramRenameCache; the rejected params learned so far are also passed as
	// extraStrip so the retry drops them even before the cache writes are observed.
	rebuilt := paramrewrite.BuildUpstreamBody(st.bodyBytes, providerType, candidate.model.ModelID, st.reqModel, st.isStreaming, &h.deprecationCache, &h.paramRenameCache, strip, learnedScopeFor(candidate))
	retryCtx, rc := context.WithTimeout(r.Context(), st.failoverTimeout)
	retryCtx = context.WithValue(retryCtx, ctxkeys.CancelOriginKey, "retry_timeout")
	retryCtx = context.WithValue(retryCtx, ctxkeys.DialMsKey, dialMs)
	retryReq, retryErr := newRequestWithContext(retryCtx, "POST", targetURL, bytes.NewReader(rebuilt))
	if retryErr != nil {
		rc()
		return nil, nil, &reqError{Kind: KindInternal, Attempt: attempt, Provider: candidate.provider.Name, Underlying: errString(retryErr)}
	}
	util.SetProviderAuthHeaders(retryReq, providerType, candidate.apiKey)
	retryReq.Header.Set("Content-Type", "application/json")
	var retryCheckRedirect func(req *http.Request, via []*http.Request) error
	if h.safeDialer != nil {
		retryCheckRedirect = h.safeDialer.CheckRedirect
	}
	retryClient := &http.Client{Transport: h.upstreamTransport, CheckRedirect: retryCheckRedirect}
	//nolint:bodyclose // retryResp.Body is returned to the caller, which consumes and closes it
	retryResp, retryErr := retryClient.Do(retryReq)
	if retryErr != nil {
		rc() // no body to consume on retry error
		debuglog.Warn("proxy: auto-retry request failed", "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "error", retryErr)
		if errors.Is(retryErr, context.Canceled) || errors.Is(retryErr, context.DeadlineExceeded) {
			// Branch like the main failover loop: Canceled = client
			// disconnect, DeadlineExceeded = retry timeout.
			origin := "retry_timeout"
			if errors.Is(retryErr, context.Canceled) {
				origin = "client_disconnect"
			}
			return nil, nil, &reqError{Kind: cancelOriginToKind(origin), Attempt: attempt, Provider: candidate.provider.Name}
		}
		return nil, nil, &reqError{Kind: KindProviderError, Attempt: attempt, Provider: candidate.provider.Name, Underlying: errString(retryErr)}
	}
	return retryResp, rc, nil
}

// readLearnable400 consumes a 400 body for the param self-heal: it reads what
// the learner can parse, drains the rest so the connection stays reusable,
// closes the original body and restores a readable one in its place — the
// response is handed on to whoever renders the client's error, so it must never
// leave here empty.
//
// The bound is the parse-sized responsesLearnBodyCap rather than the
// scan-sized failoverErrorClassifyCap: learning json.Unmarshals the whole
// document, so a body cut mid-JSON parses to nothing and teaches nothing. The
// cap sits far past any real provider error, and everything above it is
// discarded rather than held.
func readLearnable400(resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, responsesLearnBodyCap))
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	return body, err
}

// retryWithStrippedParams handles a 400 from an upstream: it reads and restores
// the error body, cancels the original (now-finished) request context, and — if
// the body is a recognizable param-rejection — learns the rejected params and
// renames into the deprecation/rename caches, rebuilds the request with them
// applied, and re-issues it. A retry that is itself rejected with a 400 is read
// and learned from in exactly the same way, and re-issued once more when it
// names a param the retry did not already carry. See paramRetryResult for how
// the loop interprets the return value.
//
// failoverCancel is the original request's cancel func; it is invoked here once
// the 400 body has been consumed. dialMs is the per-request dial-timing pointer
// threaded into every retry context so SafeDialer records the retries' DNS time.
func (h *Handler) retryWithStrippedParams(
	r *http.Request,
	st *requestState,
	candidate modelCandidate,
	providerType, targetURL string,
	resp *http.Response,
	attempt int,
	dialMs *float64,
	failoverCancel context.CancelFunc,
	streamCancelOrigin string,
) paramRetryResult {
	// Restored before anything else, so downstream error handling can read the
	// body on every path that gives up without retrying.
	body, readErr := readLearnable400(resp)
	failoverCancel() // 400 body consumed, context no longer needed
	debuglog.Debug("proxy: received 400 from upstream, checking for param rejection", "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidate.model.ModelID, "body_length", len(body))

	res := paramRetryResult{resp: resp, streamCancelOrigin: streamCancelOrigin}
	if readErr != nil {
		return res
	}

	// Everything applied to the outgoing body so far, accumulated across rounds:
	// strip holds the rejected params (dropped), renamed the moves (value kept
	// under the new name). They are also the termination guard — a round is only
	// worth issuing when a 400 names something outside these sets.
	strip := map[string]bool{}
	renamed := map[string]string{}

	// A 400 can ask us to drop a param (rejected → strip) and/or move a param to
	// a new name (rename → preserve value). Both feed the same self-heal: learn,
	// cache for future preemptive application, and retry. learnFrom does the
	// learning half for one 400 body and reports whether that body named anything
	// the request does not already carry — i.e. whether re-issuing could help.
	learnFrom := func(errBody []byte) bool {
		rejected := paramrewrite.ParseProviderParamError(errBody)
		renames := paramrewrite.ParseProviderParamRename(errBody)
		if rejected == nil && renames == nil {
			return false
		}
		// Cache the learned rejections and renames for future preemptive
		// application. Each cache is merged with any existing entries via
		// CompareAndSwap to avoid data races from concurrent goroutines mutating
		// the same map.
		h.learnRejectedParams(candidate, errBody)
		progressed := false
		for p := range rejected {
			if !strip[p] {
				progressed = true
			}
			strip[p] = true
		}
		for oldName, newName := range renames {
			if _, seen := renamed[oldName]; !seen {
				progressed = true
			}
			renamed[oldName] = newName
		}
		return progressed
	}

	// Two guards, both required, so the loop always terminates: at most
	// paramRetryRounds rounds, and a round only when the current 400 names
	// something the request does not already carry (learnFrom's report), which
	// makes strip∪renamed grow strictly on every iteration.
	//
	// learnFrom runs before the round check on purpose — it is the left operand,
	// so it is evaluated even on the iteration that ends the loop. That is what
	// makes the last 400 learned rather than discarded: the round it would have
	// paid for is refused, but the next request still starts with what it named.
	//
	// The first 400 that names nothing recognisable falls straight out here and
	// is handed back untouched.
	for round := 0; learnFrom(body) && round < paramRetryRounds; round++ {
		res.streamCancelOrigin = "retry_timeout"
		retryResp, rc, retryErr := h.issueParamRetry(r, st, candidate, providerType, targetURL, strip, attempt, dialMs)
		if retryErr != nil {
			res.lastReqErr = *retryErr
			res.cont = true
			return res
		}
		if retryResp.StatusCode != http.StatusBadRequest {
			// rc must NOT be called here — retryResp.Body is read by the caller.
			// It is returned for deferred cleanup after body consumption.
			res.resp = retryResp
			res.retryCancel = rc
			res.retried = true
			debuglog.Info("proxy: auto-retry succeeded", "model", candidate.model.ModelID, "rounds", round+1, "rejected_params", mapKeys(strip), "renamed_params", renameKeys(renamed))
			return res
		}
		// The retry was rejected too. Read its body so the params it names are
		// learned as well, then hand that response on with the body restored: it
		// is the client's 400 if no further round is issued.
		body, readErr = readLearnable400(retryResp)
		rc() // retry body consumed and buffered, context no longer needed
		res.resp = retryResp
		res.retryCancel = nil
		res.retried = true
		if readErr != nil {
			return res
		}
	}
	return res
}

// renameKeys returns the old param names from a rename map, for log fields.
func renameKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
