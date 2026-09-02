package anthropic

import (
	"encoding/json"
	"strings"
	"testing"
)

// decodeOAI unmarshals a translated OpenAI request body for assertions.
func decodeOAI(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("translated body is not valid JSON: %v\n%s", err, body)
	}
	return m
}

func TestTranslateRequest_SystemAndText(t *testing.T) {
	body := []byte(`{
		"model": "hotel/claude",
		"max_tokens": 100,
		"system": "You are helpful.",
		"temperature": 0.7,
		"stop_sequences": ["STOP"],
		"messages": [
			{"role": "user", "content": "Hello"}
		]
	}`)
	out, model, stream, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest: %v", err)
	}
	if model != "hotel/claude" {
		t.Errorf("model = %q, want hotel/claude", model)
	}
	if stream {
		t.Errorf("stream = true, want false")
	}
	m := decodeOAI(t, out)
	msgs := m["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages len = %d, want 2", len(msgs))
	}
	sys := msgs[0].(map[string]any)
	if sys["role"] != "system" || sys["content"] != "You are helpful." {
		t.Errorf("system msg = %v", sys)
	}
	usr := msgs[1].(map[string]any)
	if usr["role"] != "user" || usr["content"] != "Hello" {
		t.Errorf("user msg = %v", usr)
	}
	if m["max_tokens"].(float64) != 100 {
		t.Errorf("max_tokens = %v, want 100", m["max_tokens"])
	}
	if m["temperature"].(float64) != 0.7 {
		t.Errorf("temperature = %v", m["temperature"])
	}
	stop := m["stop"].([]any)
	if len(stop) != 1 || stop[0] != "STOP" {
		t.Errorf("stop = %v", stop)
	}
}

func TestTranslateRequest_ImageBase64(t *testing.T) {
	body := []byte(`{
		"model": "p/m", "max_tokens": 50,
		"messages": [
			{"role": "user", "content": [
				{"type": "text", "text": "what is this?"},
				{"type": "image", "source": {"type": "base64", "media_type": "image/png", "data": "AAAA"}}
			]}
		]
	}`)
	out, _, _, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest: %v", err)
	}
	m := decodeOAI(t, out)
	parts := m["messages"].([]any)[0].(map[string]any)["content"].([]any)
	if len(parts) != 2 {
		t.Fatalf("content parts = %d, want 2", len(parts))
	}
	img := parts[1].(map[string]any)
	if img["type"] != "image_url" {
		t.Fatalf("part type = %v", img["type"])
	}
	url := img["image_url"].(map[string]any)["url"].(string)
	if url != "data:image/png;base64,AAAA" {
		t.Errorf("image url = %q", url)
	}
}

func TestTranslateRequest_ToolUseAndResult(t *testing.T) {
	body := []byte(`{
		"model": "p/m", "max_tokens": 50,
		"tools": [{"name": "get_weather", "description": "weather", "input_schema": {"type":"object"}}],
		"tool_choice": {"type": "any"},
		"messages": [
			{"role": "user", "content": "weather in Paris?"},
			{"role": "assistant", "content": [
				{"type": "text", "text": "Checking."},
				{"type": "tool_use", "id": "call_1", "name": "get_weather", "input": {"city": "Paris"}}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "call_1", "content": "sunny"}
			]}
		]
	}`)
	out, _, _, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest: %v", err)
	}
	m := decodeOAI(t, out)

	// tools
	tools := m["tools"].([]any)
	if len(tools) != 1 {
		t.Fatalf("tools len = %d", len(tools))
	}
	fn := tools[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "get_weather" {
		t.Errorf("tool name = %v", fn["name"])
	}
	if m["tool_choice"] != "required" {
		t.Errorf("tool_choice = %v, want required", m["tool_choice"])
	}

	msgs := m["messages"].([]any)
	// user, assistant(tool_calls), tool
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3: %v", len(msgs), msgs)
	}
	asst := msgs[1].(map[string]any)
	if asst["role"] != "assistant" || asst["content"] != "Checking." {
		t.Errorf("assistant msg = %v", asst)
	}
	tcs := asst["tool_calls"].([]any)
	if len(tcs) != 1 {
		t.Fatalf("tool_calls = %d", len(tcs))
	}
	tc := tcs[0].(map[string]any)
	if tc["id"] != "call_1" {
		t.Errorf("tool_call id = %v", tc["id"])
	}
	if args := tc["function"].(map[string]any)["arguments"].(string); args != `{"city": "Paris"}` && args != `{"city":"Paris"}` {
		t.Errorf("tool_call args = %q", args)
	}
	tool := msgs[2].(map[string]any)
	if tool["role"] != "tool" || tool["tool_call_id"] != "call_1" || tool["content"] != "sunny" {
		t.Errorf("tool msg = %v", tool)
	}
}

