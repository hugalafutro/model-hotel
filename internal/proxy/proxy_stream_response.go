package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/hugalafutro/model-hotel/internal/ctxkeys"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

func (h *Handler) handleStreamingResponse(w http.ResponseWriter, r *http.Request, logData *requestLogData, resp *http.Response, startTime time.Time, opts streamOptions) {

	// Progressive stall timeout (progressiveChunkThreshold and
	// progressiveStallMultiplier): past that many chunks the stream is clearly
	// alive, so the watchdog timeout extends to tolerate tool-call pauses and
	// long reasoning chains.

	defer func() {
		// Drain remaining bytes so the Transport reuses the connection, unless
		// the client already disconnected: the upstream body may be large and
		// draining it would block the goroutine for no benefit.
		if r.Context().Err() == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
		}
		_ = resp.Body.Close()
	}()
	debuglog.Debug("proxy: handleStreamingResponse entered", "model", logData.modelID, "provider", logData.providerName, "upstream_status", resp.StatusCode, "attempt", opts.attempt, "response_header_ms", opts.responseHeaderMs, "true_ttft_ms", opts.trueTtftMs, "has_probe_buf", opts.preReadBuf != nil)

	h.initStreamResponse(w, logData, opts, resp)

	// streamSink owns w, the flusher and the running bytesWritten total. All
	// emit paths go through it.
	sink := newStreamSink(w)

	// streamReader owns the scanner (replaying the TTFT probe buffer), the stall
	// watchdog, chunk counting, the empty-line limit, client-disconnect
	// detection, BOM/CR cleanup, and SSE classification. It yields classified
	// sseEvents; this orchestrator owns emits and transforms.
	reader := newStreamReader(r.Context(), resp.Body, opts, logData, h.shutdown)

	// st accumulates the per-stream metrics, carry flags and observer state. It
	// is created before the loop and mutated in place so the transforms,
	// observers and finalizer share one named contract instead of a fistful of
	// loop-locals. The stall flag and final chunkCount are filled from the
	// reader at logUpdate.
	st := &streamState{masker: opts.masker, content: logData.content}
	// Periodic streaming progress logging, for visibility into stream health
	// without flooding the logs.
	const chunkLogInterval = 50
	// The strip_reasoning flag is read from the context once before the scanner
	// loop: ProxyKeyMiddleware sets it and it never changes mid-stream.
	stripReasoning := false
	if v := r.Context().Value(ctxkeys.VirtualKeyStripReasoningKey); v != nil {
		if sr, ok := v.(bool); ok {
			stripReasoning = sr
		}
	}
	debuglog.Debug("proxy: strip_reasoning flag", "enabled", stripReasoning, "model", logData.modelID, "provider", logData.providerName)

	for {
		ev, ok := reader.Next()
		if !ok {
			// Reader stopped: a disconnect (which skips the stream-end error
			// flush), an empty-line abort, or a normal EOF.
			if reader.disconnected {
				st.clientDisconnected = true
				goto logUpdate
			}
			if reader.abortErrMsg != "" {
				st.lastErrMsg = reader.abortErrMsg
			}
			break
		}
		chunkCount := reader.chunkCount

		// Periodic streaming progress log.
		if chunkCount%chunkLogInterval == 0 {
			debuglog.Debug("proxy: streaming progress", "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", sink.bytesWritten, "prompt_tokens", st.promptTokens, "completion_tokens", st.completionTokens, "thinking", st.sawThinking)
		}

		if ev.kind == sseBlank {
			if h.emitBlank(sink, st, chunkCount, logData) {
				goto logUpdate
			}
			continue
		}

		if ev.kind == sseComment {
			if h.emitComment(sink, st, ev, chunkCount, logData) {
				goto logUpdate
			}
			continue
		}

		if ev.kind == sseDone {
			if h.emitDone(sink, st, ev, chunkCount, logData) {
				goto logUpdate
			}
			break
		}

		// ev.kind == sseData. Native Anthropic passthrough forwards the chunk
		// verbatim (it is already Anthropic-shaped); the translated/OpenAI path
		// parses, transforms, observes, and forwards.
		if opts.rawPassthrough {
			if h.emitRawData(sink, st, ev, chunkCount, logData) {
				goto logUpdate
			}
		} else if h.handleDataChunk(sink, st, ev, stripReasoning, chunkCount, logData) {
			goto logUpdate
		}
	}

	// Flush any accumulated error bytes still held at stream end.
	st.flushAccumulatedError("proxy: accumulated SSE error (stream end)", reader.chunkCount, logData)

