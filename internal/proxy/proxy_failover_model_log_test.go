package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestUpstreamModelAttr_CarriesTheModelNotTheBody pins the no-content-logging
// invariant on the one debug line that reads the upstream body. The value used
// to be the body sliced up to its first comma, logged whenever that slice held
// the token "model", and a caller controls their own field names: a nested
// object with a model key put the caller's text in the log verbatim.
func TestUpstreamModelAttr_CarriesTheModelNotTheBody(t *testing.T) {
	t.Parallel()
	// A nested model key before the first comma is the shape the old slice
	// logged, caller text and all. Marshal sorts the keys, so metadata leads.
	const secret = "caller text with no comma at all"
	body, err := json.Marshal(map[string]any{
		"metadata": map[string]any{"model": secret},
		"model":    "gpt-5",
	})
	if err != nil {
		t.Fatalf("marshal body: %v", err)
	}

	got, ok := upstreamModelAttr(body)
	if !ok {
		t.Fatal("a body naming a model should log one")
	}
	if got != "gpt-5" {
		t.Errorf("logged value = %q, want the model alone", got)
	}
	if strings.Contains(got, secret) {
		t.Errorf("request content reached the log: %q", got)
	}
}

// A body carrying no model, or one that does not decode at all, logs nothing
// rather than falling back to raw bytes. The second case is what keeps a
// multipart body out of the debug log.
func TestUpstreamModelAttr_SilentWithoutAModel(t *testing.T) {
	t.Parallel()
	for _, body := range []string{
		`{"messages":[{"role":"user","content":"secret"}]}`,
		`not json at all, secret`,
		"--boundary\r\nContent-Disposition: form-data; name=\"model\"\r\n\r\nsecret\r\n",
	} {
		if got, ok := upstreamModelAttr([]byte(body)); ok {
			t.Errorf("body %q logged %q, want nothing", body, got)
		}
	}
}

// A model value is caller text, so it is bounded rather than logged at whatever
// length the caller chose.
func TestUpstreamModelAttr_BoundsTheValue(t *testing.T) {
	t.Parallel()
	body, err := json.Marshal(map[string]any{"model": strings.Repeat("m", 4096)})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got, ok := upstreamModelAttr(body)
	if !ok {
		t.Fatal("a body naming a model should log one")
	}
	// The sanitizer marks a value it truncated, so the result is the cap plus
	// that marker rather than exactly the cap.
	if len(got) > upstreamModelLogCap+16 {
		t.Errorf("logged value is %d bytes, want the cap plus a truncation marker", len(got))
	}
	if !strings.HasPrefix(got, strings.Repeat("m", 64)) {
		t.Errorf("logged value = %q, want the head of the model kept", got)
	}
}

// TestLogUpstreamModel_SkipsTheDecodeWhenDebugIsOff proves the gate does the
// thing it exists for. Asserting that nothing was logged would pass either way,
// since the handler drops the record anyway; the decode counter is what
// distinguishes a skipped parse from a parsed-then-dropped one.
func TestLogUpstreamModel_SkipsTheDecodeWhenDebugIsOff(t *testing.T) {
	original := debugEnabled
	t.Cleanup(func() { debugEnabled = original })

	debugEnabled = func() bool { return false }
	before := upstreamModelDecodes.Load()
	logUpstreamModel([]byte(`{"model":"gpt-5"}`))
	if after := upstreamModelDecodes.Load(); after != before {
		t.Errorf("decode ran with debug off: %d -> %d", before, after)
	}

	debugEnabled = func() bool { return true }
	logUpstreamModel([]byte(`{"model":"gpt-5"}`))
	if after := upstreamModelDecodes.Load(); after == before {
		t.Error("decode should run when debug is on")
	}
}
