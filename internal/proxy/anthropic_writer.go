package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/anthropic"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// anthropicResponseWriter wraps the client http.ResponseWriter so the entire
// existing OpenAI-shaped proxy pipeline (failover loop, TTFT probe, stall
// watchdog, hedging, metering, every error site) can run UNCHANGED while the
// bytes it emits are converted to the Anthropic Messages wire format on the way
// out. It dispatches on the response the pipeline produces:
//
//   - text/event-stream + 200  -> streaming mode: parse the OpenAI chunk SSE and
//     re-emit the Anthropic message_start/content_block_*/message_delta/stop
//     event sequence incrementally via anthropic.StreamTranslator.
//   - application/json + 200    -> buffered mode: collect the OpenAI
//     chat-completion response and, on Finalize, emit one Anthropic message.
//   - any non-200               -> buffered mode: collect the OpenAI error body
//     and, on Finalize, emit the Anthropic {"type":"error",...} shape.
//
// This is the "wrap the client sink" seam the plan calls for, lifted one level
// to the ResponseWriter so no failover/error code path needs Anthropic awareness.
type anthropicResponseWriter struct {
	w         http.ResponseWriter
	messageID string
	model     string

	committed bool // mode decided, headers handled
	streaming bool // text/event-stream path
	verbatim  bool // native Anthropic passthrough: forward bytes unchanged
	status    int  // captured status for buffered mode

	// nativeFlag points at requestState.anthropicNativeAttempt, set per failover
	// attempt. When the attempt that actually serves a 200 is the native
	// Anthropic passthrough, the upstream bytes are already Anthropic-shaped and
	// are forwarded verbatim. Errors (status != 200) always go through
	// translation so the client still gets a well-formed Anthropic error.
	nativeFlag *bool

	// streaming-mode state
	translator *anthropic.StreamTranslator
	lineBuf    []byte // accumulates partial SSE lines across Write calls
	streamDone bool   // [DONE] seen / Finish emitted

	// buffered-mode state
	body bytes.Buffer
}

func newAnthropicResponseWriter(w http.ResponseWriter, messageID, model string) *anthropicResponseWriter {
	return &anthropicResponseWriter{w: w, messageID: messageID, model: model, status: http.StatusOK}
}

// bindNativeFlag wires the writer to the per-attempt native-passthrough flag on
// requestState, set once ingest has produced it. Called before the failover loop.
func (a *anthropicResponseWriter) bindNativeFlag(f *bool) { a.nativeFlag = f }

// Header exposes the underlying header map so the pipeline can set Content-Type
// etc. before the first write. We read Content-Type from it at commit time to
// pick streaming vs buffered mode.
func (a *anthropicResponseWriter) Header() http.Header { return a.w.Header() }

// WriteHeader captures the status and commits the mode. In streaming mode the
// status + headers pass through to the client immediately; in buffered mode they
// are withheld until Finalize, which writes the translated body and its status.
func (a *anthropicResponseWriter) WriteHeader(status int) {
	a.status = status
	a.commit()
}

// Write routes bytes according to the committed mode.
func (a *anthropicResponseWriter) Write(p []byte) (int, error) {
	if !a.committed {
		a.commit()
	}
	if a.verbatim {
		// Native passthrough forwards the upstream Anthropic response (JSON or SSE)
		// byte-for-byte. Not an XSS sink: the global security-headers middleware
		// (cmd/server/main.go) sets X-Content-Type-Options: nosniff on every
		// response, the Content-Type is always application/json or
		// text/event-stream (never text/html), and the consumer is an API client,
		// not a browser. CodeQL go/reflected-xss cannot trace the middleware header
		// through this wrapper, so the alert is dismissed as a false positive.
		//nolint:gosec // G705: see above — JSON/SSE API body, nosniff set globally, not HTML
		return a.w.Write(p)
	}
	if a.streaming {
		a.consumeStreaming(p)
		return len(p), nil
	}
	return a.body.Write(p)
}

