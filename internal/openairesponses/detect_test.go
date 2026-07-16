package openairesponses

import "testing"

func TestRequiresResponsesAPI(t *testing.T) {
	// The live gpt-5.6 rejection observed 2026-07-19 (plan §1).
	rejection := `{"error":{"message":"Function tools with reasoning_effort are not supported in the Chat Completions API for this model. Please use the /v1/responses endpoint, or set reasoning_effort to 'none'.","type":"invalid_request_error","param":null,"code":null}}`
	if !RequiresResponsesAPI([]byte(rejection)) {
		t.Error("real gpt-5.6 rejection not detected")
	}

	for name, body := range map[string]string{
		"param error":    `{"error":{"message":"Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead."}}`,
		"generic 400":    `{"error":{"message":"Invalid request"}}`,
		"empty":          ``,
		"not json":       `<html>bad gateway</html>`,
		"empty envelope": `{"error":{}}`,
		"tools only":     `{"error":{"message":"tool choice invalid"}}`,
		"responses only": `{"error":{"message":"responses api is great"}}`,
	} {
		if RequiresResponsesAPI([]byte(body)) {
			t.Errorf("%s falsely detected as Responses-required", name)
		}
	}
}

func TestNeedsResponsesRouting(t *testing.T) {
	cases := []struct {
		name string
		body string
		want bool
	}{
		{"tools + explicit effort", `{"tools":[{"type":"function"}],"reasoning_effort":"high"}`, true},
		{"tools + default reasoning", `{"tools":[{"type":"function"}]}`, true},
		{"tools + reasoning off", `{"tools":[{"type":"function"}],"reasoning_effort":"none"}`, false},
		{"reasoning only, no tools", `{"reasoning_effort":"high"}`, false},
		{"plain", `{"messages":[]}`, false},
		{"empty tools", `{"tools":[]}`, false},
		{"invalid json", `nope`, false},
	}
	for _, c := range cases {
		if got := NeedsResponsesRouting([]byte(c.body)); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}
