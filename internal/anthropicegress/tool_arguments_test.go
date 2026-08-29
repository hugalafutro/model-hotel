package anthropicegress

import (
	"strings"
	"testing"
)

// The spec says a tool call's arguments are a JSON string, and several providers
// send the object instead. #808 taught the RESPONSE side to accept both — so the
// caller receives the object form, echoes the assistant turn back in its next
// request, and this translator rejects what the gateway itself handed it.
//
// In a failover group whose next turn lands on an Anthropic member that 400s,
// and keeps 400ing for the life of the conversation.
func TestTranslateRequest_ToolArgumentsInEitherSpelling(t *testing.T) {
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
			body := `{"model":"m","max_tokens":16,"messages":[` +
				`{"role":"user","content":"weather?"},` +
				`{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"lookup","arguments":` + tc.args + `}}]},` +
				`{"role":"tool","tool_call_id":"c1","content":"sunny"}]}`
			out, _, _, err := TranslateRequest([]byte(body))
			if err != nil {
				t.Fatalf("the gateway rejected an assistant turn it can itself emit: %v", err)
			}
			if !strings.Contains(string(out), "lookup") {
				t.Errorf("the tool call was dropped: %s", out)
			}
		})
	}
}
