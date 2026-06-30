package anthropic

import "testing"

func TestMapStopReason(t *testing.T) {
	cases := map[string]string{
		"length":         "max_tokens",
		"tool_calls":     "tool_use",
		"function_call":  "tool_use",
		"stop":           "end_turn",
		"content_filter": "end_turn",
		"":               "end_turn",
		"unknown":        "end_turn",
	}
	for in, want := range cases {
		if got := mapStopReason(in); got != want {
			t.Errorf("mapStopReason(%q) = %q, want %q", in, got, want)
		}
	}
}
