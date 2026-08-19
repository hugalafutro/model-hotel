package anthropicegress

import (
	"encoding/json"
	"strings"
	"testing"
)

// build runs BuildChatCompletion and decodes the result for assertions.
func build(t *testing.T, body string) completionResponse {
	t.Helper()
	out, err := BuildChatCompletion([]byte(body), "resp_1", "claude-x", 1234)
	if err != nil {
		t.Fatalf("BuildChatCompletion failed: %v", err)
	}
	var resp completionResponse
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
	if resp.Usage.TotalTokens != 120 {
		t.Errorf("TotalTokens = %d, want 120", resp.Usage.TotalTokens)
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
