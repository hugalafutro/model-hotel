package gemini

import (
	"bytes"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// StreamAdapter wraps an upstream Gemini streamGenerateContent alt=sse body as
// an io.ReadCloser that yields chat.completion.chunk SSE bytes. Wrapping the
// UPSTREAM body (not the client writer) lets the whole existing streaming
// pipeline — TTFT probe, stall watchdog, transforms, metering — run unchanged
// on what it already understands (same trick as openairesponses.StreamAdapter).
//
// Vertex streams carry no [DONE] sentinel: EOF is the natural end, so the
// adapter emits the translator's terminal chunk + [DONE] when upstream EOF
// arrives. Any other upstream error surfaces as a stream without [DONE], which
// the pipeline already classifies as a truncation.
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
// copies out. On EOF the terminal Finish() bytes are appended before the EOF
// is surfaced; other upstream errors surface only after all translated bytes
// have been drained. A translation failure poisons the stream: already
// translated bytes drain, then the error surfaces — Finish() is never
// fabricated over a corrupt upstream, so the proxy sees a failed stream
// instead of a clean empty/partial success.
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
			if err == io.EOF && a.transErr == nil {
				fin, finErr := a.tr.Finish()
				if finErr != nil {
					debuglog.Warn("gemini: stream finish failed", "error", finErr)
				}
				a.pending = append(a.pending, fin...)
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
			// A malformed data line means a corrupt upstream; record it and
			// stop translating so Read surfaces the failure.
			debuglog.Warn("gemini: stream chunk translate failed", "error", err)
			a.transErr = err
			return
		}
		a.pending = append(a.pending, out...)
	}
}

// Close closes the upstream body. The stall watchdog calls this to unblock a
// hung read, so it must propagate to the wrapped connection.
func (a *StreamAdapter) Close() error {
	return a.upstream.Close()
}
