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

// translateGeminiResponseBody swaps a generateContent 200 body for its chat
// translation in place, and errors on a non-Gemini body so the caller can
// fail over instead of forwarding garbage.
func TestTranslateGeminiResponseBody(t *testing.T) {
	resp := &http.Response{Body: newBodyReader(`{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`)}
	if err := translateGeminiResponseBody(resp, "gemini-2.5-flash"); err != nil {
		t.Fatalf("translateGeminiResponseBody: %v", err)
	}
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("translated body not JSON: %v", err)
	}
	if out["object"] != "chat.completion" || out["model"] != "gemini-2.5-flash" {
		t.Errorf("translated = %v", out)
	}

	// Zero-candidate body (e.g. blocked prompt) must error, not forward.
	resp = &http.Response{Body: newBodyReader(`{"promptFeedback":{"blockReason":"SAFETY"}}`)}
	if err := translateGeminiResponseBody(resp, "m"); err == nil {
		t.Error("expected error for candidate-less body")
	}
	resp = &http.Response{Body: newBodyReader(`not json`)}
	if err := translateGeminiResponseBody(resp, "m"); err == nil {
		t.Error("expected error for invalid JSON body")
	}
}

func newBodyReader(s string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(s))
}

// TestChatCompletions_VertexExpressEgress drives a chat request through the
// real ChatCompletions pipeline against a fake Vertex upstream: the provider's
// base URL detects as vertex-express (aiplatform host), while a DialContext
// override routes the TCP connection to the httptest server. Proves the whole
// egress adapter chain: generateContent body + native route + x-goog-api-key
// on the way out, chat.completion(.chunk) translation on the way back.
func TestChatCompletions_VertexExpressEgress(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "test-api-key" {
			t.Errorf("x-goog-api-key = %q", r.Header.Get("x-goog-api-key"))
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected Authorization header %q", r.Header.Get("Authorization"))
		}

		var reqBody map[string]any
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		if _, hasContents := reqBody["contents"]; !hasContents {
			t.Errorf("upstream got untranslated body: %v", reqBody)
		}
		if _, hasMessages := reqBody["messages"]; hasMessages {
			t.Errorf("OpenAI messages leaked upstream: %v", reqBody)
		}

		switch {
		case strings.HasSuffix(r.URL.Path, ":streamGenerateContent"):
			if r.URL.Query().Get("alt") != "sse" {
				t.Errorf("streaming call missing alt=sse: %s", r.URL.RawQuery)
			}
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `data: {"candidates":[{"content":{"role":"model","parts":[{"text":"hello"}]}}]}`+"\n\n")
			fmt.Fprint(w, `data: {"candidates":[{"content":{"role":"model","parts":[{"text":" vertex"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2,"totalTokenCount":7}}`+"\n\n")
		case strings.HasSuffix(r.URL.Path, ":generateContent"):
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"hello vertex"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":5,"candidatesTokenCount":2,"totalTokenCount":7}}`))
		default:
			t.Errorf("unexpected upstream path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()

	env := newTestProxyEnvWithUpstream(t, upstream)

	// Re-point the provider at an aiplatform base URL so the candidate detects
	// as vertex-express, and pin the transport's dialer to the test server so
	// the request still lands there.
	pool := testDB.Pool()
	if _, err := pool.Exec(context.Background(),
		`UPDATE providers SET base_url = 'http://aiplatform.googleapis.com' WHERE id = $1`, env.ProviderID); err != nil {
		t.Fatalf("failed to update provider base URL: %v", err)
	}
	provider.InvalidateProviderCache()
	target := upstream.Listener.Addr().String()
	env.Handler.upstreamTransport = &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, target)
		},
	}

	send := func(stream bool) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"model":"%s/%s","stream":%v,"messages":[{"role":"user","content":"hi"}]}`,
			env.ProviderName, env.ModelName, stream)
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
		ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
		ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		env.Handler.ChatCompletions(w, req)
		return w
	}

	// Non-streaming: translated back to a chat.completion.
	w := send(false)
	if w.Code != http.StatusOK {
		t.Fatalf("non-streaming: %d\n%s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response not JSON: %v\n%s", err, w.Body.String())
	}
	if resp["object"] != "chat.completion" {
		t.Errorf("object = %v", resp["object"])
	}
	choice := resp["choices"].([]any)[0].(map[string]any)
	if choice["message"].(map[string]any)["content"] != "hello vertex" {
		t.Errorf("content = %v", choice["message"])
	}
	if choice["finish_reason"] != "stop" {
		t.Errorf("finish_reason = %v", choice["finish_reason"])
	}
	if resp["usage"].(map[string]any)["total_tokens"] != float64(7) {
		t.Errorf("usage = %v", resp["usage"])
	}

	// Streaming: translated chunk stream ending in [DONE].
	w = send(true)
	if w.Code != http.StatusOK {
		t.Fatalf("streaming: %d\n%s", w.Code, w.Body.String())
	}
	sse := w.Body.String()
	if !strings.Contains(sse, `"content":"hello"`) || !strings.Contains(sse, `"content":" vertex"`) {
		t.Errorf("content deltas missing:\n%s", sse)
	}
	if strings.Count(sse, `"role":"assistant"`) != 1 {
		t.Errorf("role deltas != 1:\n%s", sse)
	}
	if !strings.Contains(sse, `"finish_reason":"stop"`) || !strings.Contains(sse, "data: [DONE]") {
		t.Errorf("terminal chunks missing:\n%s", sse)
	}
	if !strings.Contains(sse, `"total_tokens":7`) {
		t.Errorf("usage missing:\n%s", sse)
	}
}
