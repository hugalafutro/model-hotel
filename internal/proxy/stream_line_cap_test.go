package proxy

import (
	"bufio"
	"context"
	"io"
	"strings"
	"testing"
)

// An image model streams a whole picture as one base64 data URL in a single
// delta, well past the old 4 MiB line cap. The reader must hand such a frame
// on as one data event rather than ending the stream on bufio.ErrTooLong.
func TestStreamReader_ImageFrameLargerThanFourMiBIsOneEvent(t *testing.T) {
	t.Parallel()
	frame := `{"choices":[{"delta":{"images":[{"image_url":{"url":"data:image/png;base64,` + strings.Repeat("A", 5<<20) + `"}}]}}]}`
	body := io.NopCloser(strings.NewReader("data: " + frame + "\n\ndata: [DONE]\n"))
	reader := newStreamReader(context.Background(), body, streamOptions{}, &requestLogData{modelID: "m", providerName: "p"}, nil)
	defer reader.Close()

	ev, ok := reader.Next()
	if !ok || ev.kind != sseData || len(ev.payload) != len(frame) {
		t.Fatalf("frame of %d bytes was not delivered whole: ok=%v kind=%d len=%d", len(frame), ok, ev.kind, len(ev.payload))
	}
	for {
		ev, ok = reader.Next()
		if !ok {
			break
		}
		if ev.kind == sseDone {
			break
		}
	}
	if err := reader.err(); err != nil {
		t.Fatalf("reader error after a large frame: %v", err)
	}
}

// A frame past sseLineCap is the gateway's own limit, not an upstream fault:
// the row and the client name the limit, and the breaker is not charged.
func TestDeriveStreamError_LineCapNamesTheLimitAndSparesTheBreaker(t *testing.T) {
	t.Parallel()
	st := &streamState{}
	logData := &requestLogData{statusCode: 200}

	got := deriveStreamError(st, bufio.ErrTooLong, streamOptions{}, logData)
	want := "stream failed: a frame exceeded the gateway's 32 MiB line limit"
	if got != want {
		t.Fatalf("errMsg = %q, want %q", got, want)
	}
	if st.clientErrMsg != want {
		t.Fatalf("client message = %q, want the same limit", st.clientErrMsg)
	}
	if logData.errorKind != KindInternal {
		t.Fatalf("errorKind = %s, want %s", logData.errorKind, KindInternal)
	}
	if v := judgeStreamForBreaker(st, logData, got, true); v.failureReason != "" || v.success {
		t.Fatalf("breaker verdict = %+v, want nothing recorded", v)
	}
}
