package openairesponses

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// decodeChat decodes the chat body the ingress emitted into a generic map so
// the assertions read like the wire.
func decodeChat(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("chat body is not JSON: %v\n%s", err, body)
	}
	return m
}

func messagesOf(t *testing.T, m map[string]any) []map[string]any {
	t.Helper()
	raw, _ := m["messages"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		out = append(out, r.(map[string]any))
	}
	return out
}

func TestTranslateRequestToChat_StringInputAndInstructions(t *testing.T) {
	body := []byte(`{"model":"hotel/g","instructions":"Be terse.","input":"hi","stream":true,
		"max_output_tokens":50,"temperature":0.2,"top_p":0.9,"parallel_tool_calls":false,
		"reasoning":{"effort":"low","summary":"auto"},"metadata":{"k":"v"},"store":false,
		"include":["reasoning.encrypted_content"],"prompt_cache_key":"abc","text":{"verbosity":"low"}}`)
	tr, err := TranslateRequestToChat(body)
	if err != nil {
		t.Fatal(err)
	}
	chat, model, stream := tr.ChatBody, tr.Model, tr.Stream
	if model != "hotel/g" || !stream {
		t.Fatalf("model=%q stream=%v", model, stream)
	}
	m := decodeChat(t, chat)
	msgs := messagesOf(t, m)
	if len(msgs) != 2 || msgs[0]["role"] != "system" || msgs[0]["content"] != "Be terse." || msgs[1]["role"] != "user" || msgs[1]["content"] != "hi" {
		t.Fatalf("messages = %v", msgs)
	}
	if m["max_tokens"] != float64(50) || m["temperature"] != 0.2 || m["top_p"] != 0.9 || m["parallel_tool_calls"] != false || m["reasoning_effort"] != "low" || m["stream"] != true {
		t.Fatalf("params = %v", m)
	}
	if md, _ := m["metadata"].(map[string]any); md["k"] != "v" {
		t.Fatalf("metadata = %v", m["metadata"])
	}
	for _, dropped := range []string{"include", "prompt_cache_key", "text", "verbosity", "store", "response_format", "tools", "tool_choice"} {
		if _, ok := m[dropped]; ok {
			t.Errorf("%s should not be forwarded: %v", dropped, m[dropped])
		}
	}
}

