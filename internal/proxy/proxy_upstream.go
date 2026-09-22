package proxy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptrace"
	"sync/atomic"
	"time"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

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
// timeout, provider error) and records a breaker failure for every cause but an
// abandoned request (client disconnect, hedge superseded).
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

	upstreamClient := h.upstreamClient(ctx)

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
		// #nosec G704 -- provider URL is admin-configured, not arbitrary user input
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
		abandoned := false
		switch {
		case isContextErr && dialCtx.Err() == nil:
			// A deadline error with the attempt's context still live is the
			// transport's own header timeout: the provider accepted the
			// connection and never answered. That is the provider's stall.
			st.setReqErr(reqError{
				Kind:       KindProviderTimeout,
				Attempt:    attempt,
				Provider:   candidate.provider.Name,
				Underlying: errString(err),
			})
			debuglog.Warn("proxy: upstream sent no response headers before the header timeout", "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "error", err)
		case isContextErr:
			cancelOrigin := resolveCancelOrigin(dialCtx, err)
			abandoned = requestAbandoned(dialCtx, err)
			// The context error is the terminal cause, but the provider error
			// that drove the retries (lastTransportErr) is preserved as
			// Underlying so it survives into the request log and response.
			st.setReqErr(reqError{
				Kind:       cancelOriginToKind(cancelOrigin),
				Attempt:    attempt,
				Provider:   candidate.provider.Name,
				Underlying: errString(lastTransportErr),
			})
			debuglog.Info("proxy: context cancelled during request to provider", "provider", logData.providerName, "provider_id", candidate.provider.ID, "model", logData.modelID, "origin", cancelOrigin,
				"error", fencedFrameMessage(logData.fence(), logData.masks(), errString(err)),
				"underlying", fencedFrameMessage(logData.fence(), logData.masks(), errString(lastTransportErr)))
		default:
			st.setReqErr(reqError{
				Kind:       KindProviderError,
				Attempt:    attempt,
				Provider:   candidate.provider.Name,
				Underlying: errString(err),
			})
			// A transport error quotes what came back off the wire (net/http's
			// "malformed HTTP status code %q" carries the upstream's own bytes), so
			// it gets the pass the Underlying above already gets downstream.
			debuglog.Warn("proxy: upstream request failed", "attempt", attempt+1, "provider", candidate.provider.Name, "provider_id", candidate.provider.ID,
				"error", fencedFrameMessage(logData.fence(), logData.masks(), errString(err)))
		}
		// An abandoned attempt (the client hung up, a hedge sibling won) says
		// nothing about the provider, so the circuit breaker is not charged for
		// it. Everything else is: a real provider error, the transport's header
		// timeout, and this gateway's own failover or retry deadline expiring
		// with the caller still waiting, which is the same stall the streaming
		// probe and the body readers (abortKind) already charge. Exactly one
		// breaker failure per candidate attempt, here, after any transient
		// retries are exhausted, so a blip that self-heals on retry never
		// counts against the provider.
		if !abandoned {
			// No status: the request never completed, so there is none.
			h.chargeBreaker(st, candidate, 0, "upstream request failed")
		}
		return nil, false
	}

	// Log upstream response metadata for debugging. The three header values are
	// the upstream's own text, so they are bounded, sanitized and fenced the way
	// every other upstream-controlled value a log line carries is, and only
	// when Debug is on: this is the success path of every request, and the
	// fence's first use parses the request body.
	debuglog.Debug("proxy: upstream response received", "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "model", candidate.model.ModelID, "status", resp.StatusCode,
		"content_type", fencedDebugText(resp.Header.Get("Content-Type"), logData),
		"x_request_id", fencedDebugText(resp.Header.Get("X-Request-Id"), logData),
		"x_ratelimit_remaining", fencedDebugText(resp.Header.Get("X-RateLimit-Remaining"), logData),
		"attempt", attempt+1)
	return resp, true
}
