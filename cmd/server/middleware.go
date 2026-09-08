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

	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/util"
)

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

// isNoisyGatewayPath names the traffic whose access-log line drops to debug:
// health checks, the fleet heartbeat, and the reads an open dashboard repeats
// on a timer. Front Desk pings every member with an announce POST roughly every
// 2.5s and polls its version via GET /api/settings, machine-to-machine liveness
// traffic that at ~24/min/member would otherwise flood app_logs (the App Logs
// page). A settings mutation is a real admin action, so only the GET is demoted.
//
// path arrives slash-normalized from httpx.AccessLogger, so a trailing slash
// from a client or a reverse proxy cannot defeat an exact match.
func isNoisyGatewayPath(method, path string) bool {
	if path == "/health" || path == "/api/fleet/announce" || strings.HasPrefix(path, "/api/logs/app") {
		return true
	}
	if method != http.MethodGet {
		return false
	}
	switch path {
	case "/api/logs", "/api/system", "/api/events", "/api/stats", "/api/stats/timeseries",
		"/api/stats/provider-distribution", "/api/models", "/api/providers", "/api/settings":
		return true
	}
	return false
}

// isLongRunningPath reports whether the request targets a multimodal proxy
// endpoint whose legitimate latency exceeds the non-streaming deadline:
// image generation/edits and audio synthesis/transcription regularly take
// minutes. The proxy's per-attempt failover timeout and overall deadline
// still bound these requests.
func isLongRunningPath(path string) bool {
	return strings.HasPrefix(path, "/v1/images/") || strings.HasPrefix(path, "/v1/audio/")
}

// streamingAwareTimeout returns middleware that sets a request deadline only
// for non-streaming requests. Streaming LLM calls (code generation that runs
// for 10+ minutes) must not be killed by a short server-side timeout.
//
// It peeks at the request body to read the "stream" field:
//   - stream=true: no context deadline (client disconnect detection still works)
//   - stream=false or absent: context deadline of maxNonStreamingDur
//
// The request body is stored in the context so downstream handlers can reuse it
// without a second allocation, and also restored as r.Body for any handler that
// reads it directly.
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
			parseMs := util.MillisSince(parseStart)

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
// timeout middleware placed by Register behind the virtual-key check, not ahead
// of it as a plain r.Use would. The peek buffers the whole body (up to
// MAX_REQUEST_SIZE), so running it first would make the gateway hold that
// allocation for an unauthenticated client's whole upload before answering 401.
// Behind the check no unauthenticated body is buffered; net/http still discards
// up to 256 KiB of it after the refusal, bounded by the body read deadline.
func mountProxyRoutes(r chi.Router, register func(chi.Router, ...func(http.Handler) http.Handler)) {
	register(r, streamingAwareTimeout(5*time.Minute))
}
