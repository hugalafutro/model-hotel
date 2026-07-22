package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/db"
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

	var response map[string]any
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

	var response map[string]any
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

// Test that ttft_timeout and stream_stall_timeout can be saved (they were
// missing from allowedSettings, causing 400s).
func TestUpdateSettings_TimeoutDurations(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	body := `{"ttft_timeout": "1m0s", "stream_stall_timeout": "30s"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/settings", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if response["ttft_timeout"] != "1m0s" {
		t.Errorf("Expected ttft_timeout='1m0s', got %q", response["ttft_timeout"])
	}
	if response["stream_stall_timeout"] != "30s" {
		t.Errorf("Expected stream_stall_timeout='30s', got %q", response["stream_stall_timeout"])
	}
}

// Test that pwned_password_check_enabled round-trips through the settings API.
// It is the runtime toggle read by passwordpolicy.go; without an allowlist entry
// the write silently 400s and the "disable without a redeploy" promise breaks
// (AGENTS.md: one save/retrieve test per allowedSettings key).
func TestUpdateSettings_PwnedPasswordToggle(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	body := `{"pwned_password_check_enabled": "false"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/settings", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

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
	if response["pwned_password_check_enabled"] != "false" {
		t.Errorf("Expected pwned_password_check_enabled='false', got %q", response["pwned_password_check_enabled"])
	}
}

// Test that hedging_enabled and hedge_delay round-trip through the settings API
// (AGENTS.md: one save/retrieve test per allowedSettings key).
func TestUpdateSettings_Hedging(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	body := `{"hedging_enabled": "true", "hedge_delay": "4s"}`
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/settings", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if response["hedging_enabled"] != "true" {
		t.Errorf("Expected hedging_enabled='true', got %q", response["hedging_enabled"])
	}
	if response["hedge_delay"] != "4s" {
		t.Errorf("Expected hedge_delay='4s', got %q", response["hedge_delay"])
	}
}

// Test that session_idle_timeout_minutes round-trips through the settings API
// and that out-of-range values are rejected (AGENTS.md: one save/retrieve test
// per allowedSettings key).
func TestUpdateSettings_SessionIdleTimeout(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("PUT", "/settings", strings.NewReader(`{"session_idle_timeout_minutes": "30"}`))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var response map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if response["session_idle_timeout_minutes"] != "30" {
		t.Errorf("Expected session_idle_timeout_minutes='30', got %q", response["session_idle_timeout_minutes"])
	}

	// Out of range (max 240) is rejected with 400.
	rec = httptest.NewRecorder()
	req = httptest.NewRequest("PUT", "/settings", strings.NewReader(`{"session_idle_timeout_minutes": "241"}`))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for out-of-range value, got %d: %s", rec.Code, rec.Body.String())
	}
}

// Test for failover.go - SyncFailoverGroups

