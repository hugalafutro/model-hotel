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

	// Progressive stall timeout (progressiveChunkThreshold /
	// progressiveStallMultiplier, package consts): after this many chunks the
	// stream is clearly alive — extend the watchdog timeout to tolerate
	// tool-call pauses and long reasoning chains.

	defer func() {
		// Drain remaining bytes so the Transport reuses the connection.
		// Skip drain if the client already disconnected: the upstream body
		// could be large and we'd block the goroutine for no benefit.
		if r.Context().Err() == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
		}
		_ = resp.Body.Close()
	}()
	debuglog.Debug("proxy: handleStreamingResponse entered", "model", logData.modelID, "provider", logData.providerName, "upstream_status", resp.StatusCode, "attempt", opts.attempt, "response_header_ms", opts.responseHeaderMs, "true_ttft_ms", opts.trueTtftMs, "has_probe_buf", opts.preReadBuf != nil)

	h.initStreamResponse(w, logData, opts, resp)

	// streamSink owns w/flusher and the running bytesWritten total (Phase 1
	// of the streaming-pipeline refactor). All emit paths go through it.
	sink := newStreamSink(w)

	// streamReader owns the scanner (replaying the TTFT probe buffer), the
	// stall watchdog, chunk counting, the empty-line limit, client-disconnect
	// detection, BOM/CR cleanup, and SSE classification (Phase 3). It yields
	// classified sseEvents; this orchestrator owns emits and transforms.
	reader := newStreamReader(r.Context(), resp.Body, opts, logData, h.shutdown)

	// st accumulates the per-stream metrics, carry flags, and observer state
	// (Phase 4 §6 migration). Created before the loop and mutated in place so
	// the transforms/observers and the finalizer share one named contract
	// instead of a fistful of loop-locals. The stall flag and final chunkCount
	// are filled from the reader at logUpdate.
	st := &streamState{masker: opts.masker}
	// Periodic streaming progress logging (every 50 chunks) to give
	// visibility into stream health without flooding logs.
	const chunkLogInterval = 50
	// Read strip_reasoning flag from context once before the scanner loop.
	// The value is set by ProxyKeyMiddleware and never changes mid-stream.
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
			// Reader stopped: disconnect (skip the stream-end error flush,
			// matching the prior goto), empty-line abort, or normal EOF.
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

		// Periodic streaming progress log for observability.
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

	// Flush any remaining accumulated error bytes at stream end.
	st.flushAccumulatedError("proxy: accumulated SSE error (stream end)", reader.chunkCount, logData)

logUpdate:
	// Stop the watchdog before reading its stall flag, matching the prior
	// inline ordering (close, then read the atomic).
	reader.Close()
	// st was accumulated in place by the loop; fill the reader-owned fields the
	// loop couldn't (final chunk count and the stall flag, read after watchdog
	// teardown), then finalize.
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
	// Fire-and-forget: the interim "streaming" state update runs before
	// the first streamed byte. Blocking on WaitForInsert (up to 5s) would
	// delay the client. The final update (completed/failed) waits properly.
	h.updateRequestLog(logData, updateLogOption{skipWaitForInsert: true})
	// The provider is now committed and the row carries it; tell the dashboard
	// so a live row can swap its "Resolving" placeholder for the real
	// provider/model while the (possibly reasoning-only) stream is still in
	// flight, instead of waiting for the terminal request.completed event.
	publishRequestStreamingEvent(logData)
}

