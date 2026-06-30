package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
)

// writeOpenAISSE mimics how the streaming pipeline emits OpenAI chunks through
// the sink: "data: ", payload, "\n\n" as separate fragmented writes.
func writeOpenAISSE(w http.ResponseWriter, payload string) {
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write([]byte(payload))
	_, _ = w.Write([]byte("\n\n"))
}

func TestAnthropicWriter_Streaming(t *testing.T) {
	rec := httptest.NewRecorder()
	aw := newAnthropicResponseWriter(rec, "msg_s", "hotel/claude")

	// Pipeline opens an SSE response.
	aw.Header().Set("Content-Type", "text/event-stream")
	aw.WriteHeader(http.StatusOK)

	writeOpenAISSE(aw, `{"choices":[{"delta":{"content":"Hello"}}]}`)
	writeOpenAISSE(aw, `{"choices":[{"delta":{"content":" world"},"finish_reason":"stop"}]}`)
	writeOpenAISSE(aw, `{"choices":[],"usage":{"prompt_tokens":3,"completion_tokens":2}}`)
	writeOpenAISSE(aw, "[DONE]")
	aw.Finalize()

	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content-type = %q, want text/event-stream", ct)
	}

	// The emitted Anthropic SSE must decode through the real SDK.
	resp := &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(rec.Body.String()))}
	stream := ssestream.NewStream[sdk.MessageStreamEventUnion](ssestream.NewDecoder(resp), nil)
	var text, model string
	var stop string
	for stream.Next() {
		ev := stream.Current()
		switch ev.Type {
		case "message_start":
			model = ev.AsMessageStart().Message.Model
		case "content_block_delta":
			d := ev.AsContentBlockDelta()
			if d.Delta.Type == "text_delta" {
				text += d.Delta.Text
			}
		case "message_delta":
			stop = string(ev.AsMessageDelta().Delta.StopReason)
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatalf("SDK decode error: %v\n%s", err, rec.Body.String())
	}
	if text != "Hello world" {
		t.Errorf("text = %q, want %q", text, "Hello world")
	}
	if model != "hotel/claude" {
		t.Errorf("model = %q", model)
	}
	if stop != "end_turn" {
		t.Errorf("stop = %q, want end_turn", stop)
	}
}

func TestAnthropicWriter_NonStreaming(t *testing.T) {
	rec := httptest.NewRecorder()
	aw := newAnthropicResponseWriter(rec, "msg_n", "p/m")

	// Pipeline writes a JSON chat completion (implicit 200).
	aw.Header().Set("Content-Type", "application/json")
	_, _ = aw.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":4,"completion_tokens":1}}`))
	aw.Finalize()

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("invalid output: %v\n%s", err, rec.Body.String())
	}
	if m["type"] != "message" || m["id"] != "msg_n" {
		t.Errorf("envelope = %v", m)
	}
	content := m["content"].([]any)
	if content[0].(map[string]any)["text"] != "Hi" {
		t.Errorf("content = %v", content)
	}
}

func TestAnthropicWriter_NativeVerbatim200(t *testing.T) {
	rec := httptest.NewRecorder()
	aw := newAnthropicResponseWriter(rec, "msg_ignored", "ignored")
	native := true
	aw.bindNativeFlag(&native)

	// Native passthrough: upstream is already an Anthropic message; forward as-is.
	anthropicBody := `{"id":"msg_upstream","type":"message","role":"assistant","content":[{"type":"text","text":"native"}],"stop_reason":"end_turn"}`
	aw.Header().Set("Content-Type", "application/json")
	aw.WriteHeader(http.StatusOK)
	_, _ = aw.Write([]byte(anthropicBody))
	aw.Finalize()

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	// Must be byte-identical (the upstream id survives, NOT our generated one).
	if rec.Body.String() != anthropicBody {
		t.Errorf("verbatim body mismatch:\n got %s\nwant %s", rec.Body.String(), anthropicBody)
	}
}

func TestAnthropicWriter_NativeVerbatimStreaming(t *testing.T) {
	rec := httptest.NewRecorder()
	aw := newAnthropicResponseWriter(rec, "msg_ignored", "ignored")
	native := true
	aw.bindNativeFlag(&native)

	// Native streaming: SSE content-type + 200, forwarded byte-for-byte (not
	// translated), with Flush passing through.
	aw.Header().Set("Content-Type", "text/event-stream")
	aw.WriteHeader(http.StatusOK)
	frame := "event: message_start\ndata: {\"type\":\"message_start\"}\n\n"
	_, _ = aw.Write([]byte(frame))
	aw.Flush()
	aw.Finalize()

	if rec.Body.String() != frame {
		t.Errorf("verbatim stream mismatch:\n got %q\nwant %q", rec.Body.String(), frame)
	}
}

func TestAnthropicWriter_NativeErrorStillTranslated(t *testing.T) {
	rec := httptest.NewRecorder()
	aw := newAnthropicResponseWriter(rec, "msg_e", "m")
	native := true
	aw.bindNativeFlag(&native)

	// A native attempt that errors (status != 200) must NOT be forwarded verbatim
	// blindly; it goes through translation so the client gets a clean envelope.
	aw.Header().Set("Content-Type", "application/json")
	aw.WriteHeader(http.StatusInternalServerError)
	_, _ = aw.Write([]byte(`{"error":{"message":"upstream boom"}}`))
	aw.Finalize()

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", rec.Code)
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("invalid output: %v", err)
	}
	if m["type"] != "error" {
		t.Errorf("type = %v, want error", m["type"])
	}
	if m["error"].(map[string]any)["type"] != "api_error" {
		t.Errorf("error type = %v, want api_error (500)", m["error"])
	}
}

func TestAnthropicWriter_Error(t *testing.T) {
	rec := httptest.NewRecorder()
	aw := newAnthropicResponseWriter(rec, "msg_e", "p/m")

	// Pipeline writes an OpenAI error (e.g. rate limit).
	aw.Header().Set("Content-Type", "application/json")
	aw.WriteHeader(http.StatusTooManyRequests)
	_, _ = aw.Write([]byte(`{"error":{"message":"slow down","type":"rate_limit"}}`))
	aw.Finalize()

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want 429", rec.Code)
	}
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("invalid output: %v", err)
	}
	if m["type"] != "error" {
		t.Errorf("type = %v, want error", m["type"])
	}
	e := m["error"].(map[string]any)
	if e["type"] != "rate_limit_error" {
		t.Errorf("error type = %v, want rate_limit_error", e["type"])
	}
	if e["message"] != "slow down" {
		t.Errorf("message = %v", e["message"])
	}
}
