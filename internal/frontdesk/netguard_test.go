package frontdesk

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// TestCheckProbeRedirect pins the redirect policy that protects the member admin
// Bearer token: cross-host and https->http-downgrade redirects are refused, while
// same-host same-or-stronger-scheme redirects are allowed.
func TestCheckProbeRedirect(t *testing.T) {
	mk := func(raw string) *http.Request {
		u, err := url.Parse(raw)
		if err != nil {
			t.Fatalf("parse %q: %v", raw, err)
		}
		return &http.Request{URL: u}
	}
	cases := []struct {
		name, from, to string
		ok             bool
	}{
		{"same host https->https", "https://m:8443/a", "https://m:8443/b", true},
		{"same host http->http", "http://m:8081/a", "http://m:8081/b", true},
		{"cross host", "https://m:8443/a", "https://evil:8443/b", false},
		{"https downgrade to http", "https://m:8443/a", "http://m:8443/b", false},
		{"initial request (no via)", "", "https://m/b", true},
	}
	for _, c := range cases {
		var via []*http.Request
		if c.from != "" {
			via = []*http.Request{mk(c.from)}
		}
		err := checkProbeRedirect(mk(c.to), via)
		if c.ok && err != nil {
			t.Errorf("%s: got %v, want nil", c.name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: got nil, want an error", c.name)
		}
	}
}

// TestProbeClientGuards exercises the live guards on the probe client (which the
// pure isProbeBlockedIP test does not reach): a dial to a blocked address is
// refused at the Control hook, a cross-host redirect is refused by CheckRedirect,
// and a plain loopback request is allowed through.
func TestProbeClientGuards(t *testing.T) {
	c := newProbeClient(2 * time.Second)

	// Dial to a cloud-metadata IP is blocked before connecting.
	if _, err := c.Get("http://169.254.169.254/"); err == nil {
		t.Error("expected a dial to the metadata IP to be blocked")
	}

	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer final.Close()
	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusFound)
	}))
	defer redir.Close()

	// A redirect to a different host:port is refused.
	if _, err := c.Get(redir.URL); err == nil {
		t.Error("expected a cross-host redirect to be refused")
	}

	// A direct loopback request is allowed (members live on the internal network).
	resp, err := c.Get(final.URL)
	if err != nil {
		t.Fatalf("loopback request should be allowed: %v", err)
	}
	_ = resp.Body.Close()
}
