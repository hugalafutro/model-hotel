package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// doMessagesRequest sends an Anthropic /v1/messages request through the proxy
// with the env's virtual key attached, returning the recorder. Mirrors
// doTransientChatRequest but for the native Messages surface.
func doMessagesRequest(env *testProxyEnv, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/v1/messages", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	env.Handler.Messages(w, req)
	return w
}

// End-to-end: an Anthropic Messages request to a non-Anthropic provider runs the
// full pipeline and the OpenAI response is translated back to an Anthropic
// message (the translated path; the upstream here is a generic OpenAI server).
func TestMessages_E2E_NonStreamingTranslated(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-x", "object": "chat.completion", "created": time.Now().Unix(),
			"model": reqBody["model"],
			"choices": []map[string]any{
				{"index": 0, "message": map[string]any{"role": "assistant", "content": "hi from upstream"}, "finish_reason": "stop"},
			},
			"usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 3, "total_tokens": 8},
		})
	}))
	defer upstream.Close()

	env := newTestProxyEnvWithUpstream(t, upstream)
	body := `{"model":"` + env.ProviderName + `/` + env.ModelName + `","max_tokens":50,"messages":[{"role":"user","content":"hello"}]}`
	w := doMessagesRequest(env, body)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid Anthropic response: %v\n%s", err, w.Body.String())
	}
	if resp["type"] != "message" || resp["role"] != "assistant" {
		t.Errorf("envelope = %v", resp)
	}
	if resp["model"] != env.ProviderName+"/"+env.ModelName {
		t.Errorf("model = %v, want echoed request model (translated path)", resp["model"])
	}
	content := resp["content"].([]any)
	if content[0].(map[string]any)["text"] != "hi from upstream" {
		t.Errorf("content = %v", content)
	}
	if resp["stop_reason"] != "end_turn" {
		t.Errorf("stop_reason = %v, want end_turn", resp["stop_reason"])
	}
	usage := resp["usage"].(map[string]any)
	if usage["input_tokens"].(float64) != 5 || usage["output_tokens"].(float64) != 3 {
		t.Errorf("usage = %v", usage)
	}
}

// End-to-end streaming: the OpenAI chunk stream is translated to the Anthropic
// SSE event sequence on the way out.
func TestMessages_E2E_StreamingTranslated(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		for _, chunk := range []string{
			`{"choices":[{"delta":{"content":"Hi"}}]}`,
			`{"choices":[{"delta":{"content":" there"},"finish_reason":"stop"}]}`,
		} {
			_, _ = w.Write([]byte("data: " + chunk + "\n\n"))
			if fl != nil {
				fl.Flush()
			}
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		if fl != nil {
			fl.Flush()
		}
	}))
	defer upstream.Close()

	env := newTestProxyEnvWithUpstream(t, upstream)
	body := `{"model":"` + env.ProviderName + `/` + env.ModelName + `","max_tokens":50,"stream":true,"messages":[{"role":"user","content":"hello"}]}`
	w := doMessagesRequest(env, body)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	out := w.Body.String()
	for _, want := range []string{"event: message_start", "event: content_block_delta", "text_delta", "Hi", "event: message_stop"} {
		if !strings.Contains(out, want) {
			t.Errorf("streamed Anthropic output missing %q\n%s", want, out)
		}
	}
}
