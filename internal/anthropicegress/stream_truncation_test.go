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

// A cut-off stream still reports the usage message_start carried: the exact
// prompt and cache-read counts go out on a usage-only chunk (no finish_reason,
// no [DONE]) ahead of the error, so the failed request is billed at the
// provider's figures and not a byte estimate that ignores the cache split.
func TestStreamAdapter_TruncatedStreamKeepsTheReportedUsage(t *testing.T) {
	t.Parallel()
	body := `data: {"type":"message_start","message":{"usage":{"input_tokens":7,"cache_read_input_tokens":100}}}` + "\n\n" +
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hel"}}` + "\n\n"
	out, err := io.ReadAll(NewStreamAdapter(io.NopCloser(strings.NewReader(body)), "m"))
	if !errors.Is(err, egress.ErrStreamTruncated) {
		t.Fatalf("err = %v, want ErrStreamTruncated", err)
	}
	chunks, done := parseChunks(t, string(out))
	if done {
		t.Fatalf("[DONE] on a truncated stream:\n%s", out)
	}
	last := chunks[len(chunks)-1]
	if last.Usage == nil || last.Usage.PromptTokens != 107 {
		t.Fatalf("last chunk usage = %+v, want the reported 7 input + 100 cache-read prompt tokens", last.Usage)
	}
	if len(last.Choices) > 0 && last.Choices[0].FinishReason != nil {
		t.Errorf("usage chunk carries finish_reason %q on a truncated stream", *last.Choices[0].FinishReason)
	}
}

// The same on a dropped connection rather than a clean EOF: the usage
// message_start reported still reaches the pipeline before the read error.
func TestStreamAdapter_ConnectionResetKeepsTheReportedUsage(t *testing.T) {
	t.Parallel()
	reset := errors.New("connection reset by peer")
	body := &dyingBody{
		data: `data: {"type":"message_start","message":{"usage":{"input_tokens":7,"cache_read_input_tokens":100}}}` + "\n\n" +
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hel"}}` + "\n\n",
		err: reset,
	}
	out, err := io.ReadAll(NewStreamAdapter(body, "m"))
	if !errors.Is(err, reset) {
		t.Fatalf("err = %v, want the reset", err)
	}
	chunks, done := parseChunks(t, string(out))
	if done {
		t.Fatalf("[DONE] on a reset stream:\n%s", out)
	}
	if u := chunks[len(chunks)-1].Usage; u == nil || u.PromptTokens != 107 {
		t.Fatalf("last chunk usage = %+v, want 107 prompt tokens", u)
	}
}

// A reset after message_delta carried the stop_reason and the final usage, but
// before message_stop: the response had ended, yet the read failed, so the
// stream is not closed off, and the reported usage (output included) still goes
// out ahead of the error.
func TestStreamAdapter_ResetAfterTheStopReasonKeepsTheReportedUsage(t *testing.T) {
	t.Parallel()
	reset := errors.New("connection reset by peer")
	body := &dyingBody{
		data: `data: {"type":"message_start","message":{"usage":{"input_tokens":7,"cache_read_input_tokens":100}}}` + "\n\n" +
			`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"hello"}}` + "\n\n" +
			`data: {"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":23}}` + "\n\n",
		err: reset,
	}
	out, err := io.ReadAll(NewStreamAdapter(body, "m"))
	if !errors.Is(err, reset) {
		t.Fatalf("err = %v, want the reset", err)
	}
	chunks, done := parseChunks(t, string(out))
	if done {
		t.Fatalf("[DONE] on a reset stream:\n%s", out)
	}
	if u := chunks[len(chunks)-1].Usage; u == nil || u.PromptTokens != 107 || u.CompletionTokens != 23 {
		t.Fatalf("last chunk usage = %+v, want 107 prompt and 23 completion tokens", u)
	}
}

var _ egress.UsageReporter = (*StreamTranslator)(nil)
