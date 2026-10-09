package openairesponses

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/egress"
)

// A Responses stream ends with a terminal response.* event (or an error
// event). EOF before one means the upstream was cut off mid-response: the
// adapter must fail the stream, with no [DONE], so the proxy records a
// truncation instead of injecting [DONE] and billing the partial answer as
// complete.
func TestStreamAdapter_EOFBeforeTheTerminalEventIsTruncated(t *testing.T) {
	t.Parallel()
	const delta = `data: {"type":"response.output_text.delta","delta":"hel"}` + "\n\n"
	for _, tc := range []struct {
		name, body string
		truncated  bool
	}{
		{"cut off mid-answer", delta, true},
		{"empty body", "", true},
		{"completed", delta + `data: {"type":"response.completed","response":{"status":"completed"}}` + "\n\n", false},
		{"incomplete", delta + `data: {"type":"response.incomplete","response":{"status":"incomplete"}}` + "\n\n", false},
		{"failed", delta + `data: {"type":"response.failed","response":{"status":"failed"}}` + "\n\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := io.ReadAll(NewStreamAdapter(io.NopCloser(strings.NewReader(tc.body)), "m"))
			if got := errors.Is(err, egress.ErrStreamTruncated); got != tc.truncated {
				t.Fatalf("err = %v, truncated = %v, want %v", err, got, tc.truncated)
			}
			if tc.truncated && strings.Contains(string(out), "[DONE]") {
				t.Errorf("[DONE] on a truncated stream:\n%s", out)
			}
		})
	}
}
