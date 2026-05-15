package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// TestUpdateSettings_MalformedJSON tests that UpdateSettings returns 400
// when the request body contains malformed JSON.
func TestUpdateSettings_MalformedJSON(t *testing.T) {
	h := &Handler{dbPool: nil}
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.UpdateSettings(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "invalid request body") {
		t.Errorf("expected body to contain %q, got %q", "invalid request body", rr.Body.String())
	}
}

// TestGetSettings_EncodeError tests the error path when JSON encoding fails.
// This covers lines 32-34 in settings.go where encode errors trigger respondError.
func TestGetSettings_EncodeError(t *testing.T) {
	mockSets := &mockSettingsStore{
		getAllFn: func(ctx context.Context) (map[string]string, error) {
			return map[string]string{"key1": "val1"}, nil
		},
	}
	h := testHandler(nil, nil, mockSets, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/settings", http.NoBody)

	fw := &trackingFailingWriter{}
	h.GetSettings(fw, req)

	// After encode fails, respondError is called with 500
	if fw.statusCode != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, fw.statusCode)
	}
}

// trackingFailingWriter is a failingResponseWriter that tracks the status code.
type trackingFailingWriter struct {
	header     http.Header
	statusCode int
}

func (f *trackingFailingWriter) Header() http.Header {
	if f.header == nil {
		f.header = make(http.Header)
	}
	return f.header
}

func (f *trackingFailingWriter) WriteHeader(code int) {
	f.statusCode = code
}

func (f *trackingFailingWriter) Write([]byte) (int, error) {
	return 0, &mockWriteError{"write failed"}
}

// TestUpdateSettings_Success tests that UpdateSettings successfully updates
// settings and returns 200 with the updated values.
func TestUpdateSettings_Success(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	// Use real test DB
	_, r := newTestHandlerWithRouter(t)

	body := bytes.NewReader([]byte(`{"rate_limit_enabled":"true"}`))
	req := httptest.NewRequest(http.MethodPut, "/settings", body)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusOK, rr.Code, rr.Body.String())
	}

	var result map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result["rate_limit_enabled"] != "true" {
		t.Errorf("expected rate_limit_enabled='true', got %q", result["rate_limit_enabled"])
	}
}

// TestUpdateSettings_SetTxError tests that UpdateSettings returns 500
// when the settings repository fails on SetTx.
func TestUpdateSettings_SetTxError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	// Use real test DB but mock settings repo that fails on SetTx
	mockSets := &mockSettingsStore{
		setTxFn: func(ctx context.Context, tx pgx.Tx, key, value string) error {
			return errors.New("db connection lost")
		},
	}

	// Create handler with real DB but mock settings
	h := newTestHandler(t)
	h.settingsRepo = mockSets
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.Register(r)

	body := bytes.NewReader([]byte(`{"rate_limit_enabled":"true"}`))
	req := httptest.NewRequest(http.MethodPut, "/settings", body)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, rr.Code, rr.Body.String())
	}
}
