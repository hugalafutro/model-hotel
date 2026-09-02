package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The Strix 2026-09-01 vuln-0001 PoC, end to end through the real handler
// and the real request_logs row: a provider whose 429 quotes the prompt back.
// Failover variant first (the echo lands in attempts[0].detail while the
// request is served), then the terminal variant (error_message too).
func TestContentFence_EchoedPromptNeverReachesTheRow(t *testing.T) {
	const prompt = "THIRD-CANARY prompt private data secret-project-name"
	echo := func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &req)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error": {"message": "rate limit exceeded while processing: `+req.Messages[len(req.Messages)-1].Content+`", "type": "rate_limit_error", "code": "rate_limit_exceeded"}}`)
	}
	for _, tc := range []struct {
		name       string
		healthyOK  bool
		wantStatus int
	}{
		{"failover served", true, http.StatusOK},
		{"terminal 429", false, http.StatusTooManyRequests},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(r.Host, "one-slot") || !tc.healthyOK {
					echo(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, chatCompletionJSON("shared-model"))
			}))
			defer upstream.Close()
			env := buildReplayEnv(t, upstream)

			body := `{"model": "hotel/` + env.group + `", "messages": [{"role": "user", "content": "` + prompt + `"}], "stream": false}`
			req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
			ctx := context.WithValue(req.Context(), virtualKeyNameKey, "replay-key")
			ctx = context.WithValue(ctx, VirtualKeyHashKey, env.keyHash)
			w := httptest.NewRecorder()
			env.h.ChatCompletions(w, req.WithContext(ctx))
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, tc.wantStatus, w.Body.String())
			}

			modelID := "hotel/" + env.group
			want := 2
			if !tc.healthyOK {
				want = 3 // two 429s and the saturation retry of the last candidate
			}
			attempts := waitForTrail(t, modelID, want)
			if len(attempts) < 2 {
				t.Fatalf("attempts = %+v, want at least the echoing 429", attempts)
			}
			for _, a := range attempts {
				if strings.Contains(a.Detail, "CANARY") || strings.Contains(a.Detail, "secret-project") {
					t.Fatalf("attempt %d detail carries the prompt: %q", a.Attempt, a.Detail)
				}
			}
			if a := attempts[0]; a.Status != 429 || !strings.Contains(a.Detail, "rate limit exceeded while processing: [content]") {
				t.Fatalf("attempt 0 = %+v, want the provider's words with the echo fenced", a)
			}

			var errMsg string
			if err := testDB.Pool().QueryRow(context.Background(), `SELECT error_message FROM request_logs WHERE model_id = $1 ORDER BY created_at DESC LIMIT 1`, modelID).Scan(&errMsg); err != nil {
				t.Fatalf("read error_message: %v", err)
			}
			if strings.Contains(errMsg, "CANARY") || strings.Contains(errMsg, "secret-project") {
				t.Fatalf("error_message carries the prompt: %q", errMsg)
			}
			if !tc.healthyOK && errMsg == "" {
				t.Fatal("terminal 429 left no error message at all")
			}
		})
	}
}
