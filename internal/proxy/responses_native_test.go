package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// The passthrough is chosen per attempt: a Responses-in request, an
// openai-typed candidate, and OpenAI's own host. A relay of the same type
// translates, and a request that came in as chat never passes through.
func TestBuildCandidateRequest_ResponsesNativeGate(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	mk := func(baseURL, ptype string) modelCandidate {
		return modelCandidate{
			model:    &model.Model{ID: uuid.New(), ModelID: "gpt-5.6-sol"},
			provider: &provider.Provider{ID: uuid.New(), Name: "P", BaseURL: baseURL, ProviderType: ptype},
			apiKey:   "sk-test",
		}
	}
	raw := []byte(`{"model":"OpenAI/gpt-5.6-sol","input":"hi","tools":[{"type":"web_search"}]}`)
	chat := []byte(`{"model":"OpenAI/gpt-5.6-sol","messages":[{"role":"user","content":"hi"}]}`)
	cases := []struct {
		name       string
		st         *requestState
		cand       modelCandidate
		wantNative bool
	}{
		{"openai host", &requestState{responsesIn: true, responsesRawBody: raw, bodyBytes: chat, logData: &requestLogData{}}, mk("https://api.openai.com/v1", "openai"), true},
		{"openai-typed relay", &requestState{responsesIn: true, responsesRawBody: raw, bodyBytes: chat, logData: &requestLogData{}}, mk("https://llm.corp.internal/v1", "openai"), false},
		{"other provider type", &requestState{responsesIn: true, responsesRawBody: raw, bodyBytes: chat, logData: &requestLogData{}}, mk("https://api.openai.com/v1", "openrouter"), false},
		{"chat in", &requestState{bodyBytes: chat, logData: &requestLogData{}}, mk("https://api.openai.com/v1", "openai"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req, _, url, err := h.buildCandidateRequest(context.Background(), tc.st, tc.cand)
			if err != nil {
				t.Fatal(err)
			}
			if tc.st.responsesNativeAttempt != tc.wantNative {
				t.Fatalf("responsesNativeAttempt = %v, want %v", tc.st.responsesNativeAttempt, tc.wantNative)
			}
			body, _ := io.ReadAll(req.Body)
			if tc.wantNative {
				if !strings.HasSuffix(url, "/v1/responses") {
					t.Errorf("url = %q", url)
				}
				if !strings.Contains(string(body), `"input":"hi"`) || !strings.Contains(string(body), `"model":"gpt-5.6-sol"`) || !strings.Contains(string(body), "web_search") {
					t.Errorf("native body must be the original with the model rewritten: %s", body)
				}
				if req.Header.Get("Authorization") != "Bearer sk-test" {
					t.Errorf("auth = %q", req.Header.Get("Authorization"))
				}
				if tc.st.sentChatCompletionsBody() {
					t.Error("a native attempt did not send a chat body")
				}
			} else if strings.Contains(string(body), `"input"`) {
				t.Errorf("translated attempt must send the chat body: %s", body)
			}
		})
	}
}

// The native non-streaming handler forwards the Response verbatim and meters
// from its usage block, cache split included.
func TestHandleNativeNonStreaming_Responses(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	nativeBody := `{"id":"resp_up","object":"response","status":"completed","output":[{"type":"message","content":[{"type":"output_text","text":"hi"}]}],"usage":{"input_tokens":9,"input_tokens_details":{"cached_tokens":4},"output_tokens":3,"total_tokens":12}}`
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(nativeBody)), Header: make(http.Header)}
	rec := httptest.NewRecorder()
	native := true
	aw := newResponsesResponseWriter(rec, "resp_ignored", "m", nil)
	aw.bindNativeFlag(&native)
	logData := &requestLogData{id: uuid.New().String(), modelID: "gpt-x", virtualKeyName: "test-key", virtualKeyID: "00000000-0000-0000-0000-000000000001", state: "streaming"}
	st := &requestState{startTime: time.Now(), logData: logData}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)

	outcome := h.handleNativeNonStreaming(aw, httptest.NewRequest("POST", "/v1/responses", http.NoBody), st, modelCandidate{}, responsesNative, resp, 1, 10.0, false)
	aw.Finalize()
	if outcome != outcomeServed || logData.state != "completed" {
		t.Errorf("outcome=%v state=%q", outcome, logData.state)
	}
	if logData.tokensPrompt != 9 || logData.tokensCompletion != 3 || logData.tokensPromptCacheHit != 4 || logData.tokensPromptCacheMiss != 5 {
		t.Errorf("usage = %d/%d hit=%d miss=%d", logData.tokensPrompt, logData.tokensCompletion, logData.tokensPromptCacheHit, logData.tokensPromptCacheMiss)
	}
	if rec.Body.String() != nativeBody {
		t.Errorf("verbatim body mismatch:\n got %s\nwant %s", rec.Body.String(), nativeBody)
	}
	if !logData.deliveredContent {
		t.Error("an output item is delivered content")
	}
}

