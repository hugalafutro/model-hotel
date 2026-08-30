package anthropic

import (
	"encoding/json"
	"testing"
)

// A count is a count however the provider spelled it. A plain int field met
// neither the quoted nor the fractional form, so ParseResponseUsage returned
// nothing and the request metered at zero — the caller's quota debited for an
// answer it received.
func TestParseResponseUsage_CountSpellings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		usage           string
		wantIn, wantOut int
	}{
		{"plain integers", `{"input_tokens":12,"output_tokens":3}`, 12, 3},
		{"quoted", `{"input_tokens":"12","output_tokens":"3"}`, 12, 3},
		{"floating point", `{"input_tokens":12.0,"output_tokens":3.0}`, 12, 3},
		{"mixed", `{"input_tokens":"12","output_tokens":3}`, 12, 3},
		// Not a count in any spelling: nothing is invented for it.
		{"unreadable", `{"input_tokens":"lots"}`, 0, 0},
		{"absent", ``, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"type":"message","content":[{"type":"text","text":"hi"}]}`
			if tc.usage != "" {
				body = `{"type":"message","content":[{"type":"text","text":"hi"}],"usage":` + tc.usage + `}`
			}
			got := ParseResponseUsage([]byte(body))
			if got.PromptTokens != tc.wantIn || got.CompletionTokens != tc.wantOut {
				t.Errorf("got %d/%d, want %d/%d", got.PromptTokens, got.CompletionTokens, tc.wantIn, tc.wantOut)
			}
		})
	}
}

// The streaming twin of ParseResponseUsage, in the same file, decoded its counts
// inline — so a spelling cost the event its TYPE along with its counts, and
// message_stop and the error events stopped being recognised for that frame.
// The adjacent Error member's own comment spells out that exact failure.
func TestInspectStreamEvent_CountSpellings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		payload  string
		wantType string
		wantOut  int
	}{
		{"plain", `{"type":"message_delta","usage":{"output_tokens":4}}`, "message_delta", 4},
		{"quoted", `{"type":"message_delta","usage":{"output_tokens":"4"}}`, "message_delta", 4},
		{"fractional", `{"type":"message_delta","usage":{"output_tokens":4.0}}`, "message_delta", 4},
		// The type survives even when the usage cannot be read at all, which is
		// the half that costs the stream its terminal frame.
		{"unreadable usage", `{"type":"message_delta","usage":"none"}`, "message_delta", 0},
		{"null usage", `{"type":"message_stop","usage":null}`, "message_stop", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := InspectStreamEvent([]byte(tc.payload))
			if got.Type != tc.wantType {
				t.Errorf("Type = %q, want %q: the event lost its type with its counts", got.Type, tc.wantType)
			}
			if got.OutputTokens != tc.wantOut {
				t.Errorf("OutputTokens = %d, want %d", got.OutputTokens, tc.wantOut)
			}
		})
	}
}

// message_start carries its usage one level down, and it had the same defect.
func TestInspectStreamEvent_MessageStartCountSpellings(t *testing.T) {
	t.Parallel()
	for _, payload := range []string{
		`{"type":"message_start","message":{"usage":{"input_tokens":11,"output_tokens":1}}}`,
		`{"type":"message_start","message":{"usage":{"input_tokens":"11","output_tokens":1}}}`,
		`{"type":"message_start","message":{"usage":{"input_tokens":11.0,"output_tokens":1}}}`,
	} {
		t.Run(payload, func(t *testing.T) {
			t.Parallel()
			got := InspectStreamEvent([]byte(payload))
			if got.Type != "message_start" {
				t.Errorf("Type = %q, want message_start", got.Type)
			}
			if got.InputTokens != 11 || !got.HasInput {
				t.Errorf("InputTokens = %d (has=%v), want 11", got.InputTokens, got.HasInput)
			}
		})
	}
}

// An explicit null is not a usage block. The *antUsage this replaced was nil for
// absent AND null alike; a json.RawMessage for null is four non-empty bytes, and
// emitRawData assigns OutputTokens unguarded — so a null usage on a later event
// would wipe the count message_start reported.
func TestInspectStreamEvent_ANullUsageIsNotAReading(t *testing.T) {
	t.Parallel()
	for _, payload := range []string{
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":null}`,
		`{"type":"message_start","message":{"usage":null}}`,
	} {
		t.Run(payload, func(t *testing.T) {
			t.Parallel()
			got := InspectStreamEvent([]byte(payload))
			if got.HasOutput || got.HasInput {
				t.Errorf("a null usage read as a count: %+v", got)
			}
		})
	}
}

