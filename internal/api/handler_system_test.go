package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/util"
)

func TestGetSystem(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)
	h.SetDockerStatsCollector(func(filter util.ContainerFilter) util.AggregatedDockerStats {
		return util.AggregatedDockerStats{}
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/system", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if len(response) == 0 {
		t.Error("Expected system info in response")
	}
}

// Settings Tests

func TestStreamEvents(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.RegisterEvents(r) // Use RegisterEvents instead of Register for SSE endpoint

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/events", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	// Use a context with cancellation to avoid hanging
	ctx, cancel := context.WithCancel(req.Context())
	req = req.WithContext(ctx)

	// Start the request in a goroutine so we can cancel it
	done := make(chan bool)
	go func() {
		r.ServeHTTP(rec, req)
		done <- true
	}()

	// Give it a moment to start
	time.Sleep(100 * time.Millisecond)

	// Cancel the request to close the SSE connection
	cancel()

	// Wait for the handler to finish
	<-done

	// Should return 200 and start streaming
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 for events stream, got %d: %s", rec.Code, rec.Body.String())
	}

	// Check that content type is event-stream
	contentType := rec.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Expected Content-Type 'text/event-stream', got %s", contentType)
	}

	// Check that initial comment is present
	body := rec.Body.String()
	if !strings.Contains(body, ": connected") {
		t.Error("Expected initial connection comment in stream")
	}
}

// Logs Handler Tests

func TestGetSystem_NoCache(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("GET", "/system", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var response map[string]any
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	// Verify response structure
	if response["app"] == nil {
		t.Error("expected 'app' in response")
	}
	if response["db"] == nil {
		t.Error("expected 'db' in response")
	}
	if response["docker"] == nil {
		t.Error("expected 'docker' in response")
	}
}

// TestGetAppLogs_EmptyResult tests the app logs endpoint with no logs

func TestStreamEvents_Connected(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.RegisterEvents(r) // Use RegisterEvents for SSE endpoint

	// Create a request to the events endpoint
	req := httptest.NewRequest("GET", "/events", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	// Use custom ResponseWriter that implements http.Flusher
	fw := &flushingResponseWriter{ResponseRecorder: httptest.NewRecorder()}

	// Create a context with timeout to avoid hanging
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	r.ServeHTTP(fw, req)

	// Verify Content-Type is set correctly
	contentType := fw.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("expected Content-Type 'text/event-stream', got '%s'", contentType)
	}

	// Verify initial connection comment is present
	body := fw.Body.String()
	if !strings.Contains(body, ": connected") {
		t.Errorf("expected ': connected' in response, got: %s", body)
	}
}

// TestUpdateModel_EnableDisable_Integration tests enabling and disabling a model

func TestGetSystem_Details(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/system", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Check top-level sections exist
	for _, section := range []string{"app", "db", "docker"} {
		if _, ok := response[section]; !ok {
			t.Errorf("Expected section '%s' in system response", section)
		}
	}

	// Check app section has expected fields
	if app, ok := response["app"].(map[string]any); ok {
		for _, field := range []string{"uptime_seconds", "goroutines", "memory_current_bytes"} {
			if _, exists := app[field]; !exists {
				t.Errorf("Expected field 'app.%s' in system response", field)
			}
		}
	}

	// Check db section has expected fields
	if db, ok := response["db"].(map[string]any); ok {
		for _, field := range []string{"connections"} {
			if _, exists := db[field]; !exists {
				t.Errorf("Expected field 'db.%s' in system response", field)
			}
		}
	}
}

// TestDeleteModel_WithFailoverGroup tests that deleting a model in a failover group cascades

func TestStreamEvents_InitialConnection(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.RegisterEvents(r)

	// Create request with admin auth
	req := httptest.NewRequest("GET", "/events", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	// Use flushing response writer
	fw := &flushingResponseWriter{ResponseRecorder: httptest.NewRecorder()}

	// Use context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	r.ServeHTTP(fw, req)

	// Verify status code
	if fw.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", fw.Code, fw.Body.String())
	}

	// Verify Content-Type
	contentType := fw.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Expected Content-Type 'text/event-stream', got '%s'", contentType)
	}

	// Verify Cache-Control header for SSE
	cacheControl := fw.Header().Get("Cache-Control")
	if cacheControl != "no-cache" {
		t.Errorf("Expected Cache-Control 'no-cache', got '%s'", cacheControl)
	}

	// Verify Connection header
	connection := fw.Header().Get("Connection")
	if connection != "keep-alive" {
		t.Errorf("Expected Connection 'keep-alive', got '%s'", connection)
	}

	// Verify initial connection comment
	body := fw.Body.String()
	if !strings.Contains(body, ": connected") {
		t.Errorf("Expected ': connected' in response body, got: %s", body)
	}
}

