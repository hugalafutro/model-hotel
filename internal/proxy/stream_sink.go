package proxy

import "net/http"

// streamSink owns the downstream SSE byte output for handleStreamingResponse:
// the http.ResponseWriter, its optional http.Flusher, and the running
// bytesWritten total. It was extracted in Phase 1 of the streaming-pipeline
// refactor (see plans/refactor-streaming-pipeline.md) so every emit path
// funnels through one type instead of repeating the
// w.Write / bytesWritten += n / flusher.Flush() triplet inline.
//
// Behavior is identical to the prior inline code: write() accounts the bytes
// actually written and returns the write error; flush() flushes only when the
// writer supports it. The blank-line separator rule itself is NOT centralised
// here yet — that is Phase 5. Callers still decide which bytes to emit and
// when to flush, so the existing flush points and goto-on-error paths are
// preserved exactly.
type streamSink struct {
	w            http.ResponseWriter
	flusher      http.Flusher
	canFlush     bool
	bytesWritten int64

	// swallowBlank records that the previous line was a data line, so the next
	// upstream blank separator should be dropped rather than forwarded (Phase 5
	// — the old skipNextEmptyLine flag, relocated here). writeData sets it
	// automatically; the dropped-chunk paths (invalid-JSON skip, finish-suppress)
	// and the raw plain-forward set it explicitly. The blank handler consumes it.
	swallowBlank bool
}

// newStreamSink wraps w, detecting http.Flusher support once up front.
func newStreamSink(w http.ResponseWriter) *streamSink {
	flusher, canFlush := w.(http.Flusher)
	return &streamSink{w: w, flusher: flusher, canFlush: canFlush}
}

// write writes p to the client and adds the bytes actually written to the
// running total (even on a short write), returning any write error. It does
// NOT flush — callers flush explicitly so the existing flush points stay
// byte-for-byte where they were.
func (s *streamSink) write(p []byte) error {
	n, err := s.w.Write(p)
	s.bytesWritten += int64(n)
	return err
}

// writeData emits a full "data: <payload>\n\n" SSE event (no flush),
// delegating to writeSSEDataChunk for byte-identical framing and accounting.
// It marks the next upstream blank for swallowing — every data emit owns its
// own separator, so the upstream's trailing blank is redundant.
func (s *streamSink) writeData(payload []byte) error {
	err := writeSSEDataChunk(s.w, payload, &s.bytesWritten)
	s.swallowBlank = true
	return err
}

// flush flushes the underlying writer when it supports flushing.
func (s *streamSink) flush() {
	if s.canFlush {
		s.flusher.Flush()
	}
}
