package proxy

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/egress"
	"github.com/hugalafutro/model-hotel/internal/openairesponses"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// ingressTranslator turns the chat.completion.chunk stream the pipeline emits
// into one ingress dialect's event stream. Translate and Finish are the egress
// contract (one payload in, dialect bytes out); Fail is the terminal error
// frame in the dialect, which ends the stream.
type ingressTranslator interface {
	egress.Translator
	Fail(message, kind string) []byte
}

// ingressDialect is one foreign wire format the gateway accepts on its own
// endpoint (Anthropic Messages on /v1/messages, OpenAI Responses on
// /v1/responses) and speaks back to the client: how a stream, a non-streaming
// answer and an error are rendered. One instance per request, since the
// dialect stamps the request's own ids onto what it emits.
type ingressDialect interface {
	label() string
	newStreamTranslator() ingressTranslator
	// buildResponse renders a non-streaming chat completion (any 2xx).
	buildResponse(chatBody []byte) ([]byte, error)
	// buildError renders a non-2xx body, an OpenAI error envelope or a raw
	// upstream body, with the status it came with.
	buildError(openaiBody []byte, status int) []byte
}

// ingressResponseWriter wraps the client http.ResponseWriter so the entire
// existing OpenAI-shaped proxy pipeline (failover loop, TTFT probe, stall
// watchdog, hedging, metering, every error site) can run UNCHANGED while the
// bytes it emits are converted to the ingress dialect on the way out. It
// dispatches on the response the pipeline produces:
//
//   - text/event-stream + 2xx  -> streaming mode: parse the OpenAI chunk SSE and
//     re-emit the dialect's event sequence incrementally via its translator.
//   - application/json + 2xx    -> buffered mode: collect the OpenAI
//     chat-completion response and, on Finalize, emit one dialect response.
//   - 204/205                   -> passed through: a status that forbids a body
//     has nothing to translate.
//   - any non-2xx               -> buffered mode: collect the OpenAI error body
//     and, on Finalize, emit the dialect's error shape.
//
// This is the "wrap the client sink" seam, lifted one level to the
// ResponseWriter so no failover/error code path needs dialect awareness.
type ingressResponseWriter struct {
	w       http.ResponseWriter
	dialect ingressDialect

	committed bool // mode decided, headers handled
	streaming bool // text/event-stream path
	verbatim  bool // native passthrough: forward bytes unchanged
	status    int  // captured status for buffered mode

	// nativeFlag points at the requestState's per-attempt native flag for this
	// dialect (anthropicNativeAttempt or responsesNativeAttempt). When the
	// attempt that actually serves a SUCCESS (any 2xx) is the native
	// passthrough, the upstream bytes are already in the dialect and are
	// forwarded verbatim. Errors (any non-2xx) always go through translation
	// so the client still gets a well-formed error in the dialect.
	nativeFlag *bool

	// streaming-mode state
	translator ingressTranslator
	lineBuf    []byte // accumulates partial SSE lines across Write calls
	streamDone bool   // [DONE] seen / Finish emitted

	// buffered-mode state
	body bytes.Buffer
}

func newIngressResponseWriter(w http.ResponseWriter, dialect ingressDialect) *ingressResponseWriter {
	return &ingressResponseWriter{w: w, dialect: dialect, status: http.StatusOK}
}

// newAnthropicResponseWriter wraps w for the Anthropic Messages dialect.
func newAnthropicResponseWriter(w http.ResponseWriter, messageID, model string) *ingressResponseWriter {
	return newIngressResponseWriter(w, anthropicIngress{messageID: messageID, model: model})
}

// newResponsesResponseWriter wraps w for the OpenAI Responses dialect.
// facts echoes the request's members and reverses its namespaced tool names.
func newResponsesResponseWriter(w http.ResponseWriter, responseID, model string, facts *openairesponses.RequestFacts) *ingressResponseWriter {
	return newIngressResponseWriter(w, responsesIngress{responseID: responseID, model: model, facts: facts})
}

// bindNativeFlag wires the writer to the per-attempt native-passthrough flag on
// requestState, set once ingest has produced it. Called before the failover loop.
func (a *ingressResponseWriter) bindNativeFlag(f *bool) { a.nativeFlag = f }

// Header exposes the underlying header map so the pipeline can set Content-Type
// etc. before the first write. We read Content-Type from it at commit time to
// pick streaming vs buffered mode.
func (a *ingressResponseWriter) Header() http.Header { return a.w.Header() }

// WriteHeader captures the status and commits the mode. In streaming mode the
// status + headers pass through to the client immediately; in buffered mode they
// are withheld until Finalize, which writes the translated body and its status.
func (a *ingressResponseWriter) WriteHeader(status int) {
	a.status = status
	a.commit()
}

