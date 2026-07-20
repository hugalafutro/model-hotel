// Package authcookie carries dashboard session auth over hardened cookies:
// an HttpOnly session cookie the browser cannot read, plus a readable CSRF
// cookie echoed back in a header for stateless double-submit verification.
package authcookie

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"net/http"
	"strings"
	"time"
)

// Cookie and header names used to carry dashboard session auth.
const (
	SessionCookie = "mh_session"
	CSRFCookie    = "mh_csrf"
	CSRFHeader    = "X-CSRF-Token"
)

// SetSession writes the session cookie (HttpOnly) and a fresh CSRF cookie
// (readable) with SameSite=Strict. secure toggles the Secure attribute so
// plain-http LAN deployments still work; callers decide via Secure().
func SetSession(w http.ResponseWriter, token string, secure bool, maxAge time.Duration) error {
	csrf, err := randomToken()
	if err != nil {
		return err
	}
	age := int(maxAge.Seconds())
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure/HttpOnly/SameSite are all set below via caller-controlled args, not omitted
		Name: SessionCookie, Value: token, Path: "/",
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: age,
	})
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // CSRF cookie is intentionally readable (HttpOnly: false) for double-submit; Secure/SameSite still set
		Name: CSRFCookie, Value: csrf, Path: "/",
		HttpOnly: false, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: age,
	})
	return nil
}

// ClearSession expires both cookies.
func ClearSession(w http.ResponseWriter, secure bool) {
	for _, name := range []string{SessionCookie, CSRFCookie} {
		http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure/HttpOnly/SameSite are all set below via caller-controlled args, not omitted
			Name: name, Value: "", Path: "/",
			HttpOnly: name == SessionCookie, Secure: secure,
			SameSite: http.SameSiteStrictMode, MaxAge: -1,
		})
	}
}

// SessionToken returns the session token from the cookie, if present.
func SessionToken(r *http.Request) (string, bool) {
	c, err := r.Cookie(SessionCookie)
	if err != nil || c.Value == "" {
		return "", false
	}
	return c.Value, true
}

// ValidCSRF reports whether the request carries a CSRF header equal to its
// CSRF cookie (constant-time). Callers apply this only to unsafe methods on
// cookie-authenticated requests.
func ValidCSRF(r *http.Request) bool {
	c, err := r.Cookie(CSRFCookie)
	if err != nil || c.Value == "" {
		return false
	}
	h := r.Header.Get(CSRFHeader)
	if h == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(h)) == 1
}

// IsSafeMethod reports whether the HTTP method is non-mutating (CSRF-exempt).
func IsSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

// Secure resolves the cookie Secure attribute. mode is "always", "never", or
// "auto" (default): auto is on for TLS or X-Forwarded-Proto=https.
func Secure(r *http.Request, mode string) bool {
	switch mode {
	case "always":
		return true
	case "never":
		return false
	default:
		if r.TLS != nil {
			return true
		}
		return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
	}
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
