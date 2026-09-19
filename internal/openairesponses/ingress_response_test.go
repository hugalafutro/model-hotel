package openairesponses

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3/responses"
)

func decodeResponse(t *testing.T, body []byte) responses.Response {
	t.Helper()
	var r responses.Response
	if err := json.Unmarshal(body, &r); err != nil {
		t.Fatalf("openai-go cannot decode the Response: %v\n%s", err, body)
	}
	return r
}

func TestBuildResponse_TextReasoningToolsUsage(t *testing.T) {
	chat := []byte(`{"id":"chatcmpl-1","object":"chat.completion","created":1700000000,"model":"upstream-name","choices":[{"index":0,"message":{"role":"assistant","content":"hi","reasoning_content":"why","tool_calls":[{"id":"call_1","type":"function","function":{"name":"ls","arguments":"{\"p\":1}"}},{"id":"call_2","type":"function","function":{"name":"cat","arguments":{"f":"x"}}},{"id":"call_3","type":"function","function":{"name":"bad","arguments":"not json"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":"7","completion_tokens":3,"prompt_tokens_details":{"cached_tokens":2},"completion_tokens_details":{"reasoning_tokens":1}}}`)
	body, err := BuildResponse(chat, "resp_x", "hotel/g", nil)
	if err != nil {
		t.Fatal(err)
	}
	r := decodeResponse(t, body)
	if r.ID != "resp_x" || r.Model != "hotel/g" || r.Status != "completed" || r.CreatedAt != 1700000000 || r.Object != "response" {
		t.Fatalf("envelope = %+v", r)
	}
	if r.OutputText() != "hi" {
		t.Errorf("text = %q", r.OutputText())
	}
	if len(r.Output) != 5 || r.Output[0].Type != "reasoning" || r.Output[1].Type != "message" || r.Output[2].Type != "function_call" {
		t.Fatalf("output = %+v", r.Output)
	}
	if rs := r.Output[0].AsReasoning(); rs.Summary[0].Text != "why" {
		t.Errorf("reasoning = %+v", rs)
	}
	c1, c2, c3 := r.Output[2].AsFunctionCall(), r.Output[3].AsFunctionCall(), r.Output[4].AsFunctionCall()
	if c1.CallID != "call_1" || c1.Name != "ls" || c1.Arguments != `{"p":1}` || c1.Status != "completed" {
		t.Errorf("call 1 = %+v", c1)
	}
	if c2.Arguments != `{"f":"x"}` || c3.Arguments != "{}" {
		t.Errorf("arguments normalised: %q %q", c2.Arguments, c3.Arguments)
	}
	u := r.Usage
	if u.InputTokens != 7 || u.OutputTokens != 3 || u.TotalTokens != 10 || u.InputTokensDetails.CachedTokens != 2 || u.OutputTokensDetails.ReasoningTokens != 1 {
		t.Errorf("usage = %+v", u)
	}
	// Constant members a strict client reads.
	var raw map[string]any
	_ = json.Unmarshal(body, &raw)
	for _, k := range []string{"error", "incomplete_details", "instructions", "metadata", "parallel_tool_calls", "tool_choice", "tools", "store", "text", "reasoning", "temperature", "top_p"} {
		if _, ok := raw[k]; !ok {
			t.Errorf("missing member %s", k)
		}
	}
	if raw["store"] != false || raw["error"] != nil {
		t.Errorf("store=%v error=%v", raw["store"], raw["error"])
	}
}

func TestBuildResponse_LengthRefusalPartsAndNoUsage(t *testing.T) {
	chat := []byte(`{"id":"c","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":[{"type":"text","text":"a"},{"type":"text","text":"b"}],"refusal":"nope"},"finish_reason":"length"}],"usage":null}`)
	body, err := BuildResponse(chat, "resp_x", "m", nil)
	if err != nil {
		t.Fatal(err)
	}
	r := decodeResponse(t, body)
	if r.Status != "incomplete" || r.IncompleteDetails.Reason != "max_output_tokens" {
		t.Errorf("status = %s %+v", r.Status, r.IncompleteDetails)
	}
	msg := r.Output[0].AsMessage()
	if len(msg.Content) != 2 || msg.Content[0].Text != "ab" || msg.Content[1].Refusal != "nope" {
		t.Errorf("content = %+v", msg.Content)
	}
	if r.CreatedAt == 0 {
		t.Error("created_at must be stamped when the chat body has none")
	}
	if strings.Contains(string(body), `"usage"`) {
		t.Error("absent usage must be omitted, not zero")
	}
}

func TestBuildResponse_NotAChatCompletion(t *testing.T) {
	for _, body := range []string{`{"id":"x","object":"list","data":[]}`, `{`, `[]`} {
		if _, err := BuildResponse([]byte(body), "r", "m", nil); err == nil {
			t.Errorf("%s: want error", body)
		}
	}
}

func TestTranslateChatUsage_PerFigure(t *testing.T) {
	u := translateChatUsage([]byte(`{"prompt_tokens":"x","completion_tokens":5}`))
	if u == nil || u.InputTokens != 0 || u.OutputTokens != 5 || u.TotalTokens != 0 {
		t.Errorf("unreadable prompt must cost the prompt and the summed total, never publish a partial total: %+v", u)
	}
	if u := translateChatUsage([]byte(`{"prompt_tokens":5,"completion_tokens":"x","total_tokens":9}`)); u == nil || u.TotalTokens != 9 || u.OutputTokens != 0 {
		t.Errorf("a stated total survives a lost addend: %+v", u)
	}
	if u := translateChatUsage([]byte(`{"prompt_tokens":5,"completion_tokens":3}`)); u == nil || u.TotalTokens != 8 {
		t.Errorf("both addends read: total is their sum: %+v", u)
	}
	if translateChatUsage([]byte(`null`)) != nil || translateChatUsage(nil) != nil || translateChatUsage([]byte(`"x"`)) != nil {
		t.Error("absent, null and non-object usage must be nil")
	}
}
