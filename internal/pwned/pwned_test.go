package pwned

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sha1("password") = 5BAA61E4C9B93F3F0682250B6CF8331B7EE68FD8
const (
	pwPassword      = "password"
	pwPasswordPfx   = "5BAA6"
	pwPasswordSfx   = "1E4C9B93F3F0682250B6CF8331B7EE68FD8"
	pwNeverBreached = "correct horse battery staple !@#$ 2026"
)

// rangeServer stands in for the HIBP range API. It records the last request it
// saw and returns the supplied body verbatim for any /range/{prefix} path.
func rangeServer(t *testing.T, body string) (*httptest.Server, *http.Request) {
	t.Helper()
	var last http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		last = *r
		if !strings.HasPrefix(r.URL.Path, "/range/") {
			http.Error(w, "bad path", http.StatusNotFound)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &last
}

func TestBreached_Match(t *testing.T) {
	body := fmt.Sprintf("00000000000000000000000000000000000:3\r\n%s:42\r\nFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF:1\r\n", pwPasswordSfx)
	srv, last := rangeServer(t, body)
	c := New(srv.URL, srv.Client())

	breached, count, err := c.Breached(context.Background(), pwPassword)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !breached || count != 42 {
		t.Fatalf("got breached=%v count=%d, want true/42", breached, count)
	}
	// Only the 5-char prefix must ever be sent to the endpoint.
	if want := "/range/" + pwPasswordPfx; last.URL.Path != want {
		t.Fatalf("request path = %q, want %q (only the prefix, never the suffix)", last.URL.Path, want)
	}
	if last.Header.Get("Add-Padding") != "true" {
		t.Fatalf("Add-Padding header = %q, want \"true\"", last.Header.Get("Add-Padding"))
	}
}

func TestBreached_MatchIsCaseInsensitive(t *testing.T) {
	// HIBP returns uppercase suffixes; accept a lowercased mirror too.
	body := strings.ToLower(pwPasswordSfx) + ":7\n"
	srv, _ := rangeServer(t, body)
	c := New(srv.URL, srv.Client())

	breached, count, err := c.Breached(context.Background(), pwPassword)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !breached || count != 7 {
		t.Fatalf("got breached=%v count=%d, want true/7", breached, count)
	}
}

func TestBreached_NoMatch(t *testing.T) {
	body := "00000000000000000000000000000000000:3\nFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF:1\n"
	srv, _ := rangeServer(t, body)
	c := New(srv.URL, srv.Client())

	breached, count, err := c.Breached(context.Background(), pwNeverBreached)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if breached || count != 0 {
		t.Fatalf("got breached=%v count=%d, want false/0", breached, count)
	}
}

func TestBreached_PaddingDecoyIgnored(t *testing.T) {
	// Add-Padding decoys carry a count of 0; a suffix that matches a decoy must
	// still read as not-breached.
	body := pwPasswordSfx + ":0\n"
	srv, _ := rangeServer(t, body)
	c := New(srv.URL, srv.Client())

	breached, _, err := c.Breached(context.Background(), pwPassword)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if breached {
		t.Fatal("a decoy suffix (count 0) must not count as breached")
	}
}

func TestBreached_Non200IsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)
	c := New(srv.URL, srv.Client())

	if _, _, err := c.Breached(context.Background(), pwPassword); err == nil {
		t.Fatal("expected an error for a non-200 response so the caller can fail open")
	}
}

func TestBreached_TransportErrorIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close() // nothing is listening now

	c := New(url, srv.Client())
	if _, _, err := c.Breached(context.Background(), pwPassword); err == nil {
		t.Fatal("expected a transport error when the endpoint is unreachable")
	}
}

func TestNew_TrimsTrailingSlash(t *testing.T) {
	c := New(" https://example.test/ ", nil)
	if c.baseURL != "https://example.test" {
		t.Fatalf("baseURL = %q, want trimmed", c.baseURL)
	}
}
