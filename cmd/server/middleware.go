package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/hugalafutro/model-hotel/internal/clientip"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// securityHeadersMiddleware sets the standard security headers on every
// response.
func securityHeadersMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			// When ALLOW_EMBED=true, X-Frame-Options and CSP frame-ancestors
			// are omitted entirely so any origin can embed the page in an
			// iframe (e.g. workspace browsers, Home Assistant).
			if !cfg.AllowEmbed {
				w.Header().Set("X-Frame-Options", "DENY")
			}
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			// HSTS only over TLS. Plain HTTP (e.g. behind a reverse proxy that
			// terminates TLS) must not set HSTS or browsers will cache a broken
			// redirect to a non-existent HTTPS listener. Currently the server
			// only serves plain HTTP (ListenAndServe), so this guard is a
			// forward-compatible placeholder: it will activate automatically if
			// TLS is added later via ListenAndServeTLS.
			if r.TLS != nil {
				w.Header().Set("Strict-Transport-Security", "max-age=63072000; includeSubDomains; preload")
			}
			// CSP allows same-origin scripts/styles (needed for embedded SPA).
			// Style 'unsafe-inline' is required for Vite's injected style tags (CSS-based
			// animations and dynamic theme overrides). Script 'unsafe-inline' is NOT
			// needed: Vite outputs module scripts, not inline ones.
			if cfg.AllowEmbed {
				w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; base-uri 'self'; form-action 'self'")
			} else {
				w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// corsMiddleware allows the configured origins (CORS_ORIGINS) and answers
// preflight requests.
func corsMiddleware(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			allowed := slices.Contains(cfg.CORSOrigins, origin)

			w.Header().Set("Vary", "Origin")

			if allowed {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Max-Age", "86400")
			}

			if r.Method == "OPTIONS" {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// maxRequestSizeMiddleware caps every request body at maxBytes.
func maxRequestSizeMiddleware(maxBytes int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// silentLogger is like chi's middleware.Logger but suppresses request log
// lines for high-frequency polling endpoints that would flood docker logs.
func silentLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		t1 := time.Now()
		next.ServeHTTP(ww, r)
		duration := time.Since(t1)

		path := r.URL.Path
		isStatic := strings.HasPrefix(path, "/assets/") || strings.HasPrefix(path, "/favicon")
		// Match the noise allowlist against a slash-normalized path so a trailing
		// slash (from a client or a reverse proxy) can't defeat an exact match and
		// leak the request back to Info. Root "/" is preserved.
		np := path
		if len(np) > 1 {
			np = strings.TrimRight(np, "/")
		}
		isNoisy := np == "/health" ||
			strings.HasPrefix(np, "/api/logs/app") ||
			(np == "/api/logs" && r.Method == "GET") ||
			(np == "/api/system" && r.Method == "GET") ||
			(np == "/api/events" && r.Method == "GET") ||
			(np == "/api/stats" && r.Method == "GET") ||
			(np == "/api/stats/timeseries" && r.Method == "GET") ||
			(np == "/api/stats/provider-distribution" && r.Method == "GET") ||
			(np == "/api/models" && r.Method == "GET") ||
			(np == "/api/providers" && r.Method == "GET") ||
			// Fleet heartbeat: Front Desk pings every member ~every 2.5s with an
			// announce POST and polls its version via GET /api/settings. Both are
			// machine-to-machine liveness traffic, not human activity, and at
			// ~24/min/member they otherwise flood app_logs (the App Logs page).
			np == "/api/fleet/announce" ||
			(np == "/api/settings" && r.Method == "GET")
		if isStatic && ww.Status() < 400 {
			return
		}
		// The path goes last in every branch below. It is caller-controlled, so
		// a log reader scanning left to right meets every field the gateway
		// vouches for before it reaches anything a visitor wrote. Values are
		// escaped too; see quoteLogValue in internal/api/applogs_slog.go.
		status := ww.Status()
		switch {
		case status >= 500:
			debuglog.Error("access: request",
				"method", r.Method,
				"host", r.Host,
				"remote", clientip.From(r),
				"status", status,
				"bytes", ww.BytesWritten(),
				"duration", duration,
				"path", r.URL.Path)
		case status >= 400:
			debuglog.Warn("access: request",
				"method", r.Method,
				"host", r.Host,
				"remote", clientip.From(r),
				"status", status,
				"bytes", ww.BytesWritten(),
				"duration", duration,
				"path", r.URL.Path)
		case isNoisy:
			debuglog.Debug("access: request",
				"method", r.Method,
				"host", r.Host,
				"remote", clientip.From(r),
				"status", status,
				"bytes", ww.BytesWritten(),
				"duration", duration,
				"path", r.URL.Path)
		default:
			debuglog.Info("access: request",
				"method", r.Method,
				"host", r.Host,
				"remote", clientip.From(r),
				"status", status,
				"bytes", ww.BytesWritten(),
				"duration", duration,
				"path", r.URL.Path)
		}
	})
}

// streamingAwareTimeout returns middleware that sets a request deadline only
// for non-streaming requests. Streaming LLM calls (e.g. code generation that
// runs for 10+ minutes) must not be killed by a short server-side timeout.
//
// It works by peeking at the request body to check the "stream" field:
//   - stream=true  → no context deadline (client disconnect detection still works)
//   - stream=false/absent → context deadline of maxNonStreamingDur
//
// The request body is stored in the context so downstream handlers can
// reuse it without a second allocation, and also restored as r.Body for
// any handler that reads it directly.
// isLongRunningPath reports whether the request targets a multimodal proxy
// endpoint whose legitimate latency exceeds the non-streaming deadline:
// image generation/edits and audio synthesis/transcription regularly take
// minutes. The proxy's per-attempt failover timeout and overall deadline
// still bound these requests.
func isLongRunningPath(path string) bool {
	return strings.HasPrefix(path, "/v1/images/") || strings.HasPrefix(path, "/v1/audio/")
}

func streamingAwareTimeout(maxNonStreamingDur time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only POST /v1/chat/completions carries a stream flag;
			// other routes (e.g. GET /v1/models) get the non-streaming timeout.
			if r.Method != http.MethodPost {
				ctx, cancel := context.WithTimeout(r.Context(), maxNonStreamingDur)
				defer cancel()
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			// Multipart uploads (audio transcription/translation, image
			// edits/variations) are never buffered here: the JSON peek cannot
			// apply (the model field lives in the form, parsed by the
			// handler), and megabytes of audio are better read once, by the
			// handler. These routes are long-running, so no deadline either.
			if strings.HasPrefix(strings.ToLower(r.Header.Get("Content-Type")), "multipart/") {
				if isLongRunningPath(r.URL.Path) {
					next.ServeHTTP(w, r)
					return
				}
				ctx, cancel := context.WithTimeout(r.Context(), maxNonStreamingDur)
				defer cancel()
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			parseStart := time.Now()
			body, err := io.ReadAll(r.Body)
			_ = r.Body.Close()
			if err != nil {
				util.WriteOpenAIError(w, "failed to read request body", http.StatusBadRequest)
				return
			}

			// Extract both stream and model in a single unmarshal so
			// downstream handlers can skip re-parsing cached bytes. The peek
			// runs regardless of Content-Type: clients send JSON chat bodies
			// with text/plain or form-urlencoded headers, and skipping them
			// would wrongly impose the non-streaming deadline on their streams.
			var parsed struct {
				Stream bool   `json:"stream"`
				Model  string `json:"model"`
			}
			isStreaming := false
			modelName := ""
			if json.Unmarshal(body, &parsed) == nil {
				isStreaming = parsed.Stream
				modelName = parsed.Model
			}
			parseMs := float64(time.Since(parseStart).Microseconds()) / 1000.0

			// Restore the body so downstream handlers can read it
			r.Body = io.NopCloser(bytes.NewReader(body))

			// Store body bytes + extracted fields + timing in context
			ctx := context.WithValue(r.Context(), ctxkeys.RequestBodyKey, body)
			ctx = context.WithValue(ctx, ctxkeys.RequestBodyParseMsKey, parseMs)
			ctx = context.WithValue(ctx, ctxkeys.RequestModelKey, modelName)
			ctx = context.WithValue(ctx, ctxkeys.IsStreamingKey, isStreaming)

			// Long-running multimodal routes (image generation, audio) get the
			// streaming treatment even without a body stream flag: their
			// legitimate latencies (image models, large transcriptions, SSE
			// synthesis) exceed the non-streaming deadline. The proxy's
			// per-attempt failover timeout still bounds each upstream call.
			if isStreaming || isLongRunningPath(r.URL.Path) {
				next.ServeHTTP(w, r.WithContext(ctx))
			} else {
				ctx, cancel := context.WithTimeout(ctx, maxNonStreamingDur)
				defer cancel()
				next.ServeHTTP(w, r.WithContext(ctx))
			}
		})
	}
}

// mountProxyRoutes mounts the OpenAI-compatible surface with the body-peeking
// timeout middleware placed by Register behind the virtual-key check, not
// ahead of it as a plain r.Use would. The peek buffers the whole body (up to
// MAX_REQUEST_SIZE) and used to run first, so an unauthenticated client made
// the gateway hold that allocation for the length of its upload before being
// told 401. Now the gateway never buffers an unauthenticated body; net/http
// still discards up to 256 KiB of it after the refusal, bounded by the body
// read deadline, so the connection itself is held no longer than that.
func mountProxyRoutes(r chi.Router, register func(chi.Router, ...func(http.Handler) http.Handler)) {
	register(r, streamingAwareTimeout(5*time.Minute))
}
