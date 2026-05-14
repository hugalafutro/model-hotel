package api

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// TestPurgeLogs_InvalidValue tests PurgeLogs with invalid older_than
func TestPurgeLogs_InvalidValue(t *testing.T) {
	h := &Handler{
		dbPool: nil,
	}

	body := bytes.NewReader([]byte(`{"older_than":"invalid"}`))
	req, w := newChiRequest(http.MethodDelete, "/logs/purge", body)

	h.PurgeLogs(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

// TestAuthMiddleware tests auth middleware
func TestAuthMiddleware(t *testing.T) {
	mockAuth := &mockAdminAuth{validateFn: func(_ string) bool { return true }}
	h := &Handler{
		adminMgr: mockAuth,
	}

	handler := h.AuthMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status %d (no token), got %d", http.StatusUnauthorized, w.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer valid-token")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d (valid token), got %d", http.StatusOK, w.Code)
	}
}

// TestCond tests cond helper
func TestCond(t *testing.T) {
	tests := []struct {
		name     string
		val      string
		cond     bool
		expected string
	}{
		{"true_condition", "value", true, "value"},
		{"false_condition", "value", false, ""},
		{"empty_value_true", "", true, ""},
		{"empty_value_false", "", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cond(tt.val, tt.cond); got != tt.expected {
				t.Errorf("cond(%q, %v) = %q, want %q", tt.val, tt.cond, got, tt.expected)
			}
		})
	}
}

// TestVirtualKeyToResponse tests virtualKeyToResponse helper
func TestVirtualKeyToResponse(t *testing.T) {
	now := time.Now()
	vk := &virtualkey.VirtualKey{
		ID:         uuid.New(),
		Name:       "test-key",
		KeyPreview: "sk-...ab",
		TokensUsed: 100,
		LastUsedAt: &now,
		CreatedAt:  now,
	}

	resp := virtualKeyToResponse(vk, true, "sk-test-key-12345")

	if resp.Name != "test-key" {
		t.Errorf("expected name 'test-key', got %q", resp.Name)
	}
	if resp.Key != "sk-test-key-12345" {
		t.Errorf("expected key 'sk-test-key-12345', got %q", resp.Key)
	}
	if resp.KeyPreview != "sk-...ab" {
		t.Errorf("expected key_preview 'sk-...ab', got %q", resp.KeyPreview)
	}
	if resp.TokensUsed != 100 {
		t.Errorf("expected tokens_used 100, got %d", resp.TokensUsed)
	}
}

// TestVirtualKeyToResponse_ExcludeKey tests virtualKeyToResponse with includeKey=false
func TestVirtualKeyToResponse_ExcludeKey(t *testing.T) {
	vk := &virtualkey.VirtualKey{
		ID:         uuid.New(),
		Name:       "test-key",
		KeyPreview: "sk-...ab",
		TokensUsed: 50,
		LastUsedAt: nil,
		CreatedAt:  time.Now(),
	}

	resp := virtualKeyToResponse(vk, false, "sk-test-key")

	if resp.Key != "" {
		t.Errorf("expected empty key for exclude, got %q", resp.Key)
	}
}

// TestVirtualKeyToResponse_NoLastUsed tests virtualKeyToResponse with nil LastUsedAt
func TestVirtualKeyToResponse_NoLastUsed(t *testing.T) {
	vk := &virtualkey.VirtualKey{
		ID:         uuid.New(),
		Name:       "test-key",
		KeyPreview: "sk-...ab",
		TokensUsed: 0,
		LastUsedAt: nil,
		CreatedAt:  time.Now(),
	}

	resp := virtualKeyToResponse(vk, false, "")

	if resp.LastUsedAt != nil {
		t.Errorf("expected nil LastUsedAt, got %v", resp.LastUsedAt)
	}
}

// TestStatsHandler_ParsePeriod tests parsePeriod helper
func TestStatsHandler_ParsePeriod(t *testing.T) {
	tests := []struct {
		period string
		want   time.Duration
	}{
		{"1h", time.Hour},
		{"7d", 7 * 24 * time.Hour},
		{"invalid", 24 * time.Hour}, // default
	}

	for _, tt := range tests {
		t.Run(tt.period, func(t *testing.T) {
			req, _ := newChiRequest(http.MethodGet, "/stats?period="+tt.period, nil)
			got := parsePeriod(req)
			if got != tt.want {
				t.Errorf("parsePeriod(%q) = %v, want %v", tt.period, got, tt.want)
			}
		})
	}
}

