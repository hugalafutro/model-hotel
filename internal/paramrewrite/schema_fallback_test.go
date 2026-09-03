package paramrewrite

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

const deepSeekSchemaRefusal = `{"error":{"message":"This response_format type is unavailable now","type":"invalid_request_error","param":null,"code":"invalid_request_error"}}`

func schemaRequest(system string) []byte {
	msgs := `[{"role":"user","content":"Facts about Tokyo."}]`
	if system != "" {
		msgs = `[{"role":"system","content":"` + system + `"},{"role":"user","content":"Facts about Tokyo."}]`
	}
	return []byte(`{"model":"m","messages":` + msgs + `,"response_format":{"type":"json_schema","json_schema":{"name":"city","strict":true,"schema":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}}}`)
}

func decodeJSONBody(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	return raw
}

// A 400 about the response format is learned as the schema fallback key, not
// as a strip of response_format.
func TestParseProviderParamError_SchemaRefusalLearnsTheFallback(t *testing.T) {
	rejected := ParseProviderParamError([]byte(deepSeekSchemaRefusal))
	if !rejected[SchemaFallbackKey] {
		t.Fatalf("rejected = %v, want %s", rejected, SchemaFallbackKey)
	}
	if rejected["response_format"] {
		t.Error("response_format itself must not be stripped")
	}
	for _, msg := range []string{
		"Unsupported parameter: 'temperature'",
		"Invalid schema for response_format 'city': 'additionalProperties' is required to be supplied and to be false",
		"Prompt must contain the word 'json' in some form to use 'response_format' of type 'json_object'.",
		"response_format.json_schema.schema.properties.email: format 'email' is not supported",
		"json_schema response format is not supported when tools are provided",
	} {
		if ParseProviderParamError([]byte(`{"error":{"message":"` + msg + `"}}`))[SchemaFallbackKey] {
			t.Errorf("%q must not learn the fallback", msg)
		}
	}
	if !ParseProviderParamError([]byte(`{"error":{"message":"'response_format' of type 'json_schema' is not supported with this model."}}`))[SchemaFallbackKey] {
		t.Error("a shape the provider does not serve must learn the fallback")
	}
}

// The fallback is kept only for a request that sent json_schema; a JSON-mode
// request's 400 drops it, and the set is nil once nothing else is left.
func TestDropSchemaFallbackUnlessRequested(t *testing.T) {
	plain := []byte(`{"messages":[],"response_format":{"type":"json_object"}}`)
	if got := DropSchemaFallbackUnlessRequested(map[string]bool{SchemaFallbackKey: true}, plain); got != nil {
		t.Errorf("json_object request: got %v, want nil", got)
	}
	got := DropSchemaFallbackUnlessRequested(map[string]bool{SchemaFallbackKey: true, "top_p": true}, plain)
	if got[SchemaFallbackKey] || !got["top_p"] {
		t.Errorf("json_object request with another param: got %v, want top_p alone", got)
	}
	if got := DropSchemaFallbackUnlessRequested(map[string]bool{SchemaFallbackKey: true}, schemaRequest("")); !got[SchemaFallbackKey] {
		t.Error("a json_schema request keeps the fallback")
	}
	if DropSchemaFallbackUnlessRequested(nil, plain) != nil {
		t.Error("nil stays nil")
	}
}

// A native rebuild keeps json_schema for its translator, even on a provider
// type or model the fallback would rewrite on the chat-completions route,
// and learned state is what makes a body worth rebuilding.
func TestBuildNativeUpstreamBody_KeepsJSONSchema(t *testing.T) {
	var dep, ren sync.Map
	MergeLearnedParamCache(&dep, LearnedCacheKey("p", "m"), map[string]bool{SchemaFallbackKey: true})
	raw := decodeJSONBody(t, BuildNativeUpstreamBody(schemaRequest(""), "deepseek", "m", "m", &dep, &ren, map[string]bool{SchemaFallbackKey: true}, "p"))
	if raw["response_format"].(map[string]any)["type"] != "json_schema" || len(raw["messages"].([]any)) != 1 {
		t.Errorf("native body = %v, want json_schema kept and no instruction", raw)
	}
	if !HasLearnedRewrites(&dep, &ren, "p", "m") || HasLearnedRewrites(&dep, &ren, "p", "other") {
		t.Error("HasLearnedRewrites must follow the learned caches")
	}
	MergeLearnedParamCache(&ren, LearnedCacheKey("q", "m"), map[string]string{"max_tokens": "max_completion_tokens"})
	if !HasLearnedRewrites(&dep, &ren, "q", "m") {
		t.Error("a learned rename counts too")
	}
}

