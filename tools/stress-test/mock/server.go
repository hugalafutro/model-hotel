package mock

import (
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// Server is a mock OpenAI-compatible upstream that returns realistic SSE
// streaming responses without hitting any real provider.
type Server struct {
	addr        string
	server      *http.Server
	ready       chan struct{}
	totalServed atomic.Int64
	totalFailed atomic.Int64

	// Configurable behaviour
	ChunkCount     int           // number of SSE chunks (default 15)
	ChunkDelay     time.Duration // delay between chunks (default 20ms), ignored when StreamDurationMin > 0
	TokensPerChunk int           // completion tokens per chunk (default 3)
	InitialDelay   time.Duration // delay before first chunk (simulates TTFT)
	ErrorRate      float64       // 0.0–1.0, probability of returning 500

	// StreamDurationMin/Max set a per-request random stream duration range.
	// When StreamDurationMin > 0, each request picks a random duration in
	// [Min, Max] and computes chunk delay = duration / ChunkCount, overriding
	// the fixed ChunkDelay. This produces sustained, varied streams.
	StreamDurationMin time.Duration
	StreamDurationMax time.Duration

	// RejectParams lists request body param names that trigger a 400 error
	// with a provider-style rejection message. This exercises the proxy's
	// parseProviderParamError auto-retry path.
	RejectParams []string
}

// NewServer creates a mock upstream listening on addr (e.g. ":9090").
func NewServer(addr string) *Server {
	return &Server{
		addr:           addr,
		ready:          make(chan struct{}),
		ChunkCount:     15,
		ChunkDelay:     20 * time.Millisecond,
		TokensPerChunk: 3,
		InitialDelay:   10 * time.Millisecond,
		ErrorRate:      0,
	}
}

func (s *Server) newHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", s.handleCompletions)
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, "OK")
	})
	// Models endpoint — returns a single mock model so that the proxy's
	// discovery can register it and provider/model routing works.
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"object": "list",
			"data": []map[string]interface{}{
				{
					"id":       "mock-model",
					"object":   "model",
					"created":  time.Now().Unix(),
					"owned_by": "stress-mock",
				},
			},
		})
	})
	return mux
}

// newHTTPServer creates a tuned http.Server for high-concurrency streaming.
// The default http.Server has no explicit timeouts, which is fine for SSE
// (WriteTimeout covers the entire write phase, so it must be 0 for long streams).
// We set ReadTimeout to prevent slowloris attacks and IdleTimeout to keep
// connections alive between requests in the same pool.
func (s *Server) newHTTPServer() *http.Server {
	return &http.Server{
		Addr:           s.addr,
		Handler:        s.newHandler(),
		ReadTimeout:    30 * time.Second,
		WriteTimeout:   0, // no limit — SSE streams can last seconds to minutes
		IdleTimeout:    120 * time.Second,
		MaxHeaderBytes: 1 << 20, // 1MB
	}
}

// Start launches the mock server in the foreground. For tests, use StartAsync.
func (s *Server) Start() error {
	s.server = s.newHTTPServer()
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("mock server failed: %w", err)
	}
	close(s.ready)
	return nil
}

// StartAsync starts the server in a goroutine and signals readiness.
func (s *Server) StartAsync() error {
	go func() {
		s.server = s.newHTTPServer()
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			debuglog.Error("mock: server error", "error", err)
		}
	}()

	// Give the server a moment to bind.
	time.Sleep(100 * time.Millisecond)
	close(s.ready)
	return nil
}

// Stop shuts down the mock server.
func (s *Server) Stop() {
	if s.server != nil {
		_ = s.server.Close()
	}
}

// Ready returns a channel that is closed once the server has started.
func (s *Server) Ready() <-chan struct{} {
	return s.ready
}

// Stats returns (totalServed, totalFailed).
func (s *Server) Stats() (int64, int64) {
	return s.totalServed.Load(), s.totalFailed.Load()
}

// URL returns the base URL of the mock server (includes /v1 to match
// OpenAI provider base URL convention used by the proxy).
func (s *Server) URL() string {
	return "http://localhost" + s.addr + "/v1"
}

