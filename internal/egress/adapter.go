package egress

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// MaxSSEEventBytes caps the SSE event an adapter will buffer: the data fields
// joined so far plus the line still being read. Vendor deltas are orders of
// magnitude smaller than this, so an upstream that never closes an event is a
// broken peer, not a large response — the stream fails instead of growing the
// buffer until the process suffers.
const MaxSSEEventBytes = 1 << 20

// Translator converts one upstream SSE data payload into the client-facing
// bytes for that event, and produces the stream's terminal bytes on Finish.
// Implemented by each dialect's StreamTranslator.
type Translator interface {
	// Translate maps one event's payload — every "data:" field of that event,
	// joined with newlines — to zero or more output bytes. A non-nil error
	// means the upstream stream is corrupt or carried an error event, and
	// poisons the adapter.
	Translate(payload []byte) ([]byte, error)
	// Finish returns the terminal chunk plus the [DONE] sentinel, or nothing
	// when the translator already emitted them.
	Finish() ([]byte, error)
}

// StreamAdapter wraps an upstream vendor SSE body as an io.ReadCloser that
// yields chat.completion.chunk SSE bytes. Wrapping the UPSTREAM body (not the
// client writer) lets the whole existing streaming pipeline — TTFT probe, stall
// watchdog, transforms, metering — run unchanged on what it already
// understands. All three dialect adapters — gemini, anthropicegress and
// openairesponses — are this type; each supplies only its translator and its
// log prefix.
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
	eventBuf []byte // data fields of the event under construction, newline-joined
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
// copies out. On EOF any unterminated tail is flushed through the translator
// and the terminal Finish() bytes are appended before the EOF is
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
				a.flushAtEOF()
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

// consume splits incoming bytes into SSE lines and assembles those lines into
// events. SSE lets one event fold its payload across several "data:" fields,
// so a field is appended to the event under construction rather than
// translated on its own, and the blank line that terminates the event hands
// the joined payload to the translator exactly once. Comment lines (":") and
// every other field — "event:", "id:", "retry:" — are ignored, because the
// dialect translators key off the payload's own JSON type rather than the SSE
// event name. The adapter generates its own framing on the way out.
func (a *StreamAdapter) consume(p []byte) {
	a.lineBuf = append(a.lineBuf, p...)
	for {
		idx := bytes.IndexByte(a.lineBuf, '\n')
		if idx < 0 {
			a.withinEventCap(len(a.lineBuf))
			return
		}
		line := bytes.TrimRight(a.lineBuf[:idx], "\r")
		a.lineBuf = a.lineBuf[idx+1:]
		if len(line) == 0 {
			if !a.dispatchEvent() {
				return
			}
			continue
		}
		if !bytes.HasPrefix(line, []byte("data:")) {
			continue
		}
		// The spec strips one optional leading space; trimming both ends is
		// safe on top of that because JSON ignores whitespace around a value,
		// and a folded payload cannot split inside a string literal — the
		// newline the join inserts would be illegal there — so no byte a
		// translator reads can be trimmed away. A field with no value adds
		// nothing to the event.
		value := bytes.TrimSpace(line[len("data:"):])
		if len(value) == 0 {
			continue
		}
		if len(a.eventBuf) > 0 {
			a.eventBuf = append(a.eventBuf, '\n')
		}
		a.eventBuf = append(a.eventBuf, value...)
		if !a.withinEventCap(0) {
			return
		}
	}
}

// withinEventCap poisons the stream when the event under construction outgrows
// the cap, and reports whether parsing may continue. partialLine is the length
// of the line still being read, counted with the fields already joined so that
// a folded event cannot carry more than a single line could.
func (a *StreamAdapter) withinEventCap(partialLine int) bool {
	if len(a.eventBuf)+partialLine <= MaxSSEEventBytes {
		return true
	}
	a.lineBuf, a.eventBuf = nil, nil
	a.transErr = fmt.Errorf("%s: upstream SSE event exceeds %d bytes", a.component, MaxSSEEventBytes)
	debuglog.Warn(a.component+": stream event exceeds buffer cap", "limit", MaxSSEEventBytes)
	return false
}

// dispatchEvent hands the assembled payload to the translator once and clears
// the buffer for the next event. An event that carried no data fields
// dispatches nothing. It reports whether parsing may continue.
func (a *StreamAdapter) dispatchEvent() bool {
	if len(a.eventBuf) == 0 {
		return true
	}
	payload := a.eventBuf
	a.eventBuf = nil
	out, err := a.tr.Translate(payload)
	if err != nil {
		// A malformed event or an upstream error event means the stream is
		// dead; record it and stop translating so Read surfaces the failure.
		debuglog.Warn(a.component+": stream event translate failed", "error", err)
		a.transErr = err
		return false
	}
	a.pending = append(a.pending, out...)
	return true
}

// flushAtEOF dispatches the stream's unterminated tail: the residual line the
// upstream never terminated, and then the event no blank line ever closed. An
// upstream that closes without that framing would otherwise lose its last event
// entirely — including a final message_delta's stop_reason — while Finish()
// still emitted a clean terminal chunk, which is exactly the quiet truncation
// this adapter exists to prevent. A tail that does not translate poisons the
// stream, so a connection cut mid-event surfaces as the failure it is.
func (a *StreamAdapter) flushAtEOF() {
	if a.transErr != nil {
		return
	}
	if len(a.lineBuf) > 0 {
		a.lineBuf = append(a.lineBuf, '\n')
		a.consume(nil)
	}
	// consume can only have poisoned the stream here by tripping the event cap,
	// which clears the buffer, so dispatchEvent then has nothing to hand on.
	a.dispatchEvent()
}

// Close closes the upstream body. The stall watchdog calls this to unblock a
// hung read, so it must propagate to the wrapped connection.
func (a *StreamAdapter) Close() error {
	return a.upstream.Close()
}