// A model that ignores json_schema keeps it on the request and gets the schema
// in the prompt too, on any provider type; Z.AI's coding type is JSON-mode
// only by its docs and downgrades like DeepSeek.
func TestBuildUpstreamBody_SchemaIgnoringModelKeepsSchemaAndFolds(t *testing.T) {
	var dep, ren sync.Map
	body := []byte(`{"model":"glm-5.3-flash","messages":[{"role":"user","content":"Tokyo?"}],"response_format":{"type":"json_schema","json_schema":{"name":"city","schema":{"type":"object","required":["city"]}}}}`)
	raw := decodeJSONBody(t, BuildUpstreamBody(body, "ollama-cloud", "glm-5.3-flash", "glm-5.3-flash", false, &dep, &ren, nil, "p"))
	if raw["response_format"].(map[string]any)["type"] != "json_schema" {
		t.Error("json_schema must stay on the request for a host that may enforce it")
	}
	msgs := raw["messages"].([]any)
	if text, _ := msgs[0].(map[string]any)["content"].(string); len(msgs) != 2 || !strings.Contains(text, `"required":["city"]`) {
		t.Errorf("messages = %v, want the schema folded into a leading system turn", msgs)
	}
	raw = decodeJSONBody(t, BuildUpstreamBody(body, "zai-coding", "glm-5.3-flash", "glm-5.3-flash", false, &dep, &ren, nil, "p"))
	if raw["response_format"].(map[string]any)["type"] != "json_object" || len(raw["messages"].([]any)) != 2 {
		t.Error("a JSON-mode-only type downgrades and folds")
	}
	other := []byte(`{"model":"llama3","messages":[{"role":"user","content":"Tokyo?"}],"response_format":{"type":"json_schema","json_schema":{"name":"city","schema":{"type":"object"}}}}`)
	raw = decodeJSONBody(t, BuildUpstreamBody(other, "ollama-cloud", "llama3", "llama3", false, &dep, &ren, nil, "p"))
	if len(raw["messages"].([]any)) != 1 {
		t.Error("a model outside the rule is left alone")
	}
}

// A leading system message made of content parts takes the instruction as a
// text part; a json_schema with no schema object gets the plain JSON-mode
// instruction.
func TestBuildUpstreamBody_SchemaInstructionShapes(t *testing.T) {
	var dep, ren sync.Map
	parts := []byte(`{"model":"m","messages":[{"role":"system","content":[{"type":"text","text":"Be terse."}]},{"role":"user","content":"Tokyo?"}],"response_format":{"type":"json_schema","json_schema":{"name":"city"}}}`)
	raw := decodeJSONBody(t, BuildUpstreamBody(parts, "deepseek", "m", "m", false, &dep, &ren, nil, "p"))
	msgs := raw["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want no extra turn", len(msgs))
	}
	content := msgs[0].(map[string]any)["content"].([]any)
	if len(content) != 2 {
		t.Fatalf("system parts = %d, want the instruction appended as a part", len(content))
	}
	text, _ := content[1].(map[string]any)["text"].(string)
	if !strings.HasPrefix(text, "Respond with a single JSON object") || strings.Contains(text, "JSON Schema") {
		t.Errorf("instruction part = %q, want the plain JSON-mode instruction with no schema", text)
	}
	// A leading developer turn, or a system turn with content of no usable
	// shape, takes the instruction rather than gaining a second system turn.
	for _, lead := range []string{`{"role":"developer","content":"Be terse."}`, `{"role":"system","content":null}`} {
		body := []byte(`{"model":"m","messages":[` + lead + `,{"role":"user","content":"Tokyo?"}],"response_format":{"type":"json_schema","json_schema":{"schema":{"type":"object"}}}}`)
		raw := decodeJSONBody(t, BuildUpstreamBody(body, "deepseek", "m", "m", false, &dep, &ren, nil, "p"))
		msgs := raw["messages"].([]any)
		text, _ := msgs[0].(map[string]any)["content"].(string)
		if len(msgs) != 2 || !strings.Contains(text, "JSON Schema") {
			t.Errorf("%s: messages = %v, want the instruction on the leading turn", lead, msgs)
		}
	}
	// Content of a shape the join cannot keep is left alone; the instruction
	// goes ahead of it as its own turn.
	odd := []byte(`{"model":"m","messages":[{"role":"system","content":{"weird":1}},{"role":"user","content":"Tokyo?"}],"response_format":{"type":"json_schema","json_schema":{"schema":{"type":"object"}}}}`)
	raw = decodeJSONBody(t, BuildUpstreamBody(odd, "deepseek", "m", "m", false, &dep, &ren, nil, "p"))
	if msgs := raw["messages"].([]any); len(msgs) != 3 || msgs[1].(map[string]any)["content"].(map[string]any)["weird"] == nil {
		t.Errorf("odd content: messages = %v, want the caller's turn kept behind the instruction", msgs)
	}
	// A schema past the prompt bound is left out; the model still gets JSON mode.
	big := `{"type":"object","properties":{"x":{"type":"string","description":"` + strings.Repeat("a", schemaPromptMax) + `"}}}`
	body := []byte(`{"model":"m","messages":[{"role":"user","content":"Tokyo?"}],"response_format":{"type":"json_schema","json_schema":{"schema":` + big + `}}}`)
	raw = decodeJSONBody(t, BuildUpstreamBody(body, "deepseek", "m", "m", false, &dep, &ren, nil, "p"))
	text, _ = raw["messages"].([]any)[0].(map[string]any)["content"].(string)
	if strings.Contains(text, "JSON Schema") || raw["response_format"].(map[string]any)["type"] != "json_object" {
		t.Error("an oversized schema must be left out of the prompt, JSON mode kept")
	}
}

