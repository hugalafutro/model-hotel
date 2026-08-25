package egress

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"
	"testing"
)

func TestAsJSONString(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		want    string
		wantOK  bool
		comment string
	}{
		{name: "absent field", raw: "", want: "", wantOK: false},
		{name: "string literal", raw: `"hello"`, want: "hello", wantOK: true},
		{name: "empty string literal", raw: `""`, want: "", wantOK: true},
		{name: "escapes are decoded", raw: `"a\nb"`, want: "a\nb", wantOK: true},
		{name: "null decodes to the empty string", raw: `null`, want: "", wantOK: true},
		{name: "array is not a string", raw: `[{"type":"text"}]`, want: "", wantOK: false},
		{name: "object is not a string", raw: `{"type":"text"}`, want: "", wantOK: false},
		{name: "number is not a string", raw: `42`, want: "", wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := AsJSONString(json.RawMessage(tc.raw))
			if got != tc.want || ok != tc.wantOK {
				t.Errorf("AsJSONString(%q) = (%q, %v), want (%q, %v)", tc.raw, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestDecodeStop(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "absent field", raw: "", want: nil},
		{name: "single string", raw: `"STOP"`, want: []string{"STOP"}},
		{name: "empty string is not a stop sequence", raw: `""`, want: nil},
		{name: "array", raw: `["a","b"]`, want: []string{"a", "b"}},
		{name: "empty array", raw: `[]`, want: []string{}},
		{name: "wrong type", raw: `{"a":1}`, want: nil},
		{name: "malformed", raw: `[not json`, want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := DecodeStop(json.RawMessage(tc.raw))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("DecodeStop(%q) = %#v, want %#v", tc.raw, got, tc.want)
			}
		})
	}
}

// fakeTranslator echoes each payload back in a recognisable frame, so the
// adapter's own mechanics (line splitting, EOF finish, poisoning) are what the
// assertions below measure rather than any real dialect's translation.
type fakeTranslator struct {
	failOn    string // payload that makes Translate fail
	finishErr error
	finished  int
}

func (f *fakeTranslator) Translate(payload []byte) ([]byte, error) {
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

func TestStreamAdapter_OverlongLineFailsStream(t *testing.T) {
	tr := &fakeTranslator{}
	// An upstream that never emits a newline must fail the stream rather than
	// grow the line buffer without bound.
	body := &scriptedBody{script: []string{"data: " + strings.Repeat("a", MaxSSELineBytes+1)}}

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
