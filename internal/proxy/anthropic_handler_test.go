package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
)

func TestWriteAnthropicError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeAnthropicError(rec, "bad model", http.StatusBadRequest)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("content-type = %q", ct)
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("invalid output: %v", err)
	}
	if m["type"] != "error" {
		t.Errorf("type = %v, want error", m["type"])
	}
	e := m["error"].(map[string]any)
	if e["type"] != "invalid_request_error" || e["message"] != "bad model" {
		t.Errorf("error = %v", e)
	}
}

func TestReadAnthropicBody_FromContext(t *testing.T) {
	h := &Handler{}
	cached := []byte(`{"model":"p/m"}`)
	req := httptest.NewRequest("POST", "/v1/messages", http.NoBody)
	req = req.WithContext(context.WithValue(req.Context(), ctxkeys.RequestBodyKey, cached))
	rec := httptest.NewRecorder()

	body, ok := h.readAnthropicBody(rec, req)
	if !ok || !bytes.Equal(body, cached) {
		t.Errorf("readAnthropicBody from ctx = %q, %v; want cached body", body, ok)
	}
}

func TestReadAnthropicBody_FromBody(t *testing.T) {
	h := &Handler{}
	raw := `{"model":"p/m","max_tokens":1}`
	req := httptest.NewRequest("POST", "/v1/messages", io.NopCloser(bytes.NewReader([]byte(raw))))
	rec := httptest.NewRecorder()

	body, ok := h.readAnthropicBody(rec, req)
	if !ok || string(body) != raw {
		t.Errorf("readAnthropicBody from body = %q, %v; want %q", body, ok, raw)
	}
}
