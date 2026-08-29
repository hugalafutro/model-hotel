package anthropicegress

import (
	"strings"
	"testing"
)

// This is the translator talking to Anthropic-Messages upstreams, so it is the
// one most likely to meet a relay's spelling — and it decoded the counts as part
// of the response object, so one of them failed the whole translation and cost
// the caller the answer the model had already produced.
func TestBuildChatCompletion_CountSpellings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		usage string
	}{
		{"plain integers", `{"input_tokens":12,"output_tokens":3}`},
		{"quoted", `{"input_tokens":"12","output_tokens":"3"}`},
		{"floating point", `{"input_tokens":12.0,"output_tokens":3.0}`},
		{"mixed", `{"input_tokens":"12","output_tokens":3}`},
		// Not a count in any spelling, and an unreadable member: the usage is
		// lost, the answer is not.
		{"unreadable", `{"input_tokens":"lots"}`},
		{"not an object", `[]`},
		{"null", `null`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"type":"message","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":` + tc.usage + `}`
			out, err := BuildChatCompletion([]byte(body), "id", "m", 0)
			if err != nil {
				t.Fatalf("a usage member cost the caller the whole answer: %v", err)
			}
			if !strings.Contains(string(out), "hello") {
				t.Errorf("the answer was lost: %s", out)
			}
		})
	}
}

// An event whose usage this package cannot read must not kill the stream. The
// event decode carried the counts, so a spelling failed the event — and with it
// the event's TYPE, which is what message_stop and the error events are
// recognised by.
func TestStreamTranslator_CountSpellingsDoNotKillTheStream(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		event string
	}{
		{"quoted on message_start", `{"type":"message_start","message":{"usage":{"input_tokens":"11","output_tokens":0}}}`},
		{"fractional on message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":4.0}}`},
		{"unreadable usage", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":"none"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewStreamTranslator("id", "m", 0).Translate([]byte(tc.event)); err != nil {
				t.Errorf("a usage spelling killed the stream: %v", err)
			}
		})
	}
}

// And when it IS readable, whatever the spelling, it reaches the terminal chunk.
func TestStreamTranslator_MetersASpelledCount(t *testing.T) {
	t.Parallel()
	tr := NewStreamTranslator("id", "m", 0)
	if _, err := tr.Translate([]byte(`{"type":"message_start","message":{"usage":{"input_tokens":"11","output_tokens":0}}}`)); err != nil {
		t.Fatalf("message_start: %v", err)
	}
	if _, err := tr.Translate([]byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":"4"}}`)); err != nil {
		t.Fatalf("message_delta: %v", err)
	}
	out, err := tr.Finish()
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if !strings.Contains(string(out), `"prompt_tokens":11`) || !strings.Contains(string(out), `"completion_tokens":4`) {
		t.Errorf("counts did not reach the terminal chunk: %s", out)
	}
}

// And when it IS readable, whatever the spelling, it is metered. Asserting only
// that the answer survives would pass just as well with the usage silently
// dropped — which is exactly what a strict decode inside translateUsage does.
func TestBuildChatCompletion_MetersASpelledCount(t *testing.T) {
	t.Parallel()
	for _, usage := range []string{
		`{"input_tokens":"12","output_tokens":"3"}`,
		`{"input_tokens":12.0,"output_tokens":3.0}`,
		// One unreadable member must not cost the count beside it.
		`{"input_tokens":"12","output_tokens":3,"cache_read_input_tokens":[]}`,
	} {
		t.Run(usage, func(t *testing.T) {
			t.Parallel()
			body := `{"type":"message","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":` + usage + `}`
			out, err := BuildChatCompletion([]byte(body), "id", "m", 0)
			if err != nil {
				t.Fatalf("translate: %v", err)
			}
			if !strings.Contains(string(out), `"prompt_tokens":12`) {
				t.Errorf("the prompt count was not read: %s", out)
			}
		})
	}
}

// An omitted or unreadable usage is reported as absent, not as a claim of zero.
func TestBuildChatCompletion_AnAbsentUsageIsNotAZeroedOne(t *testing.T) {
	t.Parallel()
	for _, usage := range []string{`null`, `[]`, `"none"`} {
		t.Run(usage, func(t *testing.T) {
			t.Parallel()
			body := `{"type":"message","content":[{"type":"text","text":"hello"}],"stop_reason":"end_turn","usage":` + usage + `}`
			out, err := BuildChatCompletion([]byte(body), "id", "m", 0)
			if err != nil {
				t.Fatalf("translate: %v", err)
			}
			if strings.Contains(string(out), `"usage"`) {
				t.Errorf("an unreadable usage was reported as zero tokens: %s", out)
			}
		})
	}
}
