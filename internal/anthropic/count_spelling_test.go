package anthropic

import "testing"

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
