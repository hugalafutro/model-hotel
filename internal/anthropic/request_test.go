package anthropic

import (
	"encoding/json"
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
