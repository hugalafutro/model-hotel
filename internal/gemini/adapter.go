package gemini

import (
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/egress"
)

// StreamAdapter re-frames an upstream Gemini streamGenerateContent alt=sse body
// as chat.completion.chunk SSE bytes. The mechanics (event assembly, EOF
// finish, poisoning on a bad chunk) are shared with the other dialects; see
// egress.StreamAdapter.
//
// Vertex streams carry no [DONE] sentinel: EOF is the natural end, so the
// terminal chunk + [DONE] come from the translator's Finish() when upstream EOF
// arrives.
//
// StreamAdapter is the shared egress adapter driving this dialect's
// StreamTranslator. It is an alias, not a defined type: gemini,
// anthropicegress and openairesponses all alias it, so a type switch cannot
// tell them apart.
type StreamAdapter = egress.StreamAdapter

// NewStreamAdapter builds an adapter for one streaming response. model is
// echoed in every emitted chunk (the model string the client requested).
func NewStreamAdapter(upstream io.ReadCloser, model string) *StreamAdapter {
	id := "chatcmpl-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	return egress.NewStreamAdapter("gemini", upstream, NewStreamTranslator(id, model, time.Now().Unix()))
}