// A member that could not be read costs the figures it FEEDS, and nothing else.
// The prompt figure is input_tokens plus both cache counts; output_tokens is
// read straight off its own member and is never in doubt.
func TestParseResponseUsage_AnUnreadableMemberCostsOnlyWhatItFeeds(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		usage           string
		wantIn, wantOut int
	}{
		// The cache-read count of 20000 lost here billed 4, and 4 is non-zero,
		// so the estimator never replaced it.
		{"a lost prompt addend", `{"input_tokens":4,"output_tokens":5,"cache_read_input_tokens":[]}`, 0, 5},
		{"a lost completion count", `{"input_tokens":4,"output_tokens":"lots","cache_read_input_tokens":20000}`, 20004, 0},
		{"all readable", `{"input_tokens":4,"output_tokens":5,"cache_read_input_tokens":20000}`, 20004, 5},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"type":"message","content":[{"type":"text","text":"hi"}],"usage":` + tc.usage + `}`
			got := ParseResponseUsage([]byte(body))
			if got.PromptTokens != tc.wantIn || got.CompletionTokens != tc.wantOut {
				t.Errorf("got %d/%d, want %d/%d", got.PromptTokens, got.CompletionTokens, tc.wantIn, tc.wantOut)
			}
		})
	}
}

// The streaming reader keeps the same rule — and this is the path where getting
// it wrong bites hardest: message_start reports output_tokens 1, so a
// message_delta whose usage is dropped leaves that 1 standing as the whole
// completion count for the request.
func TestInspectStreamEvent_AnUnreadableMemberCostsOnlyWhatItFeeds(t *testing.T) {
	t.Parallel()
	got := InspectStreamEvent([]byte(`{"type":"message_delta","usage":{"output_tokens":1520,"input_tokens":[]}}`))
	if !got.HasOutput || got.OutputTokens != 1520 {
		t.Errorf("OutputTokens = %d (has=%v), want the count read straight off its own member", got.OutputTokens, got.HasOutput)
	}

	// And the other half of the rule, on the event that carries the prompt: a
	// figure whose addend was lost is dropped rather than reported short.
	start := InspectStreamEvent([]byte(`{"type":"message_start","message":{"usage":{"input_tokens":4,"output_tokens":1,"cache_read_input_tokens":[]}}}`))
	if start.HasInput || start.InputTokens != 0 {
		t.Errorf("InputTokens = %d (has=%v), want the prompt figure dropped: its cache addend was lost", start.InputTokens, start.HasInput)
	}
	if !start.HasOutput || start.OutputTokens != 1 {
		t.Errorf("OutputTokens = %d (has=%v), want 1: it is read straight off its own member", start.OutputTokens, start.HasOutput)
	}
}

// The OpenAI -> Anthropic translation of a NON-streaming answer read its counts
// inline, off a struct of plain ints. The streaming twin
// (proxy.anthropicResponseWriter.handleStreamLine) already tolerates a count the
// provider spelled differently; this one did not, so a quoted prompt_tokens
// failed the decode of the whole response object and the caller got a 502 in
// place of the answer the model had already produced.
func TestBuildMessageResponse_CountSpellings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		usage           string
		wantIn, wantOut int
	}{
		{"plain integers", `{"prompt_tokens":12,"completion_tokens":3}`, 12, 3},
		{"quoted", `{"prompt_tokens":"12","completion_tokens":"3"}`, 12, 3},
		{"fractional", `{"prompt_tokens":12.0,"completion_tokens":3.0}`, 12, 3},
		{"mixed", `{"prompt_tokens":"12","completion_tokens":3}`, 12, 3},
		// A member this translator has no field for keeps the counts beside it.
		{"unmodelled sibling", `{"prompt_tokens":12,"completion_tokens":3,"prompt_tokens_details":[]}`, 12, 3},
		// Not a count in any spelling: nothing is invented for the figure that
		// could not be read, and the one that could is still reported.
		{"one figure unreadable", `{"prompt_tokens":"lots","completion_tokens":3}`, 0, 3},
		// The usage member is not a usage block at all: no counts, but the
		// answer still reaches the caller.
		{"usage is not an object", `"none"`, 0, 0},
		{"usage is null", `null`, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			body := `{"id":"chatcmpl-1","choices":[{"message":{"role":"assistant","content":"Hi there"},` +
				`"finish_reason":"stop"}],"usage":` + tc.usage + `}`
			out, err := BuildMessageResponse([]byte(body), "msg_1", "m")
			if err != nil {
				t.Fatalf("BuildMessageResponse: %v: a usage block must never cost the caller the answer", err)
			}
			var m struct {
				Content []struct {
					Text string `json:"text"`
				} `json:"content"`
				Usage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"usage"`
			}
			if err := json.Unmarshal(out, &m); err != nil {
				t.Fatalf("invalid output: %v", err)
			}
			if len(m.Content) != 1 || m.Content[0].Text != "Hi there" {
				t.Errorf("content = %+v, want the answer the model produced", m.Content)
			}
			if m.Usage.InputTokens != tc.wantIn || m.Usage.OutputTokens != tc.wantOut {
				t.Errorf("usage = %d/%d, want %d/%d", m.Usage.InputTokens, m.Usage.OutputTokens, tc.wantIn, tc.wantOut)
			}
		})
	}
}

// A count no reading can make sense of must not be invented, and bytes that are
// not a document at all must still fail: the tolerance is for a number written
// differently, not for leniency across the response.
func TestBuildMessageResponse_BrokenBytesStillFail(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"choices":[],"usage":{"prompt_tokens":1}`, // truncated object
		`[]`, // a list is not a chat completion
	} {
		t.Run(body, func(t *testing.T) {
			t.Parallel()
			if _, err := BuildMessageResponse([]byte(body), "msg_1", "m"); err == nil {
				t.Error("want an error: these bytes are broken, not a count in another spelling")
			}
		})
	}
}
