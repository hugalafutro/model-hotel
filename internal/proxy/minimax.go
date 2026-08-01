package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// miniMaxStatusToHTTP maps MiniMax base_resp business codes onto the HTTP
// status they stand for. Rate-limit (1002), token-limit (1039), and
// insufficient-balance (1008) all map to 429: each means "this provider cannot
// serve the request right now", the retry-elsewhere semantic the failover path
// gives 429. 1004 is auth rejection. Anything else maps to 502 so it stays
// failover-eligible as a generic upstream failure.
var miniMaxStatusToHTTP = map[int]int{
	1002: http.StatusTooManyRequests,
	1039: http.StatusTooManyRequests,
	1008: http.StatusTooManyRequests,
	1004: http.StatusUnauthorized,
}

// miniMaxEnvelopeCap bounds how much of a 200 body is read looking for the
// base_resp envelope. The envelope is a status code and a message; a body that
// has not produced one in 64 KiB is an answer rather than a refusal, and is left
// to stream.
const miniMaxEnvelopeCap = 64 << 10

// miniMaxRestoredBody is the body handed back after the envelope check: the
// bytes already read, then the rest of the upstream stream, closing the real one.
type miniMaxRestoredBody struct {
	io.Reader
	io.Closer
}

// remapMiniMaxBusinessError converts a MiniMax "HTTP 200 base_resp error"
// response into one carrying the equivalent HTTP status, so the failover,
// circuit-breaker, and error-forwarding paths — all keyed on status codes — see
// the failure. MiniMax returns a real 200 whose JSON envelope carries
// base_resp.status_code != 0 for rate limits, an exhausted Token Plan balance,
// and auth failures; left untouched the proxy would forward an empty 200 to the
// client and never fail over.
//
// Only non-streaming JSON responses from a minimax-typed provider are inspected.
// Streaming (SSE) responses carry no base_resp envelope and are left untouched,
// as are genuine successes (base_resp.status_code == 0) and any body that fails
// to parse — all with their bytes restored so downstream forwarding sees the
// original response.
func remapMiniMaxBusinessError(providerType, providerName string, resp *http.Response) *http.Response {
	if resp == nil || providerType != "minimax" || resp.StatusCode != http.StatusOK {
		return resp
	}
	// Streaming responses carry no base_resp envelope; never consume their body.
	// Neither does anything that is not JSON — this function reads bodies, and
	// the multimodal pass-through routes audio and image responses through it.
	// Buffering an audio/mpeg answer to look for a field its content type says
	// cannot be there would defeat the streaming that path exists to do.
	contentType := resp.Header.Get("Content-Type")
	if strings.Contains(contentType, "text/event-stream") || !strings.Contains(contentType, "json") {
		return resp
	}

	// Bounded, because an envelope is a sentence and a JSON response is not
	// necessarily one. MiniMax returns base64 audio inside JSON and the image
	// endpoints can answer with megabytes of b64_json, so reading to the end
	// would hold all of it in memory and make TTFB wait for the last upstream
	// byte — on a path that otherwise caps its buffering at
	// passthroughJSONBufferCap and streams the remainder.
	//
	// One byte past the cap, so "the whole body is in hand" is something this
	// can know rather than assume.
	head, err := io.ReadAll(io.LimitReader(resp.Body, miniMaxEnvelopeCap+1))
	if err != nil || len(head) > miniMaxEnvelopeCap {
		// Either the body is bigger than any envelope, or it failed mid-read.
		// Both are handed back as a stream: the bytes already taken, then
		// whatever the connection does next.
		//
		// Prepending rather than replacing is what fixes the read error the old
		// shape swallowed. It discarded the error and handed downstream the
		// partial body as a complete answer — a truncated 200, which the
		// pass-through path would then also count as the model having answered.
		// Here the failure is still in the stream, so it surfaces where the
		// handlers have always dealt with it.
		rest := resp.Body
		resp.Body = miniMaxRestoredBody{Reader: io.MultiReader(bytes.NewReader(head), rest), Closer: rest}
		return resp
	}

	// The whole body fits, so it is buffered and the upstream one is closed —
	// which is what this function has always done, and all it ever needed to do
	// for the chat path that has JSON answers of ordinary size.
	body := head
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	var envelope struct {
		BaseResp *struct {
			StatusCode int    `json:"status_code"`
			StatusMsg  string `json:"status_msg"`
		} `json:"base_resp"`
	}
	if json.Unmarshal(body, &envelope) != nil || envelope.BaseResp == nil || envelope.BaseResp.StatusCode == 0 {
		return resp
	}
	mapped, ok := miniMaxStatusToHTTP[envelope.BaseResp.StatusCode]
	if !ok {
		mapped = http.StatusBadGateway
	}
	debuglog.Warn("proxy: minimax business error inside HTTP 200",
		"provider", providerName,
		"minimax_status", envelope.BaseResp.StatusCode,
		"mapped_status", mapped,
		"msg", envelope.BaseResp.StatusMsg)
	resp.StatusCode = mapped
	resp.Status = http.StatusText(mapped)
	return resp
}
