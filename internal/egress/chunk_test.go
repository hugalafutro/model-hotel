package egress

import (
	"bytes"
	"math"
	"testing"
)

type testDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

type testUsage struct {
	TotalTokens int `json:"total_tokens"`
}

func newTestWriter() ChunkWriter {
	return ChunkWriter{Component: "testdialect", ID: "chatcmpl-x", Model: "m", Created: 7}
}

// The assistant role rides on the first chunk of a stream and no other, and
// every chunk carries the same envelope.
func TestWriteChunk_RoleOnFirstChunkOnly(t *testing.T) {
	w := newTestWriter()
	if w.Started() {
		t.Error("a fresh writer reports started")
	}
	var buf bytes.Buffer
	for _, content := range []string{"a", "b"} {
		delta := testDelta{Content: content}
		delta.Role = w.Role()
		if err := WriteChunk(&buf, &w, delta, nil, (*testUsage)(nil)); err != nil {
			t.Fatalf("WriteChunk: %v", err)
		}
	}
	want := `data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":7,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"a"},"finish_reason":null}]}` + "\n\n" +
		`data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":7,"model":"m","choices":[{"index":0,"delta":{"content":"b"},"finish_reason":null}]}` + "\n\n"
	if buf.String() != want {
		t.Errorf("frames =\n%s\nwant\n%s", buf.String(), want)
	}
	if !w.Started() {
		t.Error("writer does not report started after a chunk")
	}
}

func TestWriteChunk_FinishReasonAndUsage(t *testing.T) {
	w := newTestWriter()
	w.Role() // the content chunks already went out
	var buf bytes.Buffer
	finish := "stop"
	if err := WriteChunk(&buf, &w, testDelta{}, &finish, &testUsage{TotalTokens: 3}); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	want := `data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":7,"model":"m","choices":[{"index":0,"delta":{},"finish_reason":"stop"}],"usage":{"total_tokens":3}}` + "\n\n"
	if buf.String() != want {
		t.Errorf("frame =\n%s\nwant\n%s", buf.String(), want)
	}
}

// The usage-only chunk carries an empty choices array, the shape
// stream_options.include_usage produces.
func TestWriteUsageChunk(t *testing.T) {
	w := newTestWriter()
	var buf bytes.Buffer
	if err := WriteUsageChunk[testDelta](&buf, &w, &testUsage{TotalTokens: 9}); err != nil {
		t.Fatalf("WriteUsageChunk: %v", err)
	}
	want := `data: {"id":"chatcmpl-x","object":"chat.completion.chunk","created":7,"model":"m","choices":[],"usage":{"total_tokens":9}}` + "\n\n"
	if buf.String() != want {
		t.Errorf("frame =\n%s\nwant\n%s", buf.String(), want)
	}
}

// An unmarshalable delta is reported, and names the dialect that owns the
// stream, rather than framing a corrupt line.
func TestWriteChunk_MarshalFailureNamesTheDialect(t *testing.T) {
	w := newTestWriter()
	var buf bytes.Buffer
	err := WriteChunk(&buf, &w, math.NaN(), nil, (*testUsage)(nil))
	if err == nil {
		t.Fatal("marshalling NaN succeeded")
	}
	if got := err.Error(); got[:len("testdialect: ")] != "testdialect: " {
		t.Errorf("error = %q, want it to name the dialect", got)
	}
	if buf.Len() != 0 {
		t.Errorf("wrote %q on a marshal failure, want nothing", buf.String())
	}
}