// An empty native answer goes to a sibling while there is one, and an unreadable
// one fails in the Responses error shape when there is none.
func TestHandleNativeNonStreaming_ResponsesEmptyAndUnreadable(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	newState := func() (*requestState, *requestLogData) {
		logData := &requestLogData{id: uuid.New().String(), modelID: "gpt-x", virtualKeyName: "test-key", virtualKeyID: "00000000-0000-0000-0000-000000000001", state: "streaming"}
		h.insertRequestLogAsync(logData)
		time.Sleep(50 * time.Millisecond)
		return &requestState{startTime: time.Now(), logData: logData}, logData
	}
	st, _ := newState()
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":"resp_up","object":"response","output":[],"usage":{"input_tokens":9,"output_tokens":0}}`)), Header: make(http.Header)}
	rec := httptest.NewRecorder()
	if got := h.handleNativeNonStreaming(rec, httptest.NewRequest("POST", "/v1/responses", http.NoBody), st, modelCandidate{model: &model.Model{ID: uuid.New()}, provider: &provider.Provider{ID: uuid.New()}}, responsesNative, resp, 1, 10.0, true); got != outcomeFailover {
		t.Errorf("empty output with a sibling: outcome = %v, want failover", got)
	}
	// A stated status is the provider saying it finished: served, not routed.
	st, logData := newState()
	resp = &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"id":"resp_up","object":"response","status":"incomplete","output":[]}`)), Header: make(http.Header)}
	if got := h.handleNativeNonStreaming(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/responses", http.NoBody), st, modelCandidate{model: &model.Model{ID: uuid.New()}, provider: &provider.Provider{ID: uuid.New()}}, responsesNative, resp, 1, 10.0, true); got != outcomeServed || logData.state != "completed" {
		t.Errorf("stated status: outcome = %v state=%s", got, logData.state)
	}
	st, logData = newState()
	resp = &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(iotest{}), Header: make(http.Header)}
	rec = httptest.NewRecorder()
	if got := h.handleNativeNonStreaming(rec, httptest.NewRequest("POST", "/v1/responses", http.NoBody), st, modelCandidate{}, responsesNative, resp, 1, 10.0, false); got != outcomeFatal {
		t.Errorf("unreadable: outcome = %v", got)
	}
	if rec.Code != http.StatusBadGateway || logData.state != "failed" {
		t.Errorf("code=%d state=%s", rec.Code, logData.state)
	}
	if msg := openAIErrorMessage(t, rec.Body.Bytes()); !strings.Contains(msg, "failed to read upstream response") {
		t.Errorf("message = %q", msg)
	}
}

func runResponsesNativeStream(t *testing.T, sseBody string) (*httptest.ResponseRecorder, *requestLogData) {
	t.Helper()
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	resp := &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(sseBody))}
	w := httptest.NewRecorder()
	logData := &requestLogData{id: uuid.New().String(), modelID: "gpt-x", streaming: true, virtualKeyName: "test-key", virtualKeyID: "00000000-0000-0000-0000-000000000001", state: "streaming"}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)
	opts := streamOptions{responseHeaderMs: 10.0, vkHash: "test-hash", attempt: 1, rawPassthrough: responsesNative}
	h.handleStreamingResponse(w, httptest.NewRequest("POST", "/v1/responses", http.NoBody), logData, resp, time.Now(), opts)
	return w, logData
}

const responsesStreamHead = `event: response.created
data: {"type":"response.created","sequence_number":0,"response":{"id":"resp_up","status":"in_progress","output":[]}}

event: response.output_text.delta
data: {"type":"response.output_text.delta","sequence_number":1,"item_id":"msg_1","output_index":0,"content_index":0,"delta":"Hello"}

