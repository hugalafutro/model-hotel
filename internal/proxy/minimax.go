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
	if strings.Contains(resp.Header.Get("Content-Type"), "text/event-stream") {
		return resp
	}
	body, err := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(body))
	if err != nil {
		return resp
	}
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
