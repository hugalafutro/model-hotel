package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// A vllm-srun worker's POST /v1/systemone answer, as the live server sent it.
const systemOneAnswerBody = `{"model":"vela3-lite","answers":{"team":{"type":"choice","choice":"engineering","probabilities":{"billing":0.0009,"engineering":0.9882,"other":0.0108},"confidence":0.9823},"urgent":{"type":"noul","noul":0.9857}},"usage":{"input_tokens":248,"output_tokens":0}}`

const systemOneRequestTail = `"state":{"request":"Our export button fails with a 500."},"questions":{"urgent":{"type":"noul","instructions":"Does this block their work?"}}}`

// Both routes forward the body verbatim to the upstream route of the same
// name, with only the model rewritten to the upstream's id.
func TestDecisionRoutes_PassthroughAndModelRewrite(t *testing.T) {
	for _, tc := range []struct {
		route string
		serve func(*Handler) http.HandlerFunc
		want  string
	}{
		{"/v1/systemone", func(h *Handler) http.HandlerFunc { return h.SystemOne }, "/systemone"},
		{"/v1/decisions", func(h *Handler) http.HandlerFunc { return h.Decisions }, "/decisions"},
	} {
		t.Run(tc.route, func(t *testing.T) {
			var gotPath, gotModel, gotState atomic.Value
			env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath.Store(r.URL.Path)
				var req map[string]json.RawMessage
				_ = json.NewDecoder(r.Body).Decode(&req)
				gotModel.Store(string(req["model"]))
				gotState.Store(string(req["state"]))
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, systemOneAnswerBody)
			}))

			body := fmt.Sprintf(`{"model":"%s/%s",%s`, env.providerName, env.modelName, systemOneRequestTail)
			w := httptest.NewRecorder()
			tc.serve(env.handler)(w, env.request(tc.route, "application/json", strings.NewReader(body)))

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
			}
			if got := strings.TrimSpace(w.Body.String()); got != systemOneAnswerBody {
				t.Errorf("answer not passed through verbatim:\ngot  %s\nwant %s", got, systemOneAnswerBody)
			}
			if p, _ := gotPath.Load().(string); p != tc.want {
				t.Errorf("upstream path = %q, want %s", p, tc.want)
			}
			if m, _ := gotModel.Load().(string); m != `"`+env.modelName+`"` {
				t.Errorf("upstream model = %s, want %q (the model must be rewritten)", m, env.modelName)
			}
			if s, _ := gotState.Load().(string); !strings.Contains(s, "export button") {
				t.Errorf("upstream state = %s, want the caller's state untouched", s)
			}
		})
	}
}

// The upstream takes a request without a model when it serves one; the
// gateway routes on the model, so it is required here.
func TestSystemOne_ModelRequired(t *testing.T) {
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called for a request without a model")
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	env.handler.SystemOne(w, env.request("/v1/systemone", "application/json", strings.NewReader(`{`+systemOneRequestTail)))

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "model is required") {
		t.Errorf("status = %d body = %q, want a 400 naming the missing model", w.Code, w.Body.String())
	}
}

