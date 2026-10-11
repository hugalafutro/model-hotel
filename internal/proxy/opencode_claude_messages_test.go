package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// newOpenCodeGoClaudeEnv turns the integration env into an OpenCode Go provider
// (type inferred from the /zen/go base path) serving claude-haiku-5-5, with
// upstream dials redirected to the mock server, as newAnthropicEgressEnv does.
func newOpenCodeGoClaudeEnv(t *testing.T, upstream *httptest.Server) *testProxyEnv {
	t.Helper()
	env := newTestProxyEnvWithUpstream(t, upstream)
	pool := testDB.Pool()
	if _, err := pool.Exec(context.Background(),
		`UPDATE providers SET base_url = 'http://opencode.ai/zen/go/v1' WHERE id = $1`, env.ProviderID); err != nil {
		t.Fatalf("failed to update provider base URL: %v", err)
	}
	if _, err := pool.Exec(context.Background(),
		`UPDATE models SET model_id = 'claude-haiku-5-5' WHERE id = $1`, env.ModelID); err != nil {
		t.Fatalf("failed to rename model: %v", err)
	}
	env.ModelName = "claude-haiku-5-5"
	provider.InvalidateProviderCache()
	model.InvalidateModelCache()
	target := upstream.Listener.Addr().String()
	env.Handler.upstreamTransport = &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, target)
		},
	}
	return env
}

// openCodeMessagesUpstream answers like OpenCode Go's /zen/go/v1/messages:
// Anthropic Messages shape, x-api-key auth. Any other path is the chat route,
// which refuses a Claude model with the real 400 body.
func openCodeMessagesUpstream(t *testing.T, log *upstreamCallLog, sessions *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		log.record(anthropicUpstreamCall{path: r.URL.Path, body: body})
		if r.URL.Path != "/zen/go/v1/messages" {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"ModelProtocolUnsupported","message":"Model does not support this protocol."}}`))
			return
		}
		if r.Header.Get("x-api-key") != "test-api-key" {
			t.Errorf("x-api-key = %q, want the provider key", r.Header.Get("x-api-key"))
		}
		if r.Header.Get("anthropic-version") == "" {
			t.Error("anthropic-version header missing")
		}
		*sessions = append(*sessions, r.Header.Get(util.OpenCodeGoSessionHeader))
		if stream, _ := body["stream"].(bool); stream {
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			for _, ev := range []string{
				`{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-5-5","content":[],"usage":{"input_tokens":9,"output_tokens":1}}}`,
				`{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`,
				`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"PLUM-7431"}}`,
				`{"type":"content_block_stop","index":0}`,
				`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4}}`,
				`{"type":"message_stop"}`,
			} {
				fmt.Fprint(w, "data: "+ev+"\n\n")
			}
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","model":"claude-haiku-5-5","content":[{"type":"text","text":"PLUM-7431"}],"stop_reason":"end_turn","usage":{"input_tokens":9,"output_tokens":4}}`))
	}))
}

// TestChatCompletions_OpenCodeGoClaudeTakesMessages: a plain chat request to a
// Claude model on OpenCode Go never touches the chat route (which would refuse
// it with "Model does not support this protocol"). It goes to /zen/go/v1/messages
// translated, carrying x-api-key and the Go session header, and the Messages
// answer comes back as a chat completion, streaming included.
func TestChatCompletions_OpenCodeGoClaudeTakesMessages(t *testing.T) {
	var log upstreamCallLog
	var sessions []string
	upstream := openCodeMessagesUpstream(t, &log, &sessions)
	defer upstream.Close()
	env := newOpenCodeGoClaudeEnv(t, upstream)

	send := func(stream bool) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"model":"%s/%s","stream":%v,"messages":[{"role":"user","content":"what is the code?"}]}`,
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

	w := send(false)
	if w.Code != http.StatusOK {
		t.Fatalf("non-streaming: %d\n%s", w.Code, w.Body.String())
	}
	call := log.takeOne(t, "non-streaming")
	if call.path != "/zen/go/v1/messages" {
		t.Fatalf("request went to %s, want /zen/go/v1/messages", call.path)
	}
	if call.body["model"] != "claude-haiku-5-5" {
		t.Errorf("upstream model = %v, want the resolved id", call.body["model"])
	}
	if _, hasMessages := call.body["messages"]; !hasMessages {
		t.Errorf("translated body has no messages: %v", call.body)
	}
	if !strings.Contains(w.Body.String(), `"object":"chat.completion"`) || !strings.Contains(w.Body.String(), "PLUM-7431") {
		t.Errorf("answer not translated back to a chat completion: %s", w.Body.String())
	}

	w = send(true)
	if w.Code != http.StatusOK {
		t.Fatalf("streaming: %d\n%s", w.Code, w.Body.String())
	}
	call = log.takeOne(t, "streaming")
	if call.path != "/zen/go/v1/messages" || call.body["stream"] != true {
		t.Fatalf("streaming request: path %s stream %v, want /zen/go/v1/messages with stream true", call.path, call.body["stream"])
	}
	if !strings.Contains(w.Body.String(), "chat.completion.chunk") || !strings.Contains(w.Body.String(), "PLUM-7431") {
		t.Errorf("stream not translated back to chat chunks: %s", w.Body.String())
	}

	if len(sessions) != 2 || sessions[0] == "" || sessions[0] != sessions[1] {
		t.Errorf("Go session header: want the same non-empty value on both calls, got %q", sessions)
	}
}

// TestMessages_OpenCodeGoClaudeStaysNative: an Anthropic-in request to a Claude
// model on OpenCode Go is forwarded verbatim to /zen/go/v1/messages with the
// Messages auth headers, exactly as it is to Anthropic itself.
func TestMessages_OpenCodeGoClaudeStaysNative(t *testing.T) {
	var log upstreamCallLog
	var sessions []string
	upstream := openCodeMessagesUpstream(t, &log, &sessions)
	defer upstream.Close()
	env := newOpenCodeGoClaudeEnv(t, upstream)

	body := fmt.Sprintf(`{"model":"%s/%s","max_tokens":64,"messages":[{"role":"user","content":"what is the code?"}]}`, env.ProviderName, env.ModelName)
	w := doMessagesRequest(env, body)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body = %s", w.Code, w.Body.String())
	}
	call := log.takeOne(t, "anthropic-in")
	if call.path != "/zen/go/v1/messages" {
		t.Fatalf("anthropic-in request went to %s, want /zen/go/v1/messages", call.path)
	}
	if call.body["max_tokens"] != float64(64) || call.body["model"] != "claude-haiku-5-5" {
		t.Errorf("body not forwarded verbatim with the resolved model: %v", call.body)
	}
	if len(sessions) != 1 || sessions[0] == "" {
		t.Errorf("Go session header missing on the native route: %q", sessions)
	}
	if !strings.Contains(w.Body.String(), `"type":"message"`) {
		t.Errorf("native answer not passed through: %s", w.Body.String())
	}
}
