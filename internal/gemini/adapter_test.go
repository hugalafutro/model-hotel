package gemini

import (
	"errors"
	"io"
	"strings"
	"testing"
)

// slowReader yields its script one entry per Read call, simulating SSE data
// arriving in arbitrary splits (including mid-line).
type slowReader struct {
	script []string
	closed bool
}

func (r *slowReader) Read(p []byte) (int, error) {
	if len(r.script) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.script[0])
	rest := r.script[0][n:]
	if rest == "" {
		r.script = r.script[1:]
	} else {
		r.script[0] = rest
	}
	return n, nil
}

func (r *slowReader) Close() error {
	r.closed = true
	return nil
}

func TestStreamAdapter_TranslatesAndFinishesOnEOF(t *testing.T) {
	// Two content chunks split awkwardly across reads, then EOF. Vertex SSE has
	// no [DONE] sentinel: EOF is the natural end and must yield the terminal
	// chunk + [DONE].
	upstream := &slowReader{script: []string{
		"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"par",
		"ts\":[{\"text\":\"Hel\"}]}}]}\r\n\n",
		"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"lo\"}]},\"finishReason\":\"STOP\"}],",
		"\"usageMetadata\":{\"promptTokenCount\":3,\"candidatesTokenCount\":2,\"totalTokenCount\":5}}\n\n",
	}}

	a := NewStreamAdapter(upstream, "gemini-2.5-flash")
	out, err := io.ReadAll(a)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	s := string(out)

	if strings.Count(s, `"role":"assistant"`) != 1 {
		t.Errorf("role deltas != 1 in:\n%s", s)
	}
	if !strings.Contains(s, `"content":"Hel"`) || !strings.Contains(s, `"content":"lo"`) {
		t.Errorf("content deltas missing in:\n%s", s)
	}
	if !strings.Contains(s, `"finish_reason":"stop"`) {
		t.Errorf("terminal finish_reason missing in:\n%s", s)
	}
	if !strings.Contains(s, `"total_tokens":5`) {
		t.Errorf("usage missing in:\n%s", s)
	}
	if !strings.HasSuffix(s, "data: [DONE]\n\n") {
		t.Errorf("stream must end with [DONE], got tail %q", s[max(0, len(s)-40):])
	}
	// Every chunk echoes the requested model.
	if strings.Count(s, `"model":"gemini-2.5-flash"`) != strings.Count(s, `"object":"chat.completion.chunk"`) {
		t.Errorf("model not echoed on every chunk:\n%s", s)
	}
}

func TestStreamAdapter_NonDataLinesIgnored(t *testing.T) {
	// SSE comments and event: lines are normal framing noise, not errors.
	upstream := &slowReader{script: []string{
		": keepalive\nevent: something\ndata: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"ok\"}]},\"finishReason\":\"STOP\"}]}\n\n",
	}}
	out, err := io.ReadAll(NewStreamAdapter(upstream, "m"))
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if !strings.Contains(string(out), `"content":"ok"`) {
		t.Errorf("valid chunk lost after non-data lines:\n%s", out)
	}
}

func TestStreamAdapter_MalformedChunkPoisonsStream(t *testing.T) {
	// A malformed data line means a corrupt upstream. Already-translated bytes
	// drain, then the stream errors — and EOF must NOT fabricate a clean
	// terminal chunk + [DONE] that would make the proxy report success.
	upstream := &slowReader{script: []string{
		"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"pre\"}]}}]}\n\n",
		"data: {not json}\n\n",
		"data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"post\"}]},\"finishReason\":\"STOP\"}]}\n\n",
	}}
	out, err := io.ReadAll(NewStreamAdapter(upstream, "m"))
	if err == nil {
		t.Fatal("expected error for malformed upstream chunk")
	}
	s := string(out)
	if !strings.Contains(s, `"content":"pre"`) {
		t.Errorf("pre-error content lost:\n%s", s)
	}
	if strings.Contains(s, "[DONE]") || strings.Contains(s, `"finish_reason":"`) {
		t.Errorf("terminal chunks fabricated after malformed upstream:\n%s", s)
	}
}

func TestStreamAdapter_UpstreamErrorAfterDrain(t *testing.T) {
	boom := errors.New("boom")
	a := NewStreamAdapter(&errReader{data: "data: {\"candidates\":[{\"content\":{\"role\":\"model\",\"parts\":[{\"text\":\"x\"}]}}]}\n\n", err: boom}, "m")
	out, err := io.ReadAll(a)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	// Bytes translated before the error are still delivered, but no terminal
	// [DONE] is fabricated for a broken stream.
	if !strings.Contains(string(out), `"content":"x"`) {
		t.Errorf("pre-error content lost:\n%s", out)
	}
	if strings.Contains(string(out), "[DONE]") {
		t.Errorf("[DONE] fabricated on upstream error:\n%s", out)
	}
}

type errReader struct {
	data string
	err  error
}

func (r *errReader) Read(p []byte) (int, error) {
	if r.data == "" {
		return 0, r.err
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}

func (r *errReader) Close() error { return nil }

func TestStreamAdapter_ClosePropagates(t *testing.T) {
	upstream := &slowReader{}
	a := NewStreamAdapter(upstream, "m")
	if err := a.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !upstream.closed {
		t.Error("Close did not propagate to upstream")
	}
}