// A decision request has no stream to interrupt, so a failed member of a
// failover group is passed over like on every other non-streaming route.
func TestSystemOne_FailoverToNextProvider(t *testing.T) {
	var badCalls, goodCalls atomic.Int32
	envBad := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		badCalls.Add(1)
		// What a worker answers while its model is still loading.
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = io.WriteString(w, `{"detail":"model is loading","code":"unavailable"}`)
	}))
	goodUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		goodCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, systemOneAnswerBody)
	}))
	t.Cleanup(goodUpstream.Close)
	_, _, goodModelUUID, _ := createMultimodalProvider(t, goodUpstream.URL)

	groupName := envBad.modelName
	if _, err := failover.NewRepository(testDB.Pool()).UpsertWithConfig(context.Background(), groupName,
		[]uuid.UUID{envBad.modelUUID, goodModelUUID},
		map[string]bool{envBad.modelUUID.String(): true, goodModelUUID.String(): true},
		nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}

	w := httptest.NewRecorder()
	envBad.handler.SystemOne(w, envBad.request("/v1/systemone", "application/json",
		strings.NewReader(fmt.Sprintf(`{"model":"hotel/%s",%s`, groupName, systemOneRequestTail))))

	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"answers"`) {
		t.Fatalf("status = %d body = %s, want the second member's answer", w.Code, w.Body.String())
	}
	if badCalls.Load() != 1 || goodCalls.Load() != 1 {
		t.Errorf("calls = %d bad, %d good; want one each", badCalls.Load(), goodCalls.Load())
	}
}

// A question the model cannot answer fails a /v1/systemone request whole with
// a 422. That is the caller's request, not the provider failing: it is
// forwarded as it came and no other member is asked.
func TestSystemOne_QuestionFailureIsForwardedNotFailedOver(t *testing.T) {
	const refusal = `{"detail":[{"loc":["body","questions","a"],"msg":"no question is valid","type":"invalid_question"}],"code":"question_failed","question":"a","reason":"box_unsupported"}`
	var calls atomic.Int32
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, refusal)
	}))

	w := httptest.NewRecorder()
	env.handler.SystemOne(w, env.request("/v1/systemone", "application/json",
		strings.NewReader(fmt.Sprintf(`{"model":"%s/%s","state":"x","questions":{"a":{"type":"box","instructions":"where?"}}}`, env.providerName, env.modelName))))

	if w.Code != http.StatusUnprocessableEntity || calls.Load() != 1 {
		t.Fatalf("status = %d after %d calls, want the upstream's 422 from one call", w.Code, calls.Load())
	}
	if !strings.Contains(w.Body.String(), "box_unsupported") {
		t.Errorf("body = %s, want the upstream's reason", w.Body.String())
	}
}

// The upstream counts usage as input_tokens with no output: that is what the
// key is charged, nothing estimated on top.
func TestDecisions_UsageIsMeteredFromInputTokens(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	vkRepo := &mockVirtualKeyRepo{}
	h.virtualKeyRepo = vkRepo

	logData := &requestLogData{
		id:              uuid.New().String(),
		modelID:         "vela3-lite",
		endpointType:    endpointTypeDecisions,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		state:           "streaming",
		promptTextBytes: 4000,
	}
	st := &requestState{startTime: time.Now(), logData: logData, vkHash: "test-hash"}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)

	rec := httptest.NewRecorder()
	resp := &http.Response{
		StatusCode: http.StatusOK,
		// No content type: a decisions answer is JSON by definition and is
		// judged as such whatever a proxy in front labelled it.
		Header: http.Header{},
		Body:   io.NopCloser(strings.NewReader(systemOneAnswerBody)),
	}
	outcome := h.servePassthroughResponse(rec, httptest.NewRequest("POST", "/v1/systemone", http.NoBody), st, modelCandidate{
		model:    &model.Model{ID: uuid.New(), ModelID: "vela3-lite"},
		provider: &provider.Provider{ID: uuid.New(), Name: "test-provider"},
	}, resp, 1, 10.0, false)

	if outcome != outcomeServed || rec.Body.String() != systemOneAnswerBody {
		t.Fatalf("outcome = %v body = %s, want the answer served", outcome, rec.Body.String())
	}
	if len(vkRepo.addTokensCalls) != 1 || vkRepo.addTokensCalls[0].tokens != 248 {
		t.Errorf("charged %+v, want one charge of the 248 input tokens the upstream counted", vkRepo.addTokensCalls)
	}
}

// Only a decisions request reads the flat shape as a refusal to forward, and
// only when it names both a code and a detail.
func TestCarriesDecisionRefusal(t *testing.T) {
	for _, tc := range []struct {
		endpoint, body string
		want           bool
	}{
		{endpointTypeDecisions, `{"detail":"this runtime serves vela3-lite, not nope","code":"model_not_found"}`, true},
		{endpointTypeDecisions, `{"detail":[{"msg":"no question is valid"}],"code":"question_failed"}`, true},
		{endpointTypeDecisions, `{"detail":"Not Found"}`, false},
		{endpointTypeDecisions, `{"code":"question_failed","detail":""}`, false},
		{endpointTypeDecisions, `<html>502</html>`, false},
		{endpointTypeChat, `{"detail":"nope","code":"model_not_found"}`, false},
	} {
		if got := carriesDecisionRefusal(tc.endpoint, []byte(tc.body)); got != tc.want {
			t.Errorf("carriesDecisionRefusal(%s, %s) = %v, want %v", tc.endpoint, tc.body, got, tc.want)
		}
	}
}
