package openairesponses

import (
	"strings"
	"testing"
)

// See the anthropicegress twin: #808 taught the response side to accept the
// object form, so the caller echoes it back and this translator rejected what
// the gateway had handed it.
func TestTranslateChatToResponses_ToolArgumentsInEitherSpelling(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args string
	}{
		{"the spec's JSON string", `"{\"city\":\"Oslo\"}"`},
		{"the object several providers send", `{"city":"Oslo"}`},
		{"an array", `["Oslo"]`},
		{"a null, which carries no arguments", `null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"model":"m","messages":[` +
				`{"role":"user","content":"weather?"},` +
				`{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"lookup","arguments":` + tc.args + `}}]},` +
				`{"role":"tool","tool_call_id":"c1","content":"sunny"}]}`
			out, err := TranslateChatToResponses([]byte(body), "m")
			if err != nil {
				t.Fatalf("the gateway rejected an assistant turn it can itself emit: %v", err)
			}
			if !strings.Contains(string(out), "lookup") {
				t.Errorf("the tool call was dropped: %s", out)
			}
		})
	}
}

// And the response side of this same translator: a provider that answers with
// the object form must not cost the caller its tool call.
func TestTranslateResponsesToChat_ToolArgumentsInEitherSpelling(t *testing.T) {
	t.Parallel()
	for _, args := range []string{`"{\"city\":\"Oslo\"}"`, `{"city":"Oslo"}`, `["Oslo"]`} {
		t.Run(args, func(t *testing.T) {
			t.Parallel()
			body := `{"id":"resp_1","status":"completed","output":[{"type":"function_call","call_id":"c1","name":"lookup","arguments":` + args + `}]}`
			out, err := TranslateResponsesToChat([]byte(body), "m")
			if err != nil {
				t.Fatalf("a tool call the provider sent cost the caller the response: %v", err)
			}
			if !strings.Contains(string(out), "lookup") {
				t.Errorf("the tool call was dropped: %s", out)
			}
		})
	}
}
