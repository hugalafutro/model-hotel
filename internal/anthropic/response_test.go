package anthropic

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildMessageResponse_Text(t *testing.T) {
	oai := []byte(`{
		"id": "chatcmpl-1", "model": "upstream-model",
		"choices": [{"message": {"role": "assistant", "content": "Hi there"}, "finish_reason": "stop"}],
		"usage": {"prompt_tokens": 12, "completion_tokens": 4}
	}`)
	out, err := BuildMessageResponse(oai, "msg_1", "hotel/claude")
	if err != nil {
		t.Fatalf("BuildMessageResponse: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("invalid output: %v", err)
	}
	if m["id"] != "msg_1" || m["type"] != "message" || m["role"] != "assistant" {
		t.Errorf("envelope = %v", m)
	}
	if m["model"] != "hotel/claude" {
		t.Errorf("model = %v, want echoed request model", m["model"])
	}
	content := m["content"].([]any)
	if len(content) != 1 || content[0].(map[string]any)["text"] != "Hi there" {
		t.Errorf("content = %v", content)
	}
	if m["stop_reason"] != "end_turn" {
		t.Errorf("stop_reason = %v, want end_turn", m["stop_reason"])
	}
	usage := m["usage"].(map[string]any)
	if usage["input_tokens"].(float64) != 12 || usage["output_tokens"].(float64) != 4 {
		t.Errorf("usage = %v", usage)
	}
}

func TestBuildMessageResponse_ArrayContent(t *testing.T) {
	// Some OpenAI-compatible providers return content as an array of parts
	// instead of a string; the translation must extract the text, not 502.
	oai := []byte(`{"choices":[{"message":{"role":"assistant","content":[{"type":"text","text":"Hello "},{"type":"text","text":"world"}]},"finish_reason":"stop"}]}`)
	out, err := BuildMessageResponse(oai, "msg_a", "m")
	if err != nil {
		t.Fatalf("BuildMessageResponse: %v", err)
	}
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	content := m["content"].([]any)
	if len(content) != 1 || content[0].(map[string]any)["text"] != "Hello world" {
		t.Errorf("array content not flattened: %v", content)
	}
}

func TestBuildMessageResponse_NullContent(t *testing.T) {
	// content:null (tool-only assistant turn) must not panic or add an empty block.
	oai := []byte(`{"choices":[{"message":{"role":"assistant","content":null,"tool_calls":[{"id":"c","type":"function","function":{"name":"f","arguments":"{}"}}]},"finish_reason":"tool_calls"}]}`)
	out, err := BuildMessageResponse(oai, "msg_n", "m")
	if err != nil {
		t.Fatalf("BuildMessageResponse: %v", err)
	}
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	content := m["content"].([]any)
	if len(content) != 1 || content[0].(map[string]any)["type"] != "tool_use" {
		t.Errorf("null content should yield only the tool_use block: %v", content)
	}
}

func TestBuildMessageResponse_InvalidToolArgsAndError(t *testing.T) {
	// Invalid tool-call arguments fall back to an empty object.
	oai := []byte(`{"choices":[{"message":{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"f","arguments":"not json"}}]},"finish_reason":"tool_calls"}]}`)
	out, err := BuildMessageResponse(oai, "msg_x", "m")
	if err != nil {
		t.Fatalf("BuildMessageResponse: %v", err)
	}
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	input := m["content"].([]any)[0].(map[string]any)["input"].(map[string]any)
	if len(input) != 0 {
		t.Errorf("invalid tool args should yield empty input, got %v", input)
	}
	// Unparseable upstream body is an error.
	if _, err := BuildMessageResponse([]byte(`not json`), "x", "m"); err == nil {
		t.Fatal("expected error for invalid upstream response")
	}
}

func TestBuildMessageResponse_ToolUse(t *testing.T) {
	oai := []byte(`{
		"choices": [{"message": {"role":"assistant","content":"","tool_calls":[
			{"id":"call_9","type":"function","function":{"name":"lookup","arguments":"{\"q\":\"x\"}"}}
		]}, "finish_reason": "tool_calls"}],
		"usage": {"prompt_tokens": 5, "completion_tokens": 7}
	}`)
	out, err := BuildMessageResponse(oai, "msg_2", "m")
	if err != nil {
		t.Fatalf("BuildMessageResponse: %v", err)
	}
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	if m["stop_reason"] != "tool_use" {
		t.Errorf("stop_reason = %v, want tool_use", m["stop_reason"])
	}
	content := m["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content len = %d, want 1 (empty text dropped)", len(content))
	}
	tu := content[0].(map[string]any)
	if tu["type"] != "tool_use" || tu["id"] != "call_9" || tu["name"] != "lookup" {
		t.Errorf("tool_use block = %v", tu)
	}
	input := tu["input"].(map[string]any)
	if input["q"] != "x" {
		t.Errorf("tool_use input = %v", input)
	}
}

