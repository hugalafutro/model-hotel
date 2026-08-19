package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/provider"
)

// pdfDataURI is a minimal base64 data: URI standing in for an uploaded PDF.
// Only its media type matters to the adapter; the payload rides through as
// opaque base64.
const pdfDataURI = "data:application/pdf;base64,JVBERi0xLjQK"

// isAnthropicEgressAttempt gates the whole adapter, so each disqualifier is
// worth pinning: the compat endpoint stays in charge of everything it can
// actually express, and an Anthropic-in request must reach the verbatim
// passthrough rather than being translated a second time.
func TestIsAnthropicEgressAttempt(t *testing.T) {
	docBody := []byte(`{"model":"m","messages":[{"role":"user","content":[{"type":"file","file":{"file_data":"` + pdfDataURI + `"}}]}]}`)
	textBody := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)

	tests := []struct {
		name         string
		st           *requestState
		providerType string
		want         bool
	}{
		{"document to anthropic", &requestState{bodyBytes: docBody}, "anthropic", true},
		{"text to anthropic", &requestState{bodyBytes: textBody}, "anthropic", false},
		{"document to another provider", &requestState{bodyBytes: docBody}, "openai", false},
		{"anthropic-in document", &requestState{bodyBytes: docBody, anthropicIn: true}, "anthropic", false},
		{"endpoint override", &requestState{bodyBytes: docBody, endpointPath: "/audio/speech"}, "anthropic", false},
		{"multimodal body builder", &requestState{
			bodyBytes:        docBody,
			makeUpstreamBody: func(string) ([]byte, string, error) { return nil, "", nil },
		}, "anthropic", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isAnthropicEgressAttempt(tt.st, tt.providerType); got != tt.want {
				t.Errorf("isAnthropicEgressAttempt = %v, want %v", got, tt.want)
			}
		})
	}
}

