package anthropicegress

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// scriptedBody yields its script one entry per Read call, simulating SSE
// arriving in arbitrary splits (including mid-line).
type scriptedBody struct {
	script []string
	closed bool
}

func (r *scriptedBody) Read(p []byte) (int, error) {
	if len(r.script) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.script[0])
	rest := r.script[0][n:]
	if rest == "" {
		r.script = r.script[1:]
	} else {
		r.script[0] = rest
	}
	return n, nil
}

func (r *scriptedBody) Close() error {
	r.closed = true
	return nil
}

// dyingBody serves its data and then fails, standing in for an upstream
// connection that drops mid-stream.
type dyingBody struct {
	data string
	err  error
}

func (r *dyingBody) Read(p []byte) (int, error) {
	if r.data == "" {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func (r *dyingBody) Close() error { return nil }

func TestStreamAdapter_TranslatesFullStream(t *testing.T) {
	// Real Anthropic framing: "event:" lines, blank lines and payloads split
	// awkwardly across reads.
	upstream := &scriptedBody{script: []string{
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":11,\"output_tokens\":1}}}\n\n",
		"event: content_block_start\r\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\r\n\r\n",
		": keepalive\nevent: ping\ndata: {\"type\":\"ping\"}\n\n",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_de",
		"lta\",\"text\":\"Hello\"}}\n\nevent: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":4}}\n\n",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n",
	}}

	out, err := io.ReadAll(NewStreamAdapter(upstream, "claude-sonnet-4-5"))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	chunks, done := parseChunks(t, string(out))
	if !done {
		t.Fatalf("stream did not end with [DONE]:\n%s", out)
	}
	if len(chunks) != 3 { // role, "Hello", terminal
		t.Fatalf("chunks = %d, want 3:\n%s", len(chunks), out)
	}
	if got := joinContent(chunks); got != "Hello" {
		t.Errorf("content = %q, want Hello (payload split across reads)", got)
	}
	// The upstream ping reaches the proxy as a keepalive comment, which is what
	// resets the stall watchdog across a long generation gap.
	if !strings.Contains(string(out), ": ping\n\n") {
		t.Errorf("adapter dropped the upstream ping keepalive:\n%s", out)
	}
	for _, c := range chunks {
		if c.Model != "claude-sonnet-4-5" {
			t.Errorf("model = %q, want the model the client requested", c.Model)
		}
		if !strings.HasPrefix(c.ID, "chatcmpl-") {
			t.Errorf("id = %q, want a chatcmpl- id", c.ID)
		}
	}
	last := chunks[len(chunks)-1]
	if r := last.Choices[0].FinishReason; r == nil || *r != "stop" {
		t.Errorf("finish_reason = %v, want stop", r)
	}
	if last.Usage == nil || last.Usage.PromptTokens != 11 || last.Usage.CompletionTokens != 4 {
		t.Errorf("usage = %+v, want prompt 11 / completion 4", last.Usage)
	}
}

func TestStreamAdapter_MessageStopFinishesOnce(t *testing.T) {
	// message_stop already emitted the terminal chunk + [DONE]; the EOF that
	// follows must not append a second one.
	upstream := &scriptedBody{script: []string{
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":2}}}\n\n",
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n",
		"data: {\"type\":\"message_stop\"}\n\n",
	}}
	out, err := io.ReadAll(NewStreamAdapter(upstream, "m"))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	s := string(out)
	if n := strings.Count(s, "data: [DONE]"); n != 1 {
		t.Errorf("[DONE] count = %d, want 1:\n%s", n, s)
	}
	if n := strings.Count(s, `"finish_reason":"stop"`); n != 1 {
		t.Errorf("terminal chunks = %d, want 1:\n%s", n, s)
	}
}

func TestStreamAdapter_EOFWithoutMessageStopStillFinishes(t *testing.T) {
	// Anthropic carries no [DONE] sentinel, so an upstream that ends at EOF
	// without message_stop still owes the client a terminal chunk.
	upstream := &scriptedBody{script: []string{
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":2}}}\n\n",
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n\n",
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\"},\"usage\":{\"output_tokens\":5}}\n\n",
	}}
	out, err := io.ReadAll(NewStreamAdapter(upstream, "m"))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	chunks, done := parseChunks(t, string(out))
	if !done {
		t.Fatalf("EOF did not produce [DONE]:\n%s", out)
	}
	if r := chunks[len(chunks)-1].Choices[0].FinishReason; r == nil || *r != "tool_calls" {
		t.Errorf("finish_reason = %v, want tool_calls", r)
	}
}

func TestStreamAdapter_ErrorEventPoisonsStream(t *testing.T) {
	// An error event mid-stream is a failed generation: translated bytes drain,
	// then the error surfaces, and no terminal chunk is fabricated over it.
	upstream := &scriptedBody{script: []string{
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":2}}}\n\n",
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"pre\"}}\n\n",
		"event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"boom\"}}\n\n",
		"data: {\"type\":\"message_stop\"}\n\n",
	}}
	out, err := io.ReadAll(NewStreamAdapter(upstream, "m"))
	if err == nil {
		t.Fatal("expected an error for an upstream error event")
	}
	if !strings.Contains(err.Error(), "overloaded_error") {
		t.Errorf("error = %q, want the upstream error type named", err)
	}
	s := string(out)
	if !strings.Contains(s, `"content":"pre"`) {
		t.Errorf("pre-error content lost:\n%s", s)
	}
	if strings.Contains(s, "[DONE]") || strings.Contains(s, `"finish_reason":"`) {
		t.Errorf("terminal chunks fabricated after an error event:\n%s", s)
	}
}

