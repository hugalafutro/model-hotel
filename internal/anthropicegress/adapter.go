package anthropicegress

import (
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/egress"
)

// StreamAdapter re-frames an upstream Anthropic /v1/messages SSE body as
// chat.completion.chunk SSE bytes. The mechanics (event assembly, EOF finish
// including the unterminated tail, poisoning on a bad event) are shared with
// the other dialects; see egress.StreamAdapter.
//
// Anthropic ends its stream with message_stop and carries no [DONE] sentinel,
// so the translator emits the terminal chunk + [DONE] on message_stop. An
// upstream that reaches EOF without message_stop gets that terminal pair from
// Finish() instead.
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
	return egress.NewStreamAdapter("anthropicegress", upstream, NewStreamTranslator(id, model, time.Now().Unix()))
}