// translateAnthropicEgressResponseBody swaps a Messages 200 body for its chat
// translation in place, and errors on anything that is not a Messages response
// so the caller fails over instead of forwarding garbage.
func TestTranslateAnthropicEgressResponseBody(t *testing.T) {
	resp := &http.Response{Body: newBodyReader(`{"id":"msg_1","type":"message","role":"assistant","model":"claude","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`)}
	if err := translateAnthropicEgressResponseBody(resp, "claude-sonnet-4-5"); err != nil {
		t.Fatalf("translateAnthropicEgressResponseBody: %v", err)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("translated body not JSON: %v", err)
	}
	if out["object"] != "chat.completion" || out["model"] != "claude-sonnet-4-5" {
		t.Errorf("translated = %v", out)
	}
	if id, _ := out["id"].(string); !strings.HasPrefix(id, "chatcmpl-") {
		t.Errorf("id = %q, want a chatcmpl- id", id)
	}

	// An Anthropic error envelope must not become a synthetic empty success.
	resp = &http.Response{Body: newBodyReader(`{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)}
	if err := translateAnthropicEgressResponseBody(resp, "m"); err == nil {
		t.Error("expected error for an error envelope")
	}
	resp = &http.Response{Body: newBodyReader(`not json`)}
	if err := translateAnthropicEgressResponseBody(resp, "m"); err == nil {
		t.Error("expected error for invalid JSON body")
	}
}

// The retirement probe reads the flag buildCandidateRequest set, so a probe
// that ever routes through the adapter translates its answer before judging it.
func TestTranslateProbeDialect_AnthropicEgress(t *testing.T) {
	resp := &http.Response{Body: newBodyReader(`{"id":"msg_1","type":"message","role":"assistant","model":"claude","content":[{"type":"text","text":"ok"}],"stop_reason":"end_turn"}`)}
	st := &requestState{anthropicEgressAttempt: true}
	if err := translateProbeDialect(resp, st, "claude-sonnet-4-5"); err != nil {
		t.Fatalf("translateProbeDialect: %v", err)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("reading translated probe body: %v", err)
	}
	if !probeDeliveredContent(endpointTypeChat, body) {
		t.Errorf("translated probe answer read as empty: %s", body)
	}
}

// newAnthropicEgressEnv builds a test env whose provider detects as Anthropic
// (api.anthropic.com base URL) while a pinned dialer routes the TCP connection
// to the supplied test server.
func newAnthropicEgressEnv(t *testing.T, upstream *httptest.Server) *testProxyEnv {
	t.Helper()
	env := newTestProxyEnvWithUpstream(t, upstream)
	pool := testDB.Pool()
	if _, err := pool.Exec(context.Background(),
		`UPDATE providers SET base_url = 'http://api.anthropic.com' WHERE id = $1`, env.ProviderID); err != nil {
		t.Fatalf("failed to update provider base URL: %v", err)
	}
	provider.InvalidateProviderCache()
	target := upstream.Listener.Addr().String()
	env.Handler.upstreamTransport = &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, target)
		},
	}
	return env
}

// anthropicUpstreamCall records which route one upstream call reached and the
// body it carried, so the tests can assert the routing decision as well as the
// wire shape.
type anthropicUpstreamCall struct {
	path string
	body map[string]any
}

// TestChatCompletions_AnthropicEgress drives chat requests through the real
// ChatCompletions pipeline against a fake Anthropic upstream. It proves the
// whole adapter chain: a document-bearing request is translated to a Messages
// body on /v1/messages and translated back to a chat.completion (streaming: to
// chunks ending in [DONE]), while a text-only request to the same provider
// stays on the untouched compat route.
func TestChatCompletions_AnthropicEgress(t *testing.T) {
	var calls []anthropicUpstreamCall
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "test-api-key" {
			t.Errorf("x-api-key = %q", r.Header.Get("x-api-key"))
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("anthropic-version header missing")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		calls = append(calls, anthropicUpstreamCall{path: r.URL.Path, body: body})

		if r.URL.Path != "/v1/messages" {
			// The compat route: answer in OpenAI shape, as Anthropic does.
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"chatcmpl-compat","object":"chat.completion","created":1,"model":"claude","choices":[{"index":0,"message":{"role":"assistant","content":"plain text answer"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`))
			return
		}
		if stream, _ := body["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			for _, ev := range []string{
				`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude","content":[],"usage":{"input_tokens":9,"output_tokens":1}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"PLUM"}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"-7431"}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}`,
				`{"type":"message_stop"}`,
			} {
				fmt.Fprint(w, "data: "+ev+"\n\n")
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude","content":[{"type":"text","text":"PLUM-7431"}],"stop_reason":"end_turn","usage":{"input_tokens":9,"output_tokens":4}}`))
	}))
	defer upstream.Close()

	env := newAnthropicEgressEnv(t, upstream)

	send := func(stream bool, content string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"model":"%s/%s","stream":%v,"messages":[{"role":"user","content":%s}]}`,
			env.ProviderName, env.ModelName, stream, content)
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
		ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
		ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		env.Handler.ChatCompletions(w, req)
		return w
	}
	documentContent := `[{"type":"text","text":"what is the code?"},{"type":"file","file":{"file_data":"` + pdfDataURI + `"}}]`

	// Non-streaming document: native route, Anthropic body, chat.completion back.
	w := send(false, documentContent)
	if w.Code != http.StatusOK {
		t.Fatalf("non-streaming: %d\n%s", w.Code, w.Body.String())
	}
	if len(calls) != 1 {
		t.Fatalf("upstream calls = %d, want 1", len(calls))
	}
	if calls[0].path != "/v1/messages" {
		t.Fatalf("document request went to %s, want /v1/messages", calls[0].path)
	}
	if _, hasMessages := calls[0].body["messages"]; !hasMessages {
		t.Errorf("translated body has no messages: %v", calls[0].body)
	}
	if calls[0].body["model"] != env.ModelName {
		t.Errorf("upstream model = %v, want the resolved id %q", calls[0].body["model"], env.ModelName)
	}
	if _, hasMaxTokens := calls[0].body["max_tokens"]; !hasMaxTokens {
		t.Errorf("Messages body missing the required max_tokens: %v", calls[0].body)
	}
	assertDocumentBlockPresent(t, calls[0].body)

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not JSON: %v\n%s", err, w.Body.String())
	}
	if resp["object"] != "chat.completion" {
		t.Errorf("object = %v", resp["object"])
	}
	choice := resp["choices"].([]any)[0].(map[string]any)
	if choice["message"].(map[string]any)["content"] != "PLUM-7431" {
		t.Errorf("content = %v", choice["message"])
	}
	if choice["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v", choice["finish_reason"])
	}
	if resp["usage"].(map[string]any)["prompt_tokens"] != float64(9) {
		t.Errorf("usage = %v", resp["usage"])
	}

	// Text-only: the gate keeps it on the untouched compat route.
	calls = nil
	w = send(false, `"just text"`)
	if w.Code != http.StatusOK {
		t.Fatalf("text-only: %d\n%s", w.Code, w.Body.String())
	}
	if len(calls) != 1 {
		t.Fatalf("upstream calls = %d, want 1", len(calls))
	}
	if calls[0].path != "/v1/chat/completions" {
		t.Errorf("text-only request went to %s, want /v1/chat/completions", calls[0].path)
	}
	if _, translated := calls[0].body["max_tokens"]; translated {
		t.Errorf("text-only body was translated: %v", calls[0].body)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("text-only response not JSON: %v\n%s", err, w.Body.String())
	}
	choice = resp["choices"].([]any)[0].(map[string]any)
	if choice["message"].(map[string]any)["content"] != "plain text answer" {
		t.Errorf("text-only content = %v", choice["message"])
	}

	// Streaming document: translated chunk stream ending in [DONE].
	calls = nil
	w = send(true, documentContent)
	if w.Code != http.StatusOK {
		t.Fatalf("streaming: %d\n%s", w.Code, w.Body.String())
	}
	if len(calls) != 1 || calls[0].path != "/v1/messages" {
		t.Fatalf("streaming document calls = %+v, want one /v1/messages", calls)
	}
	if stream, _ := calls[0].body["stream"].(bool); !stream {
		t.Errorf("streaming request lost its stream flag: %v", calls[0].body)
	}
	sse := w.Body.String()
	if !strings.Contains(sse, `"object":"chat.completion.chunk"`) {
		t.Errorf("chunks not in chat shape:\n%s", sse)
	}
	if !strings.Contains(sse, `"content":"PLUM"`) || !strings.Contains(sse, `"content":"-7431"`) {
		t.Errorf("content deltas missing:\n%s", sse)
	}
	if !strings.Contains(sse, `"finish_reason":"stop"`) || !strings.Contains(sse, "data: [DONE]") {
		t.Errorf("terminal chunks missing:\n%s", sse)
	}
	if !strings.Contains(sse, `"prompt_tokens":9`) {
		t.Errorf("usage missing:\n%s", sse)
	}
}

// assertDocumentBlockPresent checks that the translated Messages body carries
// the PDF as an Anthropic document block — the whole reason this route exists.
func assertDocumentBlockPresent(t *testing.T, body map[string]any) {
	t.Helper()
	messages, _ := body["messages"].([]any)
	if len(messages) == 0 {
		t.Fatalf("translated body carries no messages: %v", body)
	}
	parts, _ := messages[0].(map[string]any)["content"].([]any)
	for _, p := range parts {
		block, _ := p.(map[string]any)
		if block["type"] != "document" {
			continue
		}
		source, _ := block["source"].(map[string]any)
		if source["media_type"] != "application/pdf" {
			t.Errorf("document source = %v, want an application/pdf source", source)
		}
		return
	}
	t.Errorf("no document block in translated content: %v", parts)
}

// An Anthropic-in request to an Anthropic provider keeps the verbatim native
// passthrough: the egress adapter must not translate a body that already speaks
// Messages, even when it carries a document.
func TestMessages_AnthropicProviderStaysNative(t *testing.T) {
	var gotPath string
	var gotBody map[string]any
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude","content":[{"type":"text","text":"native answer"}],"stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":2}}`))
	}))
	defer upstream.Close()

	env := newAnthropicEgressEnv(t, upstream)
	body := fmt.Sprintf(`{"model":"%s/%s","max_tokens":64,"messages":[{"role":"user","content":[{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"JVBERi0xLjQK"}}]}]}`,
		env.ProviderName, env.ModelName)
	w := doMessagesRequest(env, body)

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	if gotPath != "/v1/messages" {
		t.Fatalf("anthropic-in request went to %s, want /v1/messages", gotPath)
	}
	if gotBody["max_tokens"] != float64(64) {
		t.Errorf("max_tokens = %v, want the caller's 64 (verbatim passthrough)", gotBody["max_tokens"])
	}
	parts, _ := gotBody["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if len(parts) != 1 || parts[0].(map[string]any)["type"] != "document" {
		t.Errorf("content = %v, want the caller's document block untouched", parts)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not JSON: %v\n%s", err, w.Body.String())
	}
	if resp["type"] != "message" {
		t.Errorf("response envelope = %v, want a verbatim Anthropic message", resp)
	}
}
