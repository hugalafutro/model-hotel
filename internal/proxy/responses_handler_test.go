package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/openai/openai-go/v3/packages/ssestream"
	"github.com/openai/openai-go/v3/responses"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
)

// doResponsesRequest sends a /v1/responses request through the proxy with the
// env's virtual key attached. Mirrors doMessagesRequest.
func doResponsesRequest(env *testProxyEnv, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	env.Handler.Responses(w, req)
	return w
}

func openAIErrorMessage(t *testing.T, body []byte) string {
	t.Helper()
	var env struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil || env.Error.Message == "" {
		t.Fatalf("not an OpenAI error envelope: %s", body)
	}
	return env.Error.Message
}

func TestResponses_RejectionsAreOpenAI400s(t *testing.T) {
	h := &Handler{}
	for _, tc := range []struct{ body, field string }{
		{`{"model":"p/m","input":"x","previous_response_id":"resp_1"}`, "previous_response_id"},
		{`{"model":"p/m","input":"x","tools":[{"type":"custom","name":"apply_patch"}]}`, "tools[0]"},
		{`{"input":"x"}`, "model"},
		{`{not json`, ""},
	} {
		req := httptest.NewRequest("POST", "/v1/responses", strings.NewReader(tc.body))
		rec := httptest.NewRecorder()
		h.Responses(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400", tc.body, rec.Code)
		}
		msg := openAIErrorMessage(t, rec.Body.Bytes())
		if tc.field != "" && !strings.Contains(msg, tc.field) {
			t.Errorf("%s: message %q must name %s", tc.body, msg, tc.field)
		}
		// Never the document: the decode error is a content-free diagnostic.
		if strings.Contains(msg, "not json") {
			t.Errorf("message echoes the body: %q", msg)
		}
	}
}

func TestReadRawBody(t *testing.T) {
	h := &Handler{}
	cached := []byte(`{"model":"p/m"}`)
	req := httptest.NewRequest("POST", "/v1/responses", http.NoBody)
	req = req.WithContext(context.WithValue(req.Context(), ctxkeys.RequestBodyKey, cached))
	if body, ok := h.readRawBody(httptest.NewRecorder(), req); !ok || !bytes.Equal(body, cached) {
		t.Errorf("from ctx = %q, %v", body, ok)
	}
	raw := `{"model":"p/m","input":"x"}`
	req = httptest.NewRequest("POST", "/v1/responses", io.NopCloser(bytes.NewReader([]byte(raw))))
	if body, ok := h.readRawBody(httptest.NewRecorder(), req); !ok || string(body) != raw {
		t.Errorf("from body = %q, %v", body, ok)
	}
	rec := httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/v1/responses", io.NopCloser(iotest{}))
	if _, ok := h.readRawBody(rec, req); ok || rec.Code != http.StatusBadRequest {
		t.Errorf("read failure must 400: ok=%v code=%d", ok, rec.Code)
	}
}

// iotest is a body whose read fails.
type iotest struct{}

func (iotest) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

