package gemini

import (
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/egress"
)

// StreamAdapter re-frames an upstream Gemini streamGenerateContent alt=sse body
// as chat.completion.chunk SSE bytes, driving this dialect's StreamTranslator.
// The mechanics (event assembly, EOF finish, poisoning on a bad chunk) live in
// egress.StreamAdapter.
//
// Vertex streams carry no [DONE] sentinel: EOF is the natural end, so the
// terminal chunk + [DONE] come from the translator's Finish() when upstream EOF
// arrives.
//
// It is an alias, not a defined type: the other egress dialects alias the same
// type, so a type switch cannot tell them apart.
type StreamAdapter = egress.StreamAdapter

// MaxEventBytes caps one Gemini SSE event. An image model streams its whole
// picture as a single inlineData part in one event, and a 2K or 4K PNG is
// several MiB of base64, well past the 4 MiB the text dialects need; 32 MiB
// matches the cap on a non-streaming body.
const MaxEventBytes = 32 << 20

// NewStreamAdapter builds an adapter for one streaming response. model is
// echoed in every emitted chunk (the model string the client requested).
func NewStreamAdapter(upstream io.ReadCloser, model string) *StreamAdapter {
	id := "chatcmpl-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	return egress.NewStreamAdapterWithCap("gemini", upstream, NewStreamTranslator(id, model, time.Now().Unix()), MaxEventBytes)
}
