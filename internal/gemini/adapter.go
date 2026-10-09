package gemini

import (
	"io"
	"time"

	"github.com/hugalafutro/model-hotel/internal/egress"
)

// StreamAdapter re-frames an upstream Gemini streamGenerateContent alt=sse body
// as chat.completion.chunk SSE bytes, driving this dialect's StreamTranslator.
// The mechanics (event assembly, EOF finish, poisoning on a bad chunk) live in
// egress.StreamAdapter.
//
// Vertex streams carry no [DONE] sentinel: EOF is the natural end, so the
// terminal chunk + [DONE] come from the translator's Finish() when upstream EOF
// arrives after a finishReason (or a prompt block); EOF before either fails the
// stream as truncated.
//
// It is an alias, not a defined type: the other egress dialects alias the same
// type, so a type switch cannot tell them apart.
type StreamAdapter = egress.StreamAdapter

// NewStreamAdapter builds an adapter for one streaming response. model is
// echoed in every emitted chunk (the model string the client requested).
func NewStreamAdapter(upstream io.ReadCloser, model string) *StreamAdapter {
	return egress.NewStreamAdapter("gemini", upstream, NewStreamTranslator(egress.NewChatCompletionID(), model, time.Now().Unix()))
}
