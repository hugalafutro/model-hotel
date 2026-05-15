package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
// This covers lines 32-34 in settings.go where encode errors are logged.
func TestGetSettings_EncodeError(t *testing.T) {
	mockSets := &mockSettingsStore{
		getAllFn: func(ctx context.Context) (map[string]string, error) {
			return map[string]string{"key1": "val1"}, nil
		},
	}
	h := testHandler(nil, nil, mockSets, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/settings", http.NoBody)

	// Use failingResponseWriter to trigger encode error path
	h.GetSettings(&failingResponseWriter{}, req)
	// The error path just logs, doesn't return HTTP error (headers may already be sent)
	// Test just verifies the code path doesn't panic
}
