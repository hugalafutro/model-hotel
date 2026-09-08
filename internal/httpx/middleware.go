package httpx

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/hugalafutro/model-hotel/internal/clientip"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// AccessLogger writes one line per request under "access: request", with the
// same field names in both binaries so one log parser reads either.
//
// isNoisy names the machine-to-machine traffic that drops to debug: health
// checks, liveness polls and the reads an open dashboard repeats on a timer.
// It is consulted only for successful requests, because a rejection is never
// noise. Successful static-asset fetches are dropped entirely.
//
// Mount it inside clientip.Middleware, which resolves the trusted-proxy-aware
// client address the line reports; behind a reverse proxy the TCP peer is the
// proxy and never the visitor.
func AccessLogger(isNoisy func(method, path string) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			start := time.Now()
			next.ServeHTTP(ww, r)
			duration := time.Since(start)

			status := ww.Status()
			if status == 0 {
				// A handler that wrote a body without calling WriteHeader
				// answered 200; the wrapper reports 0 until something is
				// written, and a handler that wrote nothing still answered 200.
				status = http.StatusOK
			}
			if isStaticAsset(r.URL.Path) && status < 400 {
				return
			}

			// The path goes last in every branch. It is caller-controlled, so a
			// reader scanning left to right meets every field the server
			// vouches for before it reaches anything a visitor wrote, and the
			// stdout text handler escapes the spaces inside it so it cannot
			// present a "key=value" token of its own.
			args := []any{
				"method", r.Method,
				"host", r.Host,
				"remote", clientip.From(r),
				"status", status,
				"bytes", ww.BytesWritten(),
				"duration", duration,
				"path", r.URL.Path,
			}
			switch {
			case status >= 500:
				debuglog.Error("access: request", args...)
			case status >= 400:
				debuglog.Warn("access: request", args...)
			case isNoisy != nil && isNoisy(r.Method, NormalizePath(r.URL.Path)):
				debuglog.Debug("access: request", args...)
			default:
				debuglog.Info("access: request", args...)
			}
		})
	}
}

// NormalizePath strips trailing slashes so an exact match against a noise
// allowlist cannot be defeated by a client or a reverse proxy appending one.
// Root "/" is preserved.
func NormalizePath(path string) string {
	if len(path) > 1 {
		return strings.TrimRight(path, "/")
	}
	return path
}

// isStaticAsset reports whether path serves an embedded SPA bundle. A
// successful asset fetch is not traffic anyone reads, and a page load pulls a
// dozen of them.
func isStaticAsset(path string) bool {
	return strings.HasPrefix(path, "/assets/") || strings.HasPrefix(path, "/favicon")
}

// SecurityHeaders sets the standard security headers on every response.
//
// allowEmbed drops X-Frame-Options and the CSP frame-ancestors directive so any
// origin can put the page in an iframe (workspace browsers, Home Assistant).
// HSTS is set only over TLS: plain HTTP behind a TLS-terminating proxy must not
// set it, or browsers cache a redirect to a listener that does not exist.
//
// Style 'unsafe-inline' is required for Vite's injected style tags (CSS
// animations and dynamic theme overrides). Script 'unsafe-inline' is not: Vite
// outputs module scripts, never inline ones.
func SecurityHeaders(allowEmbed bool) func(http.Handler) http.Handler {
	const baseCSP = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; "
	csp := baseCSP + "frame-ancestors 'none'; base-uri 'self'; form-action 'self'"
	if allowEmbed {
		csp = baseCSP + "base-uri 'self'; form-action 'self'"
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			if !allowEmbed {
				w.Header().Set("X-Frame-Options", "DENY")
			}
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			if r.TLS != nil {
				w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
			}
			w.Header().Set("Content-Security-Policy", csp)
			next.ServeHTTP(w, r)
		})
	}
}
