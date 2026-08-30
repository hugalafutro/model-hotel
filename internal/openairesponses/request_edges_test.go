package openairesponses

import (
	"bytes"
	"encoding/json"
	"testing"
)

// The resolved model is what the router picked; an empty one means nothing was
// resolved, and the request must still carry the model the caller asked for
// rather than go upstream with an empty "model" (which OpenAI rejects with a
// 400 the operator then has to trace back to the gateway).
func TestTranslateChat_FallsBackToTheRequestedModel(t *testing.T) {
	m := mustTranslate(t, `{"model":"gpt-5.6","messages":[{"role":"user","content":"hi"}]}`, "")
	if m["model"] != "gpt-5.6" {
		t.Errorf("model = %v, want the body's model", m["model"])
	}

	// A resolved model still wins: that is the provider-side name.
	m = mustTranslate(t, `{"model":"hotel/gpt-5.6","messages":[{"role":"user","content":"hi"}]}`, "gpt-5.6-2026")
	if m["model"] != "gpt-5.6-2026" {
		t.Errorf("model = %v, want the resolved model", m["model"])
	}
}

// Built-in tools (web_search, code_interpreter, ...) are a different shape on
// the Responses API and are out of scope; forwarding them as function tools
// would produce a malformed definition with an empty name. They are dropped,
// and dropping them must not take the real function tools with them.
func TestTranslateChat_DropsBuiltInToolsAndKeepsFunctions(t *testing.T) {
	m := mustTranslate(t, `{
		"model":"gpt-5.6",
		"messages":[{"role":"user","content":"hi"}],
		"tools":[
			{"type":"web_search"},
			{"type":"function","function":{"name":"get_weather","description":"look up weather","parameters":{"type":"object"}}},
			{"type":"code_interpreter"}
		]}`, "gpt-5.6")
	tools, ok := m["tools"].([]any)
	if !ok {
		t.Fatalf("tools missing: %v", m["tools"])
	}
	if len(tools) != 1 {
		t.Fatalf("got %d tools, want only the function one: %v", len(tools), tools)
	}
	tool := tools[0].(map[string]any)
	if tool["name"] != "get_weather" || tool["type"] != "function" {
		t.Errorf("surviving tool = %v", tool)
	}
	if tool["description"] != "look up weather" {
		t.Errorf("description = %v, want it carried over", tool["description"])
	}
}

// A user message with no content field at all has nothing to say, so it must
// not become a message item with an empty content array (which the Responses
// API rejects). Some clients emit such a placeholder turn.
func TestTranslateChat_DropsContentlessUserMessage(t *testing.T) {
	m := mustTranslate(t, `{
		"model":"gpt-5.6",
		"messages":[
			{"role":"user"},
			{"role":"user","content":"the real question"}
		]}`, "gpt-5.6")
	items := inputItems(t, m)
	if len(items) != 1 {
		t.Fatalf("got %d input items, want 1: %v", len(items), items)
	}
	parts := items[0]["content"].([]any)
	if parts[0].(map[string]any)["text"] != "the real question" {
		t.Errorf("surviving item = %v", items[0])
	}
}

// System and assistant text is flattened, not translated part by part, so a
// content array has to be concatenated rather than dropped. A system prompt
// sent as parts (which several clients do) silently vanishing would change the
// model's behaviour with no error anywhere.
func TestTranslateChat_FlattensContentPartArrays(t *testing.T) {
	m := mustTranslate(t, `{
		"model":"gpt-5.6",
		"messages":[
			{"role":"system","content":[{"type":"text","text":"be terse."},{"type":"text","text":" answer in French."}]},
			{"role":"assistant","content":[{"type":"text","text":"D'accord"}]},
			{"role":"tool","tool_call_id":"call_1","content":[{"type":"text","text":"42"}]},
			{"role":"user","content":"why"}
		]}`, "gpt-5.6")

	if got := m["instructions"]; got != "be terse. answer in French." {
		t.Errorf("instructions = %q, want both system parts concatenated", got)
	}
	items := inputItems(t, m)
	if len(items) != 3 {
		t.Fatalf("got %d input items, want 3: %v", len(items), items)
	}
	assistant := items[0]["content"].([]any)[0].(map[string]any)
	if assistant["text"] != "D'accord" {
		t.Errorf("assistant text = %v", assistant["text"])
	}
	if items[1]["output"] != "42" {
		t.Errorf("tool output = %v, want the flattened part text", items[1]["output"])
	}
}

