package anthropic

import "testing"

// A message_delta usage block arrives after the whole answer has been
// forwarded, so a hostile or broken upstream controls it with no window for
// anyone to notice. message_start has always refused a non-positive output
// figure; message_delta assigned it verbatim, and a negative output_tokens
// reached the completion metering and drew the key's usage down.

func TestInspectStreamEvent_MessageDeltaRefusesNonPositiveOutput(t *testing.T) {
	for _, payload := range []string{
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":-700}}`,
		`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":0}}`,
	} {
		ev := InspectStreamEvent([]byte(payload))
		if ev.HasOutput || ev.OutputTokens != 0 {
			t.Errorf("%s: not a reading, got %+v", payload, ev)
		}
	}
}

func TestInspectStreamEvent_MessageDeltaKeepsPositiveOutput(t *testing.T) {
	ev := InspectStreamEvent([]byte(`{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":23}}`))
	if !ev.HasOutput || ev.OutputTokens != 23 {
		t.Fatalf("a real reading must survive the guard: got %+v", ev)
	}
}
