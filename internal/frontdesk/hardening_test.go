package frontdesk

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"testing/fstest"
)

// stubTotpReader is a totpStatusReader whose IsEnabled result is fully scripted,
// so the cache's seed and refresh behaviour can be exercised without a database.
type stubTotpReader struct {
	enabled bool
	err     error
}

func (s stubTotpReader) IsEnabled(context.Context) (bool, error) { return s.enabled, s.err }

// TestTotpEnabledCacheFailsClosed guards the P0 2FA-bypass fix: a failed read of
// the TOTP-enabled state, on either seed or refresh, must leave the cache
// reporting "enabled" so a raw FRONTDESK_TOKEN is never accepted as a full
// session while the real state is unknown.
func TestTotpEnabledCacheFailsClosed(t *testing.T) {
	t.Run("seed fails closed", func(t *testing.T) {
		c := newTotpEnabledCache(stubTotpReader{err: errors.New("db down")})
		if !c.Enabled() {
			t.Fatal("seed error must fail closed (Enabled()==true)")
		}
	})

	t.Run("refresh fails closed from disabled", func(t *testing.T) {
		c := &totpEnabledCache{repo: stubTotpReader{err: errors.New("db down")}}
		c.val.Store(false) // previously observed "disabled"
		c.Refresh(context.Background())
		if !c.Enabled() {
			t.Fatal("refresh error must fail closed (Enabled()==true), got false")
		}
	})

	t.Run("refresh tracks a successful read", func(t *testing.T) {
		c := &totpEnabledCache{repo: stubTotpReader{enabled: false}}
		c.val.Store(true)
		c.Refresh(context.Background())
		if c.Enabled() {
			t.Fatal("a successful read of disabled must update the cache to false")
		}
	})
}

// TestSecurityHeaders guards the clickjacking fix: every Front Desk response
// must carry the frame, content-type, referrer, and CSP hardening headers. The
// middleware runs ahead of routing and auth, so this holds on the embedded SPA
// (the framed surface), the authenticated API, the unauthenticated
// compose-internal Traefik-config endpoint, and error responses (401/404)
// alike. Front Desk's session rides cookies, so a framed same-origin copy
// auto-authenticates; frame-ancestors 'none' / X-Frame-Options: DENY is what
// keeps this privileged console out of anyone's frame.
func TestSecurityHeaders(t *testing.T) {
	// Mount a stand-in SPA so "/" serves the real UI surface, not a 404.
	ui := fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>fd</title>")},
	}
	srv, _ := newTestServerUI(t, ui)

	want := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "DENY",
		"Referrer-Policy":         "strict-origin-when-cross-origin",
		"Content-Security-Policy": "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'",
	}

	for _, tc := range []struct {
		name     string
		path     string
		auth     bool
		wantCode int
	}{
		{"spa index (the framed page)", "/", false, http.StatusOK},
		{"authenticated API", "/api/members", true, http.StatusOK},
		{"unauthenticated 401", "/api/members", false, http.StatusUnauthorized},
		{"unauth compose-internal traefik config", "/traefik/config", false, http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := do(t, srv, http.MethodGet, tc.path, "", tc.auth)
			if rec.Code != tc.wantCode {
				t.Fatalf("%s: status = %d, want %d", tc.path, rec.Code, tc.wantCode)
			}
			for h, v := range want {
				if got := rec.Header().Get(h); got != v {
					t.Errorf("%s: header %s = %q, want %q", tc.path, h, got, v)
				}
			}
			// Plain HTTP (no TLS) must not advertise HSTS, or browsers cache a
			// broken redirect to a non-existent HTTPS listener.
			if got := rec.Header().Get("Strict-Transport-Security"); got != "" {
				t.Errorf("%s: HSTS set on plain HTTP: %q", tc.path, got)
			}
		})
	}

	// Over TLS the same middleware advertises HSTS.
	t.Run("hsts set over tls", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/traefik/config", http.NoBody)
		req.TLS = &tls.ConnectionState{} // mark the request as TLS-terminated
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		if got := rec.Header().Get("Strict-Transport-Security"); got != "max-age=63072000; includeSubDomains; preload" {
			t.Errorf("HSTS over TLS = %q, want the preload max-age", got)
		}
		// Frame protection is still present over TLS.
		if got := rec.Header().Get("X-Frame-Options"); got != "DENY" {
			t.Errorf("X-Frame-Options over TLS = %q, want DENY", got)
		}
	})
}

func TestIsProbeBlockedIP(t *testing.T) {
	cases := []struct {
		ip      string
		blocked bool
	}{
		{"169.254.169.254", true}, // cloud metadata (link-local unicast)
		{"169.254.0.1", true},     // link-local
		{"0.0.0.0", true},         // unspecified
		{"::", true},              // unspecified v6
		{"fe80::1", true},         // link-local v6
		{"10.0.0.5", false},       // private: a legitimate internal member
		{"192.168.1.20", false},   // private
		{"172.16.5.4", false},     // private
		{"127.0.0.1", false},      // loopback allowed (not our concern here)
		{"8.8.8.8", false},        // public
	}
	for _, c := range cases {
		ip := net.ParseIP(c.ip)
		if ip == nil {
			t.Fatalf("bad test IP %q", c.ip)
		}
		if got := isProbeBlockedIP(ip); got != c.blocked {
			t.Errorf("isProbeBlockedIP(%s) = %v, want %v", c.ip, got, c.blocked)
		}
	}
}