func TestTranslateRequest_ToolChoiceTool(t *testing.T) {
	body := []byte(`{"model":"p/m","max_tokens":10,"tool_choice":{"type":"tool","name":"foo"},"messages":[{"role":"user","content":"x"}]}`)
	out, _, _, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest: %v", err)
	}
	m := decodeOAI(t, out)
	tc := m["tool_choice"].(map[string]any)
	if tc["type"] != "function" {
		t.Errorf("tool_choice type = %v", tc["type"])
	}
	if tc["function"].(map[string]any)["name"] != "foo" {
		t.Errorf("tool_choice fn = %v", tc["function"])
	}
}

func TestTranslateRequest_StreamFlag(t *testing.T) {
	body := []byte(`{"model":"p/m","max_tokens":10,"stream":true,"messages":[{"role":"user","content":"x"}]}`)
	_, _, stream, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest: %v", err)
	}
	if !stream {
		t.Errorf("stream = false, want true")
	}
}

func TestTranslateRequest_MissingModel(t *testing.T) {
	_, _, _, err := TranslateRequest([]byte(`{"max_tokens":10,"messages":[]}`))
	if err == nil {
		t.Fatal("expected error for missing model")
	}
}

func TestTranslateRequest_InvalidBody(t *testing.T) {
	if _, _, _, err := TranslateRequest([]byte(`not json`)); err == nil {
		t.Fatal("expected error for invalid body")
	}
}

func TestTranslateRequest_SystemAsBlocks(t *testing.T) {
	// system can be an array of text blocks; they flatten into one system message.
	body := []byte(`{"model":"p/m","max_tokens":10,"system":[{"type":"text","text":"A"},{"type":"text","text":"B"}],"messages":[{"role":"user","content":"x"}]}`)
	out, _, _, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest: %v", err)
	}
	m := decodeOAI(t, out)
	sys := m["messages"].([]any)[0].(map[string]any)
	if sys["role"] != "system" || sys["content"] != "AB" {
		t.Errorf("system flatten = %v, want AB", sys)
	}
}

func TestTranslateRequest_ImageURLAndToolChoiceAuto(t *testing.T) {
	body := []byte(`{"model":"p/m","max_tokens":10,"tool_choice":{"type":"auto"},"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"https://x/y.png"}}]}]}`)
	out, _, _, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest: %v", err)
	}
	m := decodeOAI(t, out)
	if m["tool_choice"] != "auto" {
		t.Errorf("tool_choice = %v, want auto", m["tool_choice"])
	}
	parts := m["messages"].([]any)[0].(map[string]any)["content"].([]any)
	url := parts[0].(map[string]any)["image_url"].(map[string]any)["url"]
	if url != "https://x/y.png" {
		t.Errorf("image url = %v, want passthrough url", url)
	}
}

