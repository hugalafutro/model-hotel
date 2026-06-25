package frontdesk

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

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