// TestStreamEvents_Unauthorized tests SSE endpoint without auth

func TestStreamEvents_Unauthorized(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.RegisterEvents(r)

	req := httptest.NewRequest("GET", "/events", http.NoBody)
	// No Authorization header
	fw := &flushingResponseWriter{ResponseRecorder: httptest.NewRecorder()}

	r.ServeHTTP(fw, req)

	// Should return 401 or 403 without auth
	if fw.Code != http.StatusUnauthorized && fw.Code != http.StatusForbidden {
		t.Errorf("Expected 401 or 403, got %d: %s", fw.Code, fw.Body.String())
	}
}

// TestGetStats_WithQueryParams_Integration tests /stats endpoint with various query parameters

func TestStreamEvents_WithTypeFilter_Integration(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.RegisterEvents(r)

	// Create request with admin auth and type filter
	req := httptest.NewRequest("GET", "/events?type=model.discovered", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	// Use custom ResponseWriter that implements http.Flusher
	fw := &flushingResponseWriter{ResponseRecorder: httptest.NewRecorder()}

	// Use context with timeout to avoid hanging
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	r.ServeHTTP(fw, req)

	// Verify status code
	if fw.Code != http.StatusOK {
		t.Errorf("Expected 200, got %d: %s", fw.Code, fw.Body.String())
	}

	// Verify Content-Type header for SSE
	contentType := fw.Header().Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Expected Content-Type 'text/event-stream', got '%s'", contentType)
	}

	// Verify Cache-Control header for SSE
	cacheControl := fw.Header().Get("Cache-Control")
	if cacheControl != "no-cache" {
		t.Errorf("Expected Cache-Control 'no-cache', got '%s'", cacheControl)
	}

	// Verify Connection header
	connection := fw.Header().Get("Connection")
	if connection != "keep-alive" {
		t.Errorf("Expected Connection 'keep-alive', got '%s'", connection)
	}

	// Verify X-Accel-Buffering header
	xAccelBuffering := fw.Header().Get("X-Accel-Buffering")
	if xAccelBuffering != "no" {
		t.Errorf("Expected X-Accel-Buffering 'no', got '%s'", xAccelBuffering)
	}

	// Verify initial connection comment is sent
	body := fw.Body.String()
	if !strings.Contains(body, ": connected") {
		t.Errorf("Expected ': connected' in response body, got: %s", body)
	}
}

func TestGetOllamaCloudAccount(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	t.Run("NotFound", func(t *testing.T) {
		// Try to get account for non-existent provider
		nonExistentID := uuid.New().String()
		req := httptest.NewRequest("GET", "/providers/"+nonExistentID+"/account", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Errorf("Expected 404 for non-existent provider, got %d: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("WrongProviderType", func(t *testing.T) {
		// Create a non-Ollama-Cloud provider (OpenAI)
		providerData := `{"name":"test-openai-account","base_url":"https://api.openai.com","api_key":"sk-test"}`
		req := httptest.NewRequest("POST", "/providers", strings.NewReader(providerData))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusCreated {
			t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
		}

		var providerResp map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
			t.Fatalf("Failed to parse provider response: %v", err)
		}
		providerID := providerResp["id"].(string)

		// Try to get account for OpenAI provider (not supported)
		req = httptest.NewRequest("GET", "/providers/"+providerID+"/account", http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		rec = httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("Expected 400 for wrong provider type, got %d: %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "not supported") {
			t.Errorf("Expected error about unsupported provider type, got: %s", rec.Body.String())
		}
	})

	// Note: Success case omitted - GetOllamaCloudAccount requires real network calls
	// to the Ollama Cloud API which would hang tests or require valid credentials.
	// The negative tests above verify the handler's validation and error paths.
}
