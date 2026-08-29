package gemini

import (
	"strings"
	"testing"
)

// See the anthropicegress twin: #808 taught the response side to accept the
// object form, so the caller echoes it back and this translator rejected what
// the gateway had handed it.
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
			body := `{"model":"m","messages":[` +
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