func TestBuildErrorResponse_StatusMapping(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{400, "invalid_request_error"},
		{401, "authentication_error"},
		{403, "permission_error"},
		{404, "not_found_error"},
		{413, "request_too_large"},
		{418, "invalid_request_error"},
		{429, "rate_limit_error"},
		{500, "api_error"},
		{502, "api_error"},
		{503, "overloaded_error"},
	}
	for _, c := range cases {
		out := BuildErrorResponse([]byte(`{"error":{"message":"boom"}}`), c.status)
		var m map[string]any
		if err := json.Unmarshal(out, &m); err != nil {
			t.Fatalf("status %d: invalid output: %v", c.status, err)
		}
		if m["type"] != "error" {
			t.Errorf("status %d: type = %v", c.status, m["type"])
		}
		e := m["error"].(map[string]any)
		if e["type"] != c.want {
			t.Errorf("status %d: error type = %v, want %v", c.status, e["type"], c.want)
		}
		if e["message"] != "boom" {
			t.Errorf("status %d: message = %v, want boom (from OpenAI envelope)", c.status, e["message"])
		}
	}
}

// The proxy's WriteOpenAIError emits "code" as an int; the message must still be
// extracted (not leaked as the raw JSON envelope). Regression guard.
func TestBuildErrorResponse_NumericCode(t *testing.T) {
	out := BuildErrorResponse([]byte(`{"error":{"code":504,"message":"request timed out","type":"server_error"}}`), 504)
	var m map[string]any
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("invalid output: %v", err)
	}
	e := m["error"].(map[string]any)
	if e["message"] != "request timed out" {
		t.Errorf("message = %v, want extracted \"request timed out\"", e["message"])
	}
	if e["type"] != "api_error" {
		t.Errorf("type = %v, want api_error (504)", e["type"])
	}
}

func TestBuildErrorResponse_RawBodyFallback(t *testing.T) {
	out := BuildErrorResponse([]byte(`not json`), 500)
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	e := m["error"].(map[string]any)
	if e["message"] != "not json" {
		t.Errorf("message = %v, want raw body fallback", e["message"])
	}
}

func TestBuildMessageResponse_DecodeErrorOmitsPayload(t *testing.T) {
	// A malformed response body is model output; the decode error reports the
	// offset, never the content.
	cases := []struct {
		name    string
		payload string
		want    string
		secret  string
	}{
		{
			name:    "syntax error",
			payload: `{"choices":[{"message":{"role":"assistant","content":"Kohlrabi"`,
			want:    "malformed JSON at byte ",
			secret:  "Kohlrabi",
		},
		{
			name:    "type error",
			payload: `{"id":8675309.42}`,
			want:    "unexpected JSON value at byte ",
			secret:  "8675309.42",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := BuildMessageResponse([]byte(tc.payload), "msg_1", "m")
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.HasPrefix(err.Error(), "anthropic: invalid upstream response: "+tc.want) {
				t.Errorf("error = %q, want the sanitized %q form", err, tc.want)
			}
			if strings.Contains(err.Error(), tc.secret) {
				t.Errorf("error leaked the payload: %q", err)
			}
		})
	}
}

