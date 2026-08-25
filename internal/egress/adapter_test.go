package egress

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
)

// fakeTranslator echoes each payload back in a recognisable frame, so the
// adapter's own mechanics (line splitting, EOF finish, poisoning) are what the
// assertions below measure rather than any real dialect's translation.
type fakeTranslator struct {
	failOn    string   // payload that makes Translate fail
	seen      []string // one entry per Translate call, so folding is measurable
	finishErr error
	finished  int
}

func (f *fakeTranslator) Translate(payload []byte) ([]byte, error) {
	f.seen = append(f.seen, string(payload))
	if f.failOn != "" && string(payload) == f.failOn {
		return nil, errors.New("bad payload")
	}
	return []byte("<" + string(payload) + ">"), nil
}

func (f *fakeTranslator) Finish() ([]byte, error) {
	f.finished++
	if f.finishErr != nil {
		return nil, f.finishErr
	}
	return []byte("[DONE]"), nil
}

// scriptedBody yields its script one entry per Read call, simulating SSE data
// arriving in arbitrary splits (including mid-line), then err (default io.EOF).
type scriptedBody struct {
	script []string
	err    error
	closed bool
}

func (r *scriptedBody) Read(p []byte) (int, error) {
	if len(r.script) == 0 {
		if r.err != nil {
			return 0, r.err
		}
		return 0, io.EOF
	}
	n := copy(p, r.script[0])
	if rest := r.script[0][n:]; rest == "" {
		r.script = r.script[1:]
	} else {
		r.script[0] = rest
	}
	return n, nil
}

func (r *scriptedBody) Close() error {
	r.closed = true
	return nil
}

func TestStreamAdapter_TranslatesLinesAndFinishesOnEOF(t *testing.T) {
	tr := &fakeTranslator{}
	// The first event is split mid-line across two reads.
	body := &scriptedBody{script: []string{"data: on", "e\r\n\ndata: two\n\n"}}

	out, err := io.ReadAll(NewStreamAdapter("test", body, tr))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if got := string(out); got != "<one><two>[DONE]" {
		t.Errorf("output = %q, want the two events plus the terminal bytes", got)
	}
	if tr.finished != 1 {
		t.Errorf("Finish called %d times, want 1", tr.finished)
	}
}

func TestStreamAdapter_NonDataLinesIgnored(t *testing.T) {
	tr := &fakeTranslator{}
	body := &scriptedBody{script: []string{": keepalive\nevent: message_start\ndata:\ndata: real\n\n"}}

	out, err := io.ReadAll(NewStreamAdapter("test", body, tr))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if got := string(out); got != "<real>[DONE]" {
		t.Errorf("output = %q, want only the data payload translated", got)
	}
}

// TestStreamAdapter_FoldedEventTranslatedOnce pins the SSE folding rule: one
// event may spread its payload over several "data:" fields, and the payload is
// their newline-join. Translating each field on its own would hand the
// translator JSON fragments and poison the stream.
func TestStreamAdapter_FoldedEventTranslatedOnce(t *testing.T) {
	tr := &fakeTranslator{}
	body := &scriptedBody{script: []string{"data: {\"text\":\ndata: \"hello\"}\n\n"}}

	out, err := io.ReadAll(NewStreamAdapter("test", body, tr))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(tr.seen) != 1 {
		t.Fatalf("Translate called %d times, want 1: %q", len(tr.seen), tr.seen)
	}
	if tr.seen[0] != "{\"text\":\n\"hello\"}" {
		t.Errorf("payload = %q, want the two fields joined with a newline", tr.seen[0])
	}
	if got := string(out); got != "<{\"text\":\n\"hello\"}>[DONE]" {
		t.Errorf("output = %q", got)
	}
}

