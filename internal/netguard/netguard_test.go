package netguard

import (
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBlockedIP(t *testing.T) {
	cases := []struct {
		ip   string
		want bool
	}{
		{"169.254.169.254", true},  // AWS/GCP cloud metadata (link-local)
		{"169.254.0.1", true},      // link-local unicast
		{"0.0.0.0", true},          // unspecified
		{"0.0.0.1", true},          // 0.0.0.0/8: another spelling of "this host"
		{"0.1.2.3", true},          // 0.0.0.0/8
		{"0.255.255.255", true},    // 0.0.0.0/8, last address
		{"::ffff:0.1.2.3", true},   // 0.0.0.0/8 in IPv4-mapped form
		{"::", true},               // unspecified v6
		{"fe80::1", true},          // link-local v6
		{"1.0.0.1", false},         // 1.0.0.0/8 is public, the range next door
		{"127.0.0.1", false},       // loopback allowed (internal services)
		{"10.0.0.5", false},        // private allowed (docker network)
		{"::ffff:10.0.0.5", false}, // the same private address, IPv4-mapped
		{"192.168.1.10", false},    // private allowed
		{"172.17.0.2", false},      // docker bridge allowed (apprise-api)
		{"fc00::1", false},         // ULA allowed, the v6 equivalent of RFC1918
		{"fd12:3456::1", false},    // ULA allowed
		{"8.8.8.8", false},         // public
	}
	for _, tc := range cases {
		ip := net.ParseIP(tc.ip)
		if ip == nil {
			t.Fatalf("bad test IP %q", tc.ip)
		}
		if got := BlockedIP(ip); got != tc.want {
			t.Errorf("BlockedIP(%s) = %v, want %v", tc.ip, got, tc.want)
		}
	}
	if BlockedIP(nil) {
		t.Error("BlockedIP(nil) = true, want false")
	}
}

// TestNewClient_RejectsMetadataLiteral confirms the dial guard refuses a literal
// cloud-metadata address, the core SSRF defense for OIDC/alert outbound calls.
func TestNewClient_RejectsMetadataLiteral(t *testing.T) {
	client := NewClient(2 * time.Second)
	_, err := client.Get("http://169.254.169.254/latest/meta-data/")
	if err == nil {
		t.Fatal("expected dial to cloud-metadata address to be refused, got nil error")
	}
	if !strings.Contains(err.Error(), "blocked address") {
		t.Errorf("expected blocked-address error, got: %v", err)
	}
}

// TestDialControl confirms the guard runs on the resolved dial address, so a
// hostname that resolves to a metadata address is refused too (DNS rebinding),
// while a private address is allowed.
func TestDialControl(t *testing.T) {
	if err := dialControl("tcp", "169.254.169.254:80", nil); err == nil {
		t.Error("dialControl allowed a metadata address")
	}
	if err := dialControl("tcp", "10.0.0.1:80", nil); err != nil {
		t.Errorf("dialControl blocked a private address: %v", err)
	}
}

// TestNewClient_AllowsInternal confirms a legitimate internal (private) host is
// reachable — the apprise-api / internal-IdP case must not be blocked.
func TestNewClient_AllowsInternal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	// httptest binds to loopback, which the guard allows.
	resp, err := NewClient(2 * time.Second).Get(srv.URL)
	if err != nil {
		t.Fatalf("expected loopback reachable, got: %v", err)
	}
	_ = resp.Body.Close()
}

// TestCheckRedirect confirms the redirect guard refuses a redirect to a literal
// metadata address, caps the chain length, and lets a normal (private/public)
// redirect target through so legitimate IdP/notification redirects still work.
func TestCheckRedirect(t *testing.T) {
	mk := func(rawURL string) *http.Request {
		req, err := http.NewRequest(http.MethodGet, rawURL, http.NoBody)
		if err != nil {
			t.Fatalf("build request %q: %v", rawURL, err)
		}
		return req
	}

	if err := checkRedirect(mk("http://169.254.169.254/latest/"), nil); err == nil {
		t.Error("checkRedirect allowed a redirect to a metadata literal")
	}
	if err := checkRedirect(mk("http://0.0.0.0/"), nil); err == nil {
		t.Error("checkRedirect allowed a redirect to the unspecified address")
	}
	if err := checkRedirect(mk("https://auth.example.com/callback"), nil); err != nil {
		t.Errorf("checkRedirect blocked a legitimate hostname redirect: %v", err)
	}
	if err := checkRedirect(mk("http://10.0.0.5:9091/"), nil); err != nil {
		t.Errorf("checkRedirect blocked a private-address redirect: %v", err)
	}

	via := make([]*http.Request, maxRedirects)
	if err := checkRedirect(mk("https://auth.example.com/"), via); err == nil {
		t.Errorf("checkRedirect allowed redirect #%d, want cap at %d", maxRedirects+1, maxRedirects)
	}
}

