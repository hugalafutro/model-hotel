// Package authcookie carries session auth over hardened cookies for both the
// dashboard and Front Desk: an HttpOnly session cookie the browser cannot
// read, plus a readable CSRF cookie echoed back in a header for stateless
// double-submit verification.
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

// Jar names the cookie pair one app's session rides on. Each app gets its own
// names because cookies are host-scoped, not port-scoped: a dashboard and a
// Front Desk served from one hostname on different ports would otherwise
// overwrite each other's session cookie on every login.
type Jar struct {
	SessionCookie string
	CSRFCookie    string
}

// Dashboard is the main dashboard's cookie pair; FrontDesk is the Front Desk
// admin SPA's. The package-level functions operate on Dashboard.
var (
	Dashboard = Jar{SessionCookie: SessionCookie, CSRFCookie: CSRFCookie}
	FrontDesk = Jar{SessionCookie: "fd_session", CSRFCookie: "fd_csrf"}
)

// SetSession writes the session cookie (HttpOnly) and a fresh CSRF cookie
// (readable) with SameSite=Strict. secure toggles the Secure attribute so
// plain-http LAN deployments still work; callers decide via Secure().
func (j Jar) SetSession(w http.ResponseWriter, token string, secure bool, maxAge time.Duration) error {
	csrf, err := randomToken()
	if err != nil {
		return err
	}
	age := int(maxAge.Seconds())
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure/HttpOnly/SameSite are all set below via caller-controlled args, not omitted
		Name: j.SessionCookie, Value: token, Path: "/",
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: age,
	})
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // CSRF cookie is intentionally readable (HttpOnly: false) for double-submit; Secure/SameSite still set
		Name: j.CSRFCookie, Value: csrf, Path: "/",
		HttpOnly: false, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: age,
	})
	return nil
}

// RefreshSession re-issues the session cookie pair with a new MaxAge and the
// same values, for a session whose server-side expiry just slid forward: the
// browser enforces MaxAge on its own, so without this it would drop the cookie
// on the original schedule however alive the session is. The CSRF cookie keeps
// its value (a client may have read it already) and moves with the session
// cookie so an unsafe request never finds the session alive but the CSRF
// double-submit gone; a request carrying no CSRF cookie gets a fresh one.
func (j Jar) RefreshSession(w http.ResponseWriter, r *http.Request, token string, secure bool, maxAge time.Duration) error {
	csrf := ""
	if c, err := r.Cookie(j.CSRFCookie); err == nil && c.Value != "" {
		csrf = c.Value
	} else {
		fresh, err := randomToken()
		if err != nil {
			return err
		}
		csrf = fresh
	}
	age := int(maxAge.Seconds())
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure/HttpOnly/SameSite are all set below via caller-controlled args, not omitted
		Name: j.SessionCookie, Value: token, Path: "/",
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: age,
	})
	http.SetCookie(w, &http.Cookie{ //nolint:gosec // CSRF cookie is intentionally readable (HttpOnly: false) for double-submit; Secure/SameSite still set
		Name: j.CSRFCookie, Value: csrf, Path: "/",
		HttpOnly: false, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: age,
	})
	return nil
}

// ClearSession expires both cookies.
func (j Jar) ClearSession(w http.ResponseWriter, secure bool) {
	for _, name := range []string{j.SessionCookie, j.CSRFCookie} {
		http.SetCookie(w, &http.Cookie{ //nolint:gosec // Secure/HttpOnly/SameSite are all set below via caller-controlled args, not omitted
			Name: name, Value: "", Path: "/",
			HttpOnly: name == j.SessionCookie, Secure: secure,
			SameSite: http.SameSiteStrictMode, MaxAge: -1,
		})
	}
}

// SessionToken returns the session token from the cookie, if present.
func (j Jar) SessionToken(r *http.Request) (string, bool) {
	c, err := r.Cookie(j.SessionCookie)
	if err != nil || c.Value == "" {
		return "", false
	}
	return c.Value, true
}

// ValidCSRF reports whether the request carries a CSRF header equal to its
// CSRF cookie (constant-time). Callers apply this only to unsafe methods on
// cookie-authenticated requests.
func (j Jar) ValidCSRF(r *http.Request) bool {
	c, err := r.Cookie(j.CSRFCookie)
	if err != nil || c.Value == "" {
		return false
	}
	h := r.Header.Get(CSRFHeader)
	if h == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(c.Value), []byte(h)) == 1
}

// SetSession writes the dashboard session cookie pair. See Jar.SetSession.
func SetSession(w http.ResponseWriter, token string, secure bool, maxAge time.Duration) error {
	return Dashboard.SetSession(w, token, secure, maxAge)
}

// ClearSession expires the dashboard session cookie pair. See Jar.ClearSession.
func ClearSession(w http.ResponseWriter, secure bool) {
	Dashboard.ClearSession(w, secure)
}

// SessionToken returns the dashboard session token from the cookie, if present. See Jar.SessionToken.
func SessionToken(r *http.Request) (string, bool) {
	return Dashboard.SessionToken(r)
}

// ValidCSRF reports whether the request carries a valid dashboard CSRF header. See Jar.ValidCSRF.
func ValidCSRF(r *http.Request) bool {
	return Dashboard.ValidCSRF(r)
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
