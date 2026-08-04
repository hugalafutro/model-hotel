package netguard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// stubTransport returns a scripted result per call and records the request body
// it saw each time, so a test can prove the body was replayed intact.
type stubTransport struct {
	results []stubResult
	calls   int
	bodies  []string
}

type stubResult struct {
	resp *http.Response
	err  error
}

func (s *stubTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body := ""
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		_ = req.Body.Close()
		body = string(b)
	}
	s.bodies = append(s.bodies, body)
	i := s.calls
	s.calls++
	if i >= len(s.results) {
		return nil, fmt.Errorf("stub: unexpected call %d", i+1)
	}
	return s.results[i].resp, s.results[i].err
}

func okResponse() *http.Response {
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("ok"))}
}

// dnsFailure mirrors the shape net/http surfaces for the prod incident:
// "dial tcp: lookup auth.zmrd.uk on 127.0.0.11:53: server misbehaving".
func dnsFailure() error {
	return &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: &net.DNSError{Err: "server misbehaving", Name: "auth.example.test", Server: "127.0.0.11:53"},
	}
}

func postRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, "https://idp.example.test/token", strings.NewReader(body))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	return req
}

// TestRetryTransport_RetriesDNSFailure is the prod incident: the token POST dies
// on DNS before reaching the IdP, and the retry has to carry the same body.
func TestRetryTransport_RetriesDNSFailure(t *testing.T) {
	stub := &stubTransport{results: []stubResult{
		{err: dnsFailure()},
		{resp: okResponse()},
	}}
	rt := &RetryTransport{Base: stub, Attempts: 2, Delay: time.Millisecond}

	resp, err := rt.RoundTrip(postRequest(t, "grant_type=authorization_code&code=abc"))
	if err != nil {
		t.Fatalf("RoundTrip = %v, want success on the retry", err)
	}
	_ = resp.Body.Close()
	if stub.calls != 2 {
		t.Fatalf("calls = %d, want 2", stub.calls)
	}
	for i, got := range stub.bodies {
		if got != "grant_type=authorization_code&code=abc" {
			t.Fatalf("attempt %d body = %q, want the original body replayed", i+1, got)
		}
	}
}

// TestRetryTransport_RetriesDialFailure covers a dial that fails for a reason
// other than DNS (refused, unreachable): still nothing reached the server.
func TestRetryTransport_RetriesDialFailure(t *testing.T) {
	stub := &stubTransport{results: []stubResult{
		{err: &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}},
		{resp: okResponse()},
	}}
	rt := &RetryTransport{Base: stub, Attempts: 2, Delay: time.Millisecond}

	resp, err := rt.RoundTrip(postRequest(t, "x=1"))
	if err != nil {
		t.Fatalf("RoundTrip = %v, want success on the retry", err)
	}
	_ = resp.Body.Close()
	if stub.calls != 2 {
		t.Fatalf("calls = %d, want 2", stub.calls)
	}
}

// TestRetryTransport_RetriesNoBodyRequest covers a GET built the idiomatic
// way, with http.NoBody: net/http leaves req.Body non-nil (http.NoBody) and
// req.GetBody nil for this case, so it exercises both the req.Body ==
// http.NoBody clause in replayable and the req.GetBody == nil branch in
// replayRequest.
func TestRetryTransport_RetriesNoBodyRequest(t *testing.T) {
	stub := &stubTransport{results: []stubResult{
		{err: dnsFailure()},
		{resp: okResponse()},
	}}
	rt := &RetryTransport{Base: stub, Attempts: 2, Delay: time.Millisecond}
	req, err := http.NewRequest(http.MethodGet, "https://idp.example.test/jwks", http.NoBody)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip = %v, want success on the retry", err)
	}
	_ = resp.Body.Close()
	if stub.calls != 2 {
		t.Fatalf("calls = %d, want 2", stub.calls)
	}
}

