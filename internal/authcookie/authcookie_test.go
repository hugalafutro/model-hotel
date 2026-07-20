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
