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
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// paramRetryResult is the outcome of the 400 param-stripping auto-retry
// (retryWithStrippedParams). It tells the failover loop how to proceed:
//   - resp: the response to continue handling with — the retry's response on a
//     successful retry, otherwise the original 400 response with its body
//     restored (so normal non-200 handling can read it).
//   - retryCancel: the retry context's cancel func, non-nil only when a retry
//     response is live and its body has NOT yet been consumed. The caller must
//     call it after consuming the body.
//   - streamCancelOrigin: "retry_timeout" once a retry was issued, otherwise
//     the caller's original value, unchanged.
//   - retried: true when a retry request succeeded — the caller must fold the
//     retry's dial time into the running totals.
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
	// Skipped for native Anthropic passthrough: this self-heal rebuilds the
	// OpenAI-shaped st.bodyBytes via buildUpstreamBody and re-POSTs it, but a
	// native attempt's targetURL is the provider's /v1/messages (Anthropic wire
	// format). Stripping OpenAI params off the wrong body and sending it to the
	// native endpoint would be malformed; a native 400 is forwarded as-is.
	if resp.StatusCode == 400 && !st.anthropicNativeAttempt {
		res := h.retryWithStrippedParams(r, st, candidate, providerType, targetURL, resp, attempt, &dialMs, failoverCancel, streamCancelOrigin)
		resp = res.resp
		streamCancelOrigin = res.streamCancelOrigin
		retryCancel = res.retryCancel
		if res.cont {
			st.setReqErr(res.lastReqErr)
			return outcomeFailover
		}
		if res.retried {
			// Accumulate retry's dial time into total.
			st.timings.dialMs += dialMs
			dialMs = 0
			st.proxyOverhead = st.timings.proxyOverheadMs(st.parseMs)
		}
	}

	responseHeaderMs := float64(time.Since(st.startTime).Microseconds()) / 1000.0

	hasMoreCandidates := attempt < totalCandidates-1
	isFailoverEligible := h.shouldFailover(r.Context(), resp.StatusCode)

	h.recordBreakerOutcome(st, candidate, resp.StatusCode, isFailoverEligible)

	shouldFailoverNow := isFailoverEligible && hasMoreCandidates
	debuglog.Debug("proxy: failover decision", "status", resp.StatusCode, "is_failover_eligible", isFailoverEligible, "has_more_candidates", hasMoreCandidates, "should_failover_now", shouldFailoverNow, "attempt", attempt+1)

	if shouldFailoverNow {
		_, _ = io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		st.setReqErr(reqError{Kind: KindProviderError, Attempt: attempt, Provider: candidate.provider.Name, Detail: fmt.Sprintf("HTTP %d", resp.StatusCode)})
		debuglog.Info("proxy: failover triggered", "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "status", resp.StatusCode)
		logData.failoverAttempt = attempt
		return outcomeFailover
	}

	if resp.StatusCode != http.StatusOK {
		return h.forwardUpstreamError(w, st, candidate, resp, attempt, hasMoreCandidates, responseHeaderMs)
	}

	debuglog.Debug("proxy: upstream responded OK, dispatching to handler", "stream", st.isStreaming, "native_anthropic", st.anthropicNativeAttempt, "model", logData.modelID, "provider", logData.providerName, "provider_id", candidate.provider.ID, "status", resp.StatusCode)
	if st.isStreaming {
		return h.dispatchStreaming(w, r, st, candidate, resp, attempt, responseHeaderMs, streamCancelOrigin)
	}

	if st.anthropicNativeAttempt {
		return h.handleNativeNonStreaming(w, r, st, resp, attempt, responseHeaderMs)
	}

	h.handleNonStreamingResponse(w, r, logData, resp, st.startTime, st.proxyOverhead, st.parseMs, st.timings.failoverLookupMs, st.timings.modelLookupMs, st.timings.providerLookupMs, st.timings.keyDecryptMs, st.timings.dialMs, st.timings.settingsReadMs, responseHeaderMs, st.vkHash, attempt)
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
	debuglog.Debug("proxy: candidate details", "provider_id", candidate.provider.ID, "provider", candidate.provider.Name, "model_id", candidate.model.ModelID, "provider_type", provider.DetectProviderType(candidate.provider.BaseURL), "attempt", attempt+1, "total_candidates", totalCandidates)
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
// rewrite (buildUpstreamBody: model rename, stream_options, param stripping).
// Multimodal requests provide st.makeUpstreamBody, which owns the body rewrite
// and its Content-Type (JSON model rename, or multipart reconstruction).
func (h *Handler) buildCandidateRequest(ctx context.Context, st *requestState, candidate modelCandidate) (*http.Request, string, string, error) {
	logData := st.logData
	providerType := provider.DetectProviderType(candidate.provider.BaseURL)
	debuglog.Debug("proxy: detected provider type", "provider_type", providerType, "base_url", util.SanitizeBaseURL(candidate.provider.BaseURL))

	// Native Anthropic passthrough: an Anthropic-in request resolved to an
	// Anthropic-family provider forwards the ORIGINAL Messages body to the
	// provider's native /v1/messages (max fidelity: thinking blocks,
	// cache_control, fine-grained tool streaming survive). Every non-Anthropic
	// candidate in the same failover group still goes through translation, so a
	// single hotel/claude-* request can fail over from native to translated
	// seamlessly. The flag is read by the response dispatch + writer.
	st.anthropicNativeAttempt = st.anthropicIn && providerType == "anthropic"
	if st.anthropicNativeAttempt {
		return h.buildNativeAnthropicRequest(ctx, st, candidate, providerType)
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
		needsRewrite := st.reqModel != candidate.model.ModelID || providerType == "anthropic" || NeedsProviderInjection(providerType) || st.isStreaming
		debuglog.Debug("proxy: request rewrite check", "needs_rewrite", needsRewrite, "request_model", logData.modelID, "provider", logData.providerName, "resolved_model", candidate.model.ModelID, "provider_type", providerType)
		if needsRewrite {
			upstreamBody = buildUpstreamBody(st.bodyBytes, providerType, candidate.model.ModelID, st.reqModel, st.isStreaming, &h.deprecationCache, &h.paramRenameCache, nil)
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
		// "context canceled" is opaque — we need to know if the client
		// disconnected, the failover timeout expired, or the retry timeout expired.
		// Key insight: context.Canceled means the parent (client) context was
		// canceled — always a client disconnect. context.DeadlineExceeded means
		// the derived context's deadline expired — read CancelOriginKey to
		// distinguish failover_timeout from retry_timeout.
		isContextErr := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
		if isContextErr {
			cancelOrigin := "client_disconnect"
			if errors.Is(err, context.DeadlineExceeded) {
				if v := dialCtx.Value(ctxkeys.CancelOriginKey); v != nil {
					if s, ok := v.(string); ok {
						cancelOrigin = s
					}
				}
			}
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

// forwardUpstreamError handles a non-200 upstream response that is NOT being
// failed over (phase G): log + meter the failure via failRequest, then either
// return a generic OpenAI error when the candidates are exhausted or forward the
// upstream body (wrapping non-JSON bodies in an OpenAI envelope) so clients can
// react to semantic errors. Drains/closes resp.Body exactly once and always
// returns outcomeFatal.
func (h *Handler) forwardUpstreamError(w http.ResponseWriter, st *requestState, candidate modelCandidate, resp *http.Response, attempt int, hasMoreCandidates bool, responseHeaderMs float64) candidateOutcome {
	logData := st.logData
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	errMsg := util.SanitizeLogBody(string(body), 10000)
	debuglog.Warn("proxy: upstream non-200", "status", resp.StatusCode, "model", logData.modelID, "provider", logData.providerName, "provider_id", candidate.provider.ID)
	debuglog.Debug("proxy: upstream error response", "status", resp.StatusCode, "model", logData.modelID, "provider", logData.providerName, "provider_id", candidate.provider.ID, "body_length", len(body), "attempt", attempt+1)
	logData.responseHeaderMs = responseHeaderMs
	h.failRequest(logData, resp.StatusCode, KindProviderError, errMsg, attempt, st.startTime, st.parseMs, st.timings, st.cacheHits, st.proxyOverhead)

	if !hasMoreCandidates {
		// All failover candidates exhausted — return a generic error.
		// The upstream body is recorded to the DB request log via failRequest
		// (not the structured server log) and is not forwarded to the client,
		// as it may contain provider-specific details.
		writeOpenAIError(w, fmt.Sprintf("upstream provider returned HTTP %d", resp.StatusCode), resp.StatusCode)
		return outcomeFatal
	}

	// Non-failover-eligible error with remaining candidates — forward
	// the upstream response so clients can react to semantic errors
	// (e.g. context_length_exceeded, rate_limit_exceeded).
	if json.Valid(body) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(resp.StatusCode)
		_, _ = w.Write(body)
	} else {
		// Body is not JSON (e.g. HTML from a CDN). Wrap in an
		// OpenAI-compatible envelope so JSON-parsing clients don't crash.
		writeOpenAIError(w, errMsg, resp.StatusCode)
	}
	return outcomeFatal
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
			// returns true for {5xx,429,401,403,404,499}, all of which map to
			// failure or no-op above. Retained so the switch stays exhaustive
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

// retryWithStrippedParams handles a 400 from an upstream: it reads and restores
// the error body, cancels the original (now-finished) request context, and — if
// the body is a recognizable param-rejection — learns the rejected params into
// the deprecation cache, rebuilds the request with them stripped, and re-issues
// it once. See paramRetryResult for how the loop interprets the return value.
//
// failoverCancel is the original request's cancel func; it is invoked here once
// the 400 body has been consumed (and again, harmlessly, on the successful-retry
// path). dialMs is the per-request dial-timing pointer threaded into the retry
// context so SafeDialer records the retry's DNS time.
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
	body, readErr := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	failoverCancel() // 400 body consumed, context no longer needed
	debuglog.Debug("proxy: received 400 from upstream, checking for param rejection", "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidate.model.ModelID, "body_length", len(body))
	// Restore the body so downstream error handling can read it if we don't
	// successfully retry. Must be set before any fallthrough.
	resp.Body = io.NopCloser(bytes.NewReader(body))

	res := paramRetryResult{resp: resp, streamCancelOrigin: streamCancelOrigin}
	if readErr != nil {
		return res
	}
	// A 400 can ask us to drop a param (rejected → strip) and/or move a param to
	// a new name (rename → preserve value). Both feed the same self-heal: learn,
	// cache for future preemptive application, and retry once.
	rejected := parseProviderParamError(body)
	renames := parseProviderParamRename(body)
	if rejected == nil && renames == nil {
		return res
	}

	// Cache the learned rejections and renames for future preemptive application.
	// Each cache is merged with any existing entries via CompareAndSwap to avoid
	// data races from concurrent goroutines mutating the same map.
	cacheKey := fmt.Sprintf("%s:%s", providerType, candidate.model.ModelID)
	if rejected != nil {
		mergeLearnedParamCache(&h.deprecationCache, cacheKey, rejected)
	}
	if renames != nil {
		mergeLearnedParamCache(&h.paramRenameCache, cacheKey, renames)
	}

	// Rebuild the request body using the shared rewrite path. This ensures
	// stream_options injection, provider injection, universal/learned param
	// stripping, and learned renaming are all applied on retry, preventing drift
	// from the initial attempt path. The renames just cached are picked up from
	// paramRenameCache; the freshly-rejected params are also passed as extraStrip
	// so the immediate retry strips them even before the cache write is observed.
	rebuilt := buildUpstreamBody(st.bodyBytes, providerType, candidate.model.ModelID, st.reqModel, st.isStreaming, &h.deprecationCache, &h.paramRenameCache, rejected)
	retryCtx, rc := context.WithTimeout(r.Context(), st.failoverTimeout)
	retryCtx = context.WithValue(retryCtx, ctxkeys.CancelOriginKey, "retry_timeout")
	retryCtx = context.WithValue(retryCtx, ctxkeys.DialMsKey, dialMs)
	res.streamCancelOrigin = "retry_timeout"
	retryReq, retryErr := newRequestWithContext(retryCtx, "POST", targetURL, bytes.NewReader(rebuilt))
	if retryErr != nil {
		rc()
		res.lastReqErr = reqError{Kind: KindInternal, Attempt: attempt, Provider: candidate.provider.Name, Underlying: errString(retryErr)}
		res.cont = true
		return res
	}
	util.SetProviderAuthHeaders(retryReq, providerType, candidate.apiKey)
	retryReq.Header.Set("Content-Type", "application/json")
	var retryCheckRedirect func(req *http.Request, via []*http.Request) error
	if h.safeDialer != nil {
		retryCheckRedirect = h.safeDialer.CheckRedirect
	}
	retryClient := &http.Client{Transport: h.upstreamTransport, CheckRedirect: retryCheckRedirect}
	//nolint:bodyclose // retryResp.Body is returned to the caller (failover loop), which consumes and closes it after dispatch
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
			res.lastReqErr = reqError{Kind: cancelOriginToKind(origin), Attempt: attempt, Provider: candidate.provider.Name}
		} else {
			res.lastReqErr = reqError{Kind: KindProviderError, Attempt: attempt, Provider: candidate.provider.Name, Underlying: errString(retryErr)}
		}
		res.cont = true
		return res
	}
	failoverCancel() // original 400 body already consumed, original context no longer needed
	// retryCancel must NOT be called here — the retry resp.Body is read by the
	// caller. It is returned for deferred cleanup after body consumption.
	res.resp = retryResp
	res.retryCancel = rc
	res.retried = true
	debuglog.Info("proxy: auto-retry succeeded", "model", candidate.model.ModelID, "rejected_params", mapKeys(rejected), "renamed_params", renameKeys(renames))
	return res
}

// mergeLearnedParamCache merges newly-learned per-model param metadata into a
// sync.Map cache keyed by "providerType:modelID", race-free under concurrent
// goroutines via CompareAndSwap. Values are stored as *map[string]V (pointers,
// because maps are not comparable and CompareAndSwap requires comparable values).
func mergeLearnedParamCache[V any](cache *sync.Map, key string, learned map[string]V) {
	for {
		existing, loaded := cache.LoadOrStore(key, &learned)
		if !loaded {
			return // first entry for this key — we just stored 'learned'
		}
		existingMap, ok := existing.(*map[string]V)
		if !ok {
			debuglog.Error("learned param cache: unexpected type", "key", key, "type", fmt.Sprintf("%T", existing))
			return
		}
		merged := make(map[string]V, len(*existingMap)+len(learned))
		for k, v := range *existingMap {
			merged[k] = v
		}
		for k, v := range learned {
			merged[k] = v
		}
		if cache.CompareAndSwap(key, existing, &merged) {
			return
		}
		// CompareAndSwap failed — another goroutine updated it, retry.
	}
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
