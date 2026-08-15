package authcookie

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSetSession_SetsHardenedCookies(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := SetSession(rec, "sess-abc", true, time.Hour); err != nil {
		t.Fatalf("SetSession: %v", err)
	}
	cookies := rec.Result().Cookies()
	var sess, csrf *http.Cookie
	for _, c := range cookies {
		switch c.Name {
		case SessionCookie:
			sess = c
		case CSRFCookie:
			csrf = c
		}
	}
	if sess == nil || csrf == nil {
		t.Fatalf("expected both %s and %s cookies, got %+v", SessionCookie, CSRFCookie, cookies)
	}
	if !sess.HttpOnly {
		t.Error("session cookie must be HttpOnly")
	}
	if csrf.HttpOnly {
		t.Error("csrf cookie must be readable (not HttpOnly)")
	}
	if sess.SameSite != http.SameSiteStrictMode || csrf.SameSite != http.SameSiteStrictMode {
		t.Error("both cookies must be SameSite=Strict")
	}
	if !sess.Secure || !csrf.Secure {
		t.Error("secure=true must set Secure on both cookies")
	}
	if sess.Value != "sess-abc" {
		t.Errorf("session value = %q, want sess-abc", sess.Value)
	}
	if csrf.Value == "" {
		t.Error("csrf cookie must carry a generated value")
	}
}

func TestValidCSRF_HeaderMatchesCookie(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/api/x", http.NoBody)
	r.AddCookie(&http.Cookie{Name: CSRFCookie, Value: "tok123"})
	r.Header.Set(CSRFHeader, "tok123")
	if !ValidCSRF(r) {
		t.Error("matching header and cookie should be valid")
	}
	r2 := httptest.NewRequest(http.MethodPost, "/api/x", http.NoBody)
	r2.AddCookie(&http.Cookie{Name: CSRFCookie, Value: "tok123"})
	r2.Header.Set(CSRFHeader, "wrong")
	if ValidCSRF(r2) {
		t.Error("mismatched header should be invalid")
	}
	r3 := httptest.NewRequest(http.MethodPost, "/api/x", http.NoBody)
	r3.AddCookie(&http.Cookie{Name: CSRFCookie, Value: "tok123"})
	if ValidCSRF(r3) {
		t.Error("missing header should be invalid")
	}
}

func TestClearSession_ExpiresBothCookies(t *testing.T) {
	rec := httptest.NewRecorder()
	ClearSession(rec, true)

	cookies := rec.Result().Cookies()
	var sess, csrf *http.Cookie
	for _, c := range cookies {
		switch c.Name {
		case SessionCookie:
			sess = c
		case CSRFCookie:
			csrf = c
		}
	}
	if sess == nil || csrf == nil {
		t.Fatalf("expected both %s and %s cookies, got %+v", SessionCookie, CSRFCookie, cookies)
	}
	if sess.MaxAge != -1 || csrf.MaxAge != -1 {
		t.Errorf("both cookies must expire with MaxAge -1, got session=%d csrf=%d", sess.MaxAge, csrf.MaxAge)
	}
	if !sess.HttpOnly {
		t.Error("session cookie must stay HttpOnly on clear")
	}
	if csrf.HttpOnly {
		t.Error("csrf cookie must stay readable (not HttpOnly) on clear")
	}
	if sess.SameSite != http.SameSiteStrictMode || csrf.SameSite != http.SameSiteStrictMode {
		t.Error("both cleared cookies must stay SameSite=Strict")
	}
	if !sess.Secure || !csrf.Secure {
		t.Error("secure=true must set Secure on both cleared cookies")
	}
}

func TestClearSession_SecureFalse(t *testing.T) {
	rec := httptest.NewRecorder()
	ClearSession(rec, false)

	for _, c := range rec.Result().Cookies() {
		if c.Secure {
			t.Errorf("secure=false must not set Secure on %s", c.Name)
		}
	}
}