// TestRetryTransport_RetriesNilBodyRequest covers the shape go-oidc sends for
// three of the four OIDC hops: discovery, JWKS and UserInfo are built with
// http.NewRequest and a literal nil body, which leaves both req.Body and
// req.GetBody nil. That is the req.Body == nil clause in replayable, a different
// clause from the http.NoBody one GitHub's helpers exercise.
func TestRetryTransport_RetriesNilBodyRequest(t *testing.T) {
	stub := &stubTransport{results: []stubResult{
		{err: dnsFailure()},
		{resp: okResponse()},
	}}
	rt := &RetryTransport{Base: stub, Attempts: 2, Delay: time.Millisecond}
	//nolint:gocritic // httpNoBody: go-oidc passes a literal nil body, and the
	// point of this test is that exact request shape.
	req, err := http.NewRequest(http.MethodGet, "https://idp.example.test/.well-known/openid-configuration", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	if req.Body != nil || req.GetBody != nil {
		t.Fatalf("Body = %v, GetBody set = %t; want both nil so the nil-body clause is what is under test", req.Body, req.GetBody != nil)
	}

	resp, err := rt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip = %v, want success on the retry", err)
	}
	_ = resp.Body.Close()
	if stub.calls != 2 {
		t.Fatalf("calls = %d, want 2: a nil-body request is replayable", stub.calls)
	}
}

// TestRetryTransport_SucceedsWithoutRetry keeps the common path honest: a
// working request is issued exactly once.
func TestRetryTransport_SucceedsWithoutRetry(t *testing.T) {
	stub := &stubTransport{results: []stubResult{{resp: okResponse()}}}
	rt := &RetryTransport{Base: stub, Attempts: 2, Delay: time.Millisecond}

	resp, err := rt.RoundTrip(postRequest(t, "x=1"))
	if err != nil {
		t.Fatalf("RoundTrip = %v, want success", err)
	}
	_ = resp.Body.Close()
	if stub.calls != 1 {
		t.Fatalf("calls = %d, want 1", stub.calls)
	}
}

// TestRetryTransport_DoesNotRetryAfterConnect is the safety property: once a
// connection exists, the server may have consumed the single-use authorization
// code, so a lost response must never be replayed.
func TestRetryTransport_DoesNotRetryAfterConnect(t *testing.T) {
	stub := &stubTransport{results: []stubResult{
		{err: &net.OpError{Op: "read", Net: "tcp", Err: io.ErrUnexpectedEOF}},
	}}
	rt := &RetryTransport{Base: stub, Attempts: 2, Delay: time.Millisecond}

	if _, err := rt.RoundTrip(postRequest(t, "x=1")); err == nil {
		t.Fatal("RoundTrip = nil error, want the read failure surfaced")
	}
	if stub.calls != 1 {
		t.Fatalf("calls = %d, want 1: a post-connection failure must not be replayed", stub.calls)
	}
}

// TestRetryTransport_DoesNotRetryBlockedAddress: an SSRF denial is permanent,
// so retrying it only doubles the delay before the same refusal.
func TestRetryTransport_DoesNotRetryBlockedAddress(t *testing.T) {
	stub := &stubTransport{results: []stubResult{
		{err: &net.OpError{Op: "dial", Net: "tcp", Err: fmt.Errorf("%w %s", ErrBlockedAddress, "169.254.169.254")}},
	}}
	rt := &RetryTransport{Base: stub, Attempts: 2, Delay: time.Millisecond}

	_, err := rt.RoundTrip(postRequest(t, "x=1"))
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("RoundTrip = %v, want ErrBlockedAddress", err)
	}
	if stub.calls != 1 {
		t.Fatalf("calls = %d, want 1: a security denial must not be retried", stub.calls)
	}
}

// TestRetryTransport_DoesNotRetryUnrewindableBody: without GetBody the body is
// already drained, so a second attempt would send a truncated request.
func TestRetryTransport_DoesNotRetryUnrewindableBody(t *testing.T) {
	stub := &stubTransport{results: []stubResult{{err: dnsFailure()}}}
	rt := &RetryTransport{Base: stub, Attempts: 2, Delay: time.Millisecond}
	req := postRequest(t, "x=1")
	req.GetBody = nil

	if _, err := rt.RoundTrip(req); err == nil {
		t.Fatal("RoundTrip = nil error, want the dial failure surfaced")
	}
	if stub.calls != 1 {
		t.Fatalf("calls = %d, want 1: an unrewindable body must not be replayed", stub.calls)
	}
}

