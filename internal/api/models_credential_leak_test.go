package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// An upstream that quotes the operator's own key back in an auth failure is
// the case the proxy's credential scrub exists for, and the dashboard's model
// test is a second path to the same sinks: the string it builds is BOTH
// returned to the browser and persisted to request_logs.error_message, where
// it outlives the request and can leave the host through the OTLP log export.
// Self-hosted gateways and relays do echo the key; first-party providers
// mostly redact it.
func TestTestModel_DoesNotLeakTheProviderKey(t *testing.T) {
	// Shapeless on purpose: no known prefix, no digit. The shape layer cannot
	// see it, so this pins the exact-match layer at this call site rather than
	// the shared widening of SanitizeLogBody.
	const key = "selfhosted-gateway-secret"
	h, r := newTestHandlerWithRouter(t)

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{
				"message": "Incorrect API key provided: " + key + ". You can find your API key at https://platform.example.com/.",
				"type":    "invalid_request_error",
				"code":    "invalid_api_key",
			},
		})
	}))
	defer mock.Close()

	origTransport := h.testModelTransport
	h.testModelTransport = &http.Transport{}
	defer func() { h.testModelTransport = origTransport }()

	providerData := fmt.Sprintf(`{"name": "leak-provider-%s", "base_url": "%s", "api_key": "%s"}`,
		uuid.New().String()[:8], mock.URL, key)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("provider create failed: %d %s", rec.Code, rec.Body.String())
	}
	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("provider response: %v", err)
	}

	modelID := uuid.New().String()
	if _, err := h.Pool().Pool().Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled) VALUES ($1, $2, $3, $4, $5)`,
		modelID, providerResp.ID, "leak-model", "Leak Model", true); err != nil {
		t.Fatalf("model insert: %v", err)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/models/"+modelID+"/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("test model: %d %s", rec.Code, rec.Body.String())
	}

	var testResp TestModelResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &testResp); err != nil {
		t.Fatalf("test response: %v", err)
	}
	if testResp.Error == "" {
		t.Fatal("expected an error field from the 401")
	}
	// The response reaches the dashboard toast.
	if strings.Contains(testResp.Error, key) {
		t.Errorf("the provider key is in the model-test response: %q", testResp.Error)
	}
	if !strings.Contains(testResp.Error, "[redacted]") {
		t.Errorf("no redaction marker in %q", testResp.Error)
	}
	// The diagnostic has to survive the scrub, or the operator cannot tell an
	// auth failure from anything else.
	if !strings.Contains(testResp.Error, "Incorrect API key provided") {
		t.Errorf("the diagnostic text was lost: %q", testResp.Error)
	}

	// The persisted row is the sink that outlives the request.
	var stored string
	if err := h.Pool().Pool().QueryRow(context.Background(),
		`SELECT COALESCE(error_message, '') FROM request_logs WHERE model_id = 'leak-model' ORDER BY created_at DESC LIMIT 1`,
	).Scan(&stored); err != nil {
		t.Fatalf("reading back the request log row: %v", err)
	}
	if strings.Contains(stored, key) {
		t.Errorf("the provider key is in request_logs.error_message: %q", stored)
	}
}
