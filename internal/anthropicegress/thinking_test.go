package anthropicegress

import "testing"

// The two error bodies are copied from live Anthropic responses (2026-08-20):
// claude-sonnet-5 refusing the budget shape, and claude-haiku-4-5 refusing the
// adaptive one. If Anthropic rewords them, this test is where it shows up.
const (
	budgetRefusedBody   = `{"type":"error","error":{"type":"invalid_request_error","message":"\"thinking.type.enabled\" is not supported for this model. Use \"thinking.type.adaptive\" and \"output_config.effort\" to control thinking behavior."}}`
	adaptiveRefusedBody = `{"type":"error","error":{"type":"invalid_request_error","message":"adaptive thinking is not supported on this model"}}`
)

func TestDialectFromError(t *testing.T) {
	tests := []struct {
		name        string
		body        string
		wantDialect ThinkingDialect
		wantOK      bool
	}{
		{
			name:        "budget refused means the model is adaptive-only",
			body:        budgetRefusedBody,
			wantDialect: ThinkingAdaptive,
			wantOK:      true,
		},
		{
			name:        "adaptive refused means the model wants a budget",
			body:        adaptiveRefusedBody,
			wantDialect: ThinkingBudget,
			wantOK:      true,
		},
		// Everything below must NOT be read as a dialect complaint: acting on one
		// would re-issue a request whose 400 has nothing to do with thinking, and
		// would poison the learned cache for every later request to that model.
		{
			name:   "an unrelated 400 is not a dialect complaint",
			body:   `{"type":"error","error":{"type":"invalid_request_error","message":"messages.0.user.content.str: Input should be a valid string"}}`,
			wantOK: false,
		},
		{
			name:   "a missing max_tokens is not a dialect complaint",
			body:   `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens: Field required"}}`,
			wantOK: false,
		},
		{
			name:   "an unknown model is not a dialect complaint",
			body:   `{"type":"error","error":{"type":"not_found_error","message":"model: claude-nope"}}`,
			wantOK: false,
		},
		{
			// The word alone is not enough: a message about adaptive anything else
			// must not flip a model onto a shape it cannot serve.
			name:   "adaptive mentioned without thinking is not a dialect complaint",
			body:   `{"type":"error","error":{"type":"invalid_request_error","message":"adaptive streaming is not supported on this endpoint"}}`,
			wantOK: false,
		},
		{
			name:   "the temperature complaint mentions thinking but names no dialect",
			body:   `{"type":"error","error":{"type":"invalid_request_error","message":"` + "`temperature`" + ` may only be set to 1 when thinking is enabled or in adaptive mode."}}`,
			wantOK: false,
		},
		{name: "not JSON", body: `<html>502 Bad Gateway</html>`, wantOK: false},
		{name: "empty", body: ``, wantOK: false},
		{name: "empty JSON object", body: `{}`, wantOK: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dialect, ok := DialectFromError([]byte(tt.body))
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if ok && dialect != tt.wantDialect {
				t.Errorf("dialect = %s, want %s", dialect, tt.wantDialect)
			}
		})
	}
}

// The temperature complaint deserves its own note: it is the one non-dialect
// error that mentions BOTH "thinking" and "adaptive", so it is the case a
// looser matcher would get wrong. Retrying it in the other dialect would fail
// identically, since the sampling params, not the shape, are the problem.
func TestDialectFromError_TemperatureComplaintIsNotADialectSwitch(t *testing.T) {
	body := []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"temperature may only be set to 1 when thinking is enabled or in adaptive mode. Please consult our documentation."}}`)
	if _, ok := DialectFromError(body); ok {
		t.Error("the temperature complaint was read as a dialect switch")
	}
}

func TestRequestAsksForThinking(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{name: "adaptive", body: `{"model":"m","thinking":{"type":"adaptive"}}`, want: true},
		{name: "budget", body: `{"model":"m","thinking":{"type":"enabled","budget_tokens":1024}}`, want: true},
		{name: "no thinking key", body: `{"model":"m","max_tokens":10}`, want: false},
		{name: "explicit null", body: `{"model":"m","thinking":null}`, want: false},
		{name: "not JSON", body: `nope`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RequestAsksForThinking([]byte(tt.body)); got != tt.want {
				t.Errorf("RequestAsksForThinking = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestThinkingDialectString(t *testing.T) {
	if ThinkingAdaptive.String() != "adaptive" || ThinkingBudget.String() != "budget" {
		t.Errorf("dialect names = %q / %q", ThinkingAdaptive.String(), ThinkingBudget.String())
	}
}
