package gemini

import (
	"bytes"
	"encoding/json"
	"fmt"

	"github.com/hugalafutro/model-hotel/internal/egress"
	"github.com/hugalafutro/model-hotel/internal/jsonfault"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// StreamTranslator converts a Gemini streamGenerateContent SSE stream
// (alt=sse: each data line is a full generateContent-shaped JSON chunk) into
// an OpenAI chat.completion.chunk SSE stream ending in "data: [DONE]".
//
// It is the streaming counterpart of BuildChatCompletion and single-goroutine:
// the proxy's egress loop feeds it one upstream data payload at a time and
// forwards whatever bytes come back.
type StreamTranslator struct {
	w egress.ChunkWriter

	finished     bool   // Finish() already emitted
	blocked      bool   // promptFeedback.blockReason seen
	finishReason string // last Gemini finishReason observed
	toolCalls    int    // tool_calls emitted so far (drives index + ids)
	// Raw, as on genResponse.UsageMetadata: a count spelled differently must
	// not cost the caller the stream.
	usage json.RawMessage
}

// NewStreamTranslator builds a translator for one response. id, model and
// created are echoed in every chunk envelope (the model string the client
// requested, not Gemini's modelVersion).
func NewStreamTranslator(id, model string, created int64) *StreamTranslator {
	return &StreamTranslator{w: egress.ChunkWriter{Component: "gemini", ID: id, Model: model, Created: created}}
}

type oaiChunkDelta struct {
	Role      string                `json:"role,omitempty"`
	Content   string                `json:"content,omitempty"`
	ToolCalls []oaiChunkToolCallOut `json:"tool_calls,omitempty"`
	// Images carries a generated image part, as the non-streaming message's
	// images does: an image model streams its image as one inlineData part.
	Images []oaiImageOut `json:"images,omitempty"`
}

// oaiChunkToolCallOut is a streamed tool call. Gemini delivers each
// functionCall complete in a single part, so the whole call goes out as one
// delta fragment (index + id + name + full arguments).
type oaiChunkToolCallOut struct {
	Index int `json:"index"`
	oaiToolCallOut
}

// writeChunk appends one framed SSE chunk ("data: <json>\n\n").
func (t *StreamTranslator) writeChunk(buf *bytes.Buffer, delta oaiChunkDelta, finishReason *string, usage *oaiUsage) error {
	delta.Role = t.w.Role()
	return egress.WriteChunk(buf, &t.w, delta, finishReason, usage)
}

// Translate processes one upstream Gemini data payload and returns the SSE
// bytes to forward to the client (possibly empty: thought-only and usage-only
// chunks update state silently). finishReason and usage are recorded for the
// terminal Finish() chunk, matching how Gemini delivers both on the last line.
func (t *StreamTranslator) Translate(chunkJSON []byte) ([]byte, error) {
	var chunk genResponse
	if err := json.Unmarshal(chunkJSON, &chunk); err != nil {
		return nil, fmt.Errorf("gemini: invalid stream chunk: %s", jsonfault.Describe(err, len(chunkJSON)))
	}

	// JSONMemberSet, as in translateUsage: an explicit "usageMetadata": null
	// is four bytes, so reading only the length lets it overwrite the counts
	// an earlier chunk reported.
	if util.JSONMemberSet(chunk.UsageMetadata) {
		t.usage = chunk.UsageMetadata
	}
	// A blocked prompt streams as a candidate-less chunk with promptFeedback;
	// remember it so Finish() reports content_filter instead of a clean stop.
	if chunk.PromptFeedback != nil && chunk.PromptFeedback.BlockReason != "" {
		t.blocked = true
	}
	if len(chunk.Candidates) == 0 {
		return nil, nil
	}
	cand := chunk.Candidates[0]
	if cand.FinishReason != "" {
		t.finishReason = cand.FinishReason
	}

	var buf bytes.Buffer
	delta := oaiChunkDelta{}
	for _, p := range cand.Content.Parts {
		if p.Thought {
			continue
		}
		if img, ok := imageOut(p.InlineData); ok {
			delta.Images = append(delta.Images, img)
			continue
		}
		if p.FunctionCall != nil {
			// Upstreams carry the signature in the same part as the call; the
			// tool_call delta is emitted here, so a later part could not reach it.
			delta.ToolCalls = append(delta.ToolCalls, oaiChunkToolCallOut{
				Index:          t.toolCalls,
				oaiToolCallOut: p.toolCall(t.w.ID, t.toolCalls),
			})
			t.toolCalls++
			continue
		}
		delta.Content += p.Text
	}

	if delta.Content == "" && len(delta.ToolCalls) == 0 && len(delta.Images) == 0 {
		return nil, nil
	}
	if err := t.writeChunk(&buf, delta, nil, nil); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Finish emits the terminal chunk (empty delta, mapped finish_reason, usage
// when the upstream reported it) followed by "data: [DONE]". It is idempotent
// and emits a well-formed terminal chunk even when no content chunk ever
// arrived (the role rides on the terminal delta then).
func (t *StreamTranslator) Finish() ([]byte, error) {
	if t.finished {
		return nil, nil
	}
	t.finished = true

	var buf bytes.Buffer
	reason := mapFinishReason(t.finishReason, t.toolCalls > 0)
	if t.blocked {
		reason = "content_filter"
	}
	if err := t.writeChunk(&buf, oaiChunkDelta{}, &reason, translateUsage(t.usage)); err != nil {
		return nil, err
	}
	buf.WriteString(egress.Done)
	return buf.Bytes(), nil
}
