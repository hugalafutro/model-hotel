package anthropicegress

import (
	"encoding/json"
	"strings"
	"testing"
)

// The types below mirror the wire shape BuildChatCompletion produces, but are
// declared independently of completionResponse and friends (the production
// types) — same pattern as translatedRequest in request_test.go. Decoding the
// output with the very struct that produced it would make a JSON-tag
// regression on the production side invisible to these tests.

type translatedCompletion struct {
	ID      string             `json:"id"`
	Object  string             `json:"object"`
	Created int64              `json:"created"`
	Model   string             `json:"model"`
	Choices []translatedChoice `json:"choices"`
	Usage   *translatedUsage   `json:"usage"`
}

type translatedChoice struct {
	Index        int                  `json:"index"`
	FinishReason string               `json:"finish_reason"`
	Message      translatedMessageOut `json:"message"`
}

type translatedMessageOut struct {
	Role             string               `json:"role"`
	Content          *string              `json:"content"`
	ReasoningContent string               `json:"reasoning_content"`
	ToolCalls        []translatedToolCall `json:"tool_calls"`
}

type translatedToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type translatedUsage struct {
	PromptTokens             int                      `json:"prompt_tokens"`
	CompletionTokens         int                      `json:"completion_tokens"`
	TotalTokens              int                      `json:"total_tokens"`
	CacheCreationInputTokens int                      `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int                      `json:"cache_read_input_tokens"`
	PromptTokensDetails      *translatedPromptDetails `json:"prompt_tokens_details"`
}

type translatedPromptDetails struct {
	CachedTokens int `json:"cached_tokens"`
}

// build runs BuildChatCompletion and decodes the result for assertions.
func build(t *testing.T, body string) translatedCompletion {
	t.Helper()
	out, err := BuildChatCompletion([]byte(body), "resp_1", "claude-x", 1234)
	if err != nil {
		t.Fatalf("BuildChatCompletion failed: %v", err)
	}
	var resp translatedCompletion
	if err := json.Unmarshal(out, &resp); err != nil {
		t.Fatalf("translated body is not valid JSON: %v", err)
	}
	return resp
}

func TestBuildChatCompletion_Envelope(t *testing.T) {
	resp := build(t, `{"type":"message","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn"}`)
	if resp.ID != "resp_1" {
		t.Errorf("ID = %q, want resp_1", resp.ID)
	}
	if resp.Object != "chat.completion" {
		t.Errorf("Object = %q, want chat.completion", resp.Object)
	}
	if resp.Created != 1234 {
		t.Errorf("Created = %d, want 1234", resp.Created)
	}
	if resp.Model != "claude-x" {
		t.Errorf("Model = %q, want claude-x", resp.Model)
	}
	if len(resp.Choices) != 1 || resp.Choices[0].Index != 0 {
		t.Fatalf("Choices = %+v, want exactly one choice at index 0", resp.Choices)
	}
}

func TestBuildChatCompletion_TextBlocksConcatenate(t *testing.T) {
	resp := build(t, `{"type":"message","content":[
		{"type":"text","text":"Hello, "},
		{"type":"text","text":"world."}
	],"stop_reason":"end_turn"}`)
	msg := resp.Choices[0].Message
	if msg.Content == nil || *msg.Content != "Hello, world." {
		t.Errorf("Content = %v, want %q", msg.Content, "Hello, world.")
	}
	if msg.Role != "assistant" {
		t.Errorf("Role = %q, want assistant", msg.Role)
	}
}

func TestBuildChatCompletion_NoTextBlocksIsEmptyString(t *testing.T) {
	resp := build(t, `{"type":"message","content":[],"stop_reason":"end_turn"}`)
	msg := resp.Choices[0].Message
	if msg.Content == nil || *msg.Content != "" {
		t.Errorf("Content = %v, want empty string (not omitted)", msg.Content)
	}
}

func TestBuildChatCompletion_ThinkingBlocksConcatenate(t *testing.T) {
	resp := build(t, `{"type":"message","content":[
		{"type":"thinking","thinking":"step one. "},
		{"type":"text","text":"answer"},
		{"type":"thinking","thinking":"step two."}
	],"stop_reason":"end_turn"}`)
	msg := resp.Choices[0].Message
	if msg.ReasoningContent != "step one. step two." {
		t.Errorf("ReasoningContent = %q, want %q", msg.ReasoningContent, "step one. step two.")
	}
	if msg.Content == nil || *msg.Content != "answer" {
		t.Errorf("Content = %v, want %q", msg.Content, "answer")
	}
}

func TestBuildChatCompletion_ReasoningContentOmittedWhenEmpty(t *testing.T) {
	out, err := BuildChatCompletion(
		[]byte(`{"type":"message","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn"}`),
		"resp_1", "claude-x", 1234)
	if err != nil {
		t.Fatalf("BuildChatCompletion failed: %v", err)
	}
	if strings.Contains(string(out), "reasoning_content") {
		t.Errorf("output carries reasoning_content when no thinking block was present: %s", out)
	}
}

func TestBuildChatCompletion_ToolUseBlocksBecomeToolCalls(t *testing.T) {
	resp := build(t, `{"type":"message","content":[
		{"type":"tool_use","id":"toolu_1","name":"get_weather","input":{"city":"Lyon"}},
		{"type":"tool_use","id":"toolu_2","name":"get_time","input":{}}
	],"stop_reason":"tool_use"}`)
	msg := resp.Choices[0].Message
	if len(msg.ToolCalls) != 2 {
		t.Fatalf("ToolCalls = %+v, want 2 entries", msg.ToolCalls)
	}
	first := msg.ToolCalls[0]
	if first.ID != "toolu_1" || first.Type != "function" || first.Function.Name != "get_weather" {
		t.Errorf("first tool call = %+v", first)
	}
	if first.Function.Arguments != `{"city":"Lyon"}` {
		t.Errorf("Arguments = %q, want a compact JSON string of the input object", first.Function.Arguments)
	}
	var decoded map[string]string
	if err := json.Unmarshal([]byte(first.Function.Arguments), &decoded); err != nil {
		t.Fatalf("Arguments is not valid JSON: %v", err)
	}
	second := msg.ToolCalls[1]
	if second.ID != "toolu_2" || second.Function.Arguments != "{}" {
		t.Errorf("second tool call = %+v, want empty-object arguments", second)
	}
	if resp.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want tool_calls", resp.Choices[0].FinishReason)
	}
}

// TestBuildChatCompletion_ToolUseInputAbsent covers a realistic zero-argument
// tool call: Anthropic omits the "input" key entirely rather than sending
// "input":{}. This is the only live branch of toolArguments' len(trimmed)==0
// guard (an "input":{} block never reaches it).
func TestBuildChatCompletion_ToolUseInputAbsent(t *testing.T) {
	resp := build(t, `{"type":"message","content":[
		{"type":"tool_use","id":"toolu_3","name":"noop"}
	],"stop_reason":"tool_use"}`)
	msg := resp.Choices[0].Message
	if len(msg.ToolCalls) != 1 {
		t.Fatalf("ToolCalls = %+v, want 1 entry", msg.ToolCalls)
	}
	if got := msg.ToolCalls[0].Function.Arguments; got != "{}" {
		t.Errorf("Arguments = %q, want {} for an absent input key", got)
	}
}

// TestBuildChatCompletion_ToolUseInputNull covers an explicit JSON null
// input, which is valid JSON but not the object shape a tool-call arguments
// string is supposed to carry.
func TestBuildChatCompletion_ToolUseInputNull(t *testing.T) {
	resp := build(t, `{"type":"message","content":[
		{"type":"tool_use","id":"toolu_4","name":"noop","input":null}
	],"stop_reason":"tool_use"}`)
	msg := resp.Choices[0].Message
	if got := msg.ToolCalls[0].Function.Arguments; got != "{}" {
		t.Errorf("Arguments = %q, want {} for an explicit null input", got)
	}
}

func TestBuildChatCompletion_ToolCallsOmittedWhenAbsent(t *testing.T) {
	out, err := BuildChatCompletion(
		[]byte(`{"type":"message","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn"}`),
		"resp_1", "claude-x", 1234)
	if err != nil {
		t.Fatalf("BuildChatCompletion failed: %v", err)
	}
	if strings.Contains(string(out), "tool_calls") {
		t.Errorf("output carries tool_calls when no tool_use block was present: %s", out)
	}
}

func TestBuildChatCompletion_UnknownBlockTypeIgnored(t *testing.T) {
	resp := build(t, `{"type":"message","content":[
		{"type":"redacted_thinking","data":"opaque"},
		{"type":"text","text":"visible"}
	],"stop_reason":"end_turn"}`)
	msg := resp.Choices[0].Message
	if msg.Content == nil || *msg.Content != "visible" {
		t.Errorf("Content = %v, want %q", msg.Content, "visible")
	}
	if msg.ReasoningContent != "" {
		t.Errorf("ReasoningContent = %q, want empty", msg.ReasoningContent)
	}
}

func TestBuildChatCompletion_FinishReasonMapping(t *testing.T) {
	cases := []struct {
		stopReason string
		want       string
	}{
		{"end_turn", "stop"},
		{"stop_sequence", "stop"},
		{"max_tokens", "length"},
		{"tool_use", "tool_calls"},
		{"", "stop"},
		{"pause_turn", "stop"},
		{"refusal", "stop"},
	}
	for _, c := range cases {
		body := `{"type":"message","content":[],"stop_reason":"` + c.stopReason + `"}`
		if c.stopReason == "" {
			body = `{"type":"message","content":[]}`
		}
		resp := build(t, body)
		if got := resp.Choices[0].FinishReason; got != c.want {
			t.Errorf("stop_reason=%q: FinishReason = %q, want %q", c.stopReason, got, c.want)
		}
	}
}

func TestMapFinishReason(t *testing.T) {
	cases := map[string]string{
		"end_turn":      "stop",
		"stop_sequence": "stop",
		"max_tokens":    "length",
		"tool_use":      "tool_calls",
		"":              "stop",
		"pause_turn":    "stop",
	}
	for in, want := range cases {
		if got := mapFinishReason(in); got != want {
			t.Errorf("mapFinishReason(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildChatCompletion_UsageBasic(t *testing.T) {
	resp := build(t, `{"type":"message","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn",
		"usage":{"input_tokens":10,"output_tokens":5}}`)
	if resp.Usage == nil {
		t.Fatal("Usage is nil, want populated")
	}
	if resp.Usage.PromptTokens != 10 || resp.Usage.CompletionTokens != 5 || resp.Usage.TotalTokens != 15 {
		t.Errorf("Usage = %+v, want prompt=10 completion=5 total=15", resp.Usage)
	}
	if resp.Usage.CacheCreationInputTokens != 0 || resp.Usage.CacheReadInputTokens != 0 {
		t.Errorf("Usage cache fields = %+v, want zero", resp.Usage)
	}
	if resp.Usage.PromptTokensDetails != nil {
		t.Errorf("PromptTokensDetails = %+v, want nil when no cache read", resp.Usage.PromptTokensDetails)
	}
}

func TestBuildChatCompletion_UsageAbsent(t *testing.T) {
	resp := build(t, `{"type":"message","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn"}`)
	if resp.Usage != nil {
		t.Errorf("Usage = %+v, want nil when upstream sent none", resp.Usage)
	}
}

// TestBuildChatCompletion_UsageCacheFields checks that prompt_tokens is the
// SUM of input_tokens, cache_creation_input_tokens and cache_read_input_tokens
// (Anthropic's three counts are disjoint additions), not just input_tokens —
// and that the cache pair also rides through under their own field names.
func TestBuildChatCompletion_UsageCacheFields(t *testing.T) {
	resp := build(t, `{"type":"message","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn",
		"usage":{"input_tokens":100,"output_tokens":20,"cache_creation_input_tokens":30,"cache_read_input_tokens":70}}`)
	if resp.Usage == nil {
		t.Fatal("Usage is nil, want populated")
	}
	if resp.Usage.CacheCreationInputTokens != 30 {
		t.Errorf("CacheCreationInputTokens = %d, want 30", resp.Usage.CacheCreationInputTokens)
	}
	if resp.Usage.CacheReadInputTokens != 70 {
		t.Errorf("CacheReadInputTokens = %d, want 70", resp.Usage.CacheReadInputTokens)
	}
	if resp.Usage.PromptTokensDetails == nil || resp.Usage.PromptTokensDetails.CachedTokens != 70 {
		t.Errorf("PromptTokensDetails = %+v, want CachedTokens=70", resp.Usage.PromptTokensDetails)
	}
	if resp.Usage.PromptTokens != 200 {
		t.Errorf("PromptTokens = %d, want 100+30+70=200 (Anthropic's three counts are disjoint additions)", resp.Usage.PromptTokens)
	}
	if resp.Usage.TotalTokens != 220 {
		t.Errorf("TotalTokens = %d, want 220", resp.Usage.TotalTokens)
	}
}

// TestBuildChatCompletion_UsageCacheHitInvariant reproduces the case the
// undercounting bug broke: a heavily cached prompt where input_tokens is tiny
// relative to cache_read_input_tokens. prompt_tokens must be at least the
// cached_tokens count, or internal/proxy/helpers.go's extractCacheTokens
// computes a negative-clamped miss count and hides the vast majority of the
// prompt from billing.
func TestBuildChatCompletion_UsageCacheHitInvariant(t *testing.T) {
	resp := build(t, `{"type":"message","content":[{"type":"text","text":"hi"}],"stop_reason":"end_turn",
		"usage":{"input_tokens":4,"output_tokens":50,"cache_read_input_tokens":20000}}`)
	if resp.Usage == nil {
		t.Fatal("Usage is nil, want populated")
	}
	cached := resp.Usage.PromptTokensDetails
	if cached == nil {
		t.Fatal("PromptTokensDetails is nil, want CachedTokens=20000")
	}
	if resp.Usage.PromptTokens < cached.CachedTokens {
		t.Errorf("PromptTokens = %d is less than cached_tokens = %d: cached_tokens must be a subset of prompt_tokens",
			resp.Usage.PromptTokens, cached.CachedTokens)
	}
	if resp.Usage.PromptTokens != 20004 {
		t.Errorf("PromptTokens = %d, want 4+20000=20004", resp.Usage.PromptTokens)
	}
	if resp.Usage.TotalTokens != 20054 {
		t.Errorf("TotalTokens = %d, want 20004+50=20054", resp.Usage.TotalTokens)
	}
}

func TestBuildChatCompletion_InvalidBody(t *testing.T) {
	_, err := BuildChatCompletion([]byte(`{not json`), "resp_1", "claude-x", 1234)
	if err == nil {
		t.Fatal("expected an error for an invalid body")
	}
	if !strings.HasPrefix(err.Error(), "anthropicegress:") {
		t.Errorf("error %q not prefixed anthropicegress:", err.Error())
	}
}

func TestBuildChatCompletion_InvalidBodyErrorOmitsContent(t *testing.T) {
	// The decode error describes WHERE the body broke, never what it said:
	// encoding/json quotes the offending byte and prints an offending literal
	// verbatim, and a response body is model output.
	_, err := BuildChatCompletion([]byte(`{"type":"message","content":[{"type":"text","text":"Kohlrabi"`), "resp_1", "claude-x", 1234)
	if err == nil {
		t.Fatal("expected an error for a truncated body")
	}
	if !strings.HasPrefix(err.Error(), "anthropicegress: invalid upstream response: malformed JSON at byte ") {
		t.Errorf("error = %q, want the sanitized malformed-JSON form", err)
	}
	if strings.Contains(err.Error(), "Kohlrabi") {
		t.Errorf("error leaked response content: %q", err)
	}
}

func TestBuildChatCompletion_ErrorEnvelope(t *testing.T) {
	_, err := BuildChatCompletion(
		[]byte(`{"type":"error","error":{"type":"overloaded_error","message":"secret upstream detail"}}`),
		"resp_1", "claude-x", 1234)
	if err == nil {
		t.Fatal("expected an error for an error envelope")
	}
	if !strings.HasPrefix(err.Error(), "anthropicegress:") {
		t.Errorf("error %q not prefixed anthropicegress:", err.Error())
	}
	if strings.Contains(err.Error(), "secret upstream detail") {
		t.Errorf("error %q leaks the upstream error message (response content)", err.Error())
	}
	if !strings.Contains(err.Error(), "overloaded_error") {
		t.Errorf("error %q does not name the error type", err.Error())
	}
}

func TestBuildChatCompletion_ErrorEnvelopeWithoutErrorField(t *testing.T) {
	_, err := BuildChatCompletion([]byte(`{"type":"error"}`), "resp_1", "claude-x", 1234)
	if err == nil {
		t.Fatal("expected an error for an error envelope with no error field")
	}
	if !strings.HasPrefix(err.Error(), "anthropicegress:") {
		t.Errorf("error %q not prefixed anthropicegress:", err.Error())
	}
}

// TestBuildChatCompletion_NotAMessage covers bodies that parse as valid JSON
// but are neither a Messages response nor a recognised error envelope: they
// must not become a synthetic empty success.
func TestBuildChatCompletion_NotAMessage(t *testing.T) {
	cases := map[string]string{
		"empty object":            `{}`,
		"null":                    `null`,
		"message-shaped, no type": `{"id":"msg_1","content":[]}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := BuildChatCompletion([]byte(body), "resp_1", "claude-x", 1234)
			if err == nil {
				t.Fatalf("expected an error for body %s", body)
			}
			if !strings.HasPrefix(err.Error(), "anthropicegress:") {
				t.Errorf("error %q not prefixed anthropicegress:", err.Error())
			}
		})
	}
}