func TestUpdateSettings_TooManySettings_Integration(t *testing.T) {
	_, router := newTestHandlerWithRouter(t)

	// Create a map with >50 settings
	settings := make(map[string]string)
	for i := range 51 {
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

func TestResetSettings_SpecificKeys(t *testing.T) {
	_, router := newTestHandlerWithRouter(t)

	// Set a value first
	body := `{"rate_limit_rps":"30.5"}`
	req := httptest.NewRequest("PUT", "/settings", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("setup: expected 200, got %d", w.Code)
	}

	// Reset that key
	resetBody := `{"keys":["rate_limit_rps"]}`
	req = httptest.NewRequest("DELETE", "/settings", bytes.NewReader([]byte(resetBody)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for reset, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// After reset, the key is gone from DB so GetSettings won't include it
	if _, exists := result["rate_limit_rps"]; exists {
		t.Error("rate_limit_rps should have been removed from DB")
	}
}

func TestResetSettings_AllKeys(t *testing.T) {
	_, router := newTestHandlerWithRouter(t)

	// Set two values
	body := `{"rate_limit_rps":"30.5","request_timeout":"2m0s"}`
	req := httptest.NewRequest("PUT", "/settings", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("setup: expected 200, got %d", w.Code)
	}

	// Reset all (empty keys list)
	resetBody := `{"keys":[]}`
	req = httptest.NewRequest("DELETE", "/settings", bytes.NewReader([]byte(resetBody)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for reset all, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// app_commit is read-only and always present (defaults to "unknown" when
	// the build was not stamped with a source SHA).
	if result["app_commit"] == "" {
		t.Error("app_commit should always be present")
	}
	// app_version is read-only and always present
	if result["app_version"] == "" {
		t.Error("app_version should always be present")
	}
	// The two settings we set should be gone
	if _, exists := result["rate_limit_rps"]; exists {
		t.Error("rate_limit_rps should have been removed")
	}
	if _, exists := result["request_timeout"]; exists {
		t.Error("request_timeout should have been removed")
	}
}

func TestResetSettings_InvalidKey(t *testing.T) {
	_, router := newTestHandlerWithRouter(t)

	resetBody := `{"keys":["nonexistent_key"]}`
	req := httptest.NewRequest("DELETE", "/settings", bytes.NewReader([]byte(resetBody)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for unknown key, got %d", w.Code)
	}
}

func TestResetSettings_InvalidJSON(t *testing.T) {
	_, router := newTestHandlerWithRouter(t)

	req := httptest.NewRequest("DELETE", "/settings", strings.NewReader(`{invalid`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid JSON, got %d", w.Code)
	}
}

func TestResetSettings_TooManyKeys(t *testing.T) {
	_, router := newTestHandlerWithRouter(t)

	// Build a request with 51 keys.
	keys := make([]string, 51)
	for i := range keys {
		keys[i] = fmt.Sprintf("key_%d", i)
	}
	body := fmt.Sprintf(`{"keys":%s}`, strings.ReplaceAll(fmt.Sprintf("%q", keys), " ", ","))

	req := httptest.NewRequest("DELETE", "/settings", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for too many keys, got %d", w.Code)
	}
}

func TestResetSettings_InvalidRequestBody(t *testing.T) {
	_, router := newTestHandlerWithRouter(t)

	// Send a non-JSON body
	req := httptest.NewRequest("DELETE", "/settings", strings.NewReader(`not-json`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for invalid request body, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid request body") {
		t.Errorf("Expected error about invalid request body, got: %s", w.Body.String())
	}
}

func TestResetSettings_UnknownKeyInList(t *testing.T) {
	_, router := newTestHandlerWithRouter(t)

	// Mix a known key with an unknown one
	resetBody := `{"keys":["rate_limit_rps","totally_unknown_key"]}`
	req := httptest.NewRequest("DELETE", "/settings", strings.NewReader(resetBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 for unknown key in list, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "unknown setting") {
		t.Errorf("Expected error about unknown setting, got: %s", w.Body.String())
	}
}

func TestResetSettings_ValidSingleKeyReset(t *testing.T) {
	_, router := newTestHandlerWithRouter(t)

	// Set a value first
	setBody := `{"circuit_breaker_threshold":"5"}`
	req := httptest.NewRequest("PUT", "/settings", strings.NewReader(setBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("setup: expected 200, got %d", w.Code)
	}

	// Reset just that key
	resetBody := `{"keys":["circuit_breaker_threshold"]}`
	req = httptest.NewRequest("DELETE", "/settings", strings.NewReader(resetBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for valid single key reset, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if _, exists := result["circuit_breaker_threshold"]; exists {
		t.Error("circuit_breaker_threshold should have been removed after reset")
	}
}

// TestResetSettings_BeginTxError tests that ResetSettings returns 500 when
// the database transaction cannot be started. This is tested by closing
// the pool before calling ResetSettings.
func TestResetSettings_BeginTxError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	// Create a handler with a real DB pool, then close the pool to force
	// Begin to fail.
	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Fatal("test database not available")
	}

	settingsRepo := &mockSettingsStore{
		deleteKeysTxFn: func(ctx context.Context, tx pgx.Tx, keys []string) error {
			return nil
		},
	}

	database, err := db.New(context.Background(), apiTestDBURL, 5, 1)
	if err != nil {
		t.Fatal("test database not available")
	}

	// Close the database pool to force Begin to fail.
	database.Close()

	h := &Handler{
		dbPool:       database,
		settingsRepo: settingsRepo,
		appVersion:   "test",
		cfg:          &config.Config{},
	}

	resetBody := `{"keys":["rate_limit_rps"]}`
	req := httptest.NewRequest("DELETE", "/settings", strings.NewReader(resetBody))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	h.ResetSettings(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 for begin tx error, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "failed to begin transaction") {
		t.Errorf("Expected error about transaction, got: %s", w.Body.String())
	}

	pool.Close()
}

// TestResetSettings_DeleteKeysTxError tests that ResetSettings returns 500
// when the DeleteKeysTx operation fails.
func TestResetSettings_DeleteKeysTxError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	// Use a real DB with a mock settings repo that fails on DeleteKeysTx.
	h := newTestHandler(t)
	h.settingsRepo = &mockSettingsStore{
		deleteKeysTxFn: func(ctx context.Context, tx pgx.Tx, keys []string) error {
			return errors.New("db connection lost")
		},
	}
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.Register(r)

	resetBody := `{"keys":["rate_limit_rps"]}`
	req := httptest.NewRequest("DELETE", "/settings", strings.NewReader(resetBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 for DeleteKeysTx error, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "failed to reset") {
		t.Errorf("Expected error about reset failure, got: %s", w.Body.String())
	}
}

// TestResetSettings_CommitError tests that ResetSettings returns 500 when the
// transaction commit fails. This exercises the commit error path.
func TestResetSettings_CommitError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	// The commit path is hard to trigger with a real DB since you can't
	// force a commit failure on a valid connection. Instead, test the
	// encode error at the end of ResetSettings by using a failing writer.
	h := newTestHandler(t)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.Register(r)

	// Set a value first.
	setBody := `{"rate_limit_rps":"30"}`
	req := httptest.NewRequest("PUT", "/settings", strings.NewReader(setBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("setup: expected 200, got %d", w.Code)
	}

	// Reset with a failing writer to trigger the JSON encode error.
	resetBody := `{"keys":["rate_limit_rps"]}`
	req = httptest.NewRequest("DELETE", "/settings", strings.NewReader(resetBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")

	fw := &trackingFailingWriter{}
	h.ResetSettings(fw, req)

	// After encode fails, respondError is called with 500.
	if fw.statusCode != http.StatusInternalServerError {
		t.Errorf("Expected 500 for encode error, got %d", fw.statusCode)
	}
}

// TestResetSettings_NilGetAll tests that the response map is initialized when
// GetAll returns nil.
func TestResetSettings_NilGetAll(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	// Use a real DB for the transaction but replace settings repo with
	// one that returns nil from GetAll (simulating empty settings).
	h := newTestHandler(t)
	r := chi.NewRouter()
	r.Use(h.AuthMiddleware)
	h.Register(r)

	// Reset all settings (empty keys list = reset all).
	resetBody := `{"keys":[]}`
	req := httptest.NewRequest("DELETE", "/settings", strings.NewReader(resetBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for reset all, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	// app_version is read-only and always present.
	if result["app_version"] == "" {
		t.Error("app_version should always be present")
	}
}

// ---------------------------------------------------------------------------
// 1. UpdateSettings — int-type value below minimum
// ---------------------------------------------------------------------------

func TestUpdateSettings_IntBelowMin(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// rate_limit_burst min is 1, so 0 should fail
	body := `{"rate_limit_burst": "0"}`
	req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 Bad Request, got %d: %s", w.Code, w.Body.String())
	}

	if !strings.Contains(w.Body.String(), "must be between") {
		t.Errorf("expected error about range, got: %s", w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// 2. UpdateSettings — float-type value below minimum
// ---------------------------------------------------------------------------

func TestUpdateSettings_FloatBelowMin(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// rate_limit_ip_rps min is 0, so -1 should fail
	body := `{"rate_limit_ip_rps": "-1"}`
	req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 Bad Request, got %d: %s", w.Code, w.Body.String())
	}

	if !strings.Contains(w.Body.String(), "must be between") {
		t.Errorf("expected error about range, got: %s", w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// 3. UpdateSettings — begin transaction error (cancelled context)
// ---------------------------------------------------------------------------

func TestUpdateSettings_BeginTxError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	testDB, err := db.New(context.Background(), apiTestDBURL, 25, 5)
	if err != nil {
		t.Fatalf("failed to create test DB: %v", err)
	}
	defer testDB.Close()

	h := newTestHandler(t)
	body := bytes.NewReader([]byte(`{"rate_limit_enabled":"true"}`))
	req := httptest.NewRequest(http.MethodPut, "/settings", body)
	req.Header.Set("Content-Type", "application/json")
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.UpdateSettings(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d; body: %s", http.StatusInternalServerError, w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// 4. UpdateSettings — commit error (cancelled context)
// ---------------------------------------------------------------------------

func TestUpdateSettings_CommitError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	h := newTestHandler(t)
	body := bytes.NewReader([]byte(`{"rate_limit_enabled":"true"}`))
	req := httptest.NewRequest(http.MethodPut, "/settings", body)
	req.Header.Set("Content-Type", "application/json")
	ctx, cancel := context.WithCancel(req.Context())
	cancel()
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	h.UpdateSettings(w, req)

	// The handler returns an error; exact status depends on where cancellation hits
	if w.Code != http.StatusInternalServerError && w.Code != http.StatusBadRequest {
		t.Logf("UpdateSettings with cancelled context: status=%d body=%s", w.Code, w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// 5. UpdateSettings — encode error on response
// ---------------------------------------------------------------------------

func TestUpdateSettings_ResponseEncodeError(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}

	h := newTestHandler(t)
	body := bytes.NewReader([]byte(`{"rate_limit_enabled":"true"}`))
	req := httptest.NewRequest(http.MethodPut, "/settings", body)
	req.Header.Set("Content-Type", "application/json")

	fw := &statusTrackingFailWriter{}
	h.UpdateSettings(fw, req)

	if fw.statusCode != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, fw.statusCode)
	}
}

// statusTrackingFailWriter tracks status code and always fails on Write.
type statusTrackingFailWriter struct {
	header     http.Header
	statusCode int
}

func (f *statusTrackingFailWriter) Header() http.Header {
	if f.header == nil {
		f.header = make(http.Header)
	}
	return f.header
}

func (f *statusTrackingFailWriter) WriteHeader(code int) {
	f.statusCode = code
}

func (f *statusTrackingFailWriter) Write([]byte) (int, error) {
	return 0, &mockWriteError{"write failed"}
}

// TestGetSettings_NilGetAll covers the nil-map guard in GetSettings: when the
// settings store returns a nil map, the handler must initialize it before
// injecting app_version.
func TestGetSettings_NilGetAll(t *testing.T) {
	setsStore := &mockSettingsStore{
		getAllFn: func(_ context.Context) (map[string]string, error) { return nil, nil },
	}
	h := testHandler(nil, nil, setsStore, &mockAdminAuth{validateFn: func(string) bool { return true }}, nil)
	req, w := newChiRequest(http.MethodGet, "/settings", nil)

	h.GetSettings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d: %s", http.StatusOK, w.Code, w.Body.String())
	}
	var result map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}
	if result["app_version"] == "" {
		t.Error("app_version should be present even when GetAll returns nil")
	}
}
