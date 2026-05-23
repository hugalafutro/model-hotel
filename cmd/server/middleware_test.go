package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
)

func TestStreamingAwareTimeout_StoresContextValues(t *testing.T) {
	var capturedBody []byte
	var capturedParseMs float64
	var capturedModel string
	var capturedIsStreaming bool

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v, ok := r.Context().Value(ctxkeys.RequestBodyKey).([]byte); ok {
			capturedBody = v
		}
		if v, ok := r.Context().Value(ctxkeys.RequestBodyParseMsKey).(float64); ok {
			capturedParseMs = v
		}
		if v, ok := r.Context().Value(ctxkeys.RequestModelKey).(string); ok {
			capturedModel = v
		}
		if v, ok := r.Context().Value(ctxkeys.IsStreamingKey).(bool); ok {
			capturedIsStreaming = v
		}
		w.WriteHeader(http.StatusOK)
	})

	middleware := streamingAwareTimeout(5 * time.Minute)
	wrapped := middleware(handler)

	body := []byte(`{"model":"gpt-4","stream":true}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if !bytes.Equal(capturedBody, body) {
		t.Errorf("RequestBodyKey: got %q, want %q", capturedBody, body)
	}
	if capturedParseMs <= 0 {
		t.Errorf("RequestBodyParseMsKey: got %v, want > 0", capturedParseMs)
	}
	if capturedModel != "gpt-4" {
		t.Errorf("RequestModelKey: got %q, want %q", capturedModel, "gpt-4")
	}
	if capturedIsStreaming != true {
		t.Errorf("IsStreamingKey: got %v, want true", capturedIsStreaming)
	}
}

func TestStreamingAwareTimeout_NonStreamingRequest(t *testing.T) {
	var capturedIsStreaming bool

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v, ok := r.Context().Value(ctxkeys.IsStreamingKey).(bool); ok {
			capturedIsStreaming = v
		}
		w.WriteHeader(http.StatusOK)
	})

	middleware := streamingAwareTimeout(5 * time.Minute)
	wrapped := middleware(handler)

	body := []byte(`{"model":"gpt-4","stream":false}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if capturedIsStreaming != false {
		t.Errorf("IsStreamingKey: got %v, want false", capturedIsStreaming)
	}
}

func TestStreamingAwareTimeout_NonPostSkipsParsing(t *testing.T) {
	var capturedBody interface{}
	var capturedParseMs interface{}
	var capturedModel interface{}
	var capturedIsStreaming interface{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBody = r.Context().Value(ctxkeys.RequestBodyKey)
		capturedParseMs = r.Context().Value(ctxkeys.RequestBodyParseMsKey)
		capturedModel = r.Context().Value(ctxkeys.RequestModelKey)
		capturedIsStreaming = r.Context().Value(ctxkeys.IsStreamingKey)
		w.WriteHeader(http.StatusOK)
	})

	middleware := streamingAwareTimeout(5 * time.Minute)
	wrapped := middleware(handler)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", http.NoBody)
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if capturedBody != nil {
		t.Errorf("RequestBodyKey should be nil for GET, got %v", capturedBody)
	}
	if capturedParseMs != nil {
		t.Errorf("RequestBodyParseMsKey should be nil for GET, got %v", capturedParseMs)
	}
	if capturedModel != nil {
		t.Errorf("RequestModelKey should be nil for GET, got %v", capturedModel)
	}
	if capturedIsStreaming != nil {
		t.Errorf("IsStreamingKey should be nil for GET, got %v", capturedIsStreaming)
	}
}

func TestStreamingAwareTimeout_RestoresBody(t *testing.T) {
	var readBody []byte

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		readBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read restored body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	})

	middleware := streamingAwareTimeout(5 * time.Minute)
	wrapped := middleware(handler)

	originalBody := []byte(`{"model":"gpt-4","stream":true}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(originalBody))
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if !bytes.Equal(readBody, originalBody) {
		t.Errorf("restored body: got %q, want %q", readBody, originalBody)
	}
}

func TestStreamingAwareTimeout_MalformedJSON(t *testing.T) {
	var capturedBody []byte
	var capturedModel string
	var capturedIsStreaming bool

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v, ok := r.Context().Value(ctxkeys.RequestBodyKey).([]byte); ok {
			capturedBody = v
		}
		if v, ok := r.Context().Value(ctxkeys.RequestModelKey).(string); ok {
			capturedModel = v
		}
		if v, ok := r.Context().Value(ctxkeys.IsStreamingKey).(bool); ok {
			capturedIsStreaming = v
		}
		w.WriteHeader(http.StatusOK)
	})

	middleware := streamingAwareTimeout(5 * time.Minute)
	wrapped := middleware(handler)

	// Invalid JSON body
	body := []byte(`{"model":"gpt-4",}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if !bytes.Equal(capturedBody, body) {
		t.Errorf("RequestBodyKey: got %q, want %q", capturedBody, body)
	}
	if capturedModel != "" {
		t.Errorf("RequestModelKey should be empty for malformed JSON, got %q", capturedModel)
	}
	if capturedIsStreaming != false {
		t.Errorf("IsStreamingKey should be false for malformed JSON, got %v", capturedIsStreaming)
	}
}

// recordHandler implements slog.Handler to capture log records for testing
type recordHandler struct {
	mu      *sync.Mutex
	records *[]slog.Record
}

func (h *recordHandler) Enabled(_ context.Context, _ slog.Level) bool {
	return true
}

func (h *recordHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	*h.records = append(*h.records, r.Clone())
	return nil
}

func (h *recordHandler) WithAttrs(_ []slog.Attr) slog.Handler {
	return h
}

func (h *recordHandler) WithGroup(_ string) slog.Handler {
	return h
}

func TestSilentLogger_NoisyEndpointsAtDebugLevel(t *testing.T) {
	// Capture slog output
	var mu sync.Mutex
	var records []slog.Record
	origDefault := slog.Default()
	defer slog.SetDefault(origDefault)

	impl := &recordHandler{mu: &mu, records: &records}
	slog.SetDefault(slog.New(impl))

	handler := silentLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Request to noisy endpoint
	req := httptest.NewRequest(http.MethodGet, "/api/logs/app/cursor", http.NoBody)
	req.Host = "test"
	handler.ServeHTTP(httptest.NewRecorder(), req)

	// Request to normal endpoint (not in noisy list)
	req2 := httptest.NewRequest(http.MethodGet, "/api/settings", http.NoBody)
	req2.Host = "test"
	handler.ServeHTTP(httptest.NewRecorder(), req2)

	mu.Lock()
	defer mu.Unlock()
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	// First record (noisy endpoint) should be at Debug level
	if records[0].Level != slog.LevelDebug {
		t.Errorf("noisy endpoint: expected Debug level, got %v", records[0].Level)
	}
	// Second record (normal endpoint) should be at Info level
	if records[1].Level != slog.LevelInfo {
		t.Errorf("normal endpoint: expected Info level, got %v", records[1].Level)
	}
}
