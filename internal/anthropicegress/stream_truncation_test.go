package anthropicegress

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/egress"
)

// An Anthropic stream ends with message_stop, and a message_delta carrying its
// stop_reason already says the response is over (a relay can drop the final
// event). EOF with neither means the upstream was cut off mid-response: the
// adapter must fail the stream, with no [DONE], so the proxy records a
// truncation instead of billing the partial answer as complete.
func TestStreamAdapter_EOFBeforeAnEndSignalIsTruncated(t *testing.T) {
	t.Parallel()
	const delta = `data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hel"}}` + "\n\n"
	for _, tc := range []struct {
		name, body string
		truncated  bool
	}{
		{"cut off mid-answer", delta, true},
		{"empty body", "", true},
		{"stop_reason without message_stop", delta + `data: {"type":"message_delta","delta":{"stop_reason":"end_turn"}}` + "\n\n", false},
		{"message_stop", delta + `data: {"type":"message_stop"}` + "\n\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := io.ReadAll(NewStreamAdapter(io.NopCloser(strings.NewReader(tc.body)), "m"))
			if got := errors.Is(err, egress.ErrStreamTruncated); got != tc.truncated {
				t.Fatalf("err = %v, truncated = %v, want %v", err, got, tc.truncated)
			}
			if tc.truncated == strings.Contains(string(out), "[DONE]") {
				t.Errorf("[DONE] present = %v on a stream with truncated = %v:\n%s", !tc.truncated, tc.truncated, out)
			}
		})
	}
}
