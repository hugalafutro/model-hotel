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

	"github.com/hugalafutro/model-hotel/internal/settings"
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

	// The response body has already started when the encode fails, so the
	// handler must not fabricate a second status: it logs the failure and
	// leaves the truncated stream for the client to detect. Writing a 500 here
	// would be a superfluous WriteHeader on a committed response.
	if fw.statusCode != 0 {
		t.Errorf("encode failure wrote status %d after the body started; want none", fw.statusCode)
	}
}

// TestGetSettings_AppVersion tests that app_version is injected into the
// settings response from the Handler's appVersion field.
func TestGetSettings_AppVersion(t *testing.T) {
	mockSets := &mockSettingsStore{
		getAllFn: func(ctx context.Context) (map[string]string, error) {
			return map[string]string{"key1": "val1"}, nil
		},
	}
	h := testHandler(nil, nil, mockSets, nil, nil)
	h.appVersion = "v1.2.3"

	req := httptest.NewRequest(http.MethodGet, "/settings", http.NoBody)
	rr := httptest.NewRecorder()
	h.GetSettings(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}

	var result map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if result["app_version"] != "v1.2.3" {
		t.Errorf("expected app_version='v1.2.3', got %q", result["app_version"])
	}
	if result["key1"] != "val1" {
		t.Errorf("expected key1='val1', got %q", result["key1"])
	}
}

// TestGetSettings_LogExportStatus verifies the read-only log-export status keys
// are injected and reflect process state: metrics tracks METRICS_TOKEN, and the
// JSON/OTEL keys reflect their env vars. These keys must never be writable.
func TestGetSettings_LogExportStatus(t *testing.T) {
	t.Setenv("LOG_FORMAT", "json")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", "")

	mockSets := &mockSettingsStore{
		getAllFn: func(ctx context.Context) (map[string]string, error) {
			return map[string]string{"key1": "val1"}, nil
		},
	}
	h := testHandler(nil, nil, mockSets, nil, nil)
	h.cfg.MetricsToken = "secret-scrape-token" // test-only fixture, not a real credential

	req := httptest.NewRequest(http.MethodGet, "/settings", http.NoBody)
	rr := httptest.NewRecorder()
	h.GetSettings(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d", http.StatusOK, rr.Code)
	}
	var result map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	checks := map[string]string{
		"log_export_json":    "true",  // json log format requested
		"log_export_metrics": "true",  // metrics token configured
		"log_export_otel":    "false", // no otlp endpoint configured
	}
	for key, want := range checks {
		if result[key] != want {
			t.Errorf("expected %s=%q, got %q", key, want, result[key])
		}
	}

	// The status keys must not be writable via PUT.
	for key := range checks {
		if _, ok := allowedSettings[key]; ok {
			t.Errorf("%s must not be in allowedSettings (read-only)", key)
		}
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
		t.Fatal("test database not available")
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
		t.Fatal("test database not available")
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

// ---------------------------------------------------------------------------
// Tests moved from coverage_gap_test.go
// ---------------------------------------------------------------------------

// TestUpdateSettings_Integration_MultipleKeys tests updating multiple settings at once.
func TestUpdateSettings_Integration_MultipleKeys(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	body := `{"rate_limit_enabled": "true", "rate_limit_rps": "50", "rate_limit_burst": "30"}`
	req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var response map[string]string
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["rate_limit_enabled"] != "true" {
		t.Errorf("expected rate_limit_enabled='true', got %s", response["rate_limit_enabled"])
	}
	if response["rate_limit_rps"] != "50" {
		t.Errorf("expected rate_limit_rps='50', got %s", response["rate_limit_rps"])
	}
	if response["rate_limit_burst"] != "30" {
		t.Errorf("expected rate_limit_burst='30', got %s", response["rate_limit_burst"])
	}
}

// TestUpdateSettings_URLValidation rejects SSRF-bait and malformed values for
// URL-typed settings, and accepts a legitimate internal endpoint.
func TestUpdateSettings_URLValidation(t *testing.T) {
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}
	cases := []struct {
		name     string
		body     string
		wantCode int
	}{
		{"issuer metadata literal", `{"oidc_issuer_url": "http://169.254.169.254/latest"}`, http.StatusBadRequest},
		{"apprise metadata literal", `{"alert_apprise_api_url": "http://169.254.169.254"}`, http.StatusBadRequest},
		{"issuer wrong scheme", `{"oidc_issuer_url": "ftp://idp.example.com"}`, http.StatusBadRequest},
		{"public base malformed", `{"oidc_public_base_url": "notaurl"}`, http.StatusBadRequest},
		{"issuer internal host ok", `{"oidc_issuer_url": "http://authelia:9091"}`, http.StatusOK},
		{"apprise internal ok", `{"alert_apprise_api_url": "http://apprise:8000"}`, http.StatusOK},
		{"public base ok", `{"oidc_public_base_url": "https://app.example.com"}`, http.StatusOK},
		{"clear issuer ok", `{"oidc_issuer_url": ""}`, http.StatusOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, r := newTestHandlerWithRouter(t)
			req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(tc.body))
			req.Header.Set("Authorization", "Bearer test-admin-token")
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.wantCode {
				t.Errorf("body %s: expected %d, got %d: %s", tc.body, tc.wantCode, w.Code, w.Body.String())
			}
		})
	}
}

