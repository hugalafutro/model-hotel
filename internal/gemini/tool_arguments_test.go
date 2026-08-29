package gemini

import (
	"strings"
	"testing"
)

// See the anthropicegress twin: #808 taught the response side to accept the
// object form, so an ingress decoder that did not was stricter than the decoder
// that produced the value.
//
// The assertion is the ARGUMENT TEXT, not the tool name — the name comes from a
// field this change never touched.
func TestTranslateRequest_ToolArgumentsInEitherSpelling(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args string
		want string
	}{
		{"the spec's JSON string", `"{\"city\":\"Oslo\"}"`, `"args":{"city":"Oslo"}`},
		{"the object several providers send", `{"city":"Oslo"}`, `"args":{"city":"Oslo"}`},
		{"null", `null`, `"args":{}`},
		{"the empty string", `""`, `"args":{}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, _, _, err := TranslateRequest([]byte(toolCallBody(tc.args)))
			if err != nil {
				t.Fatalf("the gateway rejected an assistant turn it can itself emit: %v", err)
			}
			if !strings.Contains(string(out), tc.want) {
				t.Errorf("arguments did not survive translation, want %s in: %s", tc.want, out)
			}
		})
	}
}

// Arguments that are not an object become an empty one — this repo's existing
// decision (see TestTranslateRequest_ToolCallArgumentsBecomeAnObject): a model
// that emits junk mid-conversation must not kill the whole request.
//
// What was wrong was that the three egress translators each made that decision
// differently: {} here, a non-Struct `args` Gemini answers 400 to, and a quoted
// array the Responses model reads as garbage. Which one a caller met depended on
// which member of a failover group the turn landed on, and that divergence is
// what a tolerant decode exists to remove.
//
// The same inputs in all three packages, as three hand-kept tables rather than
// one shared list — so this documents the agreement rather than enforcing it,
// and editing one still breaks nothing.
func TestTranslateRequest_ArgumentsThatAreNotAnObjectBecomeAnEmptyOne(t *testing.T) {
	t.Parallel()
	for _, args := range []string{`["Oslo"]`, `42`, `"not an object"`, `true`, `"{not json"`} {
		t.Run(args, func(t *testing.T) {
			t.Parallel()
			out, _, _, err := TranslateRequest([]byte(toolCallBody(args)))
			if err != nil {
				t.Fatalf("junk arguments killed the whole request: %v", err)
			}
			if !strings.Contains(string(out), `"args":{}`) {
				t.Errorf("want the empty object every egress path agrees on, got: %s", out)
			}
		})
	}
}

func toolCallBody(args string) string {
	return `{"model":"m","messages":[` +
		`{"role":"user","content":"weather?"},` +
		`{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"lookup","arguments":` + args + `}}]},` +
		`{"role":"tool","tool_call_id":"c1","content":"sunny"}]}`
}
