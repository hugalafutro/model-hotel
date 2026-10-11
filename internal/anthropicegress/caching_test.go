package anthropicegress

import (
	"encoding/json"
	"strings"
	"testing"
)

// decodeAnt decodes a translated Messages body into a generic map for shape
// assertions that the typed request struct would hide (string vs array system).
func decodeAnt(t *testing.T, body []byte) map[string]any {
	t.Helper()
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatalf("decode translated body: %v\n%s", err, body)
	}
	return out
}

func hasCacheControl(v any) bool {
	m, _ := v.(map[string]any)
	cc, _ := m["cache_control"].(map[string]any)
	return cc["type"] == "ephemeral"
}

func lastBlock(t *testing.T, msg any) any {
	t.Helper()
	m, _ := msg.(map[string]any)
	blocks, ok := m["content"].([]any)
	if !ok || len(blocks) == 0 {
		t.Fatalf("turn content is not a non-empty block array: %v", m["content"])
	}
	return blocks[len(blocks)-1]
}

const cachedChatBody = `{
	"model":"claude-x",
	"messages":[
		{"role":"system","content":"You are a careful auditor."},
		{"role":"user","content":"first question"},
		{"role":"assistant","content":"first answer"},
		{"role":"user","content":[{"type":"text","text":"second"},{"type":"text","text":"question"}]}
	],
	"tools":[
		{"type":"function","function":{"name":"a","parameters":{"type":"object"}}},
		{"type":"function","function":{"name":"b","parameters":{"type":"object"}}}
	]
}`

// TestTranslateRequest_CacheBreakpoints pins where the four breakpoints land:
// the last tool, the system prompt (lifted to a block), and the last block of
// each of the final two turns. Everything else keeps its shape: the first tool
// carries none, the first user turn stays a plain string.
func TestTranslateRequest_CacheBreakpoints(t *testing.T) {
	body, _, _, err := TranslateRequestWithOptions([]byte(cachedChatBody), Options{Caching: true})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(body), `"cache_control"`); n != MaxCacheBreakpoints {
		t.Fatalf("cache_control markers = %d, want exactly %d\n%s", n, MaxCacheBreakpoints, body)
	}
	out := decodeAnt(t, body)

	tools := out["tools"].([]any)
	if hasCacheControl(tools[0]) || !hasCacheControl(tools[1]) {
		t.Errorf("tools: want the breakpoint on the last tool only, got %v", tools)
	}

	system, ok := out["system"].([]any)
	if !ok || len(system) != 1 || !hasCacheControl(system[0]) || system[0].(map[string]any)["text"] != "You are a careful auditor." {
		t.Errorf("system: want one text block carrying the breakpoint, got %v", out["system"])
	}

	msgs := out["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3", len(msgs))
	}
	if _, isString := msgs[0].(map[string]any)["content"].(string); !isString {
		t.Errorf("an unmarked turn must keep its string content, got %v", msgs[0])
	}
	if !hasCacheControl(lastBlock(t, msgs[1])) {
		t.Errorf("the turn before the last must carry a breakpoint on its last block, got %v", msgs[1])
	}
	last := msgs[2].(map[string]any)["content"].([]any)
	if len(last) != 2 || hasCacheControl(last[0]) || !hasCacheControl(last[1]) {
		t.Errorf("the final turn must carry a breakpoint on its last block only, got %v", last)
	}
}

// TestTranslateRequest_NoCachingByDefault: TranslateRequest and the dialect
// variant place no breakpoints (the dashboard Test button uses TranslateRequest;
// the proxy, probe included, goes through Options), and the system prompt keeps
// its plain-string shape there.
func TestTranslateRequest_NoCachingByDefault(t *testing.T) {
	for name, translate := range map[string]func([]byte) ([]byte, string, bool, error){
		"TranslateRequest":            TranslateRequest,
		"TranslateRequestWithDialect": func(b []byte) ([]byte, string, bool, error) { return TranslateRequestWithDialect(b, ThinkingBudget) },
		"Options without caching":     func(b []byte) ([]byte, string, bool, error) { return TranslateRequestWithOptions(b, Options{}) },
	} {
		t.Run(name, func(t *testing.T) {
			body, _, _, err := translate([]byte(cachedChatBody))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(body), "cache_control") {
				t.Errorf("unexpected cache_control:\n%s", body)
			}
			if _, isString := decodeAnt(t, body)["system"].(string); !isString {
				t.Errorf("system must stay a string without caching, got %v", decodeAnt(t, body)["system"])
			}
		})
	}
}