// TestStreamAdapter_NonDataFieldsInsideEventIgnored keeps comment lines and the
// other SSE fields out of the payload even when they sit between the data
// fields of one folded event: the dialect translators key off the payload's own
// JSON type, never the SSE event name.
func TestStreamAdapter_NonDataFieldsInsideEventIgnored(t *testing.T) {
	tr := &fakeTranslator{}
	body := &scriptedBody{script: []string{
		"event: content_block_delta\n: keepalive\ndata: {\"a\":1,\nid: 7\ndata: \"b\":2}\nretry: 100\n\n",
	}}

	out, err := io.ReadAll(NewStreamAdapter("test", body, tr))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(tr.seen) != 1 {
		t.Fatalf("Translate called %d times, want 1: %q", len(tr.seen), tr.seen)
	}
	if tr.seen[0] != "{\"a\":1,\n\"b\":2}" {
		t.Errorf("payload = %q, want only the data fields joined", tr.seen[0])
	}
	if got := string(out); got != "<{\"a\":1,\n\"b\":2}>[DONE]" {
		t.Errorf("output = %q", got)
	}
}

// TestStreamAdapter_BlankLineSeparatesEvents pins the other half of the rule:
// the blank line ends an event, so two of them stay two Translate calls rather
// than merging into one payload.
func TestStreamAdapter_BlankLineSeparatesEvents(t *testing.T) {
	tr := &fakeTranslator{}
	body := &scriptedBody{script: []string{"data: one\n\ndata: two\n\n"}}

	out, err := io.ReadAll(NewStreamAdapter("test", body, tr))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(tr.seen) != 2 || tr.seen[0] != "one" || tr.seen[1] != "two" {
		t.Errorf("Translate calls = %q, want [one two]", tr.seen)
	}
	if got := string(out); got != "<one><two>[DONE]" {
		t.Errorf("output = %q", got)
	}
}

// TestStreamAdapter_UnterminatedFoldedEventFlushedAtEOF covers the tail an
// upstream leaves when it closes without the blank line: the folded event is
// still dispatched once, before Finish().
func TestStreamAdapter_UnterminatedFoldedEventFlushedAtEOF(t *testing.T) {
	tr := &fakeTranslator{}
	body := &scriptedBody{script: []string{"data: {\"text\":\ndata: \"tail\"}"}}

	out, err := io.ReadAll(NewStreamAdapter("test", body, tr))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(tr.seen) != 1 {
		t.Fatalf("Translate called %d times, want 1: %q", len(tr.seen), tr.seen)
	}
	if tr.seen[0] != "{\"text\":\n\"tail\"}" {
		t.Errorf("payload = %q, want the whole folded tail", tr.seen[0])
	}
	if got := string(out); got != "<{\"text\":\n\"tail\"}>[DONE]" {
		t.Errorf("output = %q, want the tail translated before the terminal bytes", got)
	}
}

// TestStreamAdapter_FoldedEventOverCapFailsStream pins the cap as an event-size
// cap: neither field is close to the limit on its own, but the event they build
// is over it, so it must fail rather than grow the buffer.
func TestStreamAdapter_FoldedEventOverCapFailsStream(t *testing.T) {
	tr := &fakeTranslator{}
	half := strings.Repeat("a", MaxSSEEventBytes/2+1)
	body := &scriptedBody{script: []string{"data: " + half + "\ndata: " + half + "\n\n"}}

	_, err := io.ReadAll(NewStreamAdapter("test", body, tr))
	if err == nil {
		t.Fatal("expected the joined event to exceed the cap")
	}
	if !strings.Contains(err.Error(), "exceeds") || !strings.HasPrefix(err.Error(), "test: ") {
		t.Errorf("error = %q, want the component prefix and the exceeded cap", err)
	}
	if strings.Contains(err.Error(), "aaaa") {
		t.Errorf("error leaked the buffered event: %q", err)
	}
	if len(tr.seen) != 0 {
		t.Errorf("Translate called on an over-cap event: %q", tr.seen)
	}
}