logUpdate:
	// Stop the watchdog before reading its stall flag: close, then read the
	// atomic.
	reader.Close()
	// st was accumulated in place by the loop; fill in the reader-owned fields
	// it could not (the final chunk count and the stall flag, read after
	// watchdog teardown), then finalize.
	st.chunkCount = reader.chunkCount
	st.stalled = reader.stalled()
	st.interrupted = reader.interrupted()
	h.finalizeStream(st, sink, reader.err(), logData, opts, resp.StatusCode, startTime)
}

// initStreamResponse writes the SSE response headers and populates the interim
// "streaming" logData (timings + status/attempt), then fires the fire-and-forget
// interim updateRequestLog. Headers are written before any streamed byte, and
// the interim log update must NOT wait for the async INSERT (blocking on
// WaitForInsert would delay the client); the final update waits properly.
func (h *Handler) initStreamResponse(w http.ResponseWriter, logData *requestLogData, opts streamOptions, resp *http.Response) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	// 200 on the wire even when the upstream answered another 2xx, unlike the
	// non-streaming half, which passes the provider's status through. This is
	// the gateway's own SSE stream and EventSource treats any non-200 as a
	// connection failure, so forwarding a 201 would break browser clients for a
	// status they never needed to see. The upstream's real status goes on the
	// row below.
	w.WriteHeader(http.StatusOK)
	debuglog.Debug("proxy: streaming headers sent", "model", logData.modelID, "provider", logData.providerName)

	logData.statusCode = resp.StatusCode
	logData.proxyOverheadMs = opts.proxyOverheadMs
	logData.parseMs = opts.parseMs
	logData.failoverLookupMs = opts.failoverLookupMs
	logData.modelLookupMs = opts.modelLookupMs
	logData.providerLookupMs = opts.providerLookupMs
	logData.keyDecryptMs = opts.keyDecryptMs
	logData.dialMs = opts.dialMs
	logData.settingsReadMs = opts.settingsReadMs
	logData.responseHeaderMs = opts.responseHeaderMs
	logData.ttftMs = opts.trueTtftMs
	logData.failoverAttempt = opts.attempt
	logData.state = "streaming"
	// Fire-and-forget: the interim "streaming" state update runs before the
	// first streamed byte, where blocking on WaitForInsert (up to 5s) would
	// delay the client. The final update waits properly.
	h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})
	// The provider is committed and the row carries it, so the dashboard is told
	// and a live row can swap its "Resolving" placeholder for the real provider
	// and model while the stream is still in flight, instead of waiting for the
	// terminal request.completed event.
	publishRequestStreamingEvent(logData)
}

