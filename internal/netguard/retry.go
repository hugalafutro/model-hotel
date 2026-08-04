package netguard

import (
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// retryAttempts is how many times a login-path request is issued in total: the
// original plus one retry. Every attempt shares the client's single overall
// timeout, so after a resolver that took seconds to fail there is rarely budget
// left for a third.
const retryAttempts = 2

// retryDelay spaces the retry far enough to clear a momentary resolver failure
// without noticeably lengthening a login the user is already waiting on.
const retryDelay = 250 * time.Millisecond

// RetryTransport re-issues a request that failed before any byte reached the
// server: a DNS resolution failure or a dial failure. Both mean the server never
// saw the request, so re-issuing is safe for any method, including the
// non-idempotent OIDC token POST.
//
// Nothing past connection setup is retried. Once a connection exists a failure
// may mean the server processed the request and only the response was lost, and
// replaying a single-use authorization code in that state would turn a transient
// error into a hard invalid_grant.
//
// Base must be non-nil.
type RetryTransport struct {
	Base     http.RoundTripper
	Attempts int
	Delay    time.Duration
}

// RoundTrip implements http.RoundTripper. It returns the underlying error
// unchanged on the last attempt, for an error that is not a pre-connection
// failure, and for a body that cannot be rewound.
func (t *RetryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for attempt := 1; ; attempt++ {
		attemptReq := req
		if attempt > 1 {
			replay, err := replayRequest(req)
			if err != nil {
				return nil, err
			}
			attemptReq = replay
		}

		resp, err := t.Base.RoundTrip(attemptReq)
		if err == nil {
			return resp, nil
		}
		if attempt >= t.Attempts || !preConnectionFailure(err) || !replayable(req) {
			return nil, err
		}

		// Worth a WARN even when the retry succeeds: it is the only signal that
		// the resolver is flaking. The error text carries the host and resolver,
		// never a credential, which live in the request body.
		debuglog.Warn("netguard: retrying request after pre-connection failure",
			"method", req.Method, "host", req.URL.Host, "attempt", attempt, "error", err)

		select {
		case <-req.Context().Done():
			// Surface the transport failure rather than the context error: it is
			// the cause an operator needs, and http.Client reports its own
			// timeout separately.
			return nil, err
		case <-time.After(t.Delay):
		}
	}
}

// preConnectionFailure reports whether err occurred before the request could
// reach the server, which is what makes re-issuing it safe.
func preConnectionFailure(err error) bool {
	// A blocked address is a permanent security denial, not a transient fault.
	if errors.Is(err, ErrBlockedAddress) {
		return false
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}

// replayable reports whether the request body can be produced a second time.
// A nil body and http.NoBody are both trivially reproducible; otherwise it
// takes GetBody, which http.NewRequest sets for the in-memory body types the
// oauth2 and oidc packages use.
func replayable(req *http.Request) bool {
	return req.Body == nil || req.Body == http.NoBody || req.GetBody != nil
}

// replayRequest clones req with a freshly opened body, because the previous
// RoundTrip has already consumed and closed the original.
func replayRequest(req *http.Request) (*http.Request, error) {
	clone := req.Clone(req.Context())
	if req.GetBody == nil {
		return clone, nil
	}
	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	clone.Body = body
	return clone, nil
}

// NewClientWithRetry builds a NewClient that also re-issues a request which
// failed before reaching the server. Login paths use it: a momentary resolver
// failure on the token exchange would otherwise throw the user back to the login
// screen with the whole IdP round trip, consent screen included, to repeat.
// Fire-and-forget senders keep NewClient.
func NewClientWithRetry(timeout time.Duration) *http.Client {
	c := NewClient(timeout)
	c.Transport = &RetryTransport{Base: c.Transport, Attempts: retryAttempts, Delay: retryDelay}
	return c
}
