package proxy

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/provider"
)

// The breaker keys a circuit by the resolved upstream model id, and the empty
// string candidateModelID falls back to is a real key: it earns a circuit, and
// that circuit counts toward the span of distinct models that indicts the
// provider. So a candidate reaching the breaker without a model does not merely
// lose a diagnostic, it charges a circuit nothing routes by, and the operator
// needs a trail saying which provider it happened on.
func TestCandidateModelIDLogsAModellessCandidate(t *testing.T) {
	capture := captureProxyLogs(t)
	prov := &provider.Provider{ID: uuid.New(), Name: "acme"}

	if got := candidateModelID(modelCandidate{provider: prov}); got != "" {
		t.Fatalf("candidateModelID = %q, want the empty fallback", got)
	}

	records := capture.find("candidate carries no model")
	if len(records) != 1 {
		t.Fatalf("got %d log records for a modelless candidate, want exactly 1", len(records))
	}
	rec := records[0]
	if rec.level != slog.LevelError {
		t.Errorf("logged at %v, want error: charging the wrong circuit is a defect, not routine noise", rec.level)
	}
	if rec.attrs["provider"] != "acme" {
		t.Errorf("provider attr = %q, want %q", rec.attrs["provider"], "acme")
	}
	if rec.attrs["provider_id"] != prov.ID.String() {
		t.Errorf("provider_id attr = %q, want %q", rec.attrs["provider_id"], prov.ID)
	}
}

// The ordinary path must stay silent: every routed candidate carries a model,
// and a line per attempt would bury the one that matters.
func TestCandidateModelIDIsSilentForAResolvedModel(t *testing.T) {
	capture := captureProxyLogs(t)
	candidate := modelCandidate{
		provider: &provider.Provider{ID: uuid.New(), Name: "acme"},
		model:    testModelNamed("gpt-4o-mini"),
	}

	if got := candidateModelID(candidate); got != "gpt-4o-mini" {
		t.Fatalf("candidateModelID = %q, want the candidate's upstream model id", got)
	}
	if records := capture.find("candidate carries no model"); len(records) != 0 {
		t.Errorf("got %d log records for a resolved model, want none", len(records))
	}
}

// A self-heal retry answers on its own context; the refused attempt's context
// is cancelled once its body is consumed. The retried answer must be read
// under the retry's context: read under the cancelled one, a provider fault on
// the retry (here a body cut mid-JSON) was judged as the caller hanging up.
func TestChatCompletions_FaultOnSelfHealRetryIsTheProvidersNotTheCallers(t *testing.T) {
	var calls atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch calls.Add(1) {
		case 1:
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"message":"Unsupported parameter: 'temperature' is not supported with this model.","type":"invalid_request_error","param":"temperature","code":"unsupported_parameter"}}`))
		default:
			// A 200 whose body ends before the declared length: the client's
			// read fails with an unexpected EOF, a provider fault.
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Length", "4096")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"id":"chatcmpl-cut","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hel`))
		}
	}))
	env := newTestProxyEnvWithUpstream(t, upstream)
	defer upstream.Close()

	body := `{"model":"` + env.ProviderName + `/` + env.ModelName + `","stream":false,"temperature":0.5,"messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	env.Handler.ChatCompletions(w, req)

	if got := calls.Load(); got != 2 {
		t.Fatalf("upstream calls = %d, want 2 (the refusal and the retry)", got)
	}
	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 for a provider fault", w.Code)
	}
	if strings.Contains(w.Body.String(), "interrupted") {
		t.Errorf("client was told the request was interrupted while still waiting:\n%s", w.Body.String())
	}
	// The terminal request-log write lands after the response is served.
	modelID := env.ModelName // request_logs.model_id holds the bare model name for a direct provider request
	var kind string
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := testDB.Pool().QueryRow(context.Background(),
			`SELECT COALESCE(error_kind, '') FROM request_logs WHERE model_id = $1 ORDER BY created_at DESC LIMIT 1`, modelID).Scan(&kind)
		if err == nil && kind != "" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("request log row with an error_kind did not land: err=%v kind=%q", err, kind)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if kind != string(KindProviderError) {
		t.Errorf("error_kind = %q, want %q", kind, KindProviderError)
	}
}