func TestTranslateRequestToChat_ItemsMultiTurnTools(t *testing.T) {
	body := []byte(`{"model":"p/m","input":[
		{"role":"developer","content":"env context"},
		{"type":"message","role":"user","content":[
			{"type":"input_text","text":"look"},
			{"type":"input_image","image_url":"data:image/png;base64,AAA","detail":"low"},
			{"type":"input_file","file_data":"data:application/pdf;base64,BBB","filename":"a.pdf"}]},
		{"type":"reasoning","id":"rs_1","summary":[],"encrypted_content":"zzz"},
		{"type":"message","role":"assistant","content":[{"type":"output_text","text":"calling"},{"type":"refusal","refusal":"no"}]},
		{"type":"function_call","call_id":"call_1","name":"ls","arguments":"{\"p\":\".\"}"},
		{"type":"function_call","call_id":"call_2","name":"cat","arguments":{"f":"x"}},
		{"type":"function_call_output","call_id":"call_1","output":"a b"},
		{"type":"function_call_output","call_id":"call_2","output":[{"type":"input_text","text":"line1"},{"type":"input_text","text":"line2"}]},
		{"type":"function_call","call_id":"call_3","name":"ls","arguments":"{}"}
	],"tools":[
		{"type":"function","name":"ls","description":"list","parameters":{"type":"object"},"strict":true},
		{"type":"web_search","external_web_access":true},
		{"type":"function","name":"cat"}
	],"tool_choice":{"type":"function","name":"ls"},
	"text":{"format":{"type":"json_schema","name":"out","schema":{"type":"object"},"strict":true}}}`)
	tr, err := TranslateRequestToChat(body)
	if err != nil {
		t.Fatal(err)
	}
	chat := tr.ChatBody
	m := decodeChat(t, chat)
	msgs := messagesOf(t, m)
	if len(msgs) != 6 {
		t.Fatalf("got %d messages: %s", len(msgs), chat)
	}
	if msgs[0]["role"] != "system" || msgs[0]["content"] != "env context" {
		t.Errorf("developer -> system: %v", msgs[0])
	}
	parts, _ := msgs[1]["content"].([]any)
	if len(parts) != 3 {
		t.Fatalf("user parts = %v", msgs[1]["content"])
	}
	img := parts[1].(map[string]any)["image_url"].(map[string]any)
	if img["url"] != "data:image/png;base64,AAA" || img["detail"] != "low" {
		t.Errorf("image part = %v", parts[1])
	}
	file := parts[2].(map[string]any)["file"].(map[string]any)
	if file["file_data"] != "data:application/pdf;base64,BBB" || file["filename"] != "a.pdf" {
		t.Errorf("file part = %v", parts[2])
	}
	// The reasoning item is dropped; the assistant text and the two calls fold
	// into ONE assistant message; the refusal part is not text.
	asst := msgs[2]
	if asst["role"] != "assistant" || asst["content"] != "calling" {
		t.Fatalf("assistant = %v", asst)
	}
	calls, _ := asst["tool_calls"].([]any)
	if len(calls) != 2 {
		t.Fatalf("tool_calls = %v", asst["tool_calls"])
	}
	c1 := calls[0].(map[string]any)
	if c1["id"] != "call_1" || c1["type"] != "function" || c1["function"].(map[string]any)["name"] != "ls" || c1["function"].(map[string]any)["arguments"] != `{"p":"."}` {
		t.Errorf("call 1 = %v", c1)
	}
	// Object-form arguments marshal back as the spec's JSON string.
	if args := calls[1].(map[string]any)["function"].(map[string]any)["arguments"]; args != `{"f":"x"}` {
		t.Errorf("call 2 arguments = %v", args)
	}
	if msgs[3]["role"] != "tool" || msgs[3]["tool_call_id"] != "call_1" || msgs[3]["content"] != "a b" {
		t.Errorf("tool 1 = %v", msgs[3])
	}
	if msgs[4]["role"] != "tool" || msgs[4]["tool_call_id"] != "call_2" || msgs[4]["content"] != "line1\nline2" {
		t.Errorf("tool 2 = %v", msgs[4])
	}
	// A call after a tool result starts a new assistant message with null content.
	if msgs[5]["role"] != "assistant" || msgs[5]["content"] != nil || len(msgs[5]["tool_calls"].([]any)) != 1 {
		t.Errorf("trailing call = %v", msgs[5])
	}

	tools, _ := m["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("web_search should be dropped, function tools kept: %v", m["tools"])
	}
	fn := tools[0].(map[string]any)["function"].(map[string]any)
	if fn["name"] != "ls" || fn["description"] != "list" || fn["strict"] != true || fn["parameters"].(map[string]any)["type"] != "object" {
		t.Errorf("tool 0 = %v", tools[0])
	}
	if fn2 := tools[1].(map[string]any)["function"].(map[string]any); fn2["name"] != "cat" || fn2["description"] != nil || fn2["parameters"] != nil {
		t.Errorf("tool 1 should carry only the name: %v", tools[1])
	}
	tc := m["tool_choice"].(map[string]any)
	if tc["type"] != "function" || tc["function"].(map[string]any)["name"] != "ls" {
		t.Errorf("tool_choice = %v", tc)
	}
	rf := m["response_format"].(map[string]any)
	js := rf["json_schema"].(map[string]any)
	if rf["type"] != "json_schema" || js["name"] != "out" || js["strict"] != true || js["schema"].(map[string]any)["type"] != "object" {
		t.Errorf("response_format = %v", rf)
	}
}