// TestUpdateSettings_FloatValue tests updating a float-type setting.
func TestUpdateSettings_FloatValue(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	body := `{"rate_limit_rps": "25.5"}`
	req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200 OK, got %d: %s", w.Code, w.Body.String())
	}

	var response map[string]string
	if err := json.NewDecoder(w.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["rate_limit_rps"] != "25.5" {
		t.Errorf("expected rate_limit_rps='25.5', got %s", response["rate_limit_rps"])
	}
}

// TestUpdateSettings_OutOfRangeInt tests updating with an integer value out of range.
func TestUpdateSettings_OutOfRangeInt(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// rate_limit_burst max is 10000
	body := `{"rate_limit_burst": "99999"}`
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

// TestUpdateSettings_OutOfRangeFloat tests updating with a float value out of range.
func TestUpdateSettings_OutOfRangeFloat(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// rate_limit_rps max is 10000
	body := `{"rate_limit_rps": "99999"}`
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

// TestUpdateSettings_TooManyKeys tests the limit on number of settings in one request.
func TestUpdateSettings_TooManyKeys(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// Build a request with more than 50 unique keys
	// The >50 check happens before key validation, so keys don't need to be valid
	body := `{`
	for i := range 55 {
		if i > 0 {
			body += `,`
		}
		body += `"setting_key_` + string(rune('a'+(i/26))) + string(rune('a'+(i%26))) + `":"value"`
	}
	body += `}`

	req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 Bad Request, got %d: %s", w.Code, w.Body.String())
	}

	if !strings.Contains(w.Body.String(), "too many settings") {
		t.Errorf("expected error about too many settings, got: %s", w.Body.String())
	}
}

// TestUpdateSettings_NonNumericInt tests that updating an int-type setting with non-numeric value returns 400.
func TestUpdateSettings_NonNumericInt(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// rate_limit_ip_burst is an int-type setting
	body := `{"rate_limit_ip_burst": "not-a-number"}`
	req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 Bad Request, got %d: %s", w.Code, w.Body.String())
	}

	if !strings.Contains(w.Body.String(), "must be a number") {
		t.Errorf("expected error about numeric value, got: %s", w.Body.String())
	}
}

// TestUpdateSettings_NonNumericFloat tests that updating a float-type setting with non-numeric value returns 400.
func TestUpdateSettings_NonNumericFloat(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)

	// rate_limit_ip_rps is a float-type setting
	body := `{"rate_limit_ip_rps": "not-a-number"}`
	req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400 Bad Request, got %d: %s", w.Code, w.Body.String())
	}

	if !strings.Contains(w.Body.String(), "must be a number") {
		t.Errorf("expected error about numeric value, got: %s", w.Body.String())
	}
}

// TestAllowedSettingsSync verifies that the API handler allowlist and the
// settings repository allowlist have identical key sets. If they diverge,
// the handler may accept keys that the DB layer rejects (or vice versa).
func TestAllowedSettingsSync(t *testing.T) {
	apiKeys := make(map[string]bool)
	for k := range allowedSettings {
		apiKeys[k] = true
	}

	for k := range settings.AllowedSettings {
		if !apiKeys[k] {
			t.Errorf("key %q in settings.AllowedSettings but missing from api.allowedSettings", k)
		}
		delete(apiKeys, k)
	}
	for k := range apiKeys {
		t.Errorf("key %q in api.allowedSettings but missing from settings.AllowedSettings", k)
	}
}