`

func TestResponsesNativeStream_CompletedWithUsage(t *testing.T) {
	body := responsesStreamHead + "event: response.completed\ndata: {\"type\":\"response.completed\",\"sequence_number\":2,\"response\":{\"id\":\"resp_up\",\"status\":\"completed\",\"usage\":{\"input_tokens\":12,\"input_tokens_details\":{\"cached_tokens\":2},\"output_tokens\":5}}}\n\n"
	w, logData := runResponsesNativeStream(t, body)
	if logData.state != "completed" {
		t.Errorf("state = %q (err: %s)", logData.state, logData.errorMessage)
	}
	if logData.tokensPrompt != 12 || logData.tokensCompletion != 5 || logData.tokensPromptCacheHit != 2 || logData.tokensPromptCacheMiss != 10 {
		t.Errorf("usage = %d/%d hit=%d miss=%d", logData.tokensPrompt, logData.tokensCompletion, logData.tokensPromptCacheHit, logData.tokensPromptCacheMiss)
	}
	out := w.Body.String()
	for _, want := range []string{"event: response.created", "event: response.output_text.delta", `"delta":"Hello"`, "event: response.completed"} {
		if !strings.Contains(out, want) {
			t.Errorf("forwarded body missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "[DONE]") {
		t.Errorf("native stream must not inject [DONE]:\n%s", out)
	}
	if !logData.deliveredContent {
		t.Error("delta bytes are delivered content")
	}
}

func TestResponsesNativeStream_TruncatedBeforeCompleted(t *testing.T) {
	w, logData := runResponsesNativeStream(t, responsesStreamHead)
	if logData.state != "failed" || logData.errorKind != KindProviderError || !strings.Contains(logData.errorMessage, "response.completed") {
		t.Errorf("state=%q kind=%v msg=%q", logData.state, logData.errorKind, logData.errorMessage)
	}
	// The terminal frame the client sees is in dialect: a response.failed that
	// continues the forwarded stream's id and sequence.
	if out := w.Body.String(); !strings.Contains(out, "event: response.failed") || strings.Contains(out, "[DONE]") || !strings.Contains(out, `"sequence_number":2,"type":"response.failed"`) || !strings.Contains(out, `"id":"resp_up"`) {
		t.Errorf("terminal frame:\n%s", out)
	}
}

func TestResponsesNativeStream_FailedEventLogsFailed(t *testing.T) {
	body := responsesStreamHead + "event: response.failed\ndata: {\"type\":\"response.failed\",\"sequence_number\":2,\"response\":{\"id\":\"resp_up\",\"status\":\"failed\",\"error\":{\"code\":\"server_error\",\"message\":\"upstream boom\"}}}\n\n"
	w, logData := runResponsesNativeStream(t, body)
	if logData.state != "failed" || !strings.Contains(logData.errorMessage, "upstream boom") {
		t.Errorf("state=%q msg=%q", logData.state, logData.errorMessage)
	}
	// Forwarded once, verbatim, and no second terminal frame behind it.
	if out := w.Body.String(); strings.Count(out, "event: response.failed") != 1 {
		t.Errorf("terminal frames:\n%s", out)
	}
}

// cancelOnTerminalWriter is a client that hangs up the moment the terminal
// event reaches it, as Codex does on response.completed.
type cancelOnTerminalWriter struct {
	*httptest.ResponseRecorder
	cancel context.CancelFunc
}

func (w *cancelOnTerminalWriter) Write(p []byte) (int, error) {
	n, err := w.ResponseRecorder.Write(p)
	if strings.Contains(string(p), `"type":"response.completed"`) {
		w.cancel()
	}
	return n, err
}

// The terminal event ends the read: a client that hangs up right after
// response.completed (Codex does) was logged as a disconnect on a request it
// was fully served, because the loop read on for the upstream's EOF and found
// the client gone first.
func TestResponsesNativeStream_ClientHangsUpOnTerminalEvent(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write([]byte(responsesStreamHead + "event: response.completed\ndata: {\"type\":\"response.completed\",\"sequence_number\":2,\"response\":{\"id\":\"resp_up\",\"status\":\"completed\",\"usage\":{\"input_tokens\":12,\"output_tokens\":5}}}\n\n"))
		// The upstream lingers past its terminal event before closing.
		time.Sleep(300 * time.Millisecond)
		_ = pw.Close()
	}()
	logData := &requestLogData{id: uuid.New().String(), modelID: "gpt-x", streaming: true, virtualKeyName: "test-key", virtualKeyID: "00000000-0000-0000-0000-000000000001", state: "streaming"}
	h.insertRequestLogAsync(logData)
	time.Sleep(100 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest("POST", "/v1/responses", http.NoBody).WithContext(ctx)
	w := &cancelOnTerminalWriter{ResponseRecorder: httptest.NewRecorder(), cancel: cancel}
	h.handleStreamingResponse(w, req, logData, &http.Response{StatusCode: http.StatusOK, Body: pr}, time.Now(), streamOptions{responseHeaderMs: 10, vkHash: "test-hash", attempt: 1, rawPassthrough: responsesNative, streamStallTimeout: 5 * time.Second})
	if logData.state != "completed" || logData.tokensCompletion != 5 || logData.errorKind != "" {
		t.Errorf("state=%q kind=%q msg=%q tokens=%d", logData.state, logData.errorKind, logData.errorMessage, logData.tokensCompletion)
	}
}
