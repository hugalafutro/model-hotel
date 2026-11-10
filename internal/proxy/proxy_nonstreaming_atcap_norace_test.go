//go:build !race

package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// This file is left out of race builds. The test below is single-goroutine,
// so the race detector has nothing to find in it, and instrumenting a decode
// of a body the size of the whole cap took about five minutes on a CI runner:
// most of one race shard. The plain test job runs it.

// A body of exactly the cap is legitimate and must still decode and reach the
// client whole: the read takes cap+1 bytes precisely so the boundary case is
// not mistaken for an overflow.
func TestHandleNonStreamingResponse_BodyAtCapDecodesIntact(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandlerIntegration(h)

	const head = `{"id":"chatcmpl-1","object":"chat.completion","created":1,"model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"`
	const tail = `"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":2,"total_tokens":3}}`
	pad := nonStreamingBodyCap - len(head) - len(tail)

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body: io.NopCloser(io.MultiReader(
			strings.NewReader(head),
			io.LimitReader(paddingReader{}, int64(pad)),
			strings.NewReader(tail),
		)),
		Header: http.Header{"Content-Type": []string{"application/json"}},
	}
	req := withAuthContext(httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody))
	logData := nonStreamingLogData()
	rec := httptest.NewRecorder()

	h.handleNonStreamingResponse(rec, req, logData, resp, readNonStreamingBody(resp, logData.masker), time.Now(), 0, 0, resolveTimings{}, 0, "", 1)

	if logData.state != "completed" {
		t.Fatalf("state = %q (%s), want completed for a body exactly at the cap", logData.state, logData.errorMessage)
	}
	var got ChatCompletionResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("client response did not decode: %v", err)
	}
	content, _ := got.Choices[0].Message.Content.(string)
	if len(content) != pad {
		t.Fatalf("content forwarded truncated: %d bytes, want %d", len(content), pad)
	}
}
