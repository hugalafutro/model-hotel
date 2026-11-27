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

	"github.com/hugalafutro/model-hotel/internal/endpointtype"
	"github.com/hugalafutro/model-hotel/internal/model"
)

// decisionProbeResult is the part of a Test button answer these tests read,
// beside what the probe sent upstream.
type decisionProbeResult struct {
	Success  bool   `json:"success"`
	Response string `json:"response"`
	Error    string `json:"error"`

	sentPath string
	sentBody map[string]any
}

// probeSystemOneRow registers a provider answering with body on the System
// One route (404 anywhere else, as a worker does), stores it as a systemone
// provider with a decision model on it, and runs that model's Test button.
// The result carries what the probe sent.
func probeSystemOneRow(t *testing.T, h *Handler, r http.Handler, modelName, body string) decisionProbeResult {
	t.Helper()
	var testResp decisionProbeResult
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		testResp.sentPath = req.URL.Path
		w.Header().Set("Content-Type", "application/json")
		if req.URL.Path != "/v1/systemone" {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":{"code":"invalid_request","message":"no route"}}`))
			return
		}
		_ = json.NewDecoder(req.Body).Decode(&testResp.sentBody)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(mockServer.Close)

	providerData := fmt.Sprintf(`{"name": "test-provider-%s", "base_url": "%s", "api_key": "test-key"}`, uuid.New().String()[:8], mockServer.URL)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/providers", strings.NewReader(providerData))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("Failed to create provider: %d: %s", rec.Code, rec.Body.String())
	}
	var providerResp struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &providerResp); err != nil {
		t.Fatalf("Failed to parse provider response: %v", err)
	}
	// The type is set on the row: adding it as systemone through the API
	// would run the type gate's probe, which is not what is under test.
	if _, err := h.Pool().Pool().Exec(context.Background(),
		`UPDATE providers SET provider_type = 'systemone' WHERE id = $1`, providerResp.ID); err != nil {
		t.Fatalf("Failed to set provider type: %v", err)
	}
	modelID := uuid.New().String()
	if _, err := h.Pool().Pool().Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, name, enabled, modality, output_modalities) VALUES ($1, $2, $3, $4, true, 'decision', '["decisions"]')`,
		modelID, providerResp.ID, modelName, modelName); err != nil {
		t.Fatalf("Failed to insert model: %v", err)
	}

	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/models/"+modelID+"/test", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	r.ServeHTTP(rec, req)
	if err := json.Unmarshal(rec.Body.Bytes(), &testResp); err != nil {
		t.Fatalf("Failed to parse test response: %v", err)
	}
	return testResp
}

// A System One server serves no chat completions, so its decision model's Test
// button asks one yes/no question on the decision route, shows the answer and
// stores a decisions row with the input tokens the server counted.
func TestTestModel_SystemOneDecisionRowProbesTheDecisionRoute(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	testResp := probeSystemOneRow(t, h, r, "vela3-lite-probe",
		`{"model":"vela3-lite-probe","answers":{"ready":{"type":"noul","noul":0.97}},"usage":{"input_tokens":41,"output_tokens":0}}`)
	gotPath, gotBody := testResp.sentPath, testResp.sentBody

	if !testResp.Success {
		t.Fatalf("decision probe failed: %s", testResp.Error)
	}
	if gotPath != "/v1/systemone" {
		t.Errorf("probe path = %q, want /v1/systemone", gotPath)
	}
	if gotBody["model"] != "vela3-lite-probe" || gotBody["state"] == nil || gotBody["questions"] == nil || gotBody["messages"] != nil {
		t.Errorf("probe body = %v, want a decision body (model, state, questions) and no chat messages", gotBody)
	}
	if testResp.Response != `{"ready":{"type":"noul","noul":0.97}}` {
		t.Errorf("response = %q, want the answers object", testResp.Response)
	}
	var endpointType, state string
	var prompt, completion int
	if err := h.Pool().Pool().QueryRow(context.Background(),
		`SELECT COALESCE(endpoint_type, ''), state, tokens_prompt, tokens_completion FROM request_logs WHERE model_id = $1 ORDER BY created_at DESC LIMIT 1`, "vela3-lite-probe",
	).Scan(&endpointType, &state, &prompt, &completion); err != nil {
		t.Fatalf("read probe row: %v", err)
	}
	if endpointType != "decisions" || state != "completed" || prompt != 41 || completion != 0 {
		t.Errorf("probe row = %s/%s tokens %d/%d, want decisions/completed 41/0", endpointType, state, prompt, completion)
	}
}

// A 200 with no answers is the empty answer the live path rejects.
func TestTestModel_SystemOneDecisionRowWithNoAnswersFails(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)

	testResp := probeSystemOneRow(t, h, r, "vela3-lite-empty", `{"model":"vela3-lite-empty","answers":{}}`)
	if testResp.Success || !strings.Contains(testResp.Error, "no answers") {
		t.Errorf("response = %+v, want a failure naming the empty answer", testResp)
	}
	var state string
	if err := h.Pool().Pool().QueryRow(context.Background(),
		`SELECT state FROM request_logs WHERE model_id = $1 ORDER BY created_at DESC LIMIT 1`, "vela3-lite-empty",
	).Scan(&state); err != nil || state != "failed" {
		t.Errorf("probe row state = %q (%v), want failed", state, err)
	}
}

// Only a decision model on a systemone provider is probed natively: another
// provider's decision model answers through chat completions, and a chat
// model on a systemone provider is not a decision model.
func TestProbeRouting_ByClassAndProviderType(t *testing.T) {
	for _, tc := range []struct {
		modality, providerType string
		wantPlain              bool
		wantEndpoint           string
	}{
		{"decision", "systemone", true, endpointtype.Decisions},
		{"decision", "nanogpt", false, endpointtype.Chat},
		{"chat", "systemone", false, endpointtype.Chat},
		{"rerank", "cohere", true, endpointtype.Rerank},
	} {
		m := &model.Model{ModelID: "m", Modality: tc.modality, ProviderType: tc.providerType}
		if got := plainProbe(m); got != tc.wantPlain {
			t.Errorf("plainProbe(%s on %s) = %v, want %v", tc.modality, tc.providerType, got, tc.wantPlain)
		}
		if got := probeEndpointType(m); got != tc.wantEndpoint {
			t.Errorf("probeEndpointType(%s on %s) = %q, want %q", tc.modality, tc.providerType, got, tc.wantEndpoint)
		}
	}
}

// An answer that will not parse yields nothing rather than a wrong figure.
func TestParseTestDecisionResponse_Unreadable(t *testing.T) {
	content, tokens, delivered := parseTestDecisionResponse([]byte(`<html>502</html>`))
	if content != "" || tokens != 0 || delivered {
		t.Errorf("got %q, %d, %v; want nothing read and not delivered", content, tokens, delivered)
	}
}