// BuildMessageResponse must read a tool call whose arguments the upstream sent
// as an object. It is defence in depth rather than a live path today — the
// writer only ever sees bytes the proxy already normalised — but the field was
// the last one in the repo still typed as a plain string, and an untyped field
// with no test is how "today" stops being true.
func TestBuildMessageResponse_ObjectFormToolArguments(t *testing.T) {
	body := []byte(`{"id":"x","object":"chat.completion","choices":[{"index":0,"finish_reason":"tool_calls","message":{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"get_weather","arguments":{"city":"Prague"}}}]}}]}`)

	out, err := BuildMessageResponse(body, "msg_1", "claude-sonnet-4-6")
	if err != nil {
		t.Fatalf("an object-form tool call must decode: %v", err)
	}

	var msg struct {
		StopReason string `json:"stop_reason"`
		Content    []struct {
			Type  string          `json:"type"`
			Name  string          `json:"name"`
			Input json.RawMessage `json:"input"`
		} `json:"content"`
	}
	if err := json.Unmarshal(out, &msg); err != nil {
		t.Fatalf("decode built message: %v", err)
	}
	if msg.StopReason != "tool_use" {
		t.Errorf("stop_reason = %q, want tool_use", msg.StopReason)
	}
	var found bool
	for _, b := range msg.Content {
		if b.Type != "tool_use" {
			continue
		}
		found = true
		if b.Name != "get_weather" {
			t.Errorf("tool name = %q, want get_weather", b.Name)
		}
		if string(b.Input) != `{"city":"Prague"}` {
			t.Errorf("tool input = %s, want the arguments object", b.Input)
		}
	}
	if !found {
		t.Error("no tool_use block: stop_reason tool_use without one is a protocol violation")
	}
}

// A tool call signed by Gemini 3 (extra_content.google.thought_signature)
// surfaces as a tool_use whose id carries the signature, so the client
// echoes it back on the next turn without knowing; TranslateRequest recovers
// it. An unsigned call keeps its id, and an extra_content of a shape this
// package does not read is an unsigned call, not a failed translation.
func TestBuildMessageResponse_ToolUseCarriesThoughtSignature(t *testing.T) {
	oai := []byte(`{
		"choices": [{"message": {"role":"assistant","content":"","tool_calls":[
			{"id":"call_9","type":"function","function":{"name":"lookup","arguments":"{}"},"extra_content":{"google":{"thought_signature":"sig-9"}}},
			{"id":"call_10","type":"function","function":{"name":"lookup","arguments":"{}"}},
			{"id":"call_11","type":"function","function":{"name":"lookup","arguments":"{}"},"extra_content":"junk"}
		]}, "finish_reason": "tool_calls"}]
	}`)
	out, err := BuildMessageResponse(oai, "msg_2", "m")
	if err != nil {
		t.Fatalf("BuildMessageResponse: %v", err)
	}
	var m struct {
		Content []struct {
			ID string `json:"id"`
		} `json:"content"`
	}
	if err := json.Unmarshal(out, &m); err != nil || len(m.Content) != 3 {
		t.Fatalf("content = %s (%v)", out, err)
	}
	if id, sig := splitToolUseID(m.Content[0].ID); id != "call_9" || sig != "sig-9" {
		t.Errorf("signed tool_use id %q splits to (%q, %q), want (call_9, sig-9)", m.Content[0].ID, id, sig)
	}
	if m.Content[1].ID != "call_10" || m.Content[2].ID != "call_11" {
		t.Errorf("unsigned ids = %q, %q, want call_10 and call_11 untouched", m.Content[1].ID, m.Content[2].ID)
	}
}

// An upstream tool call without an id gets a synthesized one, as on the
// stream; signed or not, an empty id must never reach the wire.
func TestBuildMessageResponse_EmptyToolIDSynthesized(t *testing.T) {
	oai := []byte(`{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[
		{"id":"","type":"function","function":{"name":"f","arguments":"{}"},"extra_content":{"google":{"thought_signature":"s"}}},
		{"type":"function","function":{"name":"g","arguments":"{}"}}
	]},"finish_reason":"tool_calls"}]}`)
	out, err := BuildMessageResponse(oai, "msg_7", "m")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Content []struct {
			ID string `json:"id"`
		} `json:"content"`
	}
	_ = json.Unmarshal(out, &m)
	if id, sig := splitToolUseID(m.Content[0].ID); id != "toolu_msg_7_0" || sig != "s" {
		t.Errorf("signed empty id became %q (%q, %q), want toolu_msg_7_0 signed", m.Content[0].ID, id, sig)
	}
	if m.Content[1].ID != "toolu_msg_7_1" {
		t.Errorf("unsigned empty id became %q", m.Content[1].ID)
	}
}