func TestTranslateRequest_ToolResultArrayAndDroppedBlocks(t *testing.T) {
	// tool_result with array content flattens to text; document/thinking blocks
	// and an image with empty base64 data are dropped.
	body := []byte(`{"model":"p/m","max_tokens":10,"messages":[
		{"role":"user","content":[
			{"type":"document","source":{"type":"base64","media_type":"application/pdf","data":"x"}},
			{"type":"thinking","thinking":"hmm"},
			{"type":"image","source":{"type":"base64","media_type":"image/png","data":""}},
			{"type":"tool_result","tool_use_id":"c1","content":[{"type":"text","text":"part1 "},{"type":"text","text":"part2"}]}
		]}
	]}`)
	out, _, _, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest: %v", err)
	}
	m := decodeOAI(t, out)
	msgs := m["messages"].([]any)
	// only the tool message survives (document/thinking/empty-image dropped, no text parts)
	if len(msgs) != 1 {
		t.Fatalf("messages = %d, want 1 (tool only): %v", len(msgs), msgs)
	}
	tool := msgs[0].(map[string]any)
	if tool["role"] != "tool" || tool["tool_call_id"] != "c1" || tool["content"] != "part1 part2" {
		t.Errorf("tool msg = %v", tool)
	}
}

func TestTranslateToolChoice_Edges(t *testing.T) {
	// tool type without a name degrades to "required".
	body := []byte(`{"model":"p/m","max_tokens":10,"tool_choice":{"type":"tool"},"messages":[{"role":"user","content":"x"}]}`)
	out, _, _, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest: %v", err)
	}
	if m := decodeOAI(t, out); m["tool_choice"] != "required" {
		t.Errorf("tool_choice(tool,no-name) = %v, want required", m["tool_choice"])
	}
	// none must map to OpenAI "none" (prohibits tool use); dropping it would let
	// the upstream default to auto and call a tool the caller forbade.
	bodyNone := []byte(`{"model":"p/m","max_tokens":10,"tool_choice":{"type":"none"},"tools":[{"name":"f","input_schema":{"type":"object"}}],"messages":[{"role":"user","content":"x"}]}`)
	outNone, _, _, err := TranslateRequest(bodyNone)
	if err != nil {
		t.Fatalf("TranslateRequest: %v", err)
	}
	if m := decodeOAI(t, outNone); m["tool_choice"] != "none" {
		t.Errorf("tool_choice(none) = %v, want none", m["tool_choice"])
	}
	// unknown/invalid tool_choice is omitted entirely.
	body2 := []byte(`{"model":"p/m","max_tokens":10,"tool_choice":{"type":"weird"},"messages":[{"role":"user","content":"x"}]}`)
	out2, _, _, _ := TranslateRequest(body2)
	if m := decodeOAI(t, out2); m["tool_choice"] != nil {
		t.Errorf("unknown tool_choice = %v, want omitted", m["tool_choice"])
	}
}

func TestBuildErrorResponseFromMessage_EmptyDefaults(t *testing.T) {
	out := BuildErrorResponseFromMessage("", 503)
	m := map[string]any{}
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("invalid output: %v", err)
	}
	e := m["error"].(map[string]any)
	if e["type"] != "overloaded_error" {
		t.Errorf("type = %v, want overloaded_error", e["type"])
	}
	if e["message"] != "Service Unavailable" {
		t.Errorf("message = %v, want status text default", e["message"])
	}
}

func TestTranslateRequest_MessageContentErrorOmitsPayload(t *testing.T) {
	// The per-message content decoder sees the user's own prompt. A syntax
	// error cannot reach it (the outer body parse fails first), but a type
	// error can, and *json.UnmarshalTypeError prints the offending literal
	// verbatim — so that is what must not survive into the error.
	_, _, _, err := TranslateRequest([]byte(`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":[8675309.42]}]}`))
	if err == nil {
		t.Fatal("expected an error for undecodable message content")
	}
	if !strings.HasPrefix(err.Error(), "anthropic: invalid message content: unexpected JSON value at byte ") {
		t.Errorf("error = %q, want the sanitized unexpected-value form", err)
	}
	if strings.Contains(err.Error(), "8675309.42") {
		t.Errorf("error leaked the message content: %q", err)
	}
}

