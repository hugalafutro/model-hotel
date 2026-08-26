package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
)

// tailBuffer is an io.Writer that retains only the last capacity bytes
// written through it, used to scrape usage from the end of SSE streams
// without buffering multi-MB event payloads.
type tailBuffer struct {
	buf      []byte
	capacity int
}

func newTailBuffer(capacity int) *tailBuffer {
	return &tailBuffer{capacity: capacity}
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	if len(p) >= t.capacity {
		t.buf = append(t.buf[:0], p[len(p)-t.capacity:]...)
		return len(p), nil
	}
	if overflow := len(t.buf) + len(p) - t.capacity; overflow > 0 {
		t.buf = t.buf[:copy(t.buf, t.buf[overflow:])]
	}
	t.buf = append(t.buf, p...)
	return len(p), nil
}

// Bytes returns the retained tail.
func (t *tailBuffer) Bytes() []byte {
	return t.buf
}

// sseErrorMaskEventCap bounds how much of one SSE event the masking writer
// holds back before giving up on it and passing the rest of that event
// through raw. It matches the per-line cap of the chat stream reader; a sane
// error frame is orders of magnitude smaller, and a partial-image event that
// long is not something the mask should ever touch.
const sseErrorMaskEventCap = 4 << 20

// sseErrorMaskWriter is an io.Writer that forwards a pass-through SSE stream
// one event at a time, scrubbing credential-shaped tokens from error frames
// before they reach the client. Pass-through streams are otherwise copied
// verbatim, so this is the one place the chat paths' credentialMasker scrub
// applies to the image/audio endpoints: the exact key on every byte, the
// key-shape regex on error frames.
//
// It works per event rather than per line because SSE lets one payload span
// several `data:` lines (joined with "\n"); judging each line alone would let
// an error object split across two lines through unmasked. An event is held
// until its blank-line delimiter, then either forwarded byte-identical (the
// common case: content events, multi-MB base64 partial images that must never
// meet the mask's prefix regex) or, when the joined payload is a JSON object
// carrying an "error" member and masking changes it, re-emitted with its
// non-data lines intact and canonical "data: " lines. An event that
// grows past sseErrorMaskEventCap is passed through raw from that point.
//
// Write returns len(p) on success. On a downstream error it returns only the
// input bytes whose event was fully written, so the caller's byte count is a
// lower bound on what the client received rather than what was parsed.
// Flush releases a trailing event that lacked its delimiter.
type sseErrorMaskWriter struct {
	w       io.Writer
	cred    credentialMasker
	rawOut  *exactMaskWriter // raw-mode writes, boundary-safe for the key
	partial []byte           // unterminated line
	event   [][]byte         // complete lines of the event in progress, each with its eol
	held    int              // input bytes buffered in partial + event
	raw     bool             // event exceeded the cap: pass through until its delimiter
}

func newSSEErrorMaskWriter(w io.Writer, cred credentialMasker) *sseErrorMaskWriter {
	return &sseErrorMaskWriter{w: w, cred: cred, rawOut: newExactMaskWriter(w, cred)}
}

func (m *sseErrorMaskWriter) Write(p []byte) (int, error) {
	// delivered is the input-byte position up to which every event has been
	// written downstream; on error it is what the caller may count.
	delivered := 0
	consumed := 0
	for consumed < len(p) {
		nl := bytes.IndexByte(p[consumed:], '\n')
		if nl < 0 {
			chunk := p[consumed:]
			consumed = len(p)
			if m.raw {
				if _, err := m.rawOut.Write(chunk); err != nil {
					return delivered, err
				}
				break
			}
			m.partial = append(m.partial, chunk...)
			m.held += len(chunk)
			if m.held > sseErrorMaskEventCap {
				if err := m.spill(); err != nil {
					return delivered, err
				}
			}
			break
		}
		line := p[consumed : consumed+nl+1]
		consumed += nl + 1
		if m.raw {
			if _, err := m.rawOut.Write(line); err != nil {
				return delivered, err
			}
			if isSSEBlankLine(line) {
				// The oversized event is over: release the held tail before
				// normal emits resume so ordering is preserved.
				m.raw = false
				if err := m.rawOut.Flush(); err != nil {
					return delivered, err
				}
			}
			delivered = consumed
			continue
		}
		if len(m.partial) > 0 {
			line = append(m.partial, line...)
			m.partial = nil
		}
		if isSSEBlankLine(line) {
			if err := m.emitEvent(line); err != nil {
				return delivered, err
			}
			delivered = consumed
			continue
		}
		m.event = append(m.event, line)
		m.held += nl + 1
		if m.held > sseErrorMaskEventCap {
			if err := m.spill(); err != nil {
				return delivered, err
			}
			delivered = consumed
		}
	}
	return consumed, nil
}

