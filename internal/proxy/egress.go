package proxy

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

// chatCompletionBuilder turns one egress dialect's non-streaming success body
// into a chat.completion body. gemini.BuildChatCompletion and
// anthropicegress.BuildChatCompletion both have this shape.
type chatCompletionBuilder func(body []byte, id, model string, created int64) ([]byte, error)

// translateEgressResponseBody swaps a non-streaming native 200 body for its
// chat.completion translation so handleNonStreamingResponse can meter and
// forward it unchanged. The dialect differs only in the builder, so every
// egress adapter shares this body: read, translate, re-seat.
//
// resp.Body is always left readable — an empty reader on failure — so a caller
// that surfaces the error still hands the pipeline a closed-once, drainable
// response.
func translateEgressResponseBody(resp *http.Response, model string, build chatCompletionBuilder) error {
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
		return err
	}
	id := "chatcmpl-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	translated, err := build(body, id, model, time.Now().Unix())
	if err != nil {
		resp.Body = io.NopCloser(bytes.NewReader(nil))
		return err
	}
	resp.Body = io.NopCloser(bytes.NewReader(translated))
	return nil
}
