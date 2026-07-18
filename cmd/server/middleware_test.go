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
	var capturedBody any
	var capturedParseMs any
	var capturedModel any
	var capturedIsStreaming any

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

func TestStreamingAwareTimeout_MultipartNotBuffered(t *testing.T) {
	var capturedBodyVal any
	var readBody []byte
	var hadDeadline bool

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBodyVal = r.Context().Value(ctxkeys.RequestBodyKey)
		readBody, _ = io.ReadAll(r.Body)
		_, hadDeadline = r.Context().Deadline()
		w.WriteHeader(http.StatusOK)
	})

	middleware := streamingAwareTimeout(5 * time.Minute)
	wrapped := middleware(handler)

	// Multipart uploads must NOT be buffered into the context (pre-auth
	// memory amplification) and long-running audio routes get no deadline;
	// the raw body must still reach the handler untouched.
	body := []byte("--xyz\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\nwhisper-1\r\n--xyz--\r\n")
	req := httptest.NewRequest(http.MethodPost, "/v1/audio/transcriptions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xyz")
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if capturedBodyVal != nil {
		t.Errorf("RequestBodyKey should be nil for multipart (no pre-auth buffering), got %v", capturedBodyVal)
	}
	if !bytes.Equal(readBody, body) {
		t.Errorf("handler must receive the raw body: got %q, want %q", readBody, body)
	}
	if hadDeadline {
		t.Error("long-running multipart route should have no context deadline")
	}
}

func TestStreamingAwareTimeout_MultipartOnShortRouteGetsDeadline(t *testing.T) {
	var hadDeadline bool

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadDeadline = r.Context().Deadline()
		w.WriteHeader(http.StatusOK)
	})

	middleware := streamingAwareTimeout(5 * time.Minute)
	wrapped := middleware(handler)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader([]byte("--xyz--")))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=xyz")
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if !hadDeadline {
		t.Error("multipart on a non-long-running route should get the non-streaming deadline")
	}
}