func TestValidateURL(t *testing.T) {
	cases := []struct {
		url     string
		wantErr bool
	}{
		{"", false},
		{"https://auth.example.com", false},
		{"http://authelia:9091", false},  // internal IdP hostname
		{"http://10.0.0.5:8000", false},  // internal literal, allowed
		{"http://apprise:8000", false},   // internal apprise
		{"http://169.254.169.254", true}, // cloud metadata literal
		{"http://169.254.0.1", true},     // link-local literal
		{"http://0.0.0.0", true},         // unspecified literal
		{"ftp://example.com", true},      // wrong scheme
		{"https://", true},               // no host
		{"://bad", true},                 // unparseable
	}
	for _, tc := range cases {
		err := ValidateURL(tc.url)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidateURL(%q) err=%v, wantErr=%v", tc.url, err, tc.wantErr)
		}
	}
}

// TestDialControlErrorIsSentinel pins the blocked-address denial to a sentinel
// error. RetryTransport relies on it to tell a permanent security refusal apart
// from a transient dial fault, which it would otherwise retry pointlessly.
func TestDialControlErrorIsSentinel(t *testing.T) {
	client := NewClient(2 * time.Second)
	req, err := http.NewRequest(http.MethodGet, "http://169.254.169.254/latest/meta-data/", http.NoBody)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
		t.Fatal("dial to the link-local metadata address succeeded; expected a netguard block")
	}
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("dial guard error must wrap ErrBlockedAddress, got %v", err)
	}
}

// TestNewGuardedDialerBoundsOneAttempt pins the budget RetryTransport depends
// on. A dialer that inherits the caller's whole timeout lets one slow failure
// (a resolver that takes seconds to give up) consume the entire request, and
// the retry then has nothing left to run in. A caller whose own timeout is
// already shorter keeps that shorter bound rather than being extended.
func TestNewGuardedDialerBoundsOneAttempt(t *testing.T) {
	if got := newGuardedDialer(15 * time.Second).Timeout; got != dialTimeout {
		t.Errorf("dialer timeout = %v, want it capped at %v so a retry keeps budget", got, dialTimeout)
	}
	short := 900 * time.Millisecond
	if got := newGuardedDialer(short).Timeout; got != short {
		t.Errorf("dialer timeout = %v, want the caller's shorter %v", got, short)
	}
	// Zero is net.Dialer's "no deadline", so passing a caller's zero straight
	// through would leave the one client with no overall backstop dialling
	// forever, the opposite of the bound this function exists to impose.
	if got := newGuardedDialer(0).Timeout; got != dialTimeout {
		t.Errorf("dialer timeout for an unbounded caller = %v, want %v", got, dialTimeout)
	}
	if newGuardedDialer(time.Second).Control == nil {
		t.Error("dialer Control = nil, want the blocked-address guard installed")
	}
}

// TestNewClientBoundsTLSHandshake covers the half of connection setup the
// dialer's own timeout does not reach. net/http leaves TLSHandshakeTimeout
// unbounded at zero, so an endpoint that accepts the TCP connection and then
// stalls the handshake would burn the caller's whole budget, leaving nothing
// for the retry, and every real IdP is HTTPS.
func TestNewClientBoundsTLSHandshake(t *testing.T) {
	transport := NewClient(15 * time.Second).Transport
	tr, ok := transport.(*http.Transport)
	if !ok {
		t.Fatalf("Transport = %T, want *http.Transport", transport)
	}
	if tr.TLSHandshakeTimeout != dialTimeout {
		t.Errorf("TLSHandshakeTimeout = %v, want %v", tr.TLSHandshakeTimeout, dialTimeout)
	}
	if tr.IdleConnTimeout != idleConnTimeout {
		t.Errorf("IdleConnTimeout = %v, want %v so a pooled connection is not reused long after it died", tr.IdleConnTimeout, idleConnTimeout)
	}
}

func TestValidatePublicURL(t *testing.T) {
	cases := []struct {
		url     string
		wantErr bool
	}{
		{"", false},
		{"https://app.example.com", false},
		{"http://localhost:8080", false}, // public base only structural
		{"ftp://example.com", true},
		{"notaurl", true},
		{"https://", true},
	}
	for _, tc := range cases {
		err := ValidatePublicURL(tc.url)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidatePublicURL(%q) err=%v, wantErr=%v", tc.url, err, tc.wantErr)
		}
	}
}