// A runtime key the breaker reads has to be in BOTH allowlists, and
// TestAllowedSettingsSync only proves the two maps agree — it is equally happy
// when a key is in neither, which is the state a key nothing registers is in.
// The circuit-breaker span decides how many open model circuits it takes to skip
// a provider outright, so a member where it is unregistered is a member where the
// operator cannot change it, config sync will not carry it, and every read of it
// misses the settings cache. Name it explicitly, on both sides, with the bound
// the handler must enforce.
func TestCircuitBreakerSpanModelsIsRegisteredInBothAllowlists(t *testing.T) {
	const key = "circuit_breaker_span_models"

	if !settings.AllowedSettings[key] {
		t.Errorf("%s missing from settings.AllowedSettings: the DB layer refuses to write it", key)
	}
	rule, ok := allowedSettings[key]
	if !ok {
		t.Fatalf("%s missing from api.allowedSettings: PUT /api/settings rejects it as an unknown setting", key)
	}
	if rule.typeName != "int" {
		t.Errorf("%s type %q, want int", key, rule.typeName)
	}
	if rule.min != 1 {
		t.Errorf("%s min %v, want 1: a span below 1 is not a span, it is a provider skipped on no evidence", key, rule.min)
	}
	if rule.max < 1 {
		t.Errorf("%s max %v, want a ceiling above the floor", key, rule.max)
	}
}

// The five 429-classification keys are runtime keys the proxy reads per
// request; like the span test above, name them explicitly on both sides so a
// key in NEITHER allowlist (which the sync test cannot see) fails here.
func TestRateLimitClassificationKeysAreRegisteredInBothAllowlists(t *testing.T) {
	for _, key := range []string{
		"rate_limit_classify_enabled",
		"rate_limit_saturation_max_wait",
		"rate_limit_recent_success_window",
		"failover_exhaustion_status_429",
		"circuit_breaker_open_on_exhaustion",
		"inflight_limiter_enabled",
		"inflight_forget_after",
	} {
		if !settings.AllowedSettings[key] {
			t.Errorf("%s missing from settings.AllowedSettings: the DB layer refuses to write it", key)
		}
		rule, ok := allowedSettings[key]
		if !ok {
			t.Errorf("%s missing from api.allowedSettings: PUT /api/settings rejects it as an unknown setting", key)
			continue
		}
		if rule.typeName != "string" {
			t.Errorf("%s type %q, want string (bools and durations ride as strings)", key, rule.typeName)
		}
	}
	// The grow counter is the one integer of the family.
	if !settings.AllowedSettings["inflight_grow_after"] {
		t.Error("inflight_grow_after missing from settings.AllowedSettings")
	}
	if rule, ok := allowedSettings["inflight_grow_after"]; !ok || rule.typeName != "int" || rule.min != 1 {
		t.Errorf("inflight_grow_after rule = %+v (ok=%v), want an int with a floor of 1", rule, ok)
	}
}

// TestUpdateSettings_EmptySettings tests that UpdateSettings returns 400
// when the request body is an empty object.
func TestUpdateSettings_EmptySettings(t *testing.T) {
	h := &Handler{dbPool: nil}
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.UpdateSettings(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "no settings provided") {
		t.Errorf("expected body to contain %q, got %q", "no settings provided", rr.Body.String())
	}
}

// TestUpdateSettings_UnknownKey tests that UpdateSettings returns 400
// when the request contains a key not in the allowed settings list.
func TestUpdateSettings_UnknownKey(t *testing.T) {
	h := &Handler{dbPool: nil}
	req := httptest.NewRequest(http.MethodPut, "/api/settings", strings.NewReader(`{"not_a_real_setting":"value"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	h.UpdateSettings(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "unknown setting") {
		t.Errorf("expected body to contain %q, got %q", "unknown setting", rr.Body.String())
	}
}

// TestUpdateSettings_ModelPruneDaysRange pins the documented range: 0 (off)
// or 1..MaxModelPruneDays. There is no floor above 0: the prune keys flapping
// off a model coming back, not off its own retirement, so a horizon of a few
// days is honoured rather than silently inert.
func TestUpdateSettings_ModelPruneDaysRange(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)
	put := func(v string) int {
		t.Helper()
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(`{"model_prune_days":"`+v+`"}`))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(rec, req)
		return rec.Code
	}
	for _, tc := range []struct {
		value string
		want  int
	}{
		{"0", http.StatusOK}, {"1", http.StatusOK}, {"7", http.StatusOK}, {"180", http.StatusOK},
		{"181", http.StatusBadRequest}, {"365", http.StatusBadRequest}, {"-1", http.StatusBadRequest}, {"abc", http.StatusBadRequest},
	} {
		if got := put(tc.value); got != tc.want {
			t.Errorf("model_prune_days=%q: got %d, want %d", tc.value, got, tc.want)
		}
	}
}