// Flush writes out a trailing event that never received its delimiter (a
// stream cut mid-event), masked if it turns out to be an error frame.
func (m *sseErrorMaskWriter) Flush() error {
	if m.raw {
		m.raw = false
		return m.rawOut.Flush()
	}
	if len(m.partial) > 0 {
		m.event = append(m.event, m.partial)
		m.partial = nil
	}
	if len(m.event) == 0 {
		m.held = 0
		return nil
	}
	return m.emitEvent(nil)
}

// spill abandons masking for the event in progress: everything buffered goes
// downstream raw and the remainder of the event is passed through as it
// arrives. Only reached when an event outgrows sseErrorMaskEventCap.
func (m *sseErrorMaskWriter) spill() error {
	var out []byte
	for _, l := range m.event {
		out = append(out, l...)
	}
	out = append(out, m.partial...)
	m.event, m.partial, m.held, m.raw = nil, nil, 0, true
	_, err := m.rawOut.Write(out)
	return err
}

// emitEvent writes the buffered event plus its delimiter (nil when flushing a
// truncated tail) as a single downstream write, masked when it is an error
// frame quoting a credential.
func (m *sseErrorMaskWriter) emitEvent(delimiter []byte) error {
	lines := m.event
	m.event, m.held = nil, 0

	var out []byte
	if masked, ok := maskSSEErrorEvent(lines, m.cred); ok {
		out = masked
	} else {
		for _, l := range lines {
			out = append(out, l...)
		}
	}
	out = append(out, delimiter...)
	if len(out) == 0 {
		return nil
	}
	return m.emit(out)
}

// emit writes one complete event with the exact credential scrubbed. Every
// normal-mode byte the client receives passes here, so a gateway quoting its
// key in a content event is covered too; raw mode goes through rawOut, which
// is boundary-safe across chunks.
func (m *sseErrorMaskWriter) emit(b []byte) error {
	_, err := m.w.Write(m.cred.maskExact(b))
	return err
}

func isSSEBlankLine(line []byte) bool {
	return len(bytes.TrimRight(line, "\r\n")) == 0
}

// maskSSEErrorEvent joins the event's `data:` payloads per the SSE spec and,
// when the result is a JSON object with a non-null "error" member that
// cred.mask changes, returns the event re-serialised: non-data lines
// in their original order and framing, then canonical "data: " lines carrying
// the masked payload, one per physical line of the original. ok is false when the event is to be forwarded
// verbatim.
func maskSSEErrorEvent(lines [][]byte, cred credentialMasker) (out []byte, ok bool) {
	var payload []byte
	var eol []byte
	dataLines := 0
	for _, l := range lines {
		rest, isData := bytes.CutPrefix(l, []byte("data:"))
		if !isData {
			continue
		}
		body := bytes.TrimRight(rest, "\r\n")
		if dataLines == 0 {
			eol = rest[len(body):]
		} else {
			payload = append(payload, '\n')
		}
		payload = append(payload, bytes.TrimSpace(body)...)
		dataLines++
	}
	if dataLines == 0 || len(payload) == 0 || payload[0] != '{' || !bytes.Contains(payload, []byte(`"error"`)) {
		return nil, false
	}
	var frame struct {
		Error json.RawMessage `json:"error"`
	}
	if json.Unmarshal(payload, &frame) != nil || len(frame.Error) == 0 || bytes.Equal(frame.Error, []byte("null")) {
		return nil, false
	}
	masked := cred.mask(payload)
	if bytes.Equal(masked, payload) {
		return nil, false
	}
	for _, l := range lines {
		if !bytes.HasPrefix(l, []byte("data:")) {
			out = append(out, l...)
		}
	}
	// A payload that spanned several data lines still holds the "\n" joins;
	// each physical line needs its own prefix or SSE clients drop it.
	for _, seg := range bytes.Split(masked, []byte{'\n'}) {
		out = append(out, "data: "...)
		out = append(out, seg...)
		out = append(out, eol...)
	}
	return out, true
}

// flushWriter flushes the underlying ResponseWriter after every write so
// streamed pass-through bytes (SSE events, audio chunks) reach the client
// immediately instead of sitting in the server's buffer.
type flushWriter struct {
	w io.Writer
	f http.Flusher
}

func newFlushWriter(w http.ResponseWriter) flushWriter {
	f, _ := w.(http.Flusher)
	return flushWriter{w: w, f: f}
}
