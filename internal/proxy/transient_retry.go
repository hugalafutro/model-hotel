package proxy

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"syscall"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// maxTransientRetries is the number of additional same-provider tries after a
// transient network failure, before the failover loop moves on to the next
// candidate (or, for single-provider models, fails the request). Retries run
// inside the per-attempt failover timeout, so they never extend the request's
// total time budget.
const maxTransientRetries = 2

// isRetryableUpstreamError reports whether a transport-level error from the
// upstream request may be retried against the same provider.
//
// Context cancellations are never retried: the client disconnected or a
// failover/retry deadline fired. Timeouts (net.Error.Timeout) are never
// retried either — repeating a slow operation would burn the remaining
// failover budget for no likely gain.
//
// requestWritten is the phase signal (from httptrace): whether any request
// bytes reached the wire. When false (DNS, dial, TLS-handshake failures), the
// provider provably never saw the request, so any transport error is safe to
// retry — a duplicate completion is impossible. Once the request has been
// written, only connection-interruption errors (reset, broken pipe,
// unexpected EOF, server closing an idle connection) are retried; these
// overwhelmingly come from provider-side load-balancer connection churn, and
// a retry carries the same bounded duplicate risk that cross-provider
// failover already accepts.
func isRetryableUpstreamError(err error, requestWritten bool) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return false
	}
	if !requestWritten {
		return true
	}
	// ECONNRESET/EPIPE are the POSIX reset signatures; io.EOF and
	// io.ErrUnexpectedEOF are the cross-platform catch-all for a connection
	// the server closed mid-exchange (e.g. on Windows resets surface as
	// wsarecv errors, not those syscall values).
	if errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		return true
	}
	// net/http does not export errServerClosedIdle; match its message.
	return strings.Contains(err.Error(), "server closed idle connection")
}

// serverErrorRetryBackoff is the base of the jittered wait before the last
// candidate is retried after a retryable 5xx: long enough for a load
// balancer's failed backend to rotate out, short enough that the retry stays
// well inside the request's overall deadline.
const serverErrorRetryBackoff = 500 * time.Millisecond

// retryableServerError reports whether an upstream status is a 5xx worth one
// more try against the same provider when there is no other candidate: the
// provider's own fault, transient by its own account (500 with a "try again",
// a bad gateway, a service unavailable, a gateway timeout). 501 is a refusal
// that will not change, and every other 5xx is left as the provider gave it.
func retryableServerError(status int) bool {
	switch status {
	case http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

// deferLastCandidateRetry decides whether a failover-eligible answer from the
// last candidate earns one of the request's one-shot retries, and ends the
// attempt accordingly: a saturated 429 waits the provider's Retry-After
// (deferSaturatedRetry), a transient 5xx backs off briefly
// (deferServerErrorRetry). Each fires once per request, and the 5xx retry only
// while server_error_retry_enabled is on and the overall deadline leaves room
// for it. ok is false when the answer earns neither and the caller's terminal
// handling applies.
func (h *Handler) deferLastCandidateRetry(st *requestState, candidate modelCandidate, resp *http.Response, attempt int, rl rateLimitVerdict) (outcome candidateOutcome, ok bool) {
	if rl.class == rateLimitSaturated && !st.saturationRetried {
		return h.deferSaturatedRetry(st, candidate, resp, attempt), true
	}
	if st.serverErrorRetryEnabled && retryableServerError(resp.StatusCode) && !st.serverErrorRetried && st.serverErrorRetryBudgetLeft() {
		return h.deferServerErrorRetry(st, candidate, resp, attempt, rl), true
	}
	return outcomeFailover, false
}

// closeDeferredAttempt ends an attempt the loop will retry: the response is
// drained and closed, its error becomes the request's, and the attempt is
// closed on the trail with the detail and phrase given. The retry is its own
// attempt with its own record. Shared by the saturation and server-error
// deferrals so the two cannot record an attempt differently.
func closeDeferredAttempt(st *requestState, resp *http.Response, attempt int, reqErr reqError, detail, phrase string) {
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
	st.setReqErr(reqErr)
	st.logData.failoverAttempt = attempt
	st.logData.closeAttemptRecord(resp.StatusCode, st.lastReqErr.Kind, detail, phrase, 0)
}

// deferServerErrorRetry ends an attempt whose last candidate answered a
// retryable 5xx: the body is drained, the attempt is closed on the trail with
// the provider's sentence (the breaker has already been charged for it), and
// the loop is told to back off and try the same candidate once more
// (outcomeRetryServerError). st.serverErrorRetried caps it at one. rl is the
// attempt's own 429 verdict, handed on the way every failover-shaped close
// does; a 5xx never reads it, but the request-scoped copy is not reached for.
func (h *Handler) deferServerErrorRetry(st *requestState, candidate modelCandidate, resp *http.Response, attempt int, rl rateLimitVerdict) candidateOutcome {
	drained, _ := io.ReadAll(io.LimitReader(resp.Body, failoverErrorClassifyCap))
	st.serverErrorRetried = true
	closeDeferredAttempt(st, resp, attempt, failoverReqErr(rl, attempt, candidate.provider.Name, resp.StatusCode), util.SanitizeLogBody(string(drained), 10000), "")
	debuglog.Info("proxy: last candidate answered a retryable server error, retrying it once", "provider", candidate.provider.Name, "provider_id", candidate.provider.ID, "status", resp.StatusCode, "attempt", attempt+1)
	return outcomeRetryServerError
}