func TestSessionToken(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/x", http.NoBody)
	r.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess-xyz"})
	tok, ok := SessionToken(r)
	if !ok || tok != "sess-xyz" {
		t.Errorf("SessionToken() = (%q, %v), want (sess-xyz, true)", tok, ok)
	}

	r2 := httptest.NewRequest(http.MethodGet, "/api/x", http.NoBody)
	tok2, ok2 := SessionToken(r2)
	if ok2 || tok2 != "" {
		t.Errorf("SessionToken() with no cookie = (%q, %v), want (\"\", false)", tok2, ok2)
	}

	r3 := httptest.NewRequest(http.MethodGet, "/api/x", http.NoBody)
	r3.AddCookie(&http.Cookie{Name: SessionCookie, Value: ""})
	tok3, ok3 := SessionToken(r3)
	if ok3 || tok3 != "" {
		t.Errorf("SessionToken() with empty cookie value = (%q, %v), want (\"\", false)", tok3, ok3)
	}
}

func TestIsSafeMethod(t *testing.T) {
	safe := []string{http.MethodGet, http.MethodHead, http.MethodOptions}
	for _, m := range safe {
		if !IsSafeMethod(m) {
			t.Errorf("IsSafeMethod(%q) = false, want true", m)
		}
	}
	unsafe := []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}
	for _, m := range unsafe {
		if IsSafeMethod(m) {
			t.Errorf("IsSafeMethod(%q) = true, want false", m)
		}
	}
}

func TestSecure_Modes(t *testing.T) {
	httpReq := httptest.NewRequest(http.MethodGet, "http://x/api", http.NoBody)
	tlsReq := httptest.NewRequest(http.MethodGet, "https://x/api", http.NoBody)
	fwd := httptest.NewRequest(http.MethodGet, "http://x/api", http.NoBody)
	fwd.Header.Set("X-Forwarded-Proto", "https")

	if Secure(httpReq, "always") != true || Secure(tlsReq, "never") != false {
		t.Error("explicit modes must win")
	}
	if Secure(httpReq, "auto") != false {
		t.Error("auto over plain http must be false")
	}
	if Secure(tlsReq, "auto") != true || Secure(fwd, "auto") != true {
		t.Error("auto must detect TLS and X-Forwarded-Proto=https")
	}
}

func TestJarFrontDeskUsesOwnCookieNames(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := FrontDesk.SetSession(rec, "tok123", false, time.Hour); err != nil {
		t.Fatalf("SetSession: %v", err)
	}
	cookies := rec.Result().Cookies()
	var names []string
	for _, c := range cookies {
		names = append(names, c.Name)
	}
	want := map[string]bool{"fd_session": true, "fd_csrf": true}
	for _, n := range names {
		if !want[n] {
			t.Errorf("unexpected cookie %q (dashboard names must not leak into the Front Desk jar)", n)
		}
		delete(want, n)
	}
	if len(want) != 0 {
		t.Errorf("missing cookies: %v (got %v)", want, names)
	}
}

func TestJarSessionTokenReadsOwnNameOnly(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r.AddCookie(&http.Cookie{Name: "mh_session", Value: "dash"})
	if _, ok := FrontDesk.SessionToken(r); ok {
		t.Fatal("FrontDesk jar must not read the dashboard session cookie")
	}
	r.AddCookie(&http.Cookie{Name: "fd_session", Value: "fd"})
	tok, ok := FrontDesk.SessionToken(r)
	if !ok || tok != "fd" {
		t.Fatalf("FrontDesk.SessionToken = %q, %v; want fd, true", tok, ok)
	}
}

func TestPackageFuncsDelegateToDashboardJar(t *testing.T) {
	rec := httptest.NewRecorder()
	if err := SetSession(rec, "tok", false, time.Hour); err != nil {
		t.Fatalf("SetSession: %v", err)
	}
	found := false
	for _, c := range rec.Result().Cookies() {
		if c.Name == "mh_session" {
			found = true
		}
	}
	if !found {
		t.Fatal("package-level SetSession must keep writing mh_session")
	}
}

