package frontdesk

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/hugalafutro/model-hotel/internal/clientip"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// accessLogger writes one line per request, in the same shape and with the same
// field names the gateway's own access log uses (silentLogger in
// cmd/server/middleware.go), so one log parser reads both binaries.
//
// Front Desk is the fleet's control plane and its whole API is admin-gated, so
// until this existed nothing reading its container output could tell that a
// stranger was knocking: a rejected request left no trace whatsoever.
//
// Mount it inside clientip.Middleware, which resolves the trusted-proxy-aware
// client address the line reports; in production Front Desk sits behind Traefik,
// so the TCP peer is the proxy and never the visitor.
func accessLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)
		duration := time.Since(start)

		status := ww.Status()
		if status == 0 {
			// A handler that wrote a body without calling WriteHeader answered
			// 200; the wrapper reports 0 until something is written, and a
			// handler that wrote nothing at all still answered 200.
			status = http.StatusOK
		}
		if isStaticAsset(r.URL.Path) && status < 400 {
			return
		}

		// The path goes last in every branch. It is caller-controlled, so a
		// reader scanning left to right meets every field Front Desk vouches
		// for before it reaches anything a visitor wrote, and the stdout text
		// handler escapes the spaces inside it (debuglog.StdoutHandler) so it
		// cannot present a "key=value" token of its own.
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
		case isPollingEndpoint(r.Method, r.URL.Path):
			debuglog.Debug("access: request", args...)
		default:
			debuglog.Info("access: request", args...)
		}
	})
}

// isStaticAsset reports whether path serves the embedded SPA bundle. A
// successful asset fetch is not traffic anyone reads, and the SPA pulls a
// dozen of them per page load.
func isStaticAsset(path string) bool {
	return strings.HasPrefix(path, "/assets/") || strings.HasPrefix(path, "/favicon")
}

// isPollingEndpoint reports whether the request is machine-to-machine liveness
// traffic rather than a person using Front Desk: the container HEALTHCHECK,
// Traefik's provider poll, a Prometheus scrape, the dashboard's own event
// stream, and the reads the open dashboard repeats on a timer.
//
// The timed reads matter more here than they do on the gateway. Front Desk's
// stdout handler has no stderr filter in front of it, so an info line really is
// written, and the SPA re-reads the member list and the device list every five
// seconds (frontdesk/web/src/hooks/useMembers.ts and
// frontdesk/web/src/components/PairedDevicesPanel.tsx),
// the Traffic page re-pulls every member's traffic on the same cadence, and the
// quota panel reads every minute. One idle tab would otherwise write a dozen
// lines a minute forever, which buries the rejections this log exists for. They
// are the SPA's and Bellhop's liveness reads, not a person doing anything.
//
// Only GET qualifies among the API reads: the same paths are mutating routes
// under another method, and a mutation is never noise.
//
// The comparison is against a slash-normalized path, so a trailing slash from a
// client or a reverse proxy cannot defeat an exact match and lift the endpoint
// back to info. Root "/" is preserved.
func isPollingEndpoint(method, path string) bool {
	np := path
	if len(np) > 1 {
		np = strings.TrimRight(np, "/")
	}
	if np == "/healthz" || np == "/traefik/config" || np == "/metrics" {
		return true
	}
	if method != http.MethodGet {
		return false
	}
	switch np {
	case "/api/sse", "/api/members", "/api/devices", "/api/quota":
		return true
	}
	// Per-member traffic, which the Traffic page pulls once per member per tick.
	return strings.HasPrefix(np, "/api/members/") && strings.HasSuffix(np, "/traffic")
}