// TestNormalizeMemberURLRejectsBlockedIP guards the add-time leg of the SSRF
// fix: a literal cloud-metadata / link-local / unspecified host is rejected,
// while internal private hosts and hostnames are accepted.
func TestNormalizeMemberURLRejectsBlockedIP(t *testing.T) {
	// allowHTTP=true here so rejection is attributable to the blocked address,
	// not the scheme (the scheme gate is covered by TestNormalizeMemberURLHTTPSGate).
	rejected := []string{
		"http://169.254.169.254",
		"http://169.254.169.254:80/api",
		"http://0.0.0.0:8080",
		"http://[fe80::1]:8080",
	}
	for _, raw := range rejected {
		if _, err := normalizeMemberURL(raw, true); !errors.Is(err, ErrValidation) {
			t.Errorf("normalizeMemberURL(%q) = %v, want ErrValidation", raw, err)
		}
	}

	accepted := []string{
		"http://10.0.0.5:8080",
		"https://mh1.internal:8080",
		"http://member-2:8080/base",
		"https://example.com",
	}
	for _, raw := range accepted {
		if _, err := normalizeMemberURL(raw, true); err != nil {
			t.Errorf("normalizeMemberURL(%q) unexpected error: %v", raw, err)
		}
	}
}

// TestNormalizeMemberURLHTTPSGate guards the FRONTDESK_ALLOW_HTTP_MEMBERS flag:
// plain http is rejected by default and accepted only when opted in; https is
// always accepted.
func TestNormalizeMemberURLHTTPSGate(t *testing.T) {
	// Default (allowHTTP=false): http rejected, https accepted.
	if _, err := normalizeMemberURL("http://mh1:8080", false); !errors.Is(err, ErrInsecureURL) {
		t.Errorf("http with allowHTTP=false: got %v, want ErrInsecureURL", err)
	}
	if _, err := normalizeMemberURL("https://mh1:8080", false); err != nil {
		t.Errorf("https with allowHTTP=false: unexpected error %v", err)
	}
	// Opted in (allowHTTP=true): http accepted.
	if _, err := normalizeMemberURL("http://mh1:8080", true); err != nil {
		t.Errorf("http with allowHTTP=true: unexpected error %v", err)
	}
}

// TestNormalizeMemberURLRejectsUserinfo guards the credential-leak fix: a
// member base URL carrying userinfo (basic-auth credentials) is rejected at
// add time, because the stored URL is re-emitted verbatim to wider audiences
// (the unauthenticated /traefik/config endpoint and the device-readable
// event log).
func TestNormalizeMemberURLRejectsUserinfo(t *testing.T) {
	rejected := []string{
		"http://admin:S3cretPass@127.0.0.1:8086",
		"https://user:pass@host.example",
		"http://user@127.0.0.1:8086", // a bare username is still userinfo
	}
	for _, raw := range rejected {
		if _, err := normalizeMemberURL(raw, true); !errors.Is(err, ErrValidation) {
			t.Errorf("normalizeMemberURL(%q) = %v, want ErrValidation", raw, err)
		}
	}
	if _, err := normalizeMemberURL("http://127.0.0.1:8086", true); err != nil {
		t.Errorf("normalizeMemberURL without userinfo: unexpected error %v", err)
	}
}

