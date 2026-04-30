package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// ---------------------------------------------------------------------------
// parseUUIDParam tests
// ---------------------------------------------------------------------------

func TestParseUUIDParam_ValidUUID(t *testing.T) {
	id := uuid.New()
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/", nil)
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
	r := httptest.NewRequest(http.MethodGet, "/", nil)
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
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	// No chi context set — chi.URLParam returns empty string, which fails uuid.Parse

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
	r := httptest.NewRequest(http.MethodGet, "/", nil)
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
	r := httptest.NewRequest(http.MethodGet, "/", nil)
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
// writeJSON tests
// ---------------------------------------------------------------------------

func TestWriteJSON_ValidStruct(t *testing.T) {
	w := httptest.NewRecorder()

	data := map[string]string{"name": "test"}
	writeJSON(w, data)

	if w.Code != http.StatusOK {
		t.Errorf("expected status %d, got %d", http.StatusOK, w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode JSON body: %v", err)
	}
	if result["name"] != "test" {
		t.Errorf("name = %q, want %q", result["name"], "test")
	}
}

func TestWriteJSON_EncodingError(t *testing.T) {
	w := httptest.NewRecorder()

	// Channels cannot be JSON-encoded, so this triggers the encoding error path.
	writeJSON(w, make(chan int))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("expected status %d, got %d", http.StatusInternalServerError, w.Code)
	}
}

func TestWriteJSON_SetsContentType(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, map[string]string{"k": "v"})

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

// ---------------------------------------------------------------------------
// writeJSONCreated tests
// ---------------------------------------------------------------------------

func TestWriteJSONCreated_StatusCreated(t *testing.T) {
	w := httptest.NewRecorder()

	data := map[string]string{"name": "new-key"}
	writeJSONCreated(w, data)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, w.Code)
	}
}

func TestWriteJSONCreated_ContentType(t *testing.T) {
	w := httptest.NewRecorder()

	writeJSONCreated(w, map[string]string{"k": "v"})

	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}
}

func TestWriteJSONCreated_Body(t *testing.T) {
	w := httptest.NewRecorder()

	data := map[string]string{"name": "created-key"}
	writeJSONCreated(w, data)

	var result map[string]string
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode JSON body: %v", err)
	}
	if result["name"] != "created-key" {
		t.Errorf("name = %q, want %q", result["name"], "created-key")
	}
}

func TestWriteJSONCreated_EncodingError(t *testing.T) {
	w := httptest.NewRecorder()

	writeJSONCreated(w, make(chan int))

	// writeJSONCreated calls WriteHeader(201) before encoding, so the status
	// is already committed. The encoding error cannot override it.
	if w.Code != http.StatusCreated {
		t.Errorf("expected status %d (WriteHeader called before encoding), got %d", http.StatusCreated, w.Code)
	}
}
