package proxy

import (
	"bytes"
	"errors"
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

// errEgressBodyOversized is the refusal of a native-dialect body past
// nonStreamingBodyCap: the gateway's own limit, never a provider fault.
var errEgressBodyOversized = errors.New("upstream response body exceeds the non-streaming cap")

// translateEgressResponseBody swaps a non-streaming native 200 body for its
// chat.completion translation so handleNonStreamingResponse can meter and
// forward it unchanged. The dialect differs only in the builder, so every egress
// adapter shares this body: read, translate, re-seat.
//
// resp.Body is always left readable, an empty reader on failure, so a caller
// that surfaces the error still hands the pipeline a closed-once, drainable
// response.
func translateEgressResponseBody(resp *http.Response, model string, build chatCompletionBuilder) error {
	// Bounded like the plain non-streaming read: cap+1 is read, and a body that
	// reaches it is refused with errEgressBodyOversized rather than translated,
	// so one upstream cannot hold more than the cap (twice over, original and
	// translated) per concurrent request. The refusal is this gateway's policy
	// and not the provider failing, which translationIsProviderFault knows.
	body, err := readCappedBody(resp, nonStreamingBodyCap, errEgressBodyOversized)
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

// readCappedBody reads and closes a response body under a byte cap: cap+1 is
// read, and a body that reaches it comes back as oversized rather than
// truncated, so a caller never re-encodes a mutilated payload as a whole one.
// The oversized error is the caller's own limit, never a provider fault, which
// translationIsProviderFault knows.
func readCappedBody(resp *http.Response, limit int, oversized error) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	_ = resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if len(body) > limit {
		return nil, oversized
	}
	return body, nil
}
