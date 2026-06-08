package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
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
//   - lastErr: set only when cont is true.
//   - cont: true => the caller should `continue` to the next candidate (a retry
//     request could not be created or failed).
type paramRetryResult struct {
	resp               *http.Response
	retryCancel        context.CancelFunc
	streamCancelOrigin string
	retried            bool
	lastErr            string
	cont               bool
}

// failAllExhausted handles phase E: every candidate failed (or the overall
// deadline was hit). It logs the exhaustion, records a 502 failure row, and
// writes the failover-vs-single-provider error response. numCandidates is the
// resolved candidate count (for the failRequest attempt index).
func (h *Handler) failAllExhausted(w http.ResponseWriter, st *requestState, numCandidates int) {
	if st.isFailover {
		debuglog.Error("proxy: all providers exhausted", "model", st.logData.modelID, "provider", st.logData.providerName, "error", st.lastErr, "candidates", numCandidates, "failover_timeout", st.failoverTimeout)
	} else {
		debuglog.Error("proxy: provider request failed", "model", st.logData.modelID, "provider", st.logData.providerName, "error", st.lastErr, "request_timeout", st.failoverTimeout)
	}
	st.logData.providerID = uuid.Nil
	if st.isFailover {
		h.failRequest(st.logData, 502, fmt.Sprintf("all providers failed: %s", st.lastErr), numCandidates-1, st.startTime, st.parseMs, st.timings, st.cacheHits, st.proxyOverhead)
		writeOpenAIError(w, fmt.Sprintf("all providers failed for model %s", st.reqModel), http.StatusBadGateway)
	} else {
		h.failRequest(st.logData, 502, fmt.Sprintf("provider request failed: %s", st.lastErr), numCandidates-1, st.startTime, st.parseMs, st.timings, st.cacheHits, st.proxyOverhead)
		writeOpenAIError(w, fmt.Sprintf("provider request failed for model %s", st.reqModel), http.StatusBadGateway)
	}
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
	logData.providerID = candidate.provider.ID
	logData.providerName = candidate.provider.Name
	if st.isFailover {
		logData.resolvedModelID = candidate.model.ModelID
	}
	if attempt == 0 {
		debuglog.Info("proxy: routing to provider", "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidate.model.ModelID, "total_candidates", totalCandidates)
	} else {
		debuglog.Info("proxy: failover attempt", "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidate.model.ModelID)
	}
	debuglog.Debug("proxy: candidate details", "provider_id", candidate.provider.ID, "provider_name", candidate.provider.Name, "model_id", candidate.model.ModelID, "provider_type", provider.DetectProviderType(candidate.provider.BaseURL), "attempt", attempt+1, "total_candidates", totalCandidates)
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
	}(candidate.provider.ID)
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

	proxyReq, providerType, targetURL, err := h.buildCandidateRequest(failoverCtx, st, candidate)
	if err != nil {
		st.lastErr = fmt.Sprintf("attempt %d: failed to create request: %v", attempt, err)
		return outcomeFailover
	}

	// Reuse the shared upstream Transport instead of creating a new one
	// per request. A fresh Transport spawns persistent readLoop/writeLoop
	// goroutines per connection that only die after IdleConnTimeout, so
	// creating one per request causes unbounded goroutine growth.
	// Inject per-request dial timing pointer so SafeDialer writes
	// DNS resolution time into this request's own variable, avoiding
	// cross-request race conditions on a shared atomic.
	dialCtx := context.WithValue(failoverCtx, ctxkeys.DialMsKey, &dialMs)
	proxyReq = proxyReq.WithContext(dialCtx)

	var checkRedirect func(req *http.Request, via []*http.Request) error
	if h.safeDialer != nil {
		checkRedirect = h.safeDialer.CheckRedirect
	}
	upstreamClient := &http.Client{
		Transport:     h.upstreamTransport,
		CheckRedirect: checkRedirect,
	}
	//nolint:gosec // provider URL is admin-configured, not arbitrary user input
	resp, err := upstreamClient.Do(proxyReq)
	st.timings.dialMs += dialMs
	dialMs = 0
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
				if v := proxyReq.Context().Value(ctxkeys.CancelOriginKey); v != nil {
					if s, ok := v.(string); ok {
						cancelOrigin = s
					}
				}
			}
			st.lastErr = fmt.Sprintf("attempt %d: %s", attempt, humanReadableCancelOrigin(cancelOrigin))
			debuglog.Info("proxy: context cancelled during request to provider", "provider", logData.providerName, "provider_id", candidate.provider.ID, "model", logData.modelID, "origin", cancelOrigin, "error", err)
		} else {
			st.lastErr = fmt.Sprintf("attempt %d: provider error: %v", attempt, err)
			debuglog.Warn("proxy: upstream request failed", "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "error", err)
		}
		// Client-initiated cancellations and deadline exceeded are not
		// provider failures. If the caller disconnected (Canceled) or
		// the request timed out (DeadlineExceeded), we must not penalize
		// the circuit breaker for that.
		if !isContextErr {
			if st.circuitBreakerEnabled {
				h.circuitBreaker.RecordFailure(candidate.provider.ID, candidate.provider.Name)
			}
		}
		return outcomeFailover
	}

	// Log upstream response metadata for debugging.
	debuglog.Debug("proxy: upstream response received", "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidate.model.ModelID, "status", resp.StatusCode, "content_type", resp.Header.Get("Content-Type"), "x_request_id", resp.Header.Get("X-Request-Id"), "x_ratelimit_remaining", resp.Header.Get("X-RateLimit-Remaining"), "attempt", attempt+1)

	// Auto-retry param-rejection 400s: parse the error, learn which params
	// are rejected for this model, strip them, and retry once.
	// Works universally — any LLM API mentioning "temperature" or "top_p"
	// in a 400 error can only mean the sampling parameter.
	if resp.StatusCode == 400 {
		res := h.retryWithStrippedParams(r, st, candidate, providerType, targetURL, resp, attempt, &dialMs, failoverCancel, streamCancelOrigin)
		resp = res.resp
		streamCancelOrigin = res.streamCancelOrigin
		retryCancel = res.retryCancel
		if res.cont {
			st.lastErr = res.lastErr
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
		st.lastErr = fmt.Sprintf("attempt %d: HTTP %d", attempt, resp.StatusCode)
		debuglog.Info("proxy: failover triggered", "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "status", resp.StatusCode)
		logData.failoverAttempt = attempt
		return outcomeFailover
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		errMsg := util.SanitizeLogBody(string(body), 10000)
		debuglog.Warn("proxy: upstream non-200", "status", resp.StatusCode, "model", logData.modelID, "provider", logData.providerName, "provider_id", candidate.provider.ID, "body", errMsg)
		debuglog.Debug("proxy: upstream error response", "status", resp.StatusCode, "model", logData.modelID, "provider", logData.providerName, "provider_id", candidate.provider.ID, "body_length", len(body), "attempt", attempt+1)
		logData.responseHeaderMs = responseHeaderMs
		h.failRequest(logData, resp.StatusCode, errMsg, attempt, st.startTime, st.parseMs, st.timings, st.cacheHits, st.proxyOverhead)

		if !hasMoreCandidates {
			// All failover candidates exhausted — return a generic error.
			// The full upstream body is logged server-side above but not
			// forwarded, as it may contain provider-specific details.
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

	debuglog.Debug("proxy: upstream responded OK, dispatching to handler", "stream", st.isStreaming, "model", logData.modelID, "provider", logData.providerName, "provider_id", candidate.provider.ID, "status", resp.StatusCode)
	if st.isStreaming {
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
		}

		if ttftTimeout > 0 {
			// TTFT probe: read until first real data chunk.
			probeBuf, trueTtftMs, probeErr := h.probeFirstToken(r.Context(), resp.Body, ttftTimeout, st.startTime)
			if probeErr != nil {
				// Timeout or read error — failover. probeFirstToken may
				// or may not have closed the body (only on DeadlineExceeded);
				// close it unconditionally to release the connection.
				_ = resp.Body.Close()
				// Skip circuit-breaker recording when the client disconnected:
				// the probe failed because r.Context() was cancelled, not because
				// the provider was unhealthy.
				if st.circuitBreakerEnabled && r.Context().Err() == nil {
					h.circuitBreaker.RecordFailure(candidate.provider.ID, candidate.provider.Name)
				}
				st.lastErr = fmt.Sprintf("attempt %d: %v", attempt, probeErr)
				logData.failoverAttempt = attempt
				logData.responseHeaderMs = responseHeaderMs
				debuglog.Warn("proxy: TTFT probe failed", "attempt", attempt+1, "provider", candidate.provider.Name, "error", probeErr)
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

	h.handleNonStreamingResponse(w, r, logData, resp, st.startTime, st.proxyOverhead, st.parseMs, st.timings.failoverLookupMs, st.timings.modelLookupMs, st.timings.providerLookupMs, st.timings.keyDecryptMs, st.timings.dialMs, st.timings.settingsReadMs, responseHeaderMs, st.vkHash, attempt)
	return outcomeServed
}

// buildCandidateRequest builds the upstream HTTP request for a single failover
// candidate (phase C): detect the provider type, build the target URL, rewrite
// the request body when needed, create the request on the provided context, and
// set the auth + content-type headers. The caller owns ctx cancellation; this
// helper never cancels it. providerType and targetURL are returned so the caller
// can thread them into the 400 auto-retry path.
func (h *Handler) buildCandidateRequest(ctx context.Context, st *requestState, candidate modelCandidate) (*http.Request, string, string, error) {
	logData := st.logData
	providerType := provider.DetectProviderType(candidate.provider.BaseURL)
	debuglog.Debug("proxy: detected provider type", "provider_type", providerType, "base_url", util.SanitizeBaseURL(candidate.provider.BaseURL))
	targetURL := util.BuildProviderTargetURL(candidate.provider.BaseURL, providerType)
	debuglog.Debug("proxy: built target URL", "target_url", targetURL)

	upstreamBody := st.bodyBytes
	needsRewrite := st.reqModel != candidate.model.ModelID || providerType == "anthropic" || NeedsProviderInjection(providerType) || st.isStreaming
	debuglog.Debug("proxy: request rewrite check", "needs_rewrite", needsRewrite, "request_model", logData.modelID, "provider", logData.providerName, "resolved_model", candidate.model.ModelID, "provider_type", providerType)
	if needsRewrite {
		upstreamBody = buildUpstreamBody(st.bodyBytes, providerType, candidate.model.ModelID, st.reqModel, st.isStreaming, &h.deprecationCache, nil)
	}
	// Log the actual model name in the upstream body for debugging rewrite issues.
	if upstreamModel, _, _ := strings.Cut(string(upstreamBody), ","); strings.Contains(upstreamModel, `"model"`) {
		debuglog.Debug("proxy: upstream body model", "upstream_model_snippet", upstreamModel)
	}

	proxyReq, err := newRequestWithContext(ctx, "POST", targetURL, bytes.NewReader(upstreamBody))
	if err != nil {
		return nil, providerType, targetURL, err
	}

	util.SetProviderAuthHeaders(proxyReq, providerType, candidate.apiKey)
	proxyReq.Header.Set("Content-Type", "application/json")
	debuglog.Debug("proxy: sending upstream request", "method", proxyReq.Method, "url", targetURL, "content_length", len(upstreamBody), "has_api_key", candidate.apiKey != "")
	return proxyReq, providerType, targetURL, nil
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
	rejected := parseProviderParamError(body)
	if rejected == nil {
		return res
	}

	// Cache the learned rejections for future preemptive stripping.
	// Merge with any existing entries using CompareAndSwap to avoid
	// data races from concurrent goroutines mutating the same map.
	// NOTE: Values are stored as *map[string]bool to support CompareAndSwap
	// (maps are not comparable, so pointers are required).
	cacheKey := fmt.Sprintf("%s:%s", providerType, candidate.model.ModelID)
	for {
		existing, loaded := h.deprecationCache.LoadOrStore(cacheKey, &rejected)
		if !loaded {
			// First entry for this key — we just stored 'rejected'.
			break
		}
		// Merge with existing, creating a new map to avoid data races.
		merged := make(map[string]bool)
		existingMap, ok := existing.(*map[string]bool)
		if !ok {
			debuglog.Error("deprecationCache: unexpected type", "key", cacheKey, "type", fmt.Sprintf("%T", existing))
			break
		}
		for k := range *existingMap {
			merged[k] = true
		}
		for k := range rejected {
			merged[k] = true
		}
		if h.deprecationCache.CompareAndSwap(cacheKey, existing, &merged) {
			break
		}
		// CompareAndSwap failed — another goroutine updated it, retry.
	}

	// Rebuild the request body using the shared rewrite path.
	// This ensures stream_options injection, universal/learned param
	// stripping, and InjectProviderParams are all applied on retry,
	// preventing drift from the initial attempt path.
	rebuilt := buildUpstreamBody(st.bodyBytes, providerType, candidate.model.ModelID, st.reqModel, st.isStreaming, &h.deprecationCache, rejected)
	retryCtx, rc := context.WithTimeout(r.Context(), st.failoverTimeout)
	retryCtx = context.WithValue(retryCtx, ctxkeys.CancelOriginKey, "retry_timeout")
	retryCtx = context.WithValue(retryCtx, ctxkeys.DialMsKey, dialMs)
	res.streamCancelOrigin = "retry_timeout"
	retryReq, retryErr := newRequestWithContext(retryCtx, "POST", targetURL, bytes.NewReader(rebuilt))
	if retryErr != nil {
		rc()
		res.lastErr = fmt.Sprintf("attempt %d: failed to create retry request: %v", attempt, retryErr)
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
			res.lastErr = fmt.Sprintf("attempt %d: %s", attempt, humanReadableCancelOrigin(origin))
		} else {
			res.lastErr = fmt.Sprintf("attempt %d: retry error: %v", attempt, retryErr)
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
	debuglog.Info("proxy: auto-retry succeeded", "model", candidate.model.ModelID, "rejected_params", mapKeys(rejected))
	return res
}