// emitBlank forwards or swallows a blank SSE separator line. When the preceding
// data line was stripped (sink.swallowBlank), the trailing separator is
// suppressed so downstream parsers don't see a bare "\n" event. Returns
// stop=true on a client write failure (the caller jumps to finalize).
func (h *Handler) emitBlank(sink *streamSink, st *streamState, chunkCount int, logData *requestLogData) (stop bool) {
	// When strip_reasoning skips a reasoning chunk, the SSE separator (empty
	// line) that followed it must also be suppressed. Bare \n events break
	// parsers like openai-go's ssestream (Warp's backend). Only forward the
	// separator when the preceding data line was actually forwarded.
	if sink.swallowBlank {
		sink.swallowBlank = false
		return false
	}
	// Forward empty lines — they are SSE event separators required by the spec.
	// Clients like eventsource-parser dispatch events on blank lines; omitting
	// them causes all data lines to be concatenated into one invalid event.
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
	// Not a data line — an SSE comment (": ..."), an event/id/retry directive,
	// etc. Pass through without parsing.
	lineStr := ev.clean
	// P1-C: Detect Anthropic-style "event: error" lines for logging. Anthropic
	// streams use typed events like:
	//   event: error
	//   data: {"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}
	// We track "event: error" so the next data line is known to be an error
	// payload, allowing us to extract the message for logging.
	if strings.HasPrefix(lineStr, "event:") {
		evt := strings.TrimSpace(lineStr[6:])
		if evt == "error" {
			st.lastAnthropicEvent = "error"
		} else {
			st.lastAnthropicEvent = ""
		}
	}
	// Flush any accumulated error when a non-data line arrives
	// (the error payload has already been captured in the data line).
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

// emitDone forwards the [DONE] sentinel to the client. It sets st.sawDone before
// the write — verbatim ordering — so a disconnect mid-[DONE] still leaves sawDone
// set for finalizeStream's missing-[DONE] decision. Returns stop=true on a client
// write failure; on success the caller breaks the loop into finalize.
func (h *Handler) emitDone(sink *streamSink, st *streamState, ev sseEvent, chunkCount int, logData *requestLogData) (stop bool) {
	line := ev.raw
	st.sawDone = true
	// Write [DONE] sentinel to the downstream client.
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

// applyReasoningNormalize is the reasoning field normalization transform:
// ensure reasoning_content is populated regardless of upstream format (Ollama
// reasoning, OpenRouter/MiniMax reasoning_details, <thinking> tags in
// content). Emits the rewritten chunk itself; wrote reports that emit and
// stop=true a client write failure.
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
// reasoning-only deltas. Emits the rewritten chunk itself; wrote reports that
// emit and stop=true a client write failure.
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

	// Capture split (P1-B) and Anthropic typed (P1-C) SSE errors into streamState.
	// anthropicErrorCounted prevents the chunk.Error observer from double-counting.
	anthropicErrorCounted := st.captureSSEError(payload, &st.lastAnthropicEvent, chunkCount, logData)

	var written bool
	var chunk streamChunk
	// A shape this gateway does not model is not broken bytes. encoding/json
	// checks the whole document for well-formedness BEFORE it decodes any of it,
	// so an UnmarshalTypeError is proof the frame is valid JSON — and the decoder
	// records that error and carries on with the siblings, so chunk holds every
	// member that did fit.
	//
	// Read as "were these even JSON", one unmodelled sibling deleted the frame:
	// a relay numbering its stop reasons, or content as the array of parts the
	// OpenAI schema now permits, and the caller lost the model's output while the
	// observers, the masking and the error member all went unread. The frame is
	// the provider's answer whether or not this gateway has a struct for it.
	//
	// What it cannot go through are the transforms that rebuild the frame from
	// that struct: the member that failed is missing from it, so rebuilding a
	// content-as-parts frame would re-emit it with an empty delta, which is the
	// same loss by another route. It is observed, masked, and forwarded verbatim,
	// stopping short of strip_reasoning, the reasoning/empty-content transforms
	// and finish_reason normalisation. Tool-argument normalisation is deliberately
	// NOT skipped — it works over the payload as a map of raw members rather than
	// the struct, so it rewrites the one member it understands and leaves the rest
	// of the frame exactly as it arrived.
	raw := []byte(payload)
	decodeErr := json.Unmarshal(raw, &chunk)
	typeErr := shapeError(raw, decodeErr)
	untypeable := typeErr != nil
	jsonValid := decodeErr == nil || untypeable
	if jsonValid {
		// Side-channel observers (usage, native_finish_reason, repeated content,
		// chunk.Error) run for EVERY valid chunk — BEFORE the transforms, which may
		// emit-and-return early (strip_reasoning keep-alive/forward). Running them
		// here is what keeps usage/token metering from being silently dropped when a
		// provider rides `usage` on the same chunk as a reasoning delta. They read
		// the immutable typed chunk and never emit, so position doesn't affect output.
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
		// util.ErrorMemberCarries, not the member's mere presence. The regex runs
		// over the WHOLE frame, and it can match prose — so on a frame that is
		// not really an error it rewrites the model's answer. "error":null
		// alongside a delta is an ordinary per-frame shape for several
		// relays, and gating on presence redacted a key-shaped token out of
		// the content those frames carry: an assistant explaining an AIza… or
		// a Bearer header had its answer altered mid-stream. That is a worse
		// failure than missing the third masking layer on a frame whose error
		// member is empty, where the credential could only be in the content
		// the regex must not touch anyway.
		if util.ErrorMemberCarries(chunk.Error) {
			masked = maskKeyShapedTokens(masked)
		}
		if string(masked) != payload {
			payload = string(masked)
			line = append([]byte("data: "), masked...)
			chunk = streamChunk{}
			_ = json.Unmarshal(masked, &chunk)
		}

		// Rewrite an object-form tool call into the spec's JSON string before
		// it leaves the gateway. Accepting the object on the way IN is what
		// stops the frame being dropped; forwarding it on the way OUT would
		// hand the caller a shape this gateway's own request translators
		// (anthropicegress, gemini, openairesponses) cannot read back — and the
		// caller echoes the assistant turn into the next request. In a failover
		// group whose next turn lands on an Anthropic or Gemini member, that
		// request 400s, and keeps 400ing for the life of the conversation.
		if normalized, changed := normalizeToolArguments(payload); changed {
			payload = normalized
			line = append([]byte("data: "), normalized...)
			chunk = streamChunk{}
			_ = json.Unmarshal([]byte(normalized), &chunk)
		}

		if untypeable {
			untypedDelta, _ := parseChunkPayload(payload)
			if stripReasoning && deltaCarriesReasoning(untypedDelta.delta) {
				// strip_reasoning is a promise to the caller, and forwarding a
				// frame this gateway cannot read is no way to keep it.
				// computeStripReasoning works over the payload and would run — but
				// its "does this delta still carry anything" verdict reads content
				// as a plain string, so a content-as-parts delta looks empty to it
				// and becomes a keep-alive, taking the answer with it.
				//
				// Only a frame that actually carries reasoning is dropped. Gating
				// on untypeable alone deleted the answer out of an ordinary
				// content delta whose finish_reason happened to be a number, for
				// no gain: there was nothing there to strip.
				st.unparsedChunks++
				debuglog.Warn("proxy: dropping a chunk shape this gateway cannot strip reasoning from",
					"model", logData.modelID, "provider", logData.providerName,
					"chunk_number", chunkCount, "json_field", util.SanitizeLogBody(typeErr.Field, 200),
					"json_got", jsonShapeName(typeErr.Value), "payload_bytes", len(payload))
				sink.swallowBlank = true
				return false
			}
			st.deliveredBytes += untypedDeltaBytes(untypedDelta.delta)
			// Counted like any frame this gateway could not read: the end-of-stream
			// verdict must not conclude the provider sent nothing on the strength
			// of frames it could not see into (judgeStreamForBreaker reads
			// delivery first, so a stream that plainly answered is still
			// credited). Calling a forwarded frame delivery in its own right was
			// worse: the terminal chunk is exactly where a relay numbers its stop
			// reason, and that frame carries no output at all.
			//
			// The mismatch, not the payload: which member, and what shape
			// arrived. "choices.0.finish_reason arrived as a number" is the whole
			// diagnosis and the part an operator can act on; the frame itself is
			// the provider's, and the commonest reason one lands here is that the
			// model's own output was written in a shape this gateway has no struct
			// for — so the payload does not go in the log.
			//
			// Field is bounded even though streamChunk has no member that could
			// make it long today: encoding/json writes a MAP KEY into it
			// verbatim, so the day a map is added to that struct is the day this
			// line starts carrying provider text, and the bound is cheaper than
			// remembering.
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
				// Payload didn't parse — leave it for the later blocks.
				goto stripReasoningDone
			case stripDrop:
				// Keep-alive marshal failed (practically unreachable) — drop.
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
			// Only emit if an earlier transform hasn't already written.
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
		// Drop JSON that is not well-formed instead of forwarding broken bytes.
		// A frame this gateway merely has no struct for does NOT come here — it
		// is valid JSON, and it is forwarded verbatim below.
		//
		// The size, not the bytes. This used to log an 80-rune preview of the
		// payload, and the commonest reason a frame fails to parse is that the
		// stream was cut mid-delta — so the preview was the model's answer,
		// written into the app log, the live viewer and the OTLP export. The
		// length is what an operator can act on ("frames are arriving
		// truncated"); the content is the provider's to keep.
		//
		// Counted so the end-of-stream verdict knows its view of what was
		// delivered is incomplete, and does not charge the provider for an
		// emptiness it cannot actually vouch for.
		st.unparsedChunks++
		debuglog.Warn("proxy: skipping invalid JSON chunk from upstream",
			"model", logData.modelID, "provider", logData.providerName,
			"chunk_number", chunkCount, "payload_bytes", len(payload))
		sink.swallowBlank = true
		return false
	}
	if !written {
		// No transform applied — forward the original line verbatim (preserves
		// upstream framing like LM Studio's no-space "data:").
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

// deltaCarriesReasoning reports whether a delta holds any of the three spellings
// of reasoning this gateway knows about.
func deltaCarriesReasoning(delta map[string]json.RawMessage) bool {
	for _, key := range []string{"reasoning_content", "reasoning_details", "reasoning"} {
		if raw, ok := delta[key]; ok && len(raw) > 0 && string(raw) != "null" {
			return true
		}
	}
	return false
}

// untypedDeltaBytes approximates what the model produced in a delta this gateway
// could not type, for the usage estimate that runs when a provider reports no
// usage at all.
//
// The raw JSON length of the delta's members, which is what deliveredBytes
// already counts for reasoning_details — the one other member it holds without
// being able to break down. It overstates: the quoting and the part wrappers
// around a content array are not text the model produced. Zero understates far
// worse, and zero is what this was: a provider sending content as parts and no
// usage chunk delivered a full answer and was metered nothing at all, so the
// caller's quota and TPM bucket were debited for none of it. Reading the text
// out of a part array properly belongs with the rest of content-as-parts.
func untypedDeltaBytes(delta map[string]json.RawMessage) int {
	total := 0
	for key, raw := range delta {
		// role and refusal markers are not output.
		if key == "role" {
			continue
		}
		total += len(raw)
	}
	return total
}

// jsonShapeName reduces an UnmarshalTypeError's Value to the shape alone.
//
// encoding/json writes a number's LITERAL into that field, but only when the
// number lands on a NUMERIC field: {"content":8675309} into a *string reports
// "number", while {"n":3.5} into an int reports "number 3.5". No integer field
// under streamChunk is reachable from here — the only ones live inside Usage,
// whose own decoder keeps what it can read rather than returning a type error —
// so today the value is always a bare shape word.
//
// It is kept anyway, and cheaply: the shape is the whole diagnosis, the literal
// is provider data, and one integer field added to that struct is all it would
// take for this line to start carrying it. TestJSONShapeName is what holds it.
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
	// Cheap reject first. Without it every frame of every stream paid for three
	// json.Unmarshal calls (~5us and ~1.8KB of garbage each) to discover it has
	// no tool calls — several megabytes of garbage over a long stream, for
	// nothing. This is also what makes "an ordinary frame is neither reparsed
	// nor re-emitted" true rather than aspirational.
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
	// Unmarshal, so each RawMessage holds valid JSON. The errors are discarded
	// rather than handled, because a branch that cannot be taken is a branch
	// that cannot be tested.
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
