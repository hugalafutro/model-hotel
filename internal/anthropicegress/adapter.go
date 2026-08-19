package anthropicegress

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// maxSSELineBytes caps the unterminated SSE line the adapter will buffer.
// Anthropic's deltas are orders of magnitude smaller than this, so an upstream
// that never emits a newline is a broken peer, not a large response — the
// stream fails instead of growing the buffer until the process suffers.
const maxSSELineBytes = 1 << 20

// StreamAdapter wraps an upstream Anthropic /v1/messages SSE body as an
// io.ReadCloser that yields chat.completion.chunk SSE bytes. Wrapping the
// UPSTREAM body (not the client writer) lets the whole existing streaming
// pipeline — TTFT probe, stall watchdog, transforms, metering — run unchanged
// on what it already understands (same trick as gemini.StreamAdapter).
//
// Anthropic ends its stream with message_stop and carries no [DONE] sentinel,
// so the translator emits the terminal chunk + [DONE] on message_stop. An
// upstream that reaches EOF without message_stop gets that terminal pair from
// Finish() instead; any other upstream error surfaces as a stream without
// [DONE], which the pipeline already classifies as a truncation.
type StreamAdapter struct {
	upstream io.ReadCloser
	tr       *StreamTranslator

	lineBuf  []byte // partial SSE line carried across reads
	pending  []byte // translated bytes not yet handed to the caller
	readBuf  []byte
	srcErr   error
	transErr error // first translation failure; poisons the stream
}

// NewStreamAdapter builds an adapter for one streaming response. model is
// echoed in every emitted chunk (the model string the client requested).
func NewStreamAdapter(upstream io.ReadCloser, model string) *StreamAdapter {
	id := "chatcmpl-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	return &StreamAdapter{
		upstream: upstream,
		tr:       NewStreamTranslator(id, model, time.Now().Unix()),
		readBuf:  make([]byte, 32*1024),
	}
}

// Read refills the pending buffer from upstream (translating as it goes) and
// copies out. On EOF any residual partial line is flushed through the
// translator and the terminal Finish() bytes are appended before the EOF is
// surfaced; other upstream errors surface only after all translated bytes have
// been drained. A translation failure poisons the stream: already translated
// bytes drain, then the error surfaces — Finish() is never fabricated over a
// corrupt upstream, so the proxy sees a failed stream instead of a clean
// empty/partial success.
func (a *StreamAdapter) Read(p []byte) (int, error) {
	for len(a.pending) == 0 {
		if a.transErr != nil {
			return 0, a.transErr
		}
		if a.srcErr != nil {
			return 0, a.srcErr
		}
		n, err := a.upstream.Read(a.readBuf)
		if n > 0 {
			a.consume(a.readBuf[:n])
		}
		if err != nil {
			a.srcErr = err
			if errors.Is(err, io.EOF) {
				// io.Copy and io.ReadAll compare the terminating error against
				// io.EOF with ==, so a wrapped EOF from a reader between the
				// transport and here would be reported as a broken stream even
				// though it ended normally. This adapter ends on io.EOF itself.
				a.srcErr = io.EOF
				a.flushPartialLine()
				if a.transErr == nil {
					fin, finErr := a.tr.Finish()
					if finErr != nil {
						debuglog.Warn("anthropicegress: stream finish failed", "error", finErr)
					}
					a.pending = append(a.pending, fin...)
				}
			}
		}
	}
	n := copy(p, a.pending)
	a.pending = a.pending[n:]
	return n, nil
}

// consume splits incoming bytes into SSE lines and feeds each data payload to
// the translator. "event:"/comment/blank lines are dropped; the adapter
// generates its own framing.
func (a *StreamAdapter) consume(p []byte) {
	a.lineBuf = append(a.lineBuf, p...)
	for {
		idx := bytes.IndexByte(a.lineBuf, '\n')
		if idx < 0 {
			if len(a.lineBuf) > maxSSELineBytes {
				a.lineBuf = nil
				a.transErr = fmt.Errorf("anthropicegress: upstream SSE line exceeds %d bytes", maxSSELineBytes)
				debuglog.Warn("anthropicegress: stream line exceeds buffer cap", "limit", maxSSELineBytes)
			}
			return
		}
		line := bytes.TrimRight(a.lineBuf[:idx], "\r")
		a.lineBuf = a.lineBuf[idx+1:]
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		payload := bytes.TrimSpace(line[len("data:"):])
		if len(payload) == 0 {
			continue
		}
		out, err := a.tr.Translate(payload)
		if err != nil {
			// A malformed data line or an upstream error event means the
			// stream is dead; record it and stop translating so Read surfaces
			// the failure.
			debuglog.Warn("anthropicegress: stream event translate failed", "error", err)
			a.transErr = err
			return
		}
		a.pending = append(a.pending, out...)
	}
}

// flushPartialLine feeds a residual line to the translator at EOF. An upstream
// that closes without a trailing newline would otherwise lose its last event
// entirely — including a final message_delta's stop_reason — while Finish()
// still emitted a clean terminal chunk, which is exactly the quiet truncation
// this adapter exists to prevent.
func (a *StreamAdapter) flushPartialLine() {
	if len(a.lineBuf) == 0 || a.transErr != nil {
		return
	}
	a.lineBuf = append(a.lineBuf, '\n')
	a.consume(nil)
}

// Close closes the upstream body. The stall watchdog calls this to unblock a
// hung read, so it must propagate to the wrapped connection.
func (a *StreamAdapter) Close() error {
	return a.upstream.Close()
}