// TestStripUserinfo guards the defensive strip applied where stored member
// URLs are rendered to unauthenticated or lower-privilege audiences:
// userinfo is removed, credential-free URLs pass through unchanged, and an
// unparseable string is returned as-is rather than dropped.
func TestStripUserinfo(t *testing.T) {
	cases := []struct{ in, want string }{
		{"http://admin:S3cretPass@10.0.0.5:8080", "http://10.0.0.5:8080"},
		{"https://user@mh1.internal:8080/base", "https://mh1.internal:8080/base"},
		{"http://10.0.0.5:8080", "http://10.0.0.5:8080"},
		{"http://exa mple.com", "http://exa mple.com"}, // unparseable: passthrough
	}
	for _, c := range cases {
		if got := stripUserinfo(c.in); got != c.want {
			t.Errorf("stripUserinfo(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestRedactErrURL guards the error rendering used by monitor-readable status
// fields: userinfo embedded in a URL inside the error text is removed, whether
// the *url.Error is the top-level error or wrapped, while the host, path, and
// cause survive. An error without a URL passes through unchanged.
func TestRedactErrURL(t *testing.T) {
	base := &url.Error{Op: "Get", URL: "http://leakuser:leakpass@10.0.0.5:8080/health", Err: errors.New("dial tcp: connection refused")}
	for _, err := range []error{base, fmt.Errorf("read export: %w", base)} {
		got := redactErrURL(err)
		if strings.Contains(got, "leakuser") || strings.Contains(got, "leakpass") {
			t.Errorf("redactErrURL(%v) = %q, still carries credentials", err, got)
		}
		if !strings.Contains(got, "10.0.0.5:8080/health") {
			t.Errorf("redactErrURL(%v) = %q, lost the host/path", err, got)
		}
	}
	if got := redactErrURL(errors.New("plain failure")); got != "plain failure" {
		t.Errorf("redactErrURL(plain) = %q, want passthrough", got)
	}
	// net/http renders the username percent-decoded, so an email-style
	// username puts a literal @ inside the userinfo; the whole userinfo up to
	// the last @ before the host must go.
	multi := &url.Error{Op: "Get", URL: "http://leakuser@corp:***@10.0.0.5:8080/health", Err: errors.New("dial tcp: connection refused")}
	got := redactErrURL(multi)
	if strings.Contains(got, "leakuser") || strings.Contains(got, "corp") {
		t.Errorf("redactErrURL(multi-@) = %q, still carries part of the username", got)
	}
	if !strings.Contains(got, "10.0.0.5:8080/health") {
		t.Errorf("redactErrURL(multi-@) = %q, lost the host/path", got)
	}
}

// TestListEventsStripsStoredURLUserinfo guards the read-side leg of the
// credential-leak fix: an event row whose stored metadata predates the
// userinfo rejection (e.g. from a restored backup) is returned with the
// credentials removed, since /api/events is monitor-readable.
func TestListEventsStripsStoredURLUserinfo(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if _, err := s.InsertEvent(ctx, Event{
		Type: "member.added", Severity: "info", Source: "frontdesk", Message: "legacy added",
		Metadata: map[string]any{"url": "http://admin:S3cretPass@10.0.0.5:8080", "note": "kept"},
	}); err != nil {
		t.Fatalf("InsertEvent: %v", err)
	}
	evs, _, err := s.ListEvents(ctx, EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	if len(evs) != 1 {
		t.Fatalf("events = %d, want 1", len(evs))
	}
	if got := evs[0].Metadata["url"]; got != "http://10.0.0.5:8080" {
		t.Errorf("metadata url = %v, want credentials stripped", got)
	}
	if got := evs[0].Metadata["note"]; got != "kept" {
		t.Errorf("metadata note = %v, want untouched", got)
	}
}

// TestListMembersStripsStoredURLUserinfo guards the members-list leg of the
// credential-leak fix: a member row whose stored URL predates the userinfo
// rejection is listed without its credentials, since GET /api/members is
// monitor-readable.
func TestListMembersStripsStoredURLUserinfo(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()
	m, err := store.CreateMember(ctx, "legacy", "http://10.0.0.5:8080", "")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}
	// Plant a pre-fix row shape directly: CreateMember itself now rejects it.
	if _, err := store.db.ExecContext(ctx, "UPDATE members SET url = ? WHERE id = ?",
		"http://admin:S3cretPass@10.0.0.5:8080", m.ID); err != nil {
		t.Fatalf("plant legacy url: %v", err)
	}
	rec := do(t, srv, http.MethodGet, "/api/members", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/members = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if strings.Contains(body, "S3cretPass") {
		t.Errorf("members list still carries credentials: %s", body)
	}
	if !strings.Contains(body, "http://10.0.0.5:8080") {
		t.Errorf("members list lost the member URL: %s", body)
	}
}

// TestVersionFetchFailureRaisesEvent guards P1-3: a member whose version cannot
// be read is surfaced with a single visible event once the failure threshold is
// crossed, and a later success records a recovery event.
func TestVersionFetchFailureRaisesEvent(t *testing.T) {
	ctx := context.Background()
	var ok atomic.Bool // false => 500, true => 200 with a version

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != memberSettingsPath {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if !ok.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"app_version":"9.9.9"}`))
	}))
	defer srv.Close()

	p, s, _ := newTestPoller(t, "")
	if _, err := s.CreateMember(ctx, "m1", srv.URL, "member-admin-token"); err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	// Below threshold: no event yet.
	for range versionFetchFailThreshold - 1 {
		p.PollVersionsOnce(ctx)
	}
	if n := countEvents(t, s, "version.fetch_failed"); n != 0 {
		t.Fatalf("expected no fetch_failed event before threshold, got %d", n)
	}

	// Crossing the threshold raises exactly one event.
	p.PollVersionsOnce(ctx)
	if n := countEvents(t, s, "version.fetch_failed"); n != 1 {
		t.Fatalf("expected 1 fetch_failed event at threshold, got %d", n)
	}

	// Recovery records a recovered event and clears the version.
	ok.Store(true)
	p.PollVersionsOnce(ctx)
	if n := countEvents(t, s, "version.fetch_recovered"); n != 1 {
		t.Fatalf("expected 1 fetch_recovered event, got %d", n)
	}
}

func countEvents(t *testing.T, s *Store, typ string) int {
	t.Helper()
	evs, _, err := s.ListEvents(context.Background(), EventFilter{Type: typ})
	if err != nil {
		t.Fatalf("ListEvents: %v", err)
	}
	return len(evs)
}
