package egress

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Done is the sentinel that ends a chat-completions SSE stream.
const Done = "data: [DONE]\n\n"

// Chunk is one outgoing chat.completion.chunk payload. D is the dialect's
// delta type and U its usage type: those two are the only members that differ
// between the vendor translators, so the envelope itself lives here once.
type Chunk[D, U any] struct {
	ID      string      `json:"id"`
	Object  string      `json:"object"`
	Created int64       `json:"created"`
	Model   string      `json:"model"`
	Choices []Choice[D] `json:"choices"`
	Usage   *U          `json:"usage,omitempty"`
}

// Choice is the single choice a translated chunk carries.
type Choice[D any] struct {
	Index        int     `json:"index"`
	Delta        D       `json:"delta"`
	FinishReason *string `json:"finish_reason"`
}

// ChunkWriter stamps the constant envelope members onto every chunk of one
// response and tracks whether the assistant role has gone out yet, since the
// role rides on the first delta and no other.
type ChunkWriter struct {
	Component string // log/error prefix: the dialect that owns the stream
	ID        string
	Model     string
	Created   int64

	started bool
}

// Role returns "assistant" the first time it is called and "" afterwards, so
// a dialect can stamp the role onto its own delta type before writing.
func (w *ChunkWriter) Role() string {
	if w.started {
		return ""
	}
	w.started = true
	return "assistant"
}

// Started reports whether a chunk has already gone out on this stream.
func (w *ChunkWriter) Started() bool { return w.started }

// WriteChunk appends one framed SSE chunk ("data: <json>\n\n") carrying delta
// as the single choice.
func WriteChunk[D, U any](buf *bytes.Buffer, w *ChunkWriter, delta D, finishReason *string, usage *U) error {
	return writeFrame(buf, w, Chunk[D, U]{
		ID:      w.ID,
		Object:  "chat.completion.chunk",
		Created: w.Created,
		Model:   w.Model,
		Choices: []Choice[D]{{Index: 0, Delta: delta, FinishReason: finishReason}},
		Usage:   usage,
	})
}

// WriteUsageChunk appends a chunk carrying usage and no choices, the shape
// stream_options.include_usage produces and the metering pipeline reads.
func WriteUsageChunk[D, U any](buf *bytes.Buffer, w *ChunkWriter, usage *U) error {
	return writeFrame(buf, w, Chunk[D, U]{
		ID:      w.ID,
		Object:  "chat.completion.chunk",
		Created: w.Created,
		Model:   w.Model,
		Choices: []Choice[D]{},
		Usage:   usage,
	})
}

func writeFrame[D, U any](buf *bytes.Buffer, w *ChunkWriter, c Chunk[D, U]) error {
	payload, err := json.Marshal(c)
	if err != nil {
		return fmt.Errorf("%s: marshal stream chunk: %w", w.Component, err)
	}
	buf.WriteString("data: ")
	buf.Write(payload)
	buf.WriteString("\n\n")
	return nil
}