func TestTranslateRequest_DecodeErrorOmitsPayload(t *testing.T) {
	// A malformed request body is the user's own prompt: encoding/json quotes the
	// offending byte and prints an offending literal verbatim, so the error must
	// say WHERE the body broke and nothing about what it said.
	cases := []struct {
		name    string
		payload string
		want    string
		secret  string
	}{
		{
			name:    "syntax error",
			payload: `{"model":"claude-x","messages":[{"role":"user","content":"Kohlrabi"`,
			want:    "malformed JSON at byte ",
			secret:  "Kohlrabi",
		},
		{
			name:    "type error",
			payload: `{"model":8675309.42}`,
			want:    "unexpected JSON value at byte ",
			secret:  "8675309.42",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, err := TranslateRequest([]byte(tc.payload))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.HasPrefix(err.Error(), "anthropic: invalid request body: "+tc.want) {
				t.Errorf("error = %q, want the sanitized %q form", err, tc.want)
			}
			if strings.Contains(err.Error(), tc.secret) {
				t.Errorf("error leaked the payload: %q", err)
			}
		})
	}
}

// A tool_use id carrying a Gemini 3 thought signature (see thoughtSigMarker)
// translates to the bare id the provider issued, with the signature on the
// tool call's extra_content in the shape the Gemini translator reads; the
// tool_result naming that id maps to the bare id too. An unsigned id carries
// no extra_content member.
func TestTranslateRequest_ToolUseCarriesThoughtSignature(t *testing.T) {
	signed := signedToolUseID("call_7", "sig-bytes")
	body := []byte(`{
		"model": "p/m", "max_tokens": 50,
		"tools": [{"name": "get_weather", "input_schema": {"type":"object"}}],
		"messages": [
			{"role": "user", "content": "weather?"},
			{"role": "assistant", "content": [
				{"type": "tool_use", "id": "` + signed + `", "name": "get_weather", "input": {"city": "Paris"}},
				{"type": "tool_use", "id": "call_8", "name": "get_weather", "input": {"city": "Rome"}}
			]},
			{"role": "user", "content": [
				{"type": "tool_result", "tool_use_id": "` + signed + `", "content": "sunny"},
				{"type": "tool_result", "tool_use_id": "call_8", "content": "rain"}
			]}
		]
	}`)
	out, _, _, err := TranslateRequest(body)
	if err != nil {
		t.Fatalf("TranslateRequest: %v", err)
	}
	m := decodeOAI(t, out)
	msgs := m["messages"].([]any)
	tcs := msgs[1].(map[string]any)["tool_calls"].([]any)
	signedCall := tcs[0].(map[string]any)
	if signedCall["id"] != "call_7" {
		t.Errorf("signed call id = %v, want the bare call_7", signedCall["id"])
	}
	extra, _ := signedCall["extra_content"].(map[string]any)
	google, _ := extra["google"].(map[string]any)
	if google["thought_signature"] != "sig-bytes" {
		t.Errorf("extra_content = %v, want google.thought_signature sig-bytes", signedCall["extra_content"])
	}
	plainCall := tcs[1].(map[string]any)
	if _, has := plainCall["extra_content"]; has || plainCall["id"] != "call_8" {
		t.Errorf("unsigned call = %v, want id call_8 and no extra_content", plainCall)
	}
	if id := msgs[2].(map[string]any)["tool_call_id"]; id != "call_7" {
		t.Errorf("tool result for the signed call names %v, want call_7", id)
	}
	if id := msgs[3].(map[string]any)["tool_call_id"]; id != "call_8" {
		t.Errorf("tool result for the plain call names %v, want call_8", id)
	}
}
