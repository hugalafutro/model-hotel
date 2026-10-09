package gemini

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/egress"
)

// A Gemini stream ends with a finishReason on its last candidate (or a
// promptFeedback block). One that reaches EOF with neither was cut off
// mid-response: the adapter must fail it, with no [DONE], so the proxy records
// a truncation instead of billing the partial answer as complete.
func TestStreamAdapter_EOFBeforeFinishReasonIsTruncated(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, body string
		truncated  bool
	}{
		{"cut off mid-answer", `data: {"candidates":[{"content":{"parts":[{"text":"hel"}],"role":"model"}}]}` + "\n\n", true},
		{"empty body", "", true},
		{"finished", `data: {"candidates":[{"content":{"parts":[{"text":"hello"}],"role":"model"},"finishReason":"STOP"}]}` + "\n\n", false},
		{"blocked prompt", `data: {"promptFeedback":{"blockReason":"SAFETY"}}` + "\n\n", false},
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

// A cut-off stream still reports the usage Gemini already sent: it goes out on a
// usage-only chunk (no finish_reason, no [DONE]) ahead of the error, so the
// failed request is billed at the provider's figures and not a byte estimate.
func TestStreamAdapter_TruncatedStreamKeepsTheReportedUsage(t *testing.T) {
	t.Parallel()
	body := `data: {"candidates":[{"content":{"parts":[{"text":"hel"}],"role":"model"}}],"usageMetadata":{"promptTokenCount":12,"candidatesTokenCount":3,"totalTokenCount":15}}` + "\n\n"
	out, err := io.ReadAll(NewStreamAdapter(io.NopCloser(strings.NewReader(body)), "m"))
	if !errors.Is(err, egress.ErrStreamTruncated) {
		t.Fatalf("err = %v, want ErrStreamTruncated", err)
	}
	if strings.Contains(string(out), "[DONE]") || strings.Contains(string(out), `"finish_reason":"`) {
		t.Fatalf("a truncated stream was closed off as finished:\n%s", out)
	}
	if got := streamUsage(t, out); got.PromptTokens != 12 || got.CompletionTokens != 3 {
		t.Errorf("reported usage = %d/%d, want 12/3", got.PromptTokens, got.CompletionTokens)
	}
}
