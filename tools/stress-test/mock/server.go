package mock

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync/atomic"
	"time"
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
	ChunkCount     int           // number of SSE data chunks (default 15)
	ChunkDelay     time.Duration // delay between chunks (default 20ms)
	TokensPerChunk int           // completion tokens per chunk (default 3)
	InitialDelay   time.Duration // delay before first chunk (simulates TTFT)
	ErrorRate      float64       // 0.0–1.0, probability of returning 500

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
		fmt.Fprint(w, "OK")
	})
	// Models endpoint — returns a single mock model so that the proxy's
	// discovery can register it and provider/model routing works.
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
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

// Start launches the mock server in the foreground. For tests, use StartAsync.
func (s *Server) Start() error {
	s.server = &http.Server{Addr: s.addr, Handler: s.newHandler()}
	if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("mock server failed: %w", err)
	}
	close(s.ready)
	return nil
}

// StartAsync starts the server in a goroutine and signals readiness.
func (s *Server) StartAsync() error {
	go func() {
		s.server = &http.Server{Addr: s.addr, Handler: s.newHandler()}
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("[mock] server error: %v", err)
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
		s.server.Close()
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
		fmt.Fprintf(w, `{"error":{"message":"mock internal error","type":"server_error"}}`)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	r.Body.Close()

	// Parse into a raw map so we can check for rejected params,
	// then extract the fields we need.
	var raw map[string]interface{}
	json.Unmarshal(body, &raw)

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
				json.NewEncoder(w).Encode(map[string]interface{}{
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
	json.Unmarshal(body, &req)

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
		fmt.Fprintf(w, "data: %s\n\n", data)
		if canFlush {
			flusher.Flush()
		}

		// Inter-chunk delay (simulates token generation latency)
		if s.ChunkDelay > 0 && i < s.ChunkCount-1 {
			time.Sleep(s.ChunkDelay)
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
	fmt.Fprintf(w, "data: %s\n\n", data)
	if canFlush {
		flusher.Flush()
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
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
	json.NewEncoder(w).Encode(resp)
}

func init() {
	// Suppress the default log prefix for the mock server.
	log.SetFlags(log.Ltime | log.Lmicroseconds)
}