func TestStreamingAwareTimeout_TextPlainJSONBodyStillPeeked(t *testing.T) {
	var capturedIsStreaming bool
	var hadDeadline bool

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v, ok := r.Context().Value(ctxkeys.IsStreamingKey).(bool); ok {
			capturedIsStreaming = v
		}
		_, hadDeadline = r.Context().Deadline()
		w.WriteHeader(http.StatusOK)
	})

	middleware := streamingAwareTimeout(5 * time.Minute)
	wrapped := middleware(handler)

	// Clients send JSON chat bodies with text/plain or form-urlencoded
	// headers; the peek must still detect stream:true so their long
	// streams are not killed by the non-streaming deadline.
	body := []byte(`{"model":"gpt-4","stream":true}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Content-Type", "text/plain;charset=UTF-8")
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if !capturedIsStreaming {
		t.Error("IsStreamingKey should be true for JSON body regardless of Content-Type")
	}
	if hadDeadline {
		t.Error("streaming request should have no context deadline")
	}
}

func TestStreamingAwareTimeout_LongRunningPathNoDeadline(t *testing.T) {
	var hadDeadline bool

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, hadDeadline = r.Context().Deadline()
		w.WriteHeader(http.StatusOK)
	})

	middleware := streamingAwareTimeout(5 * time.Minute)
	wrapped := middleware(handler)

	// Image generation takes minutes without any stream flag in the body;
	// the long-running route exemption must lift the 5-minute deadline.
	body := []byte(`{"model":"prov/gpt-image-1","prompt":"a cat"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if hadDeadline {
		t.Error("long-running image route should have no context deadline")
	}
}

func TestStreamingAwareTimeout_ExplicitJSONContentTypePeeks(t *testing.T) {
	var capturedModel string

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if v, ok := r.Context().Value(ctxkeys.RequestModelKey).(string); ok {
			capturedModel = v
		}
		w.WriteHeader(http.StatusOK)
	})

	middleware := streamingAwareTimeout(5 * time.Minute)
	wrapped := middleware(handler)

	body := []byte(`{"model":"text-embedding-3-small","input":"hi"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/embeddings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	rr := httptest.NewRecorder()

	wrapped.ServeHTTP(rr, req)

	if capturedModel != "text-embedding-3-small" {
		t.Errorf("RequestModelKey: got %q, want text-embedding-3-small", capturedModel)
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

	// Request to normal endpoint (not in noisy list). A settings *mutation*
	// is a real admin action: only GET /api/settings is demoted (the fleet
	// version poll), so POST stays at Info.
	req2 := httptest.NewRequest(http.MethodPost, "/api/settings", http.NoBody)
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

// ---------------------------------------------------------------------------
// silentLogger additional coverage tests
// ---------------------------------------------------------------------------

func TestSilentLogger_ServerErrorLogLevel(t *testing.T) {
	var mu sync.Mutex
	var records []slog.Record
	origDefault := slog.Default()
	defer slog.SetDefault(origDefault)

	impl := &recordHandler{mu: &mu, records: &records}
	slog.SetDefault(slog.New(impl))

	handler := silentLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/providers", http.NoBody)
	req.Host = "test"
	handler.ServeHTTP(httptest.NewRecorder(), req)

	mu.Lock()
	defer mu.Unlock()
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Level != slog.LevelError {
		t.Errorf("500 response: expected Error level, got %v", records[0].Level)
	}
}

func TestSilentLogger_ClientErrorLogLevel(t *testing.T) {
	var mu sync.Mutex
	var records []slog.Record
	origDefault := slog.Default()
	defer slog.SetDefault(origDefault)

	impl := &recordHandler{mu: &mu, records: &records}
	slog.SetDefault(slog.New(impl))

	handler := silentLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/providers", http.NoBody)
	req.Host = "test"
	handler.ServeHTTP(httptest.NewRecorder(), req)

	mu.Lock()
	defer mu.Unlock()
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Level != slog.LevelWarn {
		t.Errorf("404 response: expected Warn level, got %v", records[0].Level)
	}
}

func TestSilentLogger_StaticAssetsSuppressed(t *testing.T) {
	var mu sync.Mutex
	var records []slog.Record
	origDefault := slog.Default()
	defer slog.SetDefault(origDefault)

	impl := &recordHandler{mu: &mu, records: &records}
	slog.SetDefault(slog.New(impl))

	handler := silentLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/assets/main.js", http.NoBody)
	req.Host = "test"
	handler.ServeHTTP(httptest.NewRecorder(), req)

	req2 := httptest.NewRequest(http.MethodGet, "/favicon.ico", http.NoBody)
	req2.Host = "test"
	handler.ServeHTTP(httptest.NewRecorder(), req2)

	mu.Lock()
	defer mu.Unlock()
	if len(records) != 0 {
		t.Errorf("expected 0 records for static assets with 200 status, got %d", len(records))
	}
}

func TestSilentLogger_StaticAssetWithErrorCodeNotSuppressed(t *testing.T) {
	var mu sync.Mutex
	var records []slog.Record
	origDefault := slog.Default()
	defer slog.SetDefault(origDefault)

	impl := &recordHandler{mu: &mu, records: &records}
	slog.SetDefault(slog.New(impl))

	handler := silentLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))

	req := httptest.NewRequest(http.MethodGet, "/assets/missing.js", http.NoBody)
	req.Host = "test"
	handler.ServeHTTP(httptest.NewRecorder(), req)

	mu.Lock()
	defer mu.Unlock()
	if len(records) != 1 {
		t.Fatalf("expected 1 record for static asset with 404 status, got %d", len(records))
	}
	// 404 on static assets should log at Warn level (status >= 400)
	if records[0].Level != slog.LevelWarn {
		t.Errorf("static asset 404: expected Warn level, got %v", records[0].Level)
	}
}

func TestSilentLogger_NoisyEndpoints(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		method string
	}{
		{"health endpoint", "/health", "GET"},
		{"app logs endpoint", "/api/logs/app/cursor", "GET"},
		{"api logs GET", "/api/logs", "GET"},
		{"api system GET", "/api/system", "GET"},
		{"api events GET", "/api/events", "GET"},
		{"api stats GET", "/api/stats", "GET"},
		{"api stats timeseries GET", "/api/stats/timeseries", "GET"},
		{"api stats provider-distribution GET", "/api/stats/provider-distribution", "GET"},
		{"api models GET", "/api/models", "GET"},
		{"api providers GET", "/api/providers", "GET"},
		{"fleet announce POST", "/api/fleet/announce", "POST"},
		{"api settings GET (fleet version poll)", "/api/settings", "GET"},
		// Trailing slashes must not defeat the exact-path noise match.
		{"fleet announce POST trailing slash", "/api/fleet/announce/", "POST"},
		{"api settings GET trailing slash", "/api/settings/", "GET"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var records []slog.Record
			origDefault := slog.Default()
			defer slog.SetDefault(origDefault)

			impl := &recordHandler{mu: &mu, records: &records}
			slog.SetDefault(slog.New(impl))

			handler := silentLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}))

			req := httptest.NewRequest(tc.method, tc.path, http.NoBody)
			req.Host = "test"
			handler.ServeHTTP(httptest.NewRecorder(), req)

			mu.Lock()
			defer mu.Unlock()
			if len(records) != 1 {
				t.Fatalf("expected 1 record, got %d", len(records))
			}
			if records[0].Level != slog.LevelDebug {
				t.Errorf("noisy endpoint %s: expected Debug level, got %v", tc.path, records[0].Level)
			}
		})
	}
}

func TestSilentLogger_LogsNonGETNoisyEndpointAtInfo(t *testing.T) {
	// Non-GET requests to noisy paths should still be logged at Info (not Debug)
	// because the isNoisy check requires specific method + path combinations
	var mu sync.Mutex
	var records []slog.Record
	origDefault := slog.Default()
	defer slog.SetDefault(origDefault)

	impl := &recordHandler{mu: &mu, records: &records}
	slog.SetDefault(slog.New(impl))

	handler := silentLogger(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// POST /api/models is NOT in the noisy list (only GET is)
	req := httptest.NewRequest(http.MethodPost, "/api/models", http.NoBody)
	req.Host = "test"
	handler.ServeHTTP(httptest.NewRecorder(), req)

	mu.Lock()
	defer mu.Unlock()
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Level != slog.LevelInfo {
		t.Errorf("POST /api/models: expected Info level (not noisy for POST), got %v", records[0].Level)
	}
}