// TestStatsHandler_ParseMetric tests parseMetric helper
func TestStatsHandler_ParseMetric(t *testing.T) {
	tests := []struct {
		metric string
		want   string
	}{
		{"tokens", "tokens"},
		{"requests", "requests"},
		{"invalid", "requests"}, // default
	}

	for _, tt := range tests {
		t.Run(tt.metric, func(t *testing.T) {
			req, _ := newChiRequest(http.MethodGet, "/stats?metric="+tt.metric, nil)
			got := parseMetric(req)
			if got != tt.want {
				t.Errorf("parseMetric(%q) = %q, want %q", tt.metric, got, tt.want)
			}
		})
	}
}

// TestStatsHandler_ParseExcludeDeleted tests parseExcludeDeleted helper
func TestStatsHandler_ParseExcludeDeleted(t *testing.T) {
	tests := []struct {
		exclude string
		want    bool
	}{
		{"true", true},
		{"false", false},
		{"invalid", false}, // default
	}

	for _, tt := range tests {
		t.Run(tt.exclude, func(t *testing.T) {
			req, _ := newChiRequest(http.MethodGet, "/stats?exclude_deleted="+tt.exclude, nil)
			got := parseExcludeDeleted(req)
			if got != tt.want {
				t.Errorf("parseExcludeDeleted(%q) = %v, want %v", tt.exclude, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Helper function tests (helpers.go)
// ---------------------------------------------------------------------------

// failingResponseWriter is a mock ResponseWriter that always fails on Write
type failingResponseWriter struct {
	header http.Header
}

func (f *failingResponseWriter) Header() http.Header {
	if f.header == nil {
		f.header = make(http.Header)
	}
	return f.header
}

func (f *failingResponseWriter) WriteHeader(_ int) {
	// no-op
}

func (f *failingResponseWriter) Write([]byte) (int, error) {
	return 0, &mockWriteError{"write failed"}
}

type mockWriteError struct {
	msg string
}

func (e *mockWriteError) Error() string {
	return e.msg
}

// TestWriteJSON_ErrorBranch tests the error path when JSON encoding fails
func TestWriteJSON_ErrorBranch(_ *testing.T) {
	fw := &failingResponseWriter{}
	data := map[string]string{"key": "value"}

	// This should not panic and should log the error
	writeJSON(fw, data)
}

// TestWriteJSON_Success tests the success path
func TestWriteJSON_Success(t *testing.T) {
	rec := httptest.NewRecorder()
	data := map[string]string{"key": "value"}

	writeJSON(rec, data)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", rec.Header().Get("Content-Type"))
	}
}

// TestWriteJSONCreated_Success tests the success path
func TestWriteJSONCreated_Success(t *testing.T) {
	rec := httptest.NewRecorder()
	data := map[string]string{"key": "value"}

	writeJSONCreated(rec, data)

	if rec.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", rec.Header().Get("Content-Type"))
	}
}

// TestWriteJSONCreated_ErrorBranch tests the error path when JSON encoding fails
func TestWriteJSONCreated_ErrorBranch(_ *testing.T) {
	fw := &failingResponseWriter{}
	data := map[string]string{"key": "value"}

	// This should not panic and should log the error
	writeJSONCreated(fw, data)
}

// ---------------------------------------------------------------------------
// parseUUIDParam tests
// ---------------------------------------------------------------------------

func TestParseUUIDParam_ValidUUID(t *testing.T) {
	id := uuid.New()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r = setChiURLParam(r, "id", id.String())

	got, ok := parseUUIDParam(w, r, "id")
	if !ok {
		t.Error("expected ok=true for valid UUID")
	}
	if got != id {
		t.Errorf("got %q, want %q", got, id)
	}
	if w.Code != http.StatusOK {
		t.Errorf("expected no error written, got status %d", w.Code)
	}
}

func TestParseUUIDParam_InvalidUUID(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r = setChiURLParam(r, "id", "not-a-uuid")

	_, ok := parseUUIDParam(w, r, "id")
	if ok {
		t.Error("expected ok=false for invalid UUID")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
	body := w.Body.String()
	if body == "" {
		t.Error("expected error body for invalid UUID")
	}
}

func TestParseUUIDParam_MissingParam(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)

	_, ok := parseUUIDParam(w, r, "id")
	if ok {
		t.Error("expected ok=false for missing param")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestParseUUIDParam_CustomLabel(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r = setChiURLParam(r, "id", "bad-uuid")

	_, ok := parseUUIDParam(w, r, "id", "virtual key ID")
	if ok {
		t.Error("expected ok=false for invalid UUID with custom label")
	}
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "virtual key ID") {
		t.Errorf("expected custom label 'virtual key ID' in body, got %q", body)
	}
}

func TestParseUUIDParam_DefaultLabel(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	r = setChiURLParam(r, "id", "bad-uuid")

	_, ok := parseUUIDParam(w, r, "id")
	if ok {
		t.Error("expected ok=false for invalid UUID with default label")
	}
	body := w.Body.String()
	if !strings.Contains(body, "id") {
		t.Errorf("expected default label 'id' in body, got %q", body)
	}
}

// ---------------------------------------------------------------------------
// respondError tests
// ---------------------------------------------------------------------------

func TestRespondError_WithErr(t *testing.T) {
	w := httptest.NewRecorder()
	respondError(w, "something failed", fmt.Errorf("db connection lost"), http.StatusInternalServerError)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
	body := w.Body.String()
	if body != "something failed\n" {
		t.Errorf("expected body %q, got %q", "something failed\n", body)
	}
}

func TestRespondError_5xxWithoutErr(t *testing.T) {
	w := httptest.NewRecorder()
	respondError(w, "internal error", nil, http.StatusInternalServerError)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestRespondError_4xxWithoutErr(t *testing.T) {
	w := httptest.NewRecorder()
	respondError(w, "not found", nil, http.StatusNotFound)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected status %d, got %d", http.StatusNotFound, w.Code)
	}
}

func TestRespondError_BodyIsMessageNotErrorDetails(t *testing.T) {
	w := httptest.NewRecorder()
	respondError(w, "user message", fmt.Errorf("internal details"), http.StatusBadRequest)
	body := w.Body.String()
	if strings.Contains(body, "internal details") {
		t.Error("response body should not contain internal error details")
	}
	if body != "user message\n" {
		t.Errorf("expected body %q, got %q", "user message\n", body)
	}
}

func TestRespondError_ContentTypeIsTextPlain(t *testing.T) {
	w := httptest.NewRecorder()
	respondError(w, "error msg", nil, http.StatusBadRequest)
	ct := w.Header().Get("Content-Type")
	if ct != "text/plain; charset=utf-8" {
		t.Errorf("expected text/plain content type, got %q", ct)
	}
}

// ---------------------------------------------------------------------------
// respondBadRequest tests
// ---------------------------------------------------------------------------

func TestRespondBadRequest_WithErr(t *testing.T) {
	w := httptest.NewRecorder()
	respondBadRequest(w, "invalid input", fmt.Errorf("name too short"))
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestRespondBadRequest_WithoutErr(t *testing.T) {
	w := httptest.NewRecorder()
	respondBadRequest(w, "bad request", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}

func TestRespondBadRequest_BodyIsMessage(t *testing.T) {
	w := httptest.NewRecorder()
	respondBadRequest(w, "invalid parameter", fmt.Errorf("internal: name too long"))
	body := w.Body.String()
	if body != "invalid parameter\n" {
		t.Errorf("expected body %q, got %q", "invalid parameter\n", body)
	}
	if strings.Contains(body, "internal") {
		t.Error("response body should not contain internal error details")
	}
}

func TestRespondBadRequest_StatusCode(t *testing.T) {
	w := httptest.NewRecorder()
	respondBadRequest(w, "bad", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status %d, got %d", http.StatusBadRequest, w.Code)
	}
}
