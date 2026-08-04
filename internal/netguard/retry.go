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
// left for a third. The retry is best-effort within that budget: a failure that
// takes the whole timeout to surface leaves no room for it, so this rescues a
// fast pre-connection failure and not a slow one.
const retryAttempts = 2

// retryDelay spaces the retry far enough to clear a momentary resolver failure
// without noticeably lengthening a login the user is already waiting on.
const retryDelay = 250 * time.Millisecond

// RetryTransport re-issues a request whose error is a DNS resolution or dial
// failure. Normally that means nothing ever left this process, so re-issuing is
// safe for any method, including the non-idempotent OIDC token POST.
//
// One shape is not purely pre-connection: net/http re-issues a request itself
// when a reused connection dies, and if the follow-up dial then fails, that dial
// error surfaces here even though an earlier attempt was already written to a
// server. The stdlib only re-issues a request it has deemed replayable (GET,
// HEAD, OPTIONS, TRACE, or one carrying an Idempotency-Key), so those are
// idempotent by HTTP semantics and one more issue changes nothing. A
// non-idempotent POST never reaches that path.
//
// Any other error is returned untouched. Once a connection carries the request,
// a lost response may mean the server processed it, and replaying a single-use
// authorization code in that state would turn a transient error into a hard
// invalid_grant.
//
// Base must be non-nil. An Attempts below 2 issues the request exactly once,
// which makes the zero value a passthrough.
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

// CloseIdleConnections keeps (*http.Client).CloseIdleConnections working: the
// client type-asserts its transport for this method, so a wrapper that omits it
// turns the call into a silent no-op. Bases without it are left alone.
func (t *RetryTransport) CloseIdleConnections() {
	if c, ok := t.Base.(interface{ CloseIdleConnections() }); ok {
		c.CloseIdleConnections()
	}
}

// preConnectionFailure reports whether err is a DNS or dial failure. See
// RetryTransport for why re-issuing on those is safe.
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
