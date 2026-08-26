package provider

import (
	"io"
	"net/http"
	"time"
)

// Fixtures and stub upstreams the discovery tests share.

func mustParseTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return t
}

// ---------------------------------------------------------------------------
// isTransientNetworkError
// ---------------------------------------------------------------------------

// timeoutError implements net.Error with Timeout()=true
type timeoutError struct{}

func (timeoutError) Error() string   { return "timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return false }

// noTimeoutError implements net.Error with Timeout()=false
type noTimeoutError struct{}

func (noTimeoutError) Error() string   { return "not a timeout" }
func (noTimeoutError) Timeout() bool   { return false }
func (noTimeoutError) Temporary() bool { return false }

// roundTripperFunc wraps a function to implement http.RoundTripper
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

// errorBodyRoundTripper returns a response with a body that fails on read
type errorBodyRoundTripper struct{}

func (e *errorBodyRoundTripper) RoundTrip(_ *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(&failingReader{}),
	}, nil
}

// failingReader returns valid JSON once, then returns an error.
// Without state tracking, the reader would satisfy the entire read in one
// call (when len(p) >= len(data)), never triggering the error path.
type failingReader struct{ called bool }

func (f *failingReader) Read(p []byte) (int, error) {
	if f.called {
		return 0, io.ErrUnexpectedEOF
	}
	f.called = true
	data := []byte(`{"models":[],"next_page_token":""}`)
	copy(p, data)
	return len(data), nil
}