// A part with no "type" is treated as text (some clients omit it), while a
// non-text part contributes nothing: an image in a system prompt has no text
// to flatten and must not stringify into the instructions.
func TestTranslateChat_FlattenIgnoresNonTextParts(t *testing.T) {
	m := mustTranslate(t, `{
		"model":"gpt-5.6",
		"messages":[
			{"role":"system","content":[{"text":"typeless counts"},{"type":"image_url","image_url":{"url":"https://x/y.png"},"text":"alt"}]},
			{"role":"user","content":"hi"}
		]}`, "gpt-5.6")
	if got := m["instructions"]; got != "typeless counts" {
		t.Errorf("instructions = %q, want only the text parts", got)
	}
}

// tool_choice is a union, and only the shapes the Responses API understands
// may be forwarded. Anything else is omitted rather than passed through, so a
// client sending a mode this gateway does not know cannot turn into a 400 from
// OpenAI about a field the operator never set.
func TestTranslateChat_ToolChoiceUnion(t *testing.T) {
	base := `{"model":"gpt-5.6","messages":[{"role":"user","content":"hi"}],"tool_choice":`
	cases := []struct {
		name string
		raw  string
		want any // nil means the field must be absent
	}{
		{"auto", `"auto"`, "auto"},
		{"none", `"none"`, "none"},
		{"required", `"required"`, "required"},
		{"unknown string mode", `"sometimes"`, nil},
		{"named function", `{"type":"function","function":{"name":"get_weather"}}`,
			map[string]any{"type": "function", "name": "get_weather"}},
		{"function without a name", `{"type":"function","function":{}}`, nil},
		{"non-function object", `{"type":"web_search"}`, nil},
		{"wrong JSON shape", `["auto"]`, nil},
		{"null", `null`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := mustTranslate(t, base+tc.raw+`}`, "gpt-5.6")
			got, present := m["tool_choice"]
			if tc.want == nil {
				if present {
					t.Fatalf("tool_choice = %v, want it omitted", got)
				}
				return
			}
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(tc.want)
			if !bytes.Equal(gotJSON, wantJSON) {
				t.Errorf("tool_choice = %s, want %s", gotJSON, wantJSON)
			}
		})
	}
}

// A json_schema response format carries an optional description alongside the
// name and schema. Dropping it changes what the model is told the structured
// output is for, so it travels with the rest of the format.
func TestTranslateChat_JSONSchemaFormatCarriesDescription(t *testing.T) {
	m := mustTranslate(t, `{
		"model":"gpt-5.6",
		"messages":[{"role":"user","content":"hi"}],
		"response_format":{"type":"json_schema","json_schema":{
			"name":"weather","description":"a weather report","strict":true,
			"schema":{"type":"object","properties":{"temp":{"type":"number"}}}}}}`, "gpt-5.6")

	text, ok := m["text"].(map[string]any)
	if !ok {
		t.Fatalf("text config missing: %v", m["text"])
	}
	format, ok := text["format"].(map[string]any)
	if !ok {
		t.Fatalf("format missing: %v", text)
	}
	if format["type"] != "json_schema" || format["name"] != "weather" {
		t.Errorf("format = %v", format)
	}
	if format["description"] != "a weather report" {
		t.Errorf("description = %v, want it carried into the format", format["description"])
	}
	if format["strict"] != true {
		t.Errorf("strict = %v, want true", format["strict"])
	}
	if _, ok := format["schema"].(map[string]any); !ok {
		t.Errorf("schema missing from the format: %v", format)
	}

	// Without a description the key is absent rather than empty: an empty
	// description is not the same instruction as none.
	m = mustTranslate(t, `{
		"model":"gpt-5.6",
		"messages":[{"role":"user","content":"hi"}],
		"response_format":{"type":"json_schema","json_schema":{"name":"weather"}}}`, "gpt-5.6")
	format = m["text"].(map[string]any)["format"].(map[string]any)
	if _, present := format["description"]; present {
		t.Errorf("description present with none given: %v", format)
	}
}
