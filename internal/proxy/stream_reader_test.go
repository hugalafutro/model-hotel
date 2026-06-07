package proxy

import (
	"context"
	"io"
	"strings"
	"testing"
)

// TestStreamReader_Classify unit-tests streamReader.Next() in isolation now that
// the scanner/cleanup/classification logic is its own component (Phase 3 of the
// streaming-pipeline refactor). It pins: first-chunk BOM stripping, both the
// "data: " and no-space "data:" forms, [DONE] detection, blank-line and comment
// classification, and that the cleaned "event:" form reaches the orchestrator.
func TestStreamReader_Classify(t *testing.T) {
	t.Parallel()

	input := "\uFEFFdata: {\"a\":1}\n" + // chunk 1: BOM + standard data form
		"\n" + // blank separator
		": keep-alive\n" + // SSE comment
		"data:{\"b\":2}\n" + // no-space data form (LM Studio)
		"event: error\n" + // event directive (carried as comment)
		"data: [DONE]\n" // sentinel

	body := io.NopCloser(strings.NewReader(input))
	logData := &requestLogData{modelID: "m", providerName: "p"}
	reader := newStreamReader(context.Background(), body, streamOptions{}, logData)
	defer reader.Close()

	type want struct {
		kind    sseEventKind
		payload string
		clean   string
	}
	wants := []want{
		{kind: sseData, payload: `{"a":1}`},
		{kind: sseBlank},
		{kind: sseComment, clean: ": keep-alive"},
		{kind: sseData, payload: `{"b":2}`},
		{kind: sseComment, clean: "event: error"},
		{kind: sseDone, payload: "[DONE]"},
	}

	for i, w := range wants {
		ev, ok := reader.Next()
		if !ok {
			t.Fatalf("event %d: Next() returned ok=false, want event kind %d", i, w.kind)
		}
		if ev.kind != w.kind {
			t.Errorf("event %d: kind = %d, want %d", i, ev.kind, w.kind)
		}
		if ev.payload != w.payload {
			t.Errorf("event %d: payload = %q, want %q", i, ev.payload, w.payload)
		}
		if ev.clean != w.clean {
			t.Errorf("event %d: clean = %q, want %q", i, ev.clean, w.clean)
		}
	}

	if _, ok := reader.Next(); ok {
		t.Error("expected Next() to return ok=false after the stream is exhausted")
	}
	if reader.disconnected {
		t.Error("reader should not be marked disconnected on clean EOF")
	}
	if reader.abortErrMsg != "" {
		t.Errorf("reader.abortErrMsg = %q, want empty", reader.abortErrMsg)
	}
	if reader.chunkCount != len(wants) {
		t.Errorf("reader.chunkCount = %d, want %d", reader.chunkCount, len(wants))
	}
}