// End-to-end: a Responses request to a generic OpenAI-compatible upstream runs
// the full pipeline on the translated chat body and the chat.completion is
// rendered as a Response object.
func TestResponses_E2E_NonStreamingTranslated(t *testing.T) {
	var upstreamBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&upstreamBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-x", "object": "chat.completion", "created": time.Now().Unix(),
			"model": upstreamBody["model"],
			"choices": []map[string]any{{"index": 0, "message": map[string]any{
				"role": "assistant", "content": nil,
				"tool_calls": []map[string]any{{"id": "call_1", "type": "function", "function": map[string]any{"name": "ls", "arguments": `{"p":"."}`}}},
			}, "finish_reason": "tool_calls"}},
			"usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8},
		})
	}))
	defer upstream.Close()

	env := newTestProxyEnvWithUpstream(t, upstream)
	modelName := env.ProviderName + "/" + env.ModelName
	body := `{"model":"` + modelName + `","instructions":"be terse","input":[{"role":"user","content":"list"}],"tools":[{"type":"function","name":"ls","parameters":{"type":"object"}},{"type":"web_search"}],"store":false,"include":["reasoning.encrypted_content"]}`
	w := doResponsesRequest(env, body)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	var resp responses.Response
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("openai-go cannot decode: %v\n%s", err, w.Body.String())
	}
	if resp.Model != modelName || resp.Status != "completed" || !strings.HasPrefix(resp.ID, "resp_") {
		t.Errorf("envelope = %+v", resp)
	}
	if len(resp.Output) != 1 || resp.Output[0].AsFunctionCall().CallID != "call_1" || resp.Output[0].AsFunctionCall().Arguments != `{"p":"."}` {
		t.Errorf("output = %+v", resp.Output)
	}
	if resp.Usage.InputTokens != 5 || resp.Usage.OutputTokens != 3 {
		t.Errorf("usage = %+v", resp.Usage)
	}
	// The upstream saw a chat body: system + user, function tool, no web_search.
	msgs, _ := upstreamBody["messages"].([]any)
	if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "system" {
		t.Errorf("upstream messages = %v", upstreamBody["messages"])
	}
	if tools, _ := upstreamBody["tools"].([]any); len(tools) != 1 {
		t.Errorf("upstream tools = %v", upstreamBody["tools"])
	}
	for _, k := range []string{"input", "instructions", "include", "store"} {
		if _, ok := upstreamBody[k]; ok {
			t.Errorf("upstream must not see %s", k)
		}
	}
}

// End-to-end streaming: the OpenAI chunk stream is translated to the Responses
// event sequence and decodes through the real openai-go stream decoder.
func TestResponses_E2E_StreamingTranslated(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		for _, chunk := range []string{
			`{"choices":[{"delta":{"role":"assistant","content":"Hi"}}]}`,
			`{"choices":[{"delta":{"content":" there"},"finish_reason":"stop"}]}`,
			`{"choices":[],"usage":{"prompt_tokens":4,"completion_tokens":2,"total_tokens":6}}`,
		} {
			_, _ = w.Write([]byte("data: " + chunk + "\n\n"))
			if fl != nil {
				fl.Flush()
			}
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer upstream.Close()

	env := newTestProxyEnvWithUpstream(t, upstream)
	body := `{"model":"` + env.ProviderName + `/` + env.ModelName + `","input":"hello","stream":true}`
	w := doResponsesRequest(env, body)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("content-type = %q", ct)
	}
	stream := ssestream.NewStream[responses.ResponseStreamEventUnion](ssestream.NewDecoder(&http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(w.Body.String()))}), nil)
	var text, last string
	var final responses.Response
	for stream.Next() {
		ev := stream.Current()
		last = ev.Type
		if ev.Type == "response.output_text.delta" {
			text += ev.Delta
		}
		if ev.Type == "response.completed" {
			final = ev.Response
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("openai-go decode: %v\n%s", err, w.Body.String())
	}
	if text != "Hi there" || last != "response.completed" || final.OutputText() != "Hi there" || final.Usage.InputTokens != 4 {
		t.Errorf("text=%q last=%s final=%+v", text, last, final)
	}
	if strings.Contains(w.Body.String(), "[DONE]") {
		t.Error("a Responses stream has no [DONE] sentinel")
	}
}

// An upstream error keeps its status and envelope: the Responses API shares
// the chat error shape, so nothing is re-rendered.
func TestResponses_E2E_UpstreamErrorEnvelope(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad prompt","type":"invalid_request_error","code":null}}`))
	}))
	defer upstream.Close()
	env := newTestProxyEnvWithUpstream(t, upstream)
	w := doResponsesRequest(env, `{"model":"`+env.ProviderName+`/`+env.ModelName+`","input":"x"}`)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	if msg := openAIErrorMessage(t, w.Body.Bytes()); msg != "bad prompt" {
		t.Errorf("message = %q", msg)
	}
}
