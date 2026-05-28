package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestGetSettings(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/settings", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// Should return settings (may have defaults)
}

func TestUpdateSettingsIntegration(t *testing.T) {

	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	settingsData := `{"rate_limit_enabled": "true", "rate_limit_rps": "10", "rate_limit_burst": "20"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/settings", strings.NewReader(settingsData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the update
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/settings", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if response["rate_limit_enabled"] != "true" {
		t.Errorf("Expected rate_limit_enabled='true', got %v", response["rate_limit_enabled"])
	}
}

// UpdateSettings Tests - Additional coverage

func TestUpdateSettings_InvalidKey(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Try to update with invalid key
	settingsData := `{"invalid_key": "value"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/settings", strings.NewReader(settingsData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid key, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "unknown setting") {
		t.Errorf("Expected error about unknown setting, got: %s", rec.Body.String())
	}
}

func TestUpdateSettings_ValueTooLong(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Try to update with value too long (maxSettingValueLen is typically 1000)
	longValue := strings.Repeat("x", 2000)
	settingsData := `{"rate_limit_rps": "` + longValue + `"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/settings", strings.NewReader(settingsData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for value too long, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "too long") {
		t.Errorf("Expected error about value length, got: %s", rec.Body.String())
	}
}

func TestUpdateSettings_InvalidIntValue(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Try to update int setting with non-numeric value
	settingsData := `{"rate_limit_rps": "not-a-number"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/settings", strings.NewReader(settingsData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid int value, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "must be a number") {
		t.Errorf("Expected error about numeric value, got: %s", rec.Body.String())
	}
}

func TestUpdateSettings_IntValueOutOfRange(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Try to update with value out of range (rate_limit_rps max is typically 1000)
	settingsData := `{"rate_limit_rps": "99999"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/settings", strings.NewReader(settingsData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for value out of range, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "must be between") {
		t.Errorf("Expected error about range, got: %s", rec.Body.String())
	}
}

func TestUpdateSettings_EmptyMap(t *testing.T) {
	h := newTestHandler(t)
	r := chi.NewRouter()
	h.Register(r)

	// Try to update with empty map
	settingsData := `{}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/settings", strings.NewReader(settingsData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for empty map, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "no settings provided") {
		t.Errorf("Expected error about no settings, got: %s", rec.Body.String())
	}
}

// App Logs Tests

func TestUpdateSettings_RateLimit(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	settingsData := `{"rate_limit_enabled": "true", "rate_limit_rps": "50", "rate_limit_burst": "100"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/settings", strings.NewReader(settingsData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Verify the update
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/settings", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if response["rate_limit_enabled"] != "true" {
		t.Errorf("Expected rate_limit_enabled='true', got %v", response["rate_limit_enabled"])
	}
	if response["rate_limit_rps"] != "50" {
		t.Errorf("Expected rate_limit_rps='50', got %v", response["rate_limit_rps"])
	}
	if response["rate_limit_burst"] != "100" {
		t.Errorf("Expected rate_limit_burst='100', got %v", response["rate_limit_burst"])
	}
}

// Test for failover.go - SyncFailoverGroups

func TestUpdateSettings_TooManySettings_Integration(t *testing.T) {
	_, router := newTestHandlerWithRouter(t)

	// Create a map with >50 settings
	settings := make(map[string]string)
	for i := 0; i < 51; i++ {
		settings[fmt.Sprintf("setting_%d", i)] = "value"
	}
	body, _ := json.Marshal(settings)

	req := httptest.NewRequest("PUT", "/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for too many settings, got %d: %s", w.Code, w.Body.String())
	}
}

func TestUpdateSettings_ValidFloatSetting_Integration(t *testing.T) {
	_, router := newTestHandlerWithRouter(t)

	body := `{"rate_limit_rps":"30.5"}`

	req := httptest.NewRequest("PUT", "/settings", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for valid float setting, got %d: %s", w.Code, w.Body.String())
	}
}