// Flush flushes the real writer when output is going out live (streaming
// translation or native verbatim); in buffered mode there is nothing to flush
// until Finalize.
func (a *anthropicResponseWriter) Flush() {
	if a.streaming || a.verbatim {
		if f, ok := a.w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

// commit decides the output mode once, from the native flag + Content-Type the
// pipeline set:
//   - native 200  -> verbatim: forward the already-Anthropic upstream bytes
//   - event-stream 200 -> streaming translation
//   - anything else (incl. all errors) -> buffered translation until Finalize
//
// Native errors deliberately fall through to buffered translation so the client
// always gets a well-formed Anthropic error envelope.
func (a *anthropicResponseWriter) commit() {
	if a.committed {
		return
	}
	a.committed = true
	if a.nativeFlag != nil && *a.nativeFlag && a.status == http.StatusOK {
		a.verbatim = true
		a.w.WriteHeader(a.status)
		return
	}
	ct := a.w.Header().Get("Content-Type")
	if a.status == http.StatusOK && strings.Contains(ct, "text/event-stream") {
		a.streaming = true
		a.translator = anthropic.NewStreamTranslator(a.messageID, a.model)
		a.w.WriteHeader(a.status)
	}
}

// consumeStreaming buffers incoming OpenAI SSE bytes, splits them into complete
// lines (writeSSEDataChunk emits "data: ", payload, and "\n\n" as separate
// writes, so bytes arrive fragmented), and translates each `data:` line. Comment,
// blank, and event: lines are dropped — we generate our own Anthropic framing.
func (a *anthropicResponseWriter) consumeStreaming(p []byte) {
	a.lineBuf = append(a.lineBuf, p...)
	for {
		idx := bytes.IndexByte(a.lineBuf, '\n')
		if idx < 0 {
			return
		}
		line := a.lineBuf[:idx]
		a.lineBuf = a.lineBuf[idx+1:]
		a.handleStreamLine(bytes.TrimRight(line, "\r"))
	}
}

// handleStreamLine translates one complete SSE line.
func (a *anthropicResponseWriter) handleStreamLine(line []byte) {
	if a.streamDone {
		return
	}
	if !bytes.HasPrefix(line, []byte("data:")) {
		return // comment / blank / event: directive — ignored
	}
	payload := bytes.TrimSpace(line[len("data:"):])
	if len(payload) == 0 {
		return
	}
	if bytes.Equal(payload, []byte("[DONE]")) {
		a.finishStream()
		return
	}
	var chunk anthropic.OAStreamChunk
	if err := json.Unmarshal(payload, &chunk); err != nil {
		debuglog.Debug("anthropic: skip unparseable upstream chunk", "error", err)
		return
	}
	out, err := a.translator.Translate(chunk)
	if err != nil {
		debuglog.Warn("anthropic: stream translate failed", "error", err)
		return
	}
	if len(out) > 0 {
		//nolint:gosec // G705 false positive: Anthropic SSE event body, not HTML; Content-Type is text/event-stream
		_, _ = a.w.Write(out)
		a.Flush()
	}
}

// finishStream emits the terminal Anthropic events once.
func (a *anthropicResponseWriter) finishStream() {
	if a.streamDone {
		return
	}
	a.streamDone = true
	out, err := a.translator.Finish()
	if err != nil {
		debuglog.Warn("anthropic: stream finish failed", "error", err)
		return
	}
	if len(out) > 0 {
		//nolint:gosec // G705 false positive: Anthropic SSE event body, not HTML; Content-Type is text/event-stream
		_, _ = a.w.Write(out)
		a.Flush()
	}
}

// Finalize emits the translated response. In streaming mode it closes the stream
// if the upstream ended without a [DONE] sentinel. In buffered mode it converts
// the collected OpenAI response (200) or error (non-200) and writes it with the
// right status. It must be called exactly once after the pipeline returns.
func (a *anthropicResponseWriter) Finalize() {
	if !a.committed {
		// Pipeline wrote nothing (e.g. it returned before any response). Nothing
		// to translate; leave the connection as-is.
		return
	}
	if a.verbatim {
		// Native passthrough already forwarded the upstream bytes as-is.
		return
	}
	if a.streaming {
		a.finishStream()
		return
	}

	raw := a.body.Bytes()
	var out []byte
	if a.status == http.StatusOK {
		translated, err := anthropic.BuildMessageResponse(raw, a.messageID, a.model)
		if err != nil {
			debuglog.Warn("anthropic: response translate failed; emitting error", "error", err)
			a.status = http.StatusBadGateway
			out = anthropic.BuildErrorResponse(nil, a.status)
		} else {
			out = translated
		}
	} else {
		out = anthropic.BuildErrorResponse(raw, a.status)
	}

	a.w.Header().Set("Content-Type", "application/json")
	a.w.WriteHeader(a.status)
	//nolint:gosec // G705 false positive: Anthropic JSON response body, not HTML; Content-Type is application/json
	_, _ = a.w.Write(out)
}