// TestRetryTransport_SurfacesGetBodyError: if the body cannot be reopened the
// caller gets that reason rather than a silent second attempt.
func TestRetryTransport_SurfacesGetBodyError(t *testing.T) {
	stub := &stubTransport{results: []stubResult{{err: dnsFailure()}}}
	rt := &RetryTransport{Base: stub, Attempts: 2, Delay: time.Millisecond}
	req := postRequest(t, "x=1")
	rewindErr := errors.New("body source is gone")
	req.GetBody = func() (io.ReadCloser, error) { return nil, rewindErr }

	_, err := rt.RoundTrip(req)
	if !errors.Is(err, rewindErr) {
		t.Fatalf("RoundTrip = %v, want the GetBody error", err)
	}
	if stub.calls != 1 {
		t.Fatalf("calls = %d, want 1", stub.calls)
	}
}

// TestRetryTransport_ReturnsLastErrorWhenAttemptsExhausted: a resolver that is
// still sick fails the same way it does today, no worse.
func TestRetryTransport_ReturnsLastErrorWhenAttemptsExhausted(t *testing.T) {
	stub := &stubTransport{results: []stubResult{
		{err: dnsFailure()},
		{err: dnsFailure()},
	}}
	rt := &RetryTransport{Base: stub, Attempts: 2, Delay: time.Millisecond}

	_, err := rt.RoundTrip(postRequest(t, "x=1"))
	var dnsErr *net.DNSError
	if !errors.As(err, &dnsErr) {
		t.Fatalf("RoundTrip = %v, want the DNS failure surfaced", err)
	}
	if stub.calls != 2 {
		t.Fatalf("calls = %d, want 2", stub.calls)
	}
}

// TestRetryTransport_StopsOnCancelledContext: the backoff must not outlive the
// request's own deadline.
func TestRetryTransport_StopsOnCancelledContext(t *testing.T) {
	stub := &stubTransport{results: []stubResult{{err: dnsFailure()}}}
	rt := &RetryTransport{Base: stub, Attempts: 2, Delay: 30 * time.Second}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := postRequest(t, "x=1").WithContext(ctx)

	start := time.Now()
	if _, err := rt.RoundTrip(req); err == nil {
		t.Fatal("RoundTrip = nil error, want the dial failure surfaced")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("RoundTrip waited %v; a cancelled context must abandon the backoff", elapsed)
	}
	if stub.calls != 1 {
		t.Fatalf("calls = %d, want 1", stub.calls)
	}
}

// closeIdleBase is a base transport that also implements the CloseIdleConnections
// interface http.Client looks for, and records that it was reached.
type closeIdleBase struct {
	stubTransport
	closed int
}

func (c *closeIdleBase) CloseIdleConnections() { c.closed++ }

// TestRetryTransport_CloseIdleConnectionsReachesBase: http.Client type-asserts
// its transport for CloseIdleConnections, so without the passthrough the wrapper
// would silently turn every caller's close into a no-op.
func TestRetryTransport_CloseIdleConnectionsReachesBase(t *testing.T) {
	base := &closeIdleBase{}
	client := &http.Client{Transport: &RetryTransport{Base: base, Attempts: 2, Delay: time.Millisecond}}

	client.CloseIdleConnections()

	if base.closed != 1 {
		t.Fatalf("base CloseIdleConnections calls = %d, want 1", base.closed)
	}
}

// TestRetryTransport_CloseIdleConnectionsIgnoresBaseWithout: a base that does not
// implement the interface makes the call a no-op, not a panic.
func TestRetryTransport_CloseIdleConnectionsIgnoresBaseWithout(t *testing.T) {
	base := &stubTransport{}
	rt := &RetryTransport{Base: base, Attempts: 2, Delay: time.Millisecond}

	rt.CloseIdleConnections()

	if base.calls != 0 {
		t.Fatalf("calls = %d, want 0: closing idle connections must not issue a request", base.calls)
	}
}

// TestNewClientWithRetry_StillBlocksMetadata proves the wrapper did not defeat
// the SSRF guard it wraps.
func TestNewClientWithRetry_StillBlocksMetadata(t *testing.T) {
	client := NewClientWithRetry(2 * time.Second)
	if _, ok := client.Transport.(*RetryTransport); !ok {
		t.Fatalf("Transport = %T, want *RetryTransport", client.Transport)
	}
	req, err := http.NewRequest(http.MethodGet, "http://169.254.169.254/latest/meta-data/", http.NoBody)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("retrying client reached the link-local metadata address")
	}
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("want ErrBlockedAddress, got %v", err)
	}
}
