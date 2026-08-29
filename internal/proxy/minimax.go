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

// miniMaxEnvelopePossible reports whether a 200 with this content type could
// carry a base_resp envelope, and so whether it is worth reading any of it.
//
// A deny-list rather than a "must say json" allow-list. The types below cannot
// contain an envelope and must not be read: SSE carries none by protocol, and
// the multimodal pass-through routes audio, image and octet-stream answers
// through here, where buffering to look for a field the content type says is not
// there would defeat the streaming that path exists to do. A missing, empty or
// text/plain content type says nothing about the body, and an intermediary
// returning the envelope under one of those is the empty-200-forwarded-as-success
// bug this function exists to fix — an allow-list would quietly restore it.
//
// Reading a little of an unlabelled body is cheap because the read is bounded
// and prepended back: the worst case is 64 KiB held for one envelope check.
func miniMaxEnvelopePossible(contentType string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.Contains(ct, "text/event-stream"),
		strings.HasPrefix(ct, "audio/"),
		strings.HasPrefix(ct, "image/"),
		strings.HasPrefix(ct, "video/"),
		strings.HasPrefix(ct, "application/octet-stream"):
		return false
	}
	return true
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
	// Any success status, not a bare 200: a business error hidden inside a 2xx
	// envelope is exactly as invisible to the status-keyed paths downstream
	// whichever 2xx carries it, and since those paths now serve and meter every
	// 2xx, a relay answering 201 with a MiniMax refusal would otherwise be
	// billed to the caller as an answer.
	if resp == nil || providerType != "minimax" || !servedSuccessStatus(resp.StatusCode) {
		return resp
	}
	if !miniMaxEnvelopePossible(resp.Header.Get("Content-Type")) {
		return resp
	}

	// Bounded, because an envelope is a sentence and a JSON response is not
	// necessarily one: MiniMax returns base64 audio inside JSON and the image
	// endpoints can answer with megabytes of b64_json, so reading to the end
	// would hold all of it in memory and make TTFB wait for the last upstream
	// byte — on a path that otherwise caps its buffering at
	// passthroughJSONBufferCap and streams the remainder.
	//
	// One byte past the cap, so "the whole body is in hand" is something this can
	// know rather than assume.
	head, err := io.ReadAll(io.LimitReader(resp.Body, miniMaxEnvelopeCap+1))
	if err != nil || len(head) > miniMaxEnvelopeCap {
		// Either the body is bigger than any envelope, or it failed mid-read.
		// Both are handed back as a stream: the bytes already taken, then
		// whatever the connection does next.
		//
		// Prepending rather than replacing is what keeps a read error in the
		// stream. Discarding it would hand downstream a partial body as a
		// complete answer — a truncated 200, which the pass-through path would
		// then count as the model having answered.
		rest := resp.Body
		resp.Body = miniMaxRestoredBody{Reader: io.MultiReader(bytes.NewReader(head), rest), Closer: rest}
		return resp
	}

	// The whole body fits, so it is buffered and the upstream one is closed.
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