// TestTranslateRequest_CacheBreakpointsFewerParts: with no tools, no system and
// one turn there is exactly one breakpoint, and a tool-result run (the shape
// every agent turn ends in) carries it on its last result.
func TestTranslateRequest_CacheBreakpointsFewerParts(t *testing.T) {
	body, _, _, err := TranslateRequestWithOptions([]byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`), Options{Caching: true})
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(body), `"cache_control"`); n != 1 {
		t.Errorf("single turn: markers = %d, want 1\n%s", n, body)
	}
	if _, present := decodeAnt(t, body)["system"]; present {
		t.Errorf("no system prompt must mean no system key at all\n%s", body)
	}

	agent := `{"model":"m","messages":[
		{"role":"user","content":"run"},
		{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"a","arguments":"{}"}},{"id":"c2","type":"function","function":{"name":"b","arguments":"{}"}}]},
		{"role":"tool","tool_call_id":"c1","content":"r1"},
		{"role":"tool","tool_call_id":"c2","content":"r2"}
	]}`
	body, _, _, err = TranslateRequestWithOptions([]byte(agent), Options{Caching: true})
	if err != nil {
		t.Fatal(err)
	}
	msgs := decodeAnt(t, body)["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages = %d, want 3 (user, assistant tool_use, user tool_result)", len(msgs))
	}
	if hasCacheControl(lastBlock(t, msgs[1])) != true || lastBlock(t, msgs[1]).(map[string]any)["type"] != "tool_use" {
		t.Errorf("the tool_use turn must carry the breakpoint on its last tool_use, got %v", msgs[1])
	}
	results := msgs[2].(map[string]any)["content"].([]any)
	if len(results) != 2 || hasCacheControl(results[0]) || !hasCacheControl(results[1]) || results[1].(map[string]any)["type"] != "tool_result" {
		t.Errorf("the tool_result run must carry the breakpoint on its last result only, got %v", results)
	}
	if n := strings.Count(string(body), `"cache_control"`); n != 2 {
		t.Errorf("agent turn: markers = %d, want 2\n%s", n, body)
	}
}

// TestTranslateRequest_EmptySystemHasNoKey guards the system field's change to
// an interface: an empty system prompt must still produce no "system" member,
// with or without caching, since Anthropic rejects an empty one.
func TestTranslateRequest_EmptySystemHasNoKey(t *testing.T) {
	for _, caching := range []bool{false, true} {
		body, _, _, err := TranslateRequestWithOptions([]byte(`{"model":"m","messages":[{"role":"system","content":""},{"role":"user","content":"hi"}]}`), Options{Caching: caching})
		if err != nil {
			t.Fatal(err)
		}
		if _, present := decodeAnt(t, body)["system"]; present {
			t.Errorf("caching=%v: empty system prompt produced a system key\n%s", caching, body)
		}
	}
}

// TestTranslateRequest_CacheBreakpointsSkipWhitespace: Anthropic rejects a
// breakpoint on an empty text block and a whitespace-only one reads as empty.
// A whitespace-only system prompt stays a plain string, and a turn ending in a
// whitespace-only text block carries its breakpoint on the block before it, or
// none when every block is whitespace.
func TestTranslateRequest_CacheBreakpointsSkipWhitespace(t *testing.T) {
	body, _, _, err := TranslateRequestWithOptions([]byte(`{"model":"m","messages":[
		{"role":"system","content":" \n "},
		{"role":"user","content":[{"type":"text","text":"real"},{"type":"text","text":"\n"}]},
		{"role":"assistant","content":"   "}
	]}`), Options{Caching: true})
	if err != nil {
		t.Fatal(err)
	}
	out := decodeAnt(t, body)
	if system, isString := out["system"].(string); !isString || system != " \n " {
		t.Errorf("a whitespace-only system prompt must stay a plain string, got %v", out["system"])
	}
	msgs := out["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("messages = %d, want 2\n%s", len(msgs), body)
	}
	user := msgs[0].(map[string]any)["content"].([]any)
	if len(user) != 2 || !hasCacheControl(user[0]) || hasCacheControl(user[1]) {
		t.Errorf("the breakpoint must skip the trailing whitespace block and land on the block before, got %v", user)
	}
	if _, isString := msgs[1].(map[string]any)["content"].(string); !isString {
		t.Errorf("a whitespace-only turn has nowhere to put a breakpoint and must keep its string content, got %v", msgs[1])
	}
	if n := strings.Count(string(body), `"cache_control"`); n != 1 {
		t.Errorf("markers = %d, want 1\n%s", n, body)
	}
}

// TestCacheControlRefused: only a 400 that names the field AND refuses it as
// unknown counts. Anthropic's own body-specific cache_control complaints
// (empty block, too many breakpoints, a thinking block) must not switch caching
// off for a model that supports it.
func TestCacheControlRefused(t *testing.T) {
	cases := map[string]bool{
		`{"type":"error","error":{"type":"invalid_request_error","message":"messages.2.content.1.cache_control: Extra inputs are not permitted"}}`: true,
		`{"error":{"message":"Unknown field: cache control"}}`:                                                                     true,
		`{"error":{"message":"unrecognized parameter 'cache_control' in system[0]"}}`:                                              true,
		`{"error":{"message":"cache_control is not supported by this endpoint"}}`:                                                  true,
		`{"error":{"message":"cache_control: unknown field (maximum properties exceeded)"}}`:                                       true,
		`{"detail":[{"loc":["body","system",0,"cache_control"],"msg":"Extra inputs are not permitted","type":"extra_forbidden"}]}`: true,
		`unknown field "cache_control" in request body`:                                                                            true,
		`{"message":"request contains the unsupported field cache_control"}`:                                                       true,
		`{"detail":"model not found"}`:                                                                                             false,
		// A pydantic item echoes the request under "input": the gateway's own
		// cache_control block and the user's words sit there, and a sibling
		// refusal of another field supplies "not permitted". Not a refusal of
		// the field, so not learned.
		`{"detail":[{"type":"missing","loc":["body","max_tokens"],"msg":"Field required","input":{"system":[{"type":"text","text":"this field is not permitted","cache_control":{"type":"ephemeral"}}]}},{"type":"extra_forbidden","loc":["body","top_k"],"msg":"Extra inputs are not permitted"}]}`: false,
		// JSON of another shape is not read at all, whatever it says.
		`{"err":"unknown field cache_control"}`: false,
		// Items are judged one by one: a required-field complaint about
		// cache_control beside a refusal of top_p is not a refusal of cache_control.
		`{"detail":[{"loc":["body","system",0,"cache_control"],"msg":"Field required"},{"loc":["body","top_p"],"msg":"Extra inputs are not permitted"}]}`: false,
		`{"detail":[{"loc":["body","top_p"],"msg":"Field required"},{"loc":["body","system",0,"cache_control"],"msg":"Extra inputs are not permitted"}]}`: true,
		`{"detail":null}`:    false,
		`{"detail":{"x":1}}`: false,
		`{"detail":[{"loc":[1,null,true],"msg":"cache_control: unknown field"}]}`: true,
		// One item's location naming the field and another's message refusing
		// something else do not add up to a refusal of the field.
		`{"detail":[{"loc":["body","system",0,"cache_control","type"],"msg":"Input should be 'ephemeral'"},{"loc":["body","top_k"],"msg":"Extra inputs are not permitted"}]}`: false,
		// A top-level array is read as items or strings, never whole: a bare
		// pydantic list echoes the request under "input" too.
		`["cache_control: Extra inputs are not permitted"]`: true,
		`"unknown field cache_control"`:                     true,
		`[{"loc":["body","top_k"],"msg":"Extra inputs are not permitted","input":{"system":[{"type":"text","text":"x","cache_control":{"type":"ephemeral"}}]}}]`: false,
		`[{"loc":["body","system",0,"cache_control"],"msg":"Extra inputs are not permitted","input":{"text":"nothing here matters"}}]`:                           true,
		`[{"loc":["body","system",0,"cache_control"],"msg":"Extra inputs are not permitted"}]`:                                                                   true,
		// An array of error envelopes is one message per envelope, judged apart.
		`[{"error":{"message":"cache_control: field required"}},{"error":{"message":"top_p is not permitted"}}]`: false,
		`[{"error":{"message":"top_p: field required"}},{"error":{"message":"unknown field cache_control"}}]`:    true,
		// A JSON body cut off by the learn-body cap is not read as text: its
		// echoed request would be.
		`{"detail":[{"loc":["body","top_k"],"msg":"Extra inputs are not permitted","input":{"system":[{"cache_control":{"type":"ephemeral"}}`: false,
		`  [{"loc":["body","cache_control"],"msg":"Extra inputs are not permitted"`:                                                           false,
		`cache_control: Extra inputs are not permitted (plain text)`:                                                                          true,
		// A "message" of another type is skipped, the items still read.
		`{"message":["x"],"detail":[{"loc":["body","cache_control"],"msg":"Extra inputs are not permitted"}]}`:                                        true,
		`{"error":{"message":"unrecognized field cache_control; ttl-based options are not permitted here"}}`:                                          true,
		`{"type":"error","error":{"type":"invalid_request_error","message":"cache_control cannot be set for empty text blocks"}}`:                     false,
		`{"type":"error","error":{"type":"invalid_request_error","message":"A maximum of 4 blocks with cache_control may be provided"}}`:              false,
		`{"type":"error","error":{"type":"invalid_request_error","message":"messages.1.content.0: cache_control is not allowed on thinking blocks"}}`: false,
		`{"type":"error","error":{"type":"invalid_request_error","message":"messages.0.content.0.cache_control.ttl: Input should be '5m' or '1h'"}}`:  false,
		`{"type":"error","error":{"type":"invalid_request_error","message":"messages.0.content.0.cache_control: field required"}}`:                    false,
		`{"type":"error","error":{"type":"invalid_request_error","message":"` + "`temperature` is deprecated for this model." + `"}}`:                 false,
		`{"type":"error","error":{"type":"invalid_request_error","message":"adaptive thinking is not supported on this model"}}`:                      false,
		`{"type":"error","error":{"type":"invalid_request_error","message":"messages.0.content.0.document: Input should be a valid string"}}`:         false,
		``: false,
	}
	for body, want := range cases {
		if got := CacheControlRefused([]byte(body)); got != want {
			t.Errorf("CacheControlRefused(%s) = %v, want %v", body, got, want)
		}
	}
}
