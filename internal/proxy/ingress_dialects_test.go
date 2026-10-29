package proxy

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/logtest"
)

// A frame that is broken bytes (not a shape the struct lacks) carries nothing
// the translator could render: it is dropped with a debug note, and the
// stream does not start on it.
func TestAnthropicIngressStream_SkipsUnparseableChunk(t *testing.T) {
	logs := logtest.Capture(t)
	tr := anthropicIngress{messageID: "msg_x", model: "m"}.newStreamTranslator()

	out, err := tr.Translate([]byte(`{"choices":[{"delta":`))
	if err != nil || out != nil {
		t.Fatalf("Translate(broken bytes) = (%q, %v), want (nil, nil)", out, err)
	}
	if !logs.Has("anthropic: skip unparseable upstream chunk") {
		t.Error("the dropped frame was not logged")
	}

	// The skip left the stream unstarted: the next real chunk opens it.
	out, err = tr.Translate([]byte(`{"choices":[{"delta":{"content":"hi"}}]}`))
	if err != nil {
		t.Fatalf("Translate(valid chunk): %v", err)
	}
	if !strings.Contains(string(out), "event: message_start") {
		t.Errorf("first real chunk did not start the message: %s", out)
	}
}

// A thought signature on a later fragment of a tool call has no carrier (the
// tool_use id is fixed when the block opens); Finish warns that the next turn
// will be refused, naming how many were lost and for which model.
func TestAnthropicIngressStream_FinishWarnsOnLateSignatures(t *testing.T) {
	logs := logtest.Capture(t)
	tr := anthropicIngress{messageID: "msg_late", model: "gemini-x"}.newStreamTranslator()
	for _, chunk := range []string{
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_l","type":"function","function":{"name":"f","arguments":""}}]}}]}`,
		`{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"},"extra_content":{"google":{"thought_signature":"late"}}}]}}]}`,
		`{"choices":[{"delta":{},"finish_reason":"tool_calls"}]}`,
	} {
		if _, err := tr.Translate([]byte(chunk)); err != nil {
			t.Fatalf("Translate(%s): %v", chunk, err)
		}
	}
	out, err := tr.Finish()
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if !strings.Contains(string(out), "event: message_stop") {
		t.Errorf("Finish did not close the message: %s", out)
	}
	const msg = "anthropic: thought signatures arrived after their tool_use block opened and could not be carried; the next turn will be refused"
	if got := logs.Attr(msg, "count"); got != int64(1) {
		t.Errorf("late-signature warning count = %v, want 1", got)
	}
	if got := logs.Attr(msg, "model"); got != "gemini-x" {
		t.Errorf("late-signature warning model = %v, want gemini-x", got)
	}
}

// A clean stream with no late signatures finishes without the warning.
func TestAnthropicIngressStream_FinishQuietWithoutLateSignatures(t *testing.T) {
	logs := logtest.Capture(t)
	tr := anthropicIngress{messageID: "msg_ok", model: "m"}.newStreamTranslator()
	if _, err := tr.Translate([]byte(`{"choices":[{"delta":{"content":"hi"},"finish_reason":"stop"}]}`)); err != nil {
		t.Fatalf("Translate: %v", err)
	}
	if _, err := tr.Finish(); err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if logs.Has("anthropic: thought signatures arrived after their tool_use block opened and could not be carried; the next turn will be refused") {
		t.Error("warned about late signatures on a stream that had none")
	}
}

// The dialect's label prefixes the writer's own log lines: a 2xx body the
// translator cannot read is answered as a 502 Anthropic error and logged
// under the "anthropic" label.
func TestAnthropicWriter_UntranslatableSuccessLogsUnderItsLabel(t *testing.T) {
	logs := logtest.Capture(t)
	rec := httptest.NewRecorder()
	aw := newAnthropicResponseWriter(rec, "msg_b", "p/m")
	aw.Header().Set("Content-Type", "application/json")
	_, _ = aw.Write([]byte(`not json`))
	aw.Finalize()

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
	if !logs.Has("anthropic: response translate failed; emitting error") {
		t.Errorf("translate failure not logged under the anthropic label; records: %+v", logs.Records())
	}
}