// RefreshSession re-issues both cookies with the new lifetime, keeping the
// session token and the CSRF value the browser already holds: a client that
// read the CSRF cookie at login must not find it swapped underneath it, and the
// two cookies must never expire on different schedules. The response is marked
// no-store so a shared cache never keeps a body that arrived with a cookie.
func TestRefreshSession_KeepsValuesMovesMaxAge(t *testing.T) {
	existing, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/x", http.NoBody)
	req.AddCookie(&http.Cookie{Name: FrontDesk.SessionCookie, Value: "sess-1"})
	req.AddCookie(&http.Cookie{Name: FrontDesk.CSRFCookie, Value: existing})
	rec := httptest.NewRecorder()
	if err := FrontDesk.RefreshSession(rec, req, "sess-1", true, 2*time.Hour); err != nil {
		t.Fatalf("RefreshSession: %v", err)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
	var sess, csrf *http.Cookie
	for _, c := range rec.Result().Cookies() {
		switch c.Name {
		case FrontDesk.SessionCookie:
			sess = c
		case FrontDesk.CSRFCookie:
			csrf = c
		}
	}
	if sess == nil || csrf == nil {
		t.Fatalf("expected both cookies re-issued, got %+v", rec.Result().Cookies())
	}
	if sess.Value != "sess-1" || csrf.Value != existing {
		t.Errorf("values changed: session=%q csrf=%q", sess.Value, csrf.Value)
	}
	if sess.MaxAge != 7200 || csrf.MaxAge != 7200 {
		t.Errorf("MaxAge = %d/%d, want 7200 on both", sess.MaxAge, csrf.MaxAge)
	}
	if !sess.HttpOnly || csrf.HttpOnly || !sess.Secure || !csrf.Secure ||
		sess.SameSite != http.SameSiteStrictMode || csrf.SameSite != http.SameSiteStrictMode {
		t.Error("refreshed cookies lost their hardening attributes")
	}
}

// The request's CSRF cookie is untrusted input: only a value shaped like one
// this package minted is echoed back. A missing cookie and a planted value of
// the wrong shape (an attacker's sibling-subdomain toss, say) both get a fresh
// token instead, so RefreshSession never promotes an arbitrary value into the
// canonical host-wide cookie.
func TestRefreshSession_MintsCSRFWhenMissingOrForeign(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string // "" = no CSRF cookie on the request
	}{
		{"missing", ""},
		{"wrong length", "planted"},
		{"right length wrong charset", "planted!value+with/bad=chars.......padded"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/x", http.NoBody)
			req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess-2"})
			if tc.value != "" {
				req.AddCookie(&http.Cookie{Name: CSRFCookie, Value: tc.value})
			}
			rec := httptest.NewRecorder()
			if err := Dashboard.RefreshSession(rec, req, "sess-2", false, time.Hour); err != nil {
				t.Fatalf("RefreshSession: %v", err)
			}
			for _, c := range rec.Result().Cookies() {
				if c.Name == CSRFCookie {
					if !isMintedToken(c.Value) || c.Value == tc.value {
						t.Errorf("csrf re-issued as %q, want a fresh minted token", c.Value)
					}
					return
				}
			}
			t.Error("no csrf cookie re-issued")
		})
	}
}

// A non-positive lifetime is not a refresh: MaxAge 0 would demote the pair to
// session cookies and a negative value would delete them, so nothing is written.
func TestRefreshSession_IgnoresNonPositiveMaxAge(t *testing.T) {
	for _, d := range []time.Duration{0, 500 * time.Millisecond, -time.Hour} {
		req := httptest.NewRequest(http.MethodGet, "/api/x", http.NoBody)
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess-3"})
		rec := httptest.NewRecorder()
		if err := Dashboard.RefreshSession(rec, req, "sess-3", false, d); err != nil {
			t.Fatalf("RefreshSession(%v): %v", d, err)
		}
		if got := rec.Result().Cookies(); len(got) != 0 {
			t.Errorf("maxAge %v wrote cookies: %+v", d, got)
		}
	}
}

func TestIsMintedToken(t *testing.T) {
	tok, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	if !isMintedToken(tok) {
		t.Errorf("a freshly minted token %q is not recognised", tok)
	}
	for _, bad := range []string{"", "short", tok + "x", tok[:42] + "=", tok[:42] + "/"} {
		if isMintedToken(bad) {
			t.Errorf("isMintedToken(%q) = true", bad)
		}
	}
}
