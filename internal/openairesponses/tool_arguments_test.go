package openairesponses

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
		{"the spec's JSON string", `"{\"city\":\"Oslo\"}"`, `"arguments":"{\"city\":\"Oslo\"}"`},
		{"the object several providers send", `{"city":"Oslo"}`, `"arguments":"{\"city\":\"Oslo\"}"`},
		{"null", `null`, `"arguments":"{}"`},
		{"the empty string", `""`, `"arguments":"{}"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := TranslateChatToResponses([]byte(toolCallBody(tc.args)), "m")
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
			out, err := TranslateChatToResponses([]byte(toolCallBody(args)), "m")
			if err != nil {
				t.Fatalf("junk arguments killed the whole request: %v", err)
			}
			if !strings.Contains(string(out), `"arguments":"{}"`) {
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

// And the response side of this same translator: a provider that answers with
// the object form must not cost the caller its tool call, and the arguments must
// come back as the spec's JSON string whatever spelling arrived.
func TestTranslateResponsesToChat_ToolArgumentsInEitherSpelling(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ args, want string }{
		{`"{\"city\":\"Oslo\"}"`, `"arguments":"{\"city\":\"Oslo\"}"`},
		{`{"city":"Oslo"}`, `"arguments":"{\"city\":\"Oslo\"}"`},
		{`["Oslo"]`, `"arguments":"[\"Oslo\"]"`},
	} {
		t.Run(tc.args, func(t *testing.T) {
			t.Parallel()
			body := `{"id":"resp_1","status":"completed","output":[{"type":"function_call","call_id":"c1","name":"lookup","arguments":` + tc.args + `}]}`
			out, err := TranslateResponsesToChat([]byte(body), "m")
			if err != nil {
				t.Fatalf("a tool call the provider sent cost the caller the response: %v", err)
			}
			if !strings.Contains(string(out), tc.want) {
				t.Errorf("arguments did not survive translation, want %s in: %s", tc.want, out)
			}
		})
	}
}