// emitBlank forwards or swallows a blank SSE separator line. When the preceding
// data line was stripped (sink.swallowBlank), the trailing separator is
// suppressed so downstream parsers don't see a bare "\n" event. Returns
// stop=true on a client write failure (the caller jumps to finalize).
func (h *Handler) emitBlank(sink *streamSink, st *streamState, chunkCount int, logData *requestLogData) (stop bool) {
	// When strip_reasoning skips a reasoning chunk, the SSE separator that
	// followed it must be suppressed too: a bare \n event breaks parsers such as
	// openai-go's ssestream. The separator is forwarded only when the preceding
	// data line was.
	if sink.swallowBlank {
		sink.swallowBlank = false
		return false
	}
	// Empty lines are the SSE event separators the spec requires. Clients such
	// as eventsource-parser dispatch events on blank lines, so omitting them
	// concatenates every data line into one invalid event.
	if err := sink.write([]byte("\n")); err != nil {
		st.clientDisconnected = true
		debuglog.Warn("proxy: client write failed during stream (blank line)", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", sink.bytesWritten)
		return true
	}
	sink.flush()
	return false
}

// emitComment passes through a non-data SSE line (comment, event/id/retry
// directive) verbatim, capturing the Anthropic-style "event: error" marker so
// the next data line is known to be an error payload, and flushing any
// accumulated split-error first. Returns stop=true on a client write failure.
func (h *Handler) emitComment(sink *streamSink, st *streamState, ev sseEvent, chunkCount int, logData *requestLogData) (stop bool) {
	line := ev.raw
	// Not a data line: an SSE comment (": ..."), or an event/id/retry directive.
	// It passes through without parsing.
	lineStr := ev.clean
	// P1-C: Anthropic streams use typed events, as in
	//   event: error
	//   data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}
	// so "event: error" is tracked and the next data line is known to be an
	// error payload whose message can be extracted for the log.
	if strings.HasPrefix(lineStr, "event:") {
		evt := strings.TrimSpace(lineStr[6:])
		if evt == "error" {
			st.lastAnthropicEvent = "error"
		} else {
			st.lastAnthropicEvent = ""
		}
	}
	// Flush any accumulated error when a non-data line arrives: the error
	// payload was already captured on the data line.
	st.flushAccumulatedError("proxy: accumulated SSE error", chunkCount, logData)
	if err := sink.write(line); err != nil {
		st.clientDisconnected = true
		debuglog.Warn("proxy: client write failed during stream", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", sink.bytesWritten)
		return true
	}
	if err := sink.write([]byte("\n")); err != nil {
		st.clientDisconnected = true
		debuglog.Warn("proxy: client write failed during stream (newline)", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", sink.bytesWritten)
		return true
	}
	sink.flush()
	return false
}

// emitDone forwards the [DONE] sentinel to the client. st.sawDone is set before
// the write, so a disconnect mid-[DONE] still leaves it set for finalizeStream's
// missing-[DONE] decision. Returns stop=true on a client write failure; on
// success the caller breaks the loop into finalize.
func (h *Handler) emitDone(sink *streamSink, st *streamState, ev sseEvent, chunkCount int, logData *requestLogData) (stop bool) {
	line := ev.raw
	st.sawDone = true
	if err := sink.write(line); err != nil {
		st.clientDisconnected = true
		debuglog.Warn("proxy: client write failed during stream", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", sink.bytesWritten)
		return true
	}
	if err := sink.write([]byte("\n\n")); err != nil {
		st.clientDisconnected = true
		debuglog.Warn("proxy: client write failed during stream (newline)", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", sink.bytesWritten)
		return true
	}
	sink.flush()
	debuglog.Debug("proxy: received [DONE] sentinel", "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
	return false
}

// emitData writes payload as an SSE data event and flushes it, returning true
// on success. On a write failure it records the client disconnect on st and
// logs it; transform names which pipeline step was emitting, for the log line.
// Callers must stop the stream (return stop=true) when this returns false.
func (st *streamState) emitData(sink *streamSink, payload []byte, transform string, chunkCount int, logData *requestLogData) bool {
	if err := sink.writeData(payload); err != nil {
		st.clientDisconnected = true
		debuglog.Warn("proxy: client write failed during "+transform, "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount)
		return false
	}
	sink.flush()
	return true
}

// applyReasoningNormalize is the reasoning-field normalization transform: it
// ensures reasoning_content is populated whatever the upstream format (Ollama
// reasoning, OpenRouter/MiniMax reasoning_details, <thinking> tags in content).
// It emits the rewritten chunk itself; wrote reports that emit, and stop=true a
// client write failure.
func (st *streamState) applyReasoningNormalize(sink *streamSink, chunk streamChunk, payload string, chunkCount int, logData *requestLogData) (wrote, stop bool) {
	if len(chunk.Choices) == 0 || chunk.Choices[0].Delta == nil {
		return false, false
	}
	delta := chunk.Choices[0].Delta
	newPayload, ok := normalizeReasoningChunk(delta.Content, delta.ReasoningContent, payload, &st.lastFinishReason, logData)
	if !ok {
		return false, false
	}
	if !st.emitData(sink, newPayload, "reasoning normalization", chunkCount, logData) {
		return false, true
	}
	debuglog.Debug("proxy: normalized reasoning fields", "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
	return true, false
}

// applyEmptyContentStrip strips the noise content:"" that accompanies
// reasoning-only deltas. It emits the rewritten chunk itself; wrote reports that
// emit, and stop=true a client write failure.
func (st *streamState) applyEmptyContentStrip(sink *streamSink, chunk streamChunk, payload string, chunkCount int, logData *requestLogData) (wrote, stop bool) {
	if len(chunk.Choices) == 0 || chunk.Choices[0].Delta == nil {
		return false, false
	}
	delta := chunk.Choices[0].Delta
	hasReasoning := delta.ReasoningContent != nil && *delta.ReasoningContent != ""
	hasEmptyContent := delta.Content != nil && *delta.Content == ""
	if !hasReasoning || !hasEmptyContent {
		return false, false
	}
	newPayload, ok := stripEmptyReasoningContent(payload, &st.lastFinishReason, logData)
	if !ok {
		return false, false
	}
	if !st.emitData(sink, newPayload, "empty content strip", chunkCount, logData) {
		return false, true
	}
	debuglog.Debug("proxy: stripped empty content from reasoning chunk", "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
	return true, false
}

// handleDataChunk processes one sseData event end-to-end: capture split/Anthropic
// SSE errors (P1-B/P1-C), parse the chunk, run the transforms (strip_reasoning,
// reasoning-normalize, empty-content-strip, finish_reason) and the side-channel
// observers, then forward. It emits at most once; the `written` flag and the
// whole transform dispatch are encapsulated here. Returns stop=true when a client
// write failed (the caller jumps to finalize), false otherwise (advance to the
// next event).
func (h *Handler) handleDataChunk(sink *streamSink, st *streamState, ev sseEvent, stripReasoning bool, chunkCount int, logData *requestLogData) (stop bool) {
	payload := ev.payload
	line := ev.raw

	// Capture truncated (P1-B) and Anthropic typed (P1-C) SSE errors into
	// streamState. anthropicErrorCounted keeps the chunk.Error observer from
	// double-counting.
	anthropicErrorCounted := st.captureSSEError(payload, &st.lastAnthropicEvent, chunkCount, logData)

	var written bool
	var chunk streamChunk
	// A shape this gateway does not model is not broken bytes. encoding/json
	// checks the whole document for well-formedness BEFORE decoding any of it,
	// so an UnmarshalTypeError is proof the frame is valid JSON, and the decoder
	// records that error and carries on with the siblings, so chunk holds every
	// member that did fit.
	//
	// Read as "were these even JSON", one unmodelled sibling deletes the frame:
	// a relay numbering its stop reasons, or content as the array of parts the
	// OpenAI schema permits, and the caller loses the model's output while the
	// observers, the masking and the error member all go unread. The frame is
	// the provider's answer whether or not this gateway has a struct for it.
	//
	// What it cannot go through are the transforms that rebuild the frame from
	// that struct: the member that failed is missing from it, so rebuilding a
	// content-as-parts frame would re-emit it with an empty delta, the same loss
	// by another route. It is observed, masked and forwarded verbatim, stopping
	// short of strip_reasoning, the reasoning and empty-content transforms and
	// finish_reason normalisation. Tool-argument normalisation is NOT skipped: it
	// works over the payload as a map of raw members rather than the struct, so
	// it rewrites the one member it understands and leaves the rest of the frame
	// as it arrived.
	raw := []byte(payload)
	decodeErr := json.Unmarshal(raw, &chunk)
	typeErr := util.ShapeError(raw, decodeErr)
	untypeable := typeErr != nil
	jsonValid := decodeErr == nil || untypeable
	if jsonValid {
		// Side-channel observers (usage, native_finish_reason, repeated content,
		// chunk.Error) run for EVERY valid chunk, BEFORE the transforms, which
		// may emit and return early (a strip_reasoning keep-alive or forward).
		// Running them here keeps token metering from being dropped when a
		// provider rides `usage` on the same chunk as a reasoning delta. They
		// read the immutable typed chunk and never emit, so their position does
		// not affect output.
		deliveredBefore := st.deliveredBytes
		st.observeDataChunk(chunk, anthropicErrorCounted, chunkCount, logData)

		// Mask the provider's credential before any emit. Every chunk gets the
		// exact-key pass (a gateway may quote the key in a frame of any shape,
		// and an HTTP 200 stream never passes through forwardUpstreamError's
		// masking); an error frame additionally gets the key-shape regex. It
		// sits here, ahead of the transforms, because every transform
		// re-marshals the whole top-level object (an error frame that also
		// carries a delta or a mappable finish_reason is rewritten and emitted
		// with "error" intact). The observer above already took the message
		// for the request log, which is exact-masked at finalize. The typed
		// chunk is re-decoded from the masked bytes because reasoning
		// normalization rebuilds the delta from it, not from payload; a stale
		// chunk would hand the transform the original text to re-emit.
		masked := st.masker.maskExact([]byte(payload))
		// util.ValueCarries, not the member's mere presence. The regex runs
		// over the WHOLE frame and can match prose, so on a frame that is not
		// really an error it would rewrite the model's answer. "error":null
		// alongside a delta is an ordinary per-frame shape for several relays,
		// and gating on presence redacts a key-shaped token out of the content
		// those frames carry, altering the answer of an assistant explaining an
		// AIza… or a Bearer header mid-stream. That is worse than missing the
		// third masking layer on a frame whose error member is empty, where the
		// credential could only be in content the regex must not touch anyway.
		if util.ValueCarries(chunk.Error) {
			// An error frame is error text: every held provider key and the
			// shape layer, not only the candidate's key the content pass
			// applied above (a relay's rejection quotes another row's key).
			masked = st.masker.mask(masked)
		}
		if string(masked) != payload {
			payload = string(masked)
			line = append([]byte("data: "), masked...)
			chunk = streamChunk{}
			_ = json.Unmarshal(masked, &chunk)
		}

		// Rewrite an object-form tool call into the spec's JSON string before it
		// leaves the gateway. Accepting the object on the way IN is what stops
		// the frame being dropped; rewriting it on the way OUT is what makes the
		// caller's next request spec-shaped, whoever reads it. This is
		// conformance: the caller asked this gateway for the OpenAI shape, and
		// an SDK or another gateway downstream is entitled to it.
		if normalized, changed := normalizeToolArguments(payload); changed {
			payload = normalized
			line = append([]byte("data: "), normalized...)
			chunk = streamChunk{}
			_ = json.Unmarshal([]byte(normalized), &chunk)
		}

		if untypeable {
			untypedDelta, _ := parseChunkPayload(payload)
			if stripReasoning && untypedFrameResistsStripping(untypedDelta.delta) {
				// strip_reasoning is a promise to the caller, and forwarding a
				// frame this gateway cannot read is no way to keep it.
				// computeStripReasoning would run over the payload, but its "does
				// this delta still carry anything" verdict reads content as a
				// plain string, so a content-as-parts delta looks empty to it and
				// becomes a keep-alive, taking the answer with it.
				//
				// Only a frame that might be hiding reasoning is dropped. Gating
				// on untypeable alone deletes the answer out of an ordinary
				// content delta whose finish_reason happens to be a number, and
				// gating on "content is not a plain string" deletes it out of
				// every content-as-parts frame.
				//
				// The observers ran before this branch and counted whatever
				// decoded. The caller is not receiving this frame, so it is not
				// delivery and must not be billed: roll that count back.
				st.deliveredBytes = deliveredBefore
				st.unparsedChunks++
				debuglog.Warn("proxy: dropping a chunk shape this gateway cannot strip reasoning from",
					"model", logData.modelID, "provider", logData.providerName,
					"chunk_number", chunkCount, "json_field", util.SanitizeLogBody(typeErr.Field, 200),
					"json_got", jsonShapeName(typeErr.Value), "payload_bytes", len(payload))
				sink.swallowBlank = true
				return false
			}
			// deliveredBytes is left exactly as the observers set it: this
			// gateway meters what it could READ and does not guess at the rest.
			// Every member of this frame that decoded is already counted, across
			// every choice; what is missing is the member that did not, and
			// estimating it goes wrong in both directions. The raw member length
			// bills a tool-call frame nine times over, and an empty
			// content-as-parts opener ({"content":[{"type":"text","text":""}]})
			// counts as delivery, crediting the circuit breaker and suppressing
			// the error charge so a provider failing every request could never
			// open its circuit.
			//
			// So an answer delivered ONLY in a shape this gateway cannot read is
			// unbilled when the provider also reports no usage. That is a revenue
			// gap, and the safe direction: the caller receives the content rather
			// than losing it. Reading text out of a part array belongs to the
			// content-as-parts work.
			//
			// The frame is counted like any this gateway could not read, so the
			// end-of-stream verdict does not conclude the provider sent nothing
			// on the strength of frames it could not see into
			// (judgeStreamForBreaker reads delivery first, so a stream that
			// plainly answered is still credited). Calling a forwarded frame
			// delivery in its own right is worse: the terminal chunk is where a
			// relay numbers its stop reason, and it carries no output at all.
			//
			// The log line carries the mismatch, not the payload: which member,
			// and what shape arrived. "choices.0.finish_reason arrived as a
			// number" is the whole diagnosis and the part an operator can act on,
			// while the commonest reason a frame lands here is that the model's
			// own output was written in a shape this gateway has no struct for.
			//
			// Field is bounded even though no streamChunk member could make it
			// long: encoding/json writes a MAP KEY into it verbatim, so a map
			// added to that struct would start this line carrying provider text.
			st.unparsedChunks++
			debuglog.Warn("proxy: forwarding a chunk shape this gateway does not model",
				"model", logData.modelID, "provider", logData.providerName,
				"chunk_number", chunkCount, "json_field", util.SanitizeLogBody(typeErr.Field, 200),
				"json_got", jsonShapeName(typeErr.Value), "payload_bytes", len(payload))
			goto forwardUntypeable
		}

		// strip_reasoning: drop reasoning-only deltas (keep-alive) or forward the
		// stripped chunk. See computeStripReasoning.
		if stripReasoning && len(chunk.Choices) > 0 && chunk.Choices[0].Delta != nil {
			switch decision, newPayload := computeStripReasoning(payload, &st.lastFinishReason, logData); decision {
			case stripPassthrough:
				// The payload did not parse: leave it for the later blocks.
				goto stripReasoningDone
			case stripDrop:
				// The keep-alive marshal failed, which is practically
				// unreachable: drop the chunk.
				return false
			case stripKeepalive:
				return !st.emitData(sink, newPayload, "reasoning keep-alive", chunkCount, logData)
			case stripForward:
				return !st.emitData(sink, newPayload, "reasoning strip", chunkCount, logData)
			}
		}
	stripReasoningDone:

		wrote, stopStream := st.applyReasoningNormalize(sink, chunk, payload, chunkCount, logData)
		if stopStream {
			return true
		}
		written = written || wrote

		wrote, stopStream = st.applyEmptyContentStrip(sink, chunk, payload, chunkCount, logData)
		if stopStream {
			return true
		}
		written = written || wrote

		// Normalize provider finish_reason and suppress P2-2 bare duplicates.
		switch decision, newPayload := computeFinishReason(chunk, payload, &st.lastFinishReason); decision {
		case finishSuppress:
			debuglog.Debug("proxy: suppressing duplicate finish_reason chunk", "finish_reason", normalizeFinishReason(*chunk.Choices[0].FinishReason), "model", logData.modelID, "provider", logData.providerName, "chunk_number", chunkCount)
			sink.swallowBlank = true
			return false
		case finishRewrite:
			// Emit only when an earlier transform has not already written.
			if !written {
				if !st.emitData(sink, newPayload, "stream", chunkCount, logData) {
					return true
				}
				written = true
				debuglog.Debug("proxy: normalized finish_reason", "original", *chunk.Choices[0].FinishReason, "normalized", normalizeFinishReason(*chunk.Choices[0].FinishReason), "model", logData.modelID, "provider", logData.providerName)
			}
		case finishNone:
		}
	}
forwardUntypeable:
	if !written && !jsonValid {
		// Drop what is not a frame instead of forwarding it: bytes that are not
		// well-formed JSON, and a top-level value that is not an object at all
		// (a bare number, a list, or the quoted sentinel "[DONE]"), none of
		// which a client can read as a chunk. A frame this gateway merely has no
		// struct for does NOT come here; it is valid JSON and is forwarded
		// verbatim below.
		//
		// The log line carries the size, not the bytes. The commonest reason a
		// frame fails to parse is a stream cut mid-delta, so a payload preview
		// would be the model's answer written into the app log, the live viewer
		// and the OTLP export. The length is what an operator can act on.
		//
		// Counted so the end-of-stream verdict knows its view of what was
		// delivered is incomplete, and does not charge the provider for an
		// emptiness it cannot vouch for.
		st.unparsedChunks++
		debuglog.Warn("proxy: skipping a data line that is not a chunk",
			"model", logData.modelID, "provider", logData.providerName,
			"chunk_number", chunkCount, "payload_bytes", len(payload))
		sink.swallowBlank = true
		return false
	}
	if !written {
		// No transform applied: forward the original line verbatim, which
		// preserves upstream framing such as LM Studio's no-space "data:".
		if err := sink.write(line); err != nil {
			st.clientDisconnected = true
			debuglog.Warn("proxy: client write failed during stream", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", sink.bytesWritten)
			return true
		}
		if err := sink.write([]byte("\n\n")); err != nil {
			st.clientDisconnected = true
			debuglog.Warn("proxy: client write failed during stream (newline)", "error", err, "model", logData.modelID, "provider", logData.providerName, "chunks", chunkCount, "bytes_written", sink.bytesWritten)
			return true
		}
		sink.flush()
		sink.swallowBlank = true
	}
	return false
}

// untypedFrameResistsStripping reports whether a delta this gateway could not
// type might be hiding reasoning it cannot remove, in which case the frame is
// dropped rather than forwarded to a key that asked for reasoning to be hidden.
//
// Two ways it can. A reasoning member that CARRIES something, by the same rule
// the rest of the gateway uses for an error member: gating on presence drops the
// answer out of every frame from a relay that stamps "reasoning":"" or
// "reasoning_details":[] on its non-reasoning deltas, as the OpenRouter family
// does.
//
// Or a content part this gateway cannot vouch for. Reasoning arrives inside a
// part array as {"type":"thinking",…} on exactly the Anthropic-shaped relays
// that make a frame untypeable in the first place, and computeStripReasoning
// would not catch it either, since it removes only the three named members. A
// part array of plain TEXT hides nothing, though, and rejecting every content
// that is not a string would delete the whole answer for a strip_reasoning key
// on any content-as-parts stream. So the part types are read, and an
// unrecognised one is what makes a frame unstrippable.
func untypedFrameResistsStripping(delta map[string]json.RawMessage) bool {
	for _, key := range []string{"reasoning_content", "reasoning_details", "reasoning"} {
		if util.ValueCarries(delta[key]) {
			return true
		}
	}
	raw, ok := delta["content"]
	if !ok {
		return false
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return false
	}
	var parts []struct {
		Type string `json:"type"`
	}
	if json.Unmarshal(raw, &parts) != nil {
		// A content shape that is neither a string nor a list of parts, so
		// nothing here can say what is in it.
		return true
	}
	for _, part := range parts {
		switch part.Type {
		case "text", "output_text", "input_text":
		default:
			// Including a part with no type at all: an unnamed part is not one
			// this gateway can promise carries no reasoning.
			return true
		}
	}
	return false
}

// jsonShapeName reduces an UnmarshalTypeError's Value to the shape alone.
//
// encoding/json writes a number's LITERAL into that field, but only when the
// number lands on a NUMERIC field: {"content":8675309} into a *string reports
// "number", while {"n":3.5} into an int reports "number 3.5". No integer field
// under streamChunk is reachable from here, the only ones living inside Usage,
// whose own decoder keeps what it can read rather than returning a type error,
// so the value is always a bare shape word.
//
// The cut is kept anyway and is cheap: the shape is the whole diagnosis, the
// literal is provider data, and one integer field added to that struct is all it
// would take for this line to start carrying it.
func jsonShapeName(v string) string {
	shape, _, _ := strings.Cut(v, " ")
	return shape
}

// normalizeToolArguments rewrites any tool-call arguments the provider sent as
// an object (or array, or number) into the spec's JSON string, and reports
// whether anything changed. The spec form is left untouched, so an ordinary
// frame is neither reparsed nor re-emitted.
//
// A JSON null counts as already-fine and is forwarded as-is: every request
// decoder reads null into a string without complaint, and rewriting it to ""
// would invent an empty-arguments call the provider did not send.
//
// It works on the raw map rather than the typed chunk because the frame is
// forwarded as bytes: rebuilding it from streamChunk would drop every field
// this gateway does not model.
func normalizeToolArguments(payload string) (string, bool) {
	// Cheap reject first. Without it every frame of every stream pays for three
	// json.Unmarshal calls (~5us and ~1.8KB of garbage each) to discover it has
	// no tool calls, which is several megabytes of garbage over a long stream.
	// It is also what makes "an ordinary frame is neither reparsed nor
	// re-emitted" true rather than aspirational.
	if !strings.Contains(payload, "tool_calls") {
		return payload, false
	}
	var root map[string]json.RawMessage
	if json.Unmarshal([]byte(payload), &root) != nil {
		return payload, false
	}
	var choices []map[string]json.RawMessage
	if json.Unmarshal(root["choices"], &choices) != nil {
		return payload, false
	}
	// Re-marshalling below cannot fail: every value came out of a successful
	// Unmarshal, so each RawMessage holds valid JSON; the errors are discarded.
	changed := false
	for _, choice := range choices {
		var delta map[string]json.RawMessage
		if json.Unmarshal(choice["delta"], &delta) != nil {
			continue
		}
		var calls []map[string]json.RawMessage
		if json.Unmarshal(delta["tool_calls"], &calls) != nil {
			continue
		}
		callsChanged := false
		for _, call := range calls {
			var fn map[string]json.RawMessage
			if json.Unmarshal(call["function"], &fn) != nil {
				continue
			}
			args, ok := fn["arguments"]
			if !ok {
				continue
			}
			var asString string
			if json.Unmarshal(args, &asString) == nil {
				continue // already the spec form
			}
			// Compact first, so a pretty-printed object does not carry its
			// whitespace into the string the caller stores and replays.
			var buf bytes.Buffer
			_ = json.Compact(&buf, args)
			quoted, _ := json.Marshal(buf.String())
			fn["arguments"] = quoted
			rebuilt, _ := json.Marshal(fn)
			call["function"] = rebuilt
			callsChanged = true
		}
		if !callsChanged {
			continue
		}
		rebuiltCalls, _ := json.Marshal(calls)
		delta["tool_calls"] = rebuiltCalls
		rebuiltDelta, _ := json.Marshal(delta)
		choice["delta"] = rebuiltDelta
		changed = true
	}
	if !changed {
		return payload, false
	}
	rebuiltChoices, _ := json.Marshal(choices)
	root["choices"] = rebuiltChoices
	out, _ := json.Marshal(root)
	return string(out), true
}
