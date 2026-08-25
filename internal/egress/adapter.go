package egress

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// MaxSSELineBytes caps the unterminated SSE line an adapter will buffer. Vendor
// deltas are orders of magnitude smaller than this, so an upstream that never
// emits a newline is a broken peer, not a large response — the stream fails
// instead of growing the buffer until the process suffers.
const MaxSSELineBytes = 1 << 20

// Translator converts one upstream SSE data payload into the client-facing
// bytes for that event, and produces the stream's terminal bytes on Finish.
// Implemented by each dialect's StreamTranslator.
type Translator interface {
	// Translate maps one "data:" payload to zero or more output bytes. A
	// non-nil error means the upstream stream is corrupt or carried an error
	// event, and poisons the adapter.
	Translate(payload []byte) ([]byte, error)
	// Finish returns the terminal chunk plus the [DONE] sentinel, or nothing
	// when the translator already emitted them.
	Finish() ([]byte, error)
}

// StreamAdapter wraps an upstream vendor SSE body as an io.ReadCloser that
// yields chat.completion.chunk SSE bytes. Wrapping the UPSTREAM body (not the
// client writer) lets the whole existing streaming pipeline — TTFT probe, stall
// watchdog, transforms, metering — run unchanged on what it already
// understands. openairesponses.StreamAdapter plays the same trick with a
// simpler shape: its translator has no Finish(), so it stays outside this type.
//
// Vendor streams carry no [DONE] sentinel of their own, so the translator's
// Finish() supplies the terminal chunk + [DONE] when upstream EOF arrives. Any
// other upstream error surfaces as a stream without [DONE], which the pipeline
// already classifies as a truncation.
type StreamAdapter struct {
	component string // log prefix: the dialect that built this adapter
	upstream  io.ReadCloser
	tr        Translator

	lineBuf  []byte // partial SSE line carried across reads
	pending  []byte // translated bytes not yet handed to the caller
	readBuf  []byte
	srcErr   error
	transErr error // first translation failure; poisons the stream
}

// NewStreamAdapter builds an adapter for one streaming response. component is
// the log prefix ("gemini", "anthropicegress"); tr is that dialect's stream
// translator, already primed with the chunk id and model to echo.
func NewStreamAdapter(component string, upstream io.ReadCloser, tr Translator) *StreamAdapter {
	return &StreamAdapter{
		component: component,
		upstream:  upstream,
		tr:        tr,
		readBuf:   make([]byte, 32*1024),
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
						debuglog.Warn(a.component+": stream finish failed", "error", finErr)
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
			if len(a.lineBuf) > MaxSSELineBytes {
				a.lineBuf = nil
				a.transErr = fmt.Errorf("%s: upstream SSE line exceeds %d bytes", a.component, MaxSSELineBytes)
				debuglog.Warn(a.component+": stream line exceeds buffer cap", "limit", MaxSSELineBytes)
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
			debuglog.Warn(a.component+": stream chunk translate failed", "error", err)
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
