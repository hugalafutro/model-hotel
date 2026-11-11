package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The two 400s OpenCode Go answered on 2026-11-11 each heal in one retry:
// Kimi K3.1 Pro refuses any temperature but 1 without quoting the param, and
// OpenCode's Gemini 4 Argon translation refuses a message's name. The retry
// body the upstream receives is the proof: the refused field is gone and the
// rest of the request is intact.
func TestRetryWithStrippedParams_OpenCodeGoQuirks(t *testing.T) {
	for _, tc := range []struct {
		name     string
		body     string
		reject   string
		checkOut func(t *testing.T, sent map[string]any)
	}{
		{
			name:   "kimi unquoted temperature",
			body:   `{"model":"kimi-k3.1-pro","messages":[{"role":"user","content":"hi"}],"temperature":0,"max_tokens":50}`,
			reject: `{"error":{"message":"Upstream request failed: [invalid_request_error] invalid temperature: only 1 is allowed for this model","type":"invalid_request_error"}}`,
			checkOut: func(t *testing.T, sent map[string]any) {
				if _, has := sent["temperature"]; has {
					t.Errorf("temperature still sent: %v", sent)
				}
				if sent["max_tokens"] == nil {
					t.Errorf("max_tokens lost: %v", sent)
				}
			},
		},
		{
			name:   "argon message name",
			body:   `{"model":"gemini-4-argon","messages":[{"role":"user","content":"temp?"},{"role":"assistant","content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"get_temperature","arguments":"{}"}}]},{"role":"tool","tool_call_id":"c1","name":"get_temperature","content":"-7"}]}`,
			reject: `{"type":"error","error":{"type":"invalid_request_error","message":"messages[2].name: google cannot preserve named conversation participants"}}`,
			checkOut: func(t *testing.T, sent map[string]any) {
				msgs, _ := sent["messages"].([]any)
				if len(msgs) != 3 {
					t.Fatalf("messages = %v", sent["messages"])
				}
				if _, has := msgs[2].(map[string]any)["name"]; has {
					t.Errorf("tool message name still sent: %v", msgs[2])
				}
				if _, has := msgs[1].(map[string]any)["tool_calls"]; !has {
					t.Errorf("assistant tool_calls lost: %v", msgs[1])
				}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var sent map[string]any
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, _ := io.ReadAll(r.Body)
				if err := json.Unmarshal(b, &sent); err != nil {
					t.Errorf("retry body is not JSON: %s", b)
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"id":"x","object":"chat.completion","choices":[]}`)
			}))
			defer upstream.Close()

			h := newRetryTestHandler()
			st := newRetryTestState()
			st.bodyBytes = []byte(tc.body)
			cand := newRetryTestCandidate(upstream.URL)
			_, cancel := context.WithCancel(context.Background())
			req := httptest.NewRequest("POST", "/v1/chat/completions", http.NoBody)
			var dialMs float64

			res := h.retryWithStrippedParams(req, st, cand, "opencode-go", upstream.URL,
				resp400(tc.reject), 0, &dialMs, cancel, "failover_timeout")
			if res.retryCancel != nil {
				defer res.retryCancel()
			}
			if res.resp != nil && res.resp.Body != nil {
				defer func() { _ = res.resp.Body.Close() }()
			}
			if !res.retried || res.resp == nil || res.resp.StatusCode != http.StatusOK {
				t.Fatalf("expected a successful retry, got retried=%v resp=%+v err=%+v", res.retried, res.resp, res.lastReqErr)
			}
			tc.checkOut(t, sent)
		})
	}
}
