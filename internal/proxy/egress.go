package proxy

import (
	"bytes"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/hugalafutro/model-hotel/internal/egress"
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
		resp.Body = bodyOver(nil, resp.Body)
		return err
	}
	translated, err := build(body, egress.NewChatCompletionID(), model, time.Now().Unix())
	if err != nil {
		resp.Body = bodyOver(nil, resp.Body)
		return err
	}
	resp.Body = bodyOver(translated, resp.Body)
	return nil
}

// readCappedBody reads a response body under a byte cap: cap+1 is read, and a
// body that reaches it comes back as oversized rather than truncated, so a
// caller never re-encodes a mutilated payload as a whole one. The oversized
// error is the caller's own limit, never a provider fault, which
// translationIsProviderFault knows.
//
// It does not close the body. The upstream body is the attempt's in-flight
// wrapper, and its close is what settles the attempt's slot, so closing here
// would settle it clean from the 2xx before the caller has judged whether the
// bytes translate. The slot is held past the read instead (holdSlotForVerdict)
// and the caller keeps the upstream close: bodyOver carries it onto whatever
// replaces the body, and a caller that replaces nothing closes resp.Body
// itself once its verdict is recorded.
func readCappedBody(resp *http.Response, limit int, oversized error) ([]byte, error) {
	holdSlotForVerdict(resp)
	body, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(body) > limit {
		return nil, oversized
	}
	return body, nil
}

// bodyOver puts bytes of this process's own in place of a fully read upstream
// body while keeping the upstream close, so whoever closes the response still
// releases the upstream connection and settles the attempt's in-flight slot.
func bodyOver(b []byte, upstream io.Closer) io.ReadCloser {
	return struct {
		io.Reader
		io.Closer
	}{bytes.NewReader(b), upstream}
}