// Write routes bytes according to the committed mode.
func (a *ingressResponseWriter) Write(p []byte) (int, error) {
	if !a.committed {
		a.commit()
	}
	if a.verbatim {
		// Native passthrough forwards the upstream response (JSON or SSE)
		// byte-for-byte. Not an XSS sink: the global security-headers middleware
		// (cmd/server/main.go) sets X-Content-Type-Options: nosniff on every
		// response, the Content-Type is always application/json or
		// text/event-stream (never text/html), and the consumer is an API client,
		// not a browser. CodeQL go/reflected-xss cannot trace the middleware header
		// through this wrapper, so the alert is dismissed as a false positive.
		// #nosec G705 -- see above — JSON/SSE API body, nosniff set globally, not HTML
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
func (a *ingressResponseWriter) Flush() {
	if a.streaming || a.verbatim {
		if f, ok := a.w.(http.Flusher); ok {
			f.Flush()
		}
	}
}

// commit decides the output mode once, from the native flag + Content-Type the
// pipeline set:
//   - native SUCCESS (any 2xx) -> verbatim: forward the already-dialect bytes
//   - event-stream SUCCESS (any 2xx) -> streaming translation
//   - anything else (incl. all errors) -> buffered translation until Finalize
//
// Any 2xx, not a bare 200: a relay may answer a completion 201 or 202, and
// reading those as failures dropped a good answer into an error envelope.
//
// Native errors deliberately fall through to buffered translation so the client
// always gets a well-formed error in the dialect.
func (a *ingressResponseWriter) commit() {
	if a.committed {
		return
	}
	a.committed = true
	if a.nativeFlag != nil && *a.nativeFlag && servedSuccessStatus(a.status) {
		a.verbatim = true
		a.w.WriteHeader(a.status)
		return
	}
	ct := a.w.Header().Get("Content-Type")
	if servedSuccessStatus(a.status) && strings.Contains(ct, "text/event-stream") {
		a.streaming = true
		a.translator = a.dialect.newStreamTranslator()
		a.w.WriteHeader(a.status)
	}
}

// consumeStreaming buffers incoming OpenAI SSE bytes, splits them into complete
// lines (writeSSEDataChunk emits "data: ", payload, and "\n\n" as separate
// writes, so bytes arrive fragmented), and translates each `data:` line. Comment,
// blank, and event: lines are dropped — we generate our own dialect framing.
func (a *ingressResponseWriter) consumeStreaming(p []byte) {
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
func (a *ingressResponseWriter) handleStreamLine(line []byte) {
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
	if a.emitStreamError(payload) {
		return
	}
	out, err := a.translator.Translate(payload)
	if err != nil {
		debuglog.Warn(a.dialect.label()+": stream translate failed", "error", err)
		return
	}
	a.writeStream(out)
}

// emitStreamError turns a frame carrying a top-level "error" member into the
// dialect's terminal error frame and ends the stream. Reports whether it
// handled the frame.
//
// Both the gateway's own terminal frame (writeTerminalError ->
// buildOpenAIStreamError, `data: {"error":…}` followed by `data: [DONE]`) and a
// provider's in-stream error object arrive here. Without this they decoded to
// an empty chunk, the translator emitted nothing, and the [DONE] behind them
// closed the stream with a clean terminal event: the client saw a normal end
// for a failed request. Marking the stream done also swallows that [DONE], an
// error frame is terminal, and a clean end after it would contradict it.
//
// The message is already masked: the gateway's frame is masked by
// opts.masker in writeTerminalError, and a provider frame by
// st.masker before it is forwarded (proxy_stream_response.go). Emptiness and
// message rendering use the shared util rules, the same pair captureSSEError
// reads on the OpenAI-shaped path, so the two cannot disagree about what counts
// as an error.
func (a *ingressResponseWriter) emitStreamError(payload []byte) bool {
	var env struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(payload, &env) != nil || !util.ValueCarries(env.Error) {
		return false
	}
	a.streamDone = true
	// The gateway's own frame names its error kind under code; a provider's
	// frame may carry anything there, and only a string is a kind.
	var kind struct {
		Code string `json:"code"`
	}
	_ = json.Unmarshal(env.Error, &kind)
	a.writeStream(a.translator.Fail(util.ErrorMemberMessage(env.Error), kind.Code))
	return true
}

// finishStream emits the terminal dialect events once.
func (a *ingressResponseWriter) finishStream() {
	if a.streamDone {
		return
	}
	a.streamDone = true
	out, err := a.translator.Finish()
	if err != nil {
		debuglog.Warn(a.dialect.label()+": stream finish failed", "error", err)
		return
	}
	a.writeStream(out)
}

func (a *ingressResponseWriter) writeStream(out []byte) {
	if len(out) == 0 {
		return
	}
	// #nosec G705 -- dialect SSE event body, not HTML; Content-Type is text/event-stream
	_, _ = a.w.Write(out)
	a.Flush()
}

// Finalize emits the translated response. In streaming mode it closes the stream
// if the upstream ended without a [DONE] sentinel. In buffered mode it converts
// the collected OpenAI response (any 2xx) or error (non-2xx) and writes it with
// the right status; a status that forbids a body is passed through untranslated.
// It must be called exactly once after the pipeline returns.
func (a *ingressResponseWriter) Finalize() {
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

	if bodilessSuccessStatus(a.status) {
		// 204/205 carry no body by definition, so there is nothing to translate.
		// Running the translator on the empty bytes fails and rewrites the answer
		// to 502, which turned a provider's legitimate No Content into a gateway
		// error while the request log still said completed/204.
		a.w.WriteHeader(a.status)
		return
	}

	raw := a.body.Bytes()
	var out []byte
	if servedSuccessStatus(a.status) {
		translated, err := a.dialect.buildResponse(raw)
		if err != nil {
			debuglog.Warn(a.dialect.label()+": response translate failed; emitting error", "error", err)
			a.status = http.StatusBadGateway
			out = a.dialect.buildError(nil, a.status)
		} else {
			out = translated
		}
	} else {
		out = a.dialect.buildError(raw, a.status)
	}

	a.w.Header().Set("Content-Type", "application/json")
	a.w.WriteHeader(a.status)
	// #nosec G705 -- dialect JSON response body, not HTML; Content-Type is application/json
	_, _ = a.w.Write(out)
}
