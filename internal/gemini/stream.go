package gemini

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// StreamTranslator converts a Gemini streamGenerateContent SSE stream
// (alt=sse: each data line is a full generateContent-shaped JSON chunk) into
// an OpenAI chat.completion.chunk SSE stream ending in "data: [DONE]".
//
// It is the streaming counterpart of BuildChatCompletion and single-goroutine
// by design: the proxy's egress loop feeds it one upstream data payload at a
// time and forwards whatever bytes come back.
type StreamTranslator struct {
	id      string
	model   string
	created int64

	started      bool   // role delta emitted
	finished     bool   // Finish() already emitted
	finishReason string // last Gemini finishReason observed
	toolCalls    int    // tool_calls emitted so far (drives index + ids)
	usage        *genUsage
}

// NewStreamTranslator builds a translator for one response. id, model and
// created are echoed in every chunk envelope (the model string the client
// requested, not Gemini's modelVersion).
func NewStreamTranslator(id, model string, created int64) *StreamTranslator {
	return &StreamTranslator{id: id, model: model, created: created}
}

// oaiChunkOut is one outgoing chat.completion.chunk payload.
type oaiChunkOut struct {
	ID      string           `json:"id"`
	Object  string           `json:"object"`
	Created int64            `json:"created"`
	Model   string           `json:"model"`
	Choices []oaiChunkChoice `json:"choices"`
	Usage   *oaiUsage        `json:"usage,omitempty"`
}

type oaiChunkChoice struct {
	Index        int           `json:"index"`
	Delta        oaiChunkDelta `json:"delta"`
	FinishReason *string       `json:"finish_reason"`
}

type oaiChunkDelta struct {
	Role      string                `json:"role,omitempty"`
	Content   string                `json:"content,omitempty"`
	ToolCalls []oaiChunkToolCallOut `json:"tool_calls,omitempty"`
}

// oaiChunkToolCallOut is a streamed tool call. Gemini delivers each
// functionCall complete in a single part, so the whole call goes out as one
// delta fragment (index + id + name + full arguments).
type oaiChunkToolCallOut struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// writeChunk appends one framed SSE chunk ("data: <json>\n\n").
func (t *StreamTranslator) writeChunk(buf *bytes.Buffer, delta oaiChunkDelta, finishReason *string, usage *oaiUsage) error {
	if !t.started {
		delta.Role = "assistant"
		t.started = true
	}
	payload, err := json.Marshal(oaiChunkOut{
		ID:      t.id,
		Object:  "chat.completion.chunk",
		Created: t.created,
		Model:   t.model,
		Choices: []oaiChunkChoice{{Index: 0, Delta: delta, FinishReason: finishReason}},
		Usage:   usage,
	})
	if err != nil {
		return fmt.Errorf("gemini: marshal stream chunk: %w", err)
	}
	buf.WriteString("data: ")
	buf.Write(payload)
	buf.WriteString("\n\n")
	return nil
}

// Translate processes one upstream Gemini data payload and returns the SSE
// bytes to forward to the client (possibly empty: thought-only and usage-only
// chunks update state silently). finishReason and usage are recorded for the
// terminal Finish() chunk, matching how Gemini delivers both on the last line.
func (t *StreamTranslator) Translate(chunkJSON []byte) ([]byte, error) {
	var chunk genResponse
	if err := json.Unmarshal(chunkJSON, &chunk); err != nil {
		return nil, fmt.Errorf("gemini: invalid stream chunk: %w", err)
	}

	if chunk.UsageMetadata != nil {
		t.usage = chunk.UsageMetadata
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
		if p.FunctionCall != nil {
			args := compactJSON(p.FunctionCall.Args)
			if args == "" {
				args = "{}"
			}
			tc := oaiChunkToolCallOut{
				Index: t.toolCalls,
				ID:    fmt.Sprintf("call_%s_%d", t.id, t.toolCalls),
				Type:  "function",
			}
			tc.Function.Name = p.FunctionCall.Name
			tc.Function.Arguments = args
			delta.ToolCalls = append(delta.ToolCalls, tc)
			t.toolCalls++
			continue
		}
		if p.Thought {
			continue
		}
		delta.Content += p.Text
	}

	if delta.Content == "" && len(delta.ToolCalls) == 0 {
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
	if err := t.writeChunk(&buf, oaiChunkDelta{}, &reason, translateUsage(t.usage)); err != nil {
		return nil, err
	}
	buf.WriteString("data: [DONE]\n\n")
	return buf.Bytes(), nil
}
