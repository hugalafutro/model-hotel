package anthropicegress

import (
	"strings"
	"testing"
)

// The spec says a tool call's arguments are a JSON string, and several providers
// send the object. #808 taught the response side to accept both, so an ingress
// decoder that did not was stricter than the decoder that produced the value —
// and a client holding the object form, from this gateway or another, had its
// assistant turn rejected on every retry of the conversation.
//
// The assertion is the ARGUMENT TEXT, not the tool name: the name comes from a
// field this change never touched, so a test that looks for it passes just as
// well when the arguments are silently erased.
func TestTranslateRequest_ToolArgumentsInEitherSpelling(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		args string
		want string
	}{
		{"the spec's JSON string", `"{\"city\":\"Oslo\"}"`, `"input":{"city":"Oslo"}`},
		{"the object several providers send", `{"city":"Oslo"}`, `"input":{"city":"Oslo"}`},
		// A call with no arguments is still a call, and null is how several
		// models spell that.
		{"null", `null`, `"input":{}`},
		{"the empty string", `""`, `"input":{}`},
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
// what a tolerant decode exists to remove. Asserted in all three, on the same
// inputs, so they cannot drift apart again.
func TestTranslateRequest_ArgumentsThatAreNotAnObjectBecomeAnEmptyOne(t *testing.T) {
	t.Parallel()
	for _, args := range []string{`["Oslo"]`, `42`, `"not an object"`, `true`, `"{not json"`} {
		t.Run(args, func(t *testing.T) {
			t.Parallel()
			out, _, _, err := TranslateRequest([]byte(toolCallBody(args)))
			if err != nil {
				t.Fatalf("junk arguments killed the whole request: %v", err)
			}
			if !strings.Contains(string(out), `"input":{}`) {
				t.Errorf("want the empty object every egress path agrees on, got: %s", out)
			}
		})
	}
}

func toolCallBody(args string) string {
	return `{"model":"m","max_tokens":16,"messages":[` +
		`{"role":"user","content":"weather?"},` +
		`{"role":"assistant","tool_calls":[{"id":"c1","type":"function","function":{"name":"lookup","arguments":` + args + `}}]},` +
		`{"role":"tool","tool_call_id":"c1","content":"sunny"}]}`
}
