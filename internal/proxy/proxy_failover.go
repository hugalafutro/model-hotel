package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
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
