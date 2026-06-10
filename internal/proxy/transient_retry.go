package proxy

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"syscall"
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