func TestStreamAdapter_MalformedEventPoisonsStream(t *testing.T) {
	upstream := &scriptedBody{script: []string{
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"pre\"}}\n\n",
		"data: {not json}\n\n",
		"data: {\"type\":\"message_stop\"}\n\n",
	}}
	out, err := io.ReadAll(NewStreamAdapter(upstream, "m"))
	if err == nil {
		t.Fatal("expected an error for a malformed event")
	}
	s := string(out)
	if !strings.Contains(s, `"content":"pre"`) {
		t.Errorf("pre-error content lost:\n%s", s)
	}
	if strings.Contains(s, "[DONE]") {
		t.Errorf("[DONE] fabricated after a malformed event:\n%s", s)
	}
}

func TestStreamAdapter_UpstreamDiesMidStream(t *testing.T) {
	boom := errors.New("connection reset")
	body := &dyingBody{
		data: "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"x\"}}\n\n",
		err:  boom,
	}
	out, err := io.ReadAll(NewStreamAdapter(body, "m"))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the upstream error", err)
	}
	// Bytes translated before the failure are still delivered, but a broken
	// stream never gets a [DONE] the pipeline would read as a clean end.
	if !strings.Contains(string(out), `"content":"x"`) {
		t.Errorf("pre-failure content lost:\n%s", out)
	}
	if strings.Contains(string(out), "[DONE]") {
		t.Errorf("[DONE] fabricated on a dropped connection:\n%s", out)
	}
}

func TestStreamAdapter_WrappedEOFStillFinishes(t *testing.T) {
	// EOF can arrive wrapped by any reader sitting between the transport and
	// this adapter; a wrapped EOF must still produce the terminal chunk rather
	// than leaving the client with no [DONE].
	body := &dyingBody{
		data: "data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"x\"}}\n\n",
		err:  fmt.Errorf("transport read: %w", io.EOF),
	}
	out, err := io.ReadAll(NewStreamAdapter(body, "m"))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if _, done := parseChunks(t, string(out)); !done {
		t.Errorf("wrapped EOF skipped the terminal chunk:\n%s", out)
	}
}

func TestStreamAdapter_FlushesPartialLineAtEOF(t *testing.T) {
	// An upstream that closes without a trailing newline must not lose its last
	// event: dropping a final message_delta would silently downgrade a
	// max_tokens truncation to a clean "stop" alongside a clean [DONE].
	upstream := &scriptedBody{script: []string{
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":2}}}\n\n",
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"tail\"}}\n\n",
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"max_tokens\"},\"usage\":{\"output_tokens\":6}}",
	}}
	out, err := io.ReadAll(NewStreamAdapter(upstream, "m"))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	chunks, done := parseChunks(t, string(out))
	if !done {
		t.Fatalf("stream did not end with [DONE]:\n%s", out)
	}
	if got := joinContent(chunks); got != "tail" {
		t.Errorf("content = %q, want tail", got)
	}
	last := chunks[len(chunks)-1]
	if r := last.Choices[0].FinishReason; r == nil || *r != "length" {
		t.Errorf("finish_reason = %v, want length from the unterminated message_delta", r)
	}
	if last.Usage == nil || last.Usage.CompletionTokens != 6 {
		t.Errorf("usage = %+v, want completion_tokens 6 from the unterminated message_delta", last.Usage)
	}
}

func TestStreamAdapter_TruncatedResidualAfterMessageStopIgnored(t *testing.T) {
	// The stream already completed on message_stop; the connection then drops
	// mid-line. Flushing that residual must not poison a finished stream or
	// append anything behind the sentinel.
	upstream := &scriptedBody{script: []string{
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":2}}}\n\n",
		"data: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":1}}\n\n",
		"data: {\"type\":\"message_stop\"}\n\n",
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_del",
	}}
	out, err := io.ReadAll(NewStreamAdapter(upstream, "m"))
	if err != nil {
		t.Fatalf("a truncated line after message_stop poisoned a finished stream: %v", err)
	}
	s := string(out)
	if n := strings.Count(s, "data: [DONE]"); n != 1 {
		t.Errorf("[DONE] count = %d, want 1:\n%s", n, s)
	}
	if n := strings.Count(s, `"finish_reason":"stop"`); n != 1 {
		t.Errorf("terminal chunks = %d, want 1:\n%s", n, s)
	}
	if !strings.HasSuffix(s, "data: [DONE]\n\n") {
		t.Errorf("bytes emitted after the sentinel:\n%s", s)
	}
}

func TestStreamAdapter_OverlongLineFailsStream(t *testing.T) {
	// An upstream that never emits a newline must fail the stream rather than
	// grow the line buffer without bound.
	upstream := &scriptedBody{script: []string{
		"data: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"" +
			strings.Repeat("a", maxSSELineBytes+1),
	}}
	out, err := io.ReadAll(NewStreamAdapter(upstream, "m"))
	if err == nil {
		t.Fatal("expected an error once the line exceeded the cap")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error = %q, want it to name the exceeded cap", err)
	}
	if strings.Contains(string(out), "[DONE]") {
		t.Errorf("[DONE] fabricated over an unterminated line:\n%s", out)
	}
	if strings.Contains(err.Error(), "aaaa") {
		t.Errorf("error leaked the buffered line: %q", err)
	}
}

func TestStreamAdapter_ClosePropagates(t *testing.T) {
	upstream := &scriptedBody{}
	a := NewStreamAdapter(upstream, "m")
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !upstream.closed {
		t.Error("Close did not propagate to the upstream body")
	}
}