// A JSON-mode-only provider type gets json_schema rewritten on the first
// attempt: JSON mode, the schema in a new leading system message.
func TestBuildUpstreamBody_JSONModeOnlyProviderDowngradesSchema(t *testing.T) {
	var dep, ren sync.Map
	raw := decodeJSONBody(t, BuildUpstreamBody(schemaRequest(""), "deepseek", "m", "m", false, &dep, &ren, nil, "p"))
	rf := raw["response_format"].(map[string]any)
	if rf["type"] != "json_object" || rf["json_schema"] != nil {
		t.Fatalf("response_format = %v, want plain json_object", rf)
	}
	msgs := raw["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want the instruction prepended", len(msgs))
	}
	first := msgs[0].(map[string]any)
	content, _ := first["content"].(string)
	if first["role"] != "system" || !strings.Contains(content, `"required":["city"]`) || !strings.Contains(strings.ToLower(content), "json") {
		t.Errorf("leading message = %v, want a system turn carrying the schema and the word json", first)
	}
	if msgs[1].(map[string]any)["role"] != "user" {
		t.Error("the caller's messages must follow the instruction")
	}
}

// The instruction joins an existing leading system message rather than adding
// a second system turn.
func TestBuildUpstreamBody_SchemaInstructionJoinsTheSystemMessage(t *testing.T) {
	var dep, ren sync.Map
	raw := decodeJSONBody(t, BuildUpstreamBody(schemaRequest("You are terse."), "deepseek", "m", "m", false, &dep, &ren, nil, "p"))
	msgs := raw["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want no extra turn", len(msgs))
	}
	content := msgs[0].(map[string]any)["content"].(string)
	if !strings.HasPrefix(content, "You are terse.") || !strings.Contains(content, "JSON Schema") {
		t.Errorf("system content = %q, want the caller's text followed by the instruction", content)
	}
}

// Other provider types keep json_schema until a 400 teaches the fallback,
// through the learned cache or the retry's extra strip, and a request in JSON
// mode already is left alone either way.
func TestBuildUpstreamBody_SchemaFallbackOnlyWhenLearned(t *testing.T) {
	var dep, ren sync.Map
	raw := decodeJSONBody(t, BuildUpstreamBody(schemaRequest(""), "openai", "m", "m", false, &dep, &ren, nil, "p"))
	if raw["response_format"].(map[string]any)["type"] != "json_schema" {
		t.Fatal("an unlearned provider must keep json_schema")
	}
	raw = decodeJSONBody(t, BuildUpstreamBody(schemaRequest(""), "openai", "m", "m", false, &dep, &ren, map[string]bool{SchemaFallbackKey: true}, "p"))
	if raw["response_format"].(map[string]any)["type"] != "json_object" {
		t.Error("the retry's extra strip must apply the fallback")
	}
	MergeLearnedParamCache(&dep, LearnedCacheKey("p", "m"), map[string]bool{SchemaFallbackKey: true})
	raw = decodeJSONBody(t, BuildUpstreamBody(schemaRequest(""), "openai", "m", "m", false, &dep, &ren, nil, "p"))
	if raw["response_format"].(map[string]any)["type"] != "json_object" {
		t.Error("the learned cache must apply the fallback on later requests")
	}
	if _, present := raw[SchemaFallbackKey]; present {
		t.Error("the pseudo-param must never reach the upstream body")
	}
	plain := []byte(`{"model":"m","messages":[{"role":"user","content":"json please"}],"response_format":{"type":"json_object"}}`)
	raw = decodeJSONBody(t, BuildUpstreamBody(plain, "deepseek", "m", "m", false, &dep, &ren, nil, "p"))
	if len(raw["messages"].([]any)) != 1 {
		t.Error("a JSON-mode request must not gain an instruction")
	}
}
