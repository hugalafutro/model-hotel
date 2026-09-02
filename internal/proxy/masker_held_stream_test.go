package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/util"
)

// A streamed error frame is error text: a held foreign key quoted in it must
// not reach the client, on the translated path (a token first, so the frame
// is handled by the stream state rather than the TTFT probe).
func TestCredentialMasker_StreamedErrorFrameMasksHeldKey(t *testing.T) {
	const foreign = "custom-key-A-stream-11112222-33334444"
	util.HoldSecret(foreign)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n")
		_, _ = io.WriteString(w, "data: {\"error\":{\"message\":\"relay rejected: bad api key "+foreign+"\",\"type\":\"server_error\"}}\n\n")
	}))
	defer upstream.Close()
	env := buildReplayEnv(t, upstream)
	body := `{"model": "hotel/` + env.group + `", "messages": [{"role": "user", "content": "hi"}], "stream": true}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "replay-key")
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.keyHash)
	w := httptest.NewRecorder()
	env.h.ChatCompletions(w, req.WithContext(ctx))
	if strings.Contains(w.Body.String(), foreign) {
		t.Fatalf("the client stream carries the held foreign key: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "bad api key [redacted]") {
		t.Fatalf("the error frame did not reach the client masked: %s", w.Body.String())
	}
}

// The pass-through writer: a JSON error event and a bare non-JSON error line
// both lose a held foreign key; a content event keeps to the candidate.
func TestSSEErrorMaskWriter_HeldKeyInErrorEvents(t *testing.T) {
	const foreign = "custom-key-A-sse-11112222-33334444"
	const own = "custom-key-B-sse-77776666-88889999"
	util.HoldSecret(foreign)
	for _, tc := range []struct{ name, in, want string }{
		{"json error event", "data: {\"error\":{\"message\":\"bad api key " + foreign + " and " + own + "\"}}\n\n", "data: {\"error\":{\"message\":\"bad api key [redacted] and [redacted]\"}}\n\n"},
		{"bare error line", "data: error: invalid api key " + foreign + "\n\n", "data: error: invalid api key [redacted]\n\n"},
		{"content keeps the candidate only", "data: {\"choices\":[{\"delta\":{\"content\":\"" + foreign + " " + own + "\"}}]}\n\n", "data: {\"choices\":[{\"delta\":{\"content\":\"" + foreign + " [redacted]\"}}]}\n\n"},
		{"done", "data: [DONE]\n\n", "data: [DONE]\n\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			w := newSSEErrorMaskWriter(&out, newCredentialMasker(own))
			if _, err := w.Write([]byte(tc.in)); err != nil {
				t.Fatal(err)
			}
			if err := w.Flush(); err != nil {
				t.Fatal(err)
			}
			if out.String() != tc.want {
				t.Fatalf("got %q\nwant %q", out.String(), tc.want)
			}
		})
	}
}