func TestTranslateRequestToChat_StringModesAndJSONObject(t *testing.T) {
	tr, err := TranslateRequestToChat([]byte(`{"model":"p/m","input":[],"tool_choice":"required","text":{"format":{"type":"json_object"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	chat := tr.ChatBody
	m := decodeChat(t, chat)
	if m["tool_choice"] != "required" || m["response_format"].(map[string]any)["type"] != "json_object" {
		t.Errorf("got %v", m)
	}
	if msgs, ok := m["messages"].([]any); !ok || len(msgs) != 0 {
		t.Errorf("messages must be an empty array, got %v", m["messages"])
	}
	tr, err = TranslateRequestToChat([]byte(`{"model":"p/m","input":"x","text":{"format":{"type":"text"}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := decodeChat(t, tr.ChatBody)["response_format"]; ok {
		t.Error("text format must keep the model default")
	}
}

func TestTranslateRequestToChat_Rejections(t *testing.T) {
	cases := []struct {
		name, body, field string
	}{
		{"previous_response_id", `{"model":"m","input":"x","previous_response_id":"resp_1"}`, "previous_response_id"},
		{"conversation", `{"model":"m","input":"x","conversation":{"id":"conv_1"}}`, "conversation"},
		{"store true", `{"model":"m","input":"x","store":true}`, "store"},
		{"background", `{"model":"m","input":"x","background":true}`, "background"},
		{"item_reference", `{"model":"m","input":[{"type":"item_reference","id":"msg_1"}]}`, "input[0]"},
		{"compaction", `{"model":"m","input":[{"type":"compaction","encrypted_content":"x"}]}`, "input[0]"},
		{"model missing", `{"input":"x"}`, "model"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := TranslateRequestToChat([]byte(tc.body))
			var rej *RejectedRequest
			if !errors.As(err, &rej) {
				t.Fatalf("want *RejectedRequest, got %v", err)
			}
			if rej.Field != tc.field {
				t.Errorf("field = %q, want %q (%v)", rej.Field, tc.field, err)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("message must name the field: %v", err)
			}
		})
	}
}

func TestTranslateRequestToChat_NullStateIsNotState(t *testing.T) {
	// The OpenAI SDKs send explicit nulls for unset members.
	tr, err := TranslateRequestToChat([]byte(`{"model":"m","input":"x","previous_response_id":null,"conversation":null,"store":null,"tool_choice":null,"text":null,"reasoning":null}`))
	if err != nil {
		t.Fatalf("nulls must translate: %v", err)
	}
	if _, ok := decodeChat(t, tr.ChatBody)["tool_choice"]; ok {
		t.Error("null tool_choice must be omitted")
	}
}

func TestTranslateRequestToChat_MalformedJSON(t *testing.T) {
	for _, body := range []string{`{`, `{"model":"m","input":[{"role":"user","content":[1]}]}`, `{"model":"m","input":"x","tools":[1]}`, `{"model":"m","input":"x","tool_choice":1}`} {
		_, err := TranslateRequestToChat([]byte(body))
		var rej *RejectedRequest
		if err == nil || errors.As(err, &rej) {
			t.Errorf("%s: want a decode error, got %v", body, err)
		}
		if err != nil && strings.Contains(err.Error(), "user") {
			t.Errorf("decode error must not echo the document: %v", err)
		}
	}
}

func TestTranslateRequestToChat_EmptyAssistantAndUnknownRoleMessages(t *testing.T) {
	tr, err := TranslateRequestToChat([]byte(`{"model":"m","input":[
		{"role":"assistant","content":[{"type":"refusal","refusal":"no"}]},
		{"role":"user","content":[]},
		{"role":"user","content":null},
		{"role":"system","content":[{"type":"input_text","text":"sys"}]},
		{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	msgs := messagesOf(t, decodeChat(t, tr.ChatBody))
	if len(msgs) != 2 || msgs[0]["role"] != "system" || msgs[0]["content"] != "sys" || msgs[1]["content"] != "hello" {
		t.Fatalf("empty turns must vanish: %v", msgs)
	}
}

// A namespace tool (Codex groups an MCP server's tools this way) is flattened
// into its function tools under namespaced chat names, and the map reverses
// them; a replayed namespaced call carries the same flat name to the provider.
func TestTranslateRequestToChat_NamespaceToolsFlatten(t *testing.T) {
	tr, err := TranslateRequestToChat([]byte(`{"model":"m","input":[
		{"type":"function_call","call_id":"call_1","name":"list","namespace":"mcp__fs","arguments":"{}"},
		{"type":"function_call_output","call_id":"call_1","output":"ok"}],
		"tools":[{"type":"namespace","name":"mcp__fs","description":"files","tools":[
			{"type":"function","name":"list","description":"ls","parameters":{"type":"object"}},
			{"type":"function","name":"read"}]},
		{"type":"function","name":"plain"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	m := decodeChat(t, tr.ChatBody)
	tools, _ := m["tools"].([]any)
	var names []string
	for _, tl := range tools {
		names = append(names, tl.(map[string]any)["function"].(map[string]any)["name"].(string))
	}
	if strings.Join(names, ",") != "mcp__fs__list,mcp__fs__read,plain" {
		t.Errorf("chat tool names = %v", names)
	}
	if got := tr.Facts.ToolNames["mcp__fs__list"]; got != (NamespacedTool{Namespace: "mcp__fs", Name: "list"}) || len(tr.Facts.ToolNames) != 2 {
		t.Errorf("tool names = %+v", tr.Facts.ToolNames)
	}
	msgs := messagesOf(t, m)
	if call := msgs[0]["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any); call["name"] != "mcp__fs__list" {
		t.Errorf("replayed namespaced call = %v", call)
	}
	if tr, err := TranslateRequestToChat([]byte(`{"model":"m","input":"x","tools":[{"type":"namespace","name":"n","tools":[{"type":"custom","name":"c"}]}]}`)); err != nil || tr.NativeOnly == nil {
		t.Errorf("a non-function tool inside a namespace is native-only: %v %+v", err, tr)
	}
	if tr, _ := TranslateRequestToChat([]byte(`{"model":"m","input":"x","tools":[{"type":"function","name":"f"}]}`)); tr.Facts.ToolNames != nil {
		t.Error("no namespaces: the map must be nil")
	}
}

func TestTranslateRequestToChat_ToolNameCollisionsAndNamespacedChoice(t *testing.T) {
	for _, body := range []string{
		`{"model":"m","input":"x","tools":[{"type":"function","name":"a__b"},{"type":"namespace","name":"a","tools":[{"type":"function","name":"b"}]}]}`,
		`{"model":"m","input":"x","tools":[{"type":"namespace","name":"a","tools":[{"type":"function","name":"b__c"}]},{"type":"namespace","name":"a__b","tools":[{"type":"function","name":"c"}]}]}`,
		`{"model":"m","input":"x","tools":[{"type":"function","name":"f"},{"type":"function","name":"f"}]}`,
	} {
		_, err := TranslateRequestToChat([]byte(body))
		var rej *RejectedRequest
		if !errors.As(err, &rej) || !strings.Contains(err.Error(), "collides") {
			t.Errorf("%s: want a collision rejection, got %v", body, err)
		}
	}
	tr, err := TranslateRequestToChat([]byte(`{"model":"m","input":"x","tool_choice":{"type":"function","name":"list"},"tools":[{"type":"namespace","name":"fs","tools":[{"type":"function","name":"list"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := decodeChat(t, tr.ChatBody)["tool_choice"].(map[string]any)["function"].(map[string]any)["name"]; got != "fs__list" {
		t.Errorf("namespaced tool_choice = %v", got)
	}
	// A top-level tool of that name wins over a namespaced one.
	tr, _ = TranslateRequestToChat([]byte(`{"model":"m","input":"x","tool_choice":{"type":"function","name":"list"},"tools":[{"type":"function","name":"list"},{"type":"namespace","name":"fs","tools":[{"type":"function","name":"list"}]}]}`))
	if got := decodeChat(t, tr.ChatBody)["tool_choice"].(map[string]any)["function"].(map[string]any)["name"]; got != "list" {
		t.Errorf("plain tool_choice = %v", got)
	}
	if tr.Facts.Instructions != "" || tr.Facts.Temperature != nil {
		t.Errorf("facts = %+v", tr.Facts)
	}
	for _, body := range []string{
		`{"model":"m","input":"x","tool_choice":{"type":"function","name":"nope"},"tools":[{"type":"function","name":"f"}]}`,
		`{"model":"m","input":"x","tool_choice":{"type":"function","name":"list"},"tools":[{"type":"namespace","name":"a","tools":[{"type":"function","name":"list"}]},{"type":"namespace","name":"b","tools":[{"type":"function","name":"list"}]}]}`,
	} {
		_, err := TranslateRequestToChat([]byte(body))
		var rej *RejectedRequest
		if !errors.As(err, &rej) || rej.Field != "tool_choice" {
			t.Errorf("%s: want a tool_choice rejection, got %v", body, err)
		}
	}
}

func TestTranslateRequestToChat_PartsJoinAndRoleWhitelist(t *testing.T) {
	tr, err := TranslateRequestToChat([]byte(`{"model":"m","instructions":[{"type":"input_text","text":"one"},{"type":"input_text","text":"two"}],"input":[{"role":"developer","content":[{"type":"input_text","text":"a"},{"type":"input_text","text":"b"}]}]}`))
	if err != nil {
		t.Fatal(err)
	}
	msgs := messagesOf(t, decodeChat(t, tr.ChatBody))
	if msgs[0]["content"] != "one\ntwo" || msgs[1]["content"] != "a\nb" || tr.Facts.Instructions != "one\ntwo" {
		t.Errorf("parts must join with newlines: %v facts=%q", msgs, tr.Facts.Instructions)
	}
	_, err = TranslateRequestToChat([]byte(`{"model":"m","input":[{"role":"tool","content":"x"}]}`))
	var rej *RejectedRequest
	if !errors.As(err, &rej) || rej.Field != "input[0]" {
		t.Errorf("unknown role must be refused, got %v", err)
	}
}

// Members only OpenAI's own endpoint serves do not fail the translation: they
// are left out of the chat body and reported as native-only, naming the first,
// for the handler to refuse once it knows a candidate would be translated.
func TestTranslateRequestToChat_NativeOnlyMembers(t *testing.T) {
	cases := []struct {
		name, body, field string
		wantTools         int // function tools that must survive beside the member
	}{
		{"custom tool call item", `{"model":"m","input":[{"type":"custom_tool_call","call_id":"c","name":"apply_patch","input":"x"},{"role":"user","content":"hi"}]}`, "input[0]", 0},
		{"image by file id", `{"model":"m","input":[{"role":"user","content":[{"type":"input_image","file_id":"file_1"},{"type":"input_text","text":"hi"}]}]}`, "input[0].content[0]", 0},
		{"file by url", `{"model":"m","input":[{"role":"user","content":[{"type":"input_file","file_url":"https://x/y.pdf"}]}]}`, "input[0].content[0]", 0},
		{"audio part", `{"model":"m","input":[{"role":"user","content":[{"type":"input_audio","input_audio":{}}]}]}`, "input[0].content[0]", 0},
		{"custom tool", `{"model":"m","input":"x","tools":[{"type":"custom","name":"apply_patch"},{"type":"function","name":"f"}]}`, "tools[0]", 1},
		{"file_search", `{"model":"m","input":"x","tools":[{"type":"function","name":"f"},{"type":"file_search"}]}`, "tools[1]", 1},
		{"custom inside namespace", `{"model":"m","input":"x","tools":[{"type":"namespace","name":"n","tools":[{"type":"custom","name":"c"},{"type":"function","name":"f"}]}]}`, "tools[0].tools[0]", 1},
		{"hosted tool_choice", `{"model":"m","input":"x","tool_choice":{"type":"web_search"}}`, "tool_choice", 0},
		{"allowed_tools", `{"model":"m","input":"x","tool_choice":{"type":"allowed_tools","mode":"auto","tools":[]}}`, "tool_choice", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr, err := TranslateRequestToChat([]byte(tc.body))
			if err != nil {
				t.Fatalf("native-only members must not fail the translation: %v", err)
			}
			if tr.NativeOnly == nil || tr.NativeOnly.Field != tc.field {
				t.Fatalf("NativeOnly = %+v, want field %s", tr.NativeOnly, tc.field)
			}
			m := decodeChat(t, tr.ChatBody)
			body := string(tr.ChatBody)
			for _, leaked := range []string{"custom", "file_search", "file_1", "input_audio", "web_search", "allowed_tools", "y.pdf"} {
				if strings.Contains(body, leaked) {
					t.Errorf("%s must not reach the chat body: %s", leaked, body)
				}
			}
			if tools, _ := m["tools"].([]any); len(tools) != tc.wantTools {
				t.Errorf("function tools beside it = %v, want %d", m["tools"], tc.wantTools)
			}
		})
	}
	plain, err := TranslateRequestToChat([]byte(`{"model":"m","input":"x","tools":[{"type":"function","name":"f"}]}`))
	if err != nil || plain.NativeOnly != nil {
		t.Errorf("a plain request has no native-only member: %v %+v", err, plain.NativeOnly)
	}
}

// A rejection names the field and never the content, the contract the
// Responses handler documents and relies on: the native-only rejection is the
// one that reaches request_logs.error_message and the request.completed
// operator event, so a caller-supplied discriminator echoed into it would put
// request-body text in the logs the gateway promises never to store.
func TestTranslateRequestToChat_RejectionsCarryNoCallerValue(t *testing.T) {
	const sentinel = "ZZSENTINELZZ"

	nativeOnly := []struct{ name, body string }{
		{"item type", `{"model":"m","input":[{"type":"ZZSENTINELZZ","call_id":"c"},{"role":"user","content":"hi"}]}`},
		{"content part type", `{"model":"m","input":[{"role":"user","content":[{"type":"ZZSENTINELZZ"}]}]}`},
		{"tool type", `{"model":"m","input":"x","tools":[{"type":"ZZSENTINELZZ","name":"t"}]}`},
		{"namespaced tool type", `{"model":"m","input":"x","tools":[{"type":"namespace","name":"n","tools":[{"type":"ZZSENTINELZZ","name":"c"}]}]}`},
		{"tool_choice type", `{"model":"m","input":"x","tool_choice":{"type":"ZZSENTINELZZ"}}`},
	}
	for _, tc := range nativeOnly {
		t.Run(tc.name, func(t *testing.T) {
			tr, err := TranslateRequestToChat([]byte(tc.body))
			if err != nil {
				t.Fatalf("a native-only member must not fail the translation: %v", err)
			}
			if tr.NativeOnly == nil {
				t.Fatal("expected a native-only rejection")
			}
			if msg := tr.NativeOnly.Error(); strings.Contains(msg, sentinel) {
				t.Errorf("the rejection echoes the caller's value into a logged message: %s", msg)
			}
			if tr.NativeOnly.Field == "" {
				t.Error("the rejection must still name the field it refuses")
			}
		})
	}

	// The other direction, so a later tidy-up does not strip these too. A
	// refusal the handler answers before the pending row exists is never
	// stored, so it may quote the caller's own value back at them, and taking
	// it away would cost diagnostics for no security gain.
	refused := []struct{ name, body string }{
		{"role", `{"model":"m","input":[{"role":"ZZSENTINELZZ","content":"hi"}]}`},
		{"duplicate tool name", `{"model":"m","input":"x","tools":[{"type":"function","name":"ZZSENTINELZZ"},{"type":"function","name":"ZZSENTINELZZ"}]}`},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			_, err := TranslateRequestToChat([]byte(tc.body))
			if err == nil {
				t.Fatal("expected the request to be refused")
			}
			var rejected *RejectedRequest
			if !errors.As(err, &rejected) {
				t.Fatalf("want a RejectedRequest, the shape the handler answers without logging: %v", err)
			}
			if !strings.Contains(err.Error(), sentinel) {
				t.Errorf("a client-only refusal should still name the value the caller sent: %s", err)
			}
		})
	}
}