func (s *Server) handleCompletions(w http.ResponseWriter, r *http.Request) {
	// Inject error if configured
	if s.ErrorRate > 0 && rand.Float64() < s.ErrorRate {
		s.totalFailed.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = fmt.Fprintf(w, `{"error":{"message":"mock internal error","type":"server_error"}}`)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	_ = r.Body.Close()

	// Parse into a raw map so we can check for rejected params,
	// then extract the fields we need.
	var raw map[string]interface{}
	_ = json.Unmarshal(body, &raw)

	// Check for params that this mock provider rejects (simulates providers
	// like Anthropic that reject top_p, or Gemini that rejects frequency_penalty).
	if len(s.RejectParams) > 0 {
		for _, p := range s.RejectParams {
			if _, exists := raw[p]; exists {
				s.totalFailed.Add(1)
				// Format matches OpenAI-style error that parseProviderParamError parses.
				msg := fmt.Sprintf("`%s` is not supported by this model", p)
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusBadRequest)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"error": map[string]string{"message": msg, "type": "invalid_request_error"},
				})
				return
			}
		}
	}

	var req struct {
		Stream bool   `json:"stream"`
		Model  string `json:"model"`
	}
	_ = json.Unmarshal(body, &req)

	// Simulate initial processing delay (provider TTFT contribution)
	if s.InitialDelay > 0 {
		time.Sleep(s.InitialDelay)
	}

	if req.Stream {
		s.handleStreaming(w, r, req.Model)
	} else {
		s.handleNonStreaming(w, r, req.Model)
	}

	s.totalServed.Add(1)
}

// chunkDelayForRequest returns the inter-chunk delay for this request.
// When StreamDurationMin > 0, it picks a random duration in [Min, Max]
// and divides by ChunkCount. Otherwise falls back to the fixed ChunkDelay.
func (s *Server) chunkDelayForRequest() time.Duration {
	if s.StreamDurationMin > 0 {
		maxD := s.StreamDurationMax
		if maxD < s.StreamDurationMin {
			maxD = s.StreamDurationMin
		}
		duration := s.StreamDurationMin + time.Duration(rand.Int63n(int64(maxD-s.StreamDurationMin)+1))
		if s.ChunkCount > 0 {
			return duration / time.Duration(s.ChunkCount)
		}
		return duration
	}
	return s.ChunkDelay
}

func (s *Server) handleStreaming(w http.ResponseWriter, r *http.Request, model string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	flusher, canFlush := w.(http.Flusher)
	completionID := fmt.Sprintf("chatcmpl-mock-%d", rand.Int63())
	created := time.Now().Unix()

	totalCompletionTokens := 0
	words := []string{"The ", "mock ", "server ", "is ", "responding ", "with ", "synthetic ", "streaming ", "data ", "for ",
		"stress ", "testing ", "the ", "LLM ", "proxy ", "gateway ", "under ", "high ", "concurrency ", "load. "}

	chunkDelay := s.chunkDelayForRequest()

	for i := 0; i < s.ChunkCount; i++ {
		// Pick content for this chunk
		content := ""
		for j := 0; j < s.TokensPerChunk; j++ {
			content += words[(i*s.TokensPerChunk+j)%len(words)]
		}
		totalCompletionTokens += s.TokensPerChunk

		chunk := map[string]interface{}{
			"id":      completionID,
			"object":  "chat.completion.chunk",
			"created": created,
			"model":   model,
			"choices": []map[string]interface{}{
				{
					"index":         0,
					"delta":         map[string]string{"content": content},
					"finish_reason": nil,
				},
			},
		}

		data, _ := json.Marshal(chunk)
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		if canFlush {
			flusher.Flush()
		}

		// Inter-chunk delay (simulates token generation latency)
		if chunkDelay > 0 && i < s.ChunkCount-1 {
			time.Sleep(chunkDelay)
		}
	}

	// Final chunk with finish_reason and usage
	finalChunk := map[string]interface{}{
		"id":      completionID,
		"object":  "chat.completion.chunk",
		"created": created,
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"delta":         map[string]string{},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     10,
			"completion_tokens": totalCompletionTokens,
			"total_tokens":      10 + totalCompletionTokens,
		},
	}
	data, _ := json.Marshal(finalChunk)
	_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
	if canFlush {
		flusher.Flush()
	}

	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	if canFlush {
		flusher.Flush()
	}
}

func (s *Server) handleNonStreaming(w http.ResponseWriter, r *http.Request, model string) {
	totalCompletionTokens := s.ChunkCount * s.TokensPerChunk

	// Build a complete response
	content := ""
	words := strings.Split("The mock server is responding with synthetic data for stress testing the Model Hotel gateway under high concurrency load.", " ")
	for i := 0; i < len(words) && i < totalCompletionTokens; i++ {
		if i > 0 {
			content += " "
		}
		content += words[i]
	}

	resp := map[string]interface{}{
		"id":      fmt.Sprintf("chatcmpl-mock-%d", rand.Int63()),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]string{
					"role":    "assistant",
					"content": content,
				},
				"finish_reason": "stop",
			},
		},
		"usage": map[string]int{
			"prompt_tokens":     10,
			"completion_tokens": totalCompletionTokens,
			"total_tokens":      10 + totalCompletionTokens,
		},
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