func TestStreamAdapter_FlushesPartialLineAtEOF(t *testing.T) {
	tr := &fakeTranslator{}
	// No trailing newline: without the EOF flush the last event is lost while
	// Finish() still emits a clean terminal — a silent truncation.
	body := &scriptedBody{script: []string{"data: first\n\ndata: last"}}

	out, err := io.ReadAll(NewStreamAdapter("test", body, tr))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if got := string(out); got != "<first><last>[DONE]" {
		t.Errorf("output = %q, want the residual line flushed before Finish", got)
	}
}

func TestStreamAdapter_WrappedEOFStillFinishes(t *testing.T) {
	tr := &fakeTranslator{}
	// io.ReadAll compares the terminating error against io.EOF with ==, so a
	// wrapped EOF must be normalised or the stream reads as broken.
	body := &scriptedBody{script: []string{"data: x\n\n"}, err: fmt.Errorf("transport: %w", io.EOF)}

	out, err := io.ReadAll(NewStreamAdapter("test", body, tr))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if got := string(out); got != "<x>[DONE]" {
		t.Errorf("output = %q, want a wrapped EOF to end the stream cleanly", got)
	}
}

func TestStreamAdapter_TranslateFailurePoisonsStream(t *testing.T) {
	tr := &fakeTranslator{failOn: "bad"}
	body := &scriptedBody{script: []string{"data: pre\n\ndata: bad\n\ndata: post\n\n"}}

	out, err := io.ReadAll(NewStreamAdapter("test", body, tr))
	if err == nil {
		t.Fatal("expected the translation failure to surface")
	}
	if got := string(out); got != "<pre>" {
		t.Errorf("output = %q, want only the bytes translated before the failure", got)
	}
	if tr.finished != 0 {
		t.Error("Finish must not be called over a corrupt upstream")
	}
}

func TestStreamAdapter_UpstreamErrorAfterDrain(t *testing.T) {
	boom := errors.New("boom")
	tr := &fakeTranslator{}
	body := &scriptedBody{script: []string{"data: x\n\n"}, err: boom}

	out, err := io.ReadAll(NewStreamAdapter("test", body, tr))
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if got := string(out); got != "<x>" {
		t.Errorf("output = %q, want the drained bytes without a fabricated terminal", got)
	}
	if tr.finished != 0 {
		t.Error("Finish must not be called on a non-EOF upstream error")
	}
}

func TestStreamAdapter_FinishErrorIsLoggedNotFatal(t *testing.T) {
	tr := &fakeTranslator{finishErr: errors.New("finish blew up")}
	body := &scriptedBody{script: []string{"data: x\n\n"}}

	out, err := io.ReadAll(NewStreamAdapter("test", body, tr))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if got := string(out); got != "<x>" {
		t.Errorf("output = %q, want the translated bytes with no terminal appended", got)
	}
}

func TestStreamAdapter_OverlongEventFailsStream(t *testing.T) {
	tr := &fakeTranslator{}
	// An upstream that never emits a newline must fail the stream rather than
	// grow the line buffer without bound.
	body := &scriptedBody{script: []string{"data: " + strings.Repeat("a", MaxSSEEventBytes+1)}}

	out, err := io.ReadAll(NewStreamAdapter("test", body, tr))
	if err == nil {
		t.Fatal("expected an error once the line exceeded the cap")
	}
	if !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("error = %q, want it to name the exceeded cap", err)
	}
	if !strings.HasPrefix(err.Error(), "test: ") {
		t.Errorf("error = %q, want the component prefix", err)
	}
	if strings.Contains(err.Error(), "aaaa") {
		t.Errorf("error leaked the buffered line: %q", err)
	}
	if strings.Contains(string(out), "[DONE]") {
		t.Errorf("terminal bytes fabricated over an unterminated line: %q", out)
	}
}

func TestStreamAdapter_ClosePropagates(t *testing.T) {
	body := &scriptedBody{}
	a := NewStreamAdapter("test", body, &fakeTranslator{})
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !body.closed {
		t.Error("Close must reach the wrapped upstream body")
	}
}
