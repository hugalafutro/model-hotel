package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
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

// The wiring the unit tests cannot see: a stream whose error frame quotes
// the prompt (fenced through streamState.errLogAttr into the app log and
// through the write boundary into the row), and the terminal path's own log
// lines (failAllExhausted renders the last provider's text). Every captured
// log record is checked, so a new unfenced line fails here too.
func TestContentFence_StreamErrorFrameAndLogLines(t *testing.T) {
	const prompt = "FOURTH-CANARY streamed private data secret-project-name"
	logs := captureProxyLogs(t)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req struct {
			Messages []struct {
				Content string `json:"content"`
			} `json:"messages"`
		}
		_ = json.Unmarshal(body, &req)
		text := req.Messages[len(req.Messages)-1].Content
		if strings.Contains(r.Host, "one-slot") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = io.WriteString(w, `{"error": {"message": "rate limit exceeded while processing: `+text+`", "type": "rate_limit_error"}}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"error\":{\"message\":\"cannot continue: "+text+"\",\"type\":\"server_error\"}}\n\n")
	}))
	defer upstream.Close()
	env := buildReplayEnv(t, upstream)

	body := `{"model": "hotel/` + env.group + `", "messages": [{"role": "user", "content": "` + prompt + `"}], "stream": true}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "replay-key")
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.keyHash)
	w := httptest.NewRecorder()
	env.h.ChatCompletions(w, req.WithContext(ctx))

	modelID := "hotel/" + env.group
	attempts := waitForTrail(t, modelID, 2)
	var errMsg string
	if err := testDB.Pool().QueryRow(context.Background(), `SELECT error_message FROM request_logs WHERE model_id = $1 ORDER BY created_at DESC LIMIT 1`, modelID).Scan(&errMsg); err != nil {
		t.Fatalf("read error_message: %v", err)
	}
	if strings.Contains(errMsg, "CANARY") {
		t.Fatalf("error_message carries the prompt: %q", errMsg)
	}
	for _, a := range attempts {
		if strings.Contains(a.Detail, "CANARY") {
			t.Fatalf("attempt %d detail carries the prompt: %q", a.Attempt, a.Detail)
		}
	}
	if !strings.Contains(errMsg, "[content]") {
		t.Fatalf("the stream's error frame was not stored fenced: %q", errMsg)
	}
	sawFrame := false
	for _, rec := range logs.all() {
		for k, v := range rec.attrs {
			if strings.Contains(v, "CANARY") {
				t.Fatalf("app log %q attr %s carries the prompt: %q", rec.msg, k, v)
			}
			if strings.Contains(v, "cannot continue: [content]") {
				sawFrame = true
			}
		}
	}
	if !sawFrame {
		t.Fatal("no app-log line carried the fenced error frame; the stream log attribute is not being exercised")
	}
}

// The terminal paths' own log lines: the exhaustion line renders the last
// provider's error (every candidate streamed an error frame) and must arrive
// fenced; a 200 that would not decode logs Go's parse error and must simply
// not leak.
func TestContentFence_TerminalLogLines(t *testing.T) {
	const prompt = "FIFTH-CANARY terminal private data secret-project-name"
	for _, tc := range []struct {
		name, line string
		stream     bool
		status     int
		upstream   func(w http.ResponseWriter, text string)
	}{
		{"every candidate streams an error frame", "all providers exhausted", true, http.StatusBadGateway, func(w http.ResponseWriter, text string) {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"error\":{\"message\":\"cannot continue: "+text+"\",\"type\":\"server_error\"}}\n\n")
		}},
		// The decode-error detail carries Go's parse error, not the body, so
		// this shape has nothing to fence: it is here to prove nothing leaks.
		{"garbled 200 body", "", false, http.StatusBadGateway, func(w http.ResponseWriter, text string) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, "garbled response while processing: "+text)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logs := captureProxyLogs(t)
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				var req struct {
					Messages []struct {
						Content string `json:"content"`
					} `json:"messages"`
				}
				_ = json.Unmarshal(body, &req)
				tc.upstream(w, req.Messages[0].Content)
			}))
			defer upstream.Close()
			env := buildReplayEnv(t, upstream)
			body := `{"model": "hotel/` + env.group + `", "messages": [{"role": "user", "content": "` + prompt + `"}], "stream": ` + map[bool]string{false: "false", true: "true"}[tc.stream] + `}`
			req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
			ctx := context.WithValue(req.Context(), virtualKeyNameKey, "replay-key")
			ctx = context.WithValue(ctx, VirtualKeyHashKey, env.keyHash)
			w := httptest.NewRecorder()
			env.h.ChatCompletions(w, req.WithContext(ctx))
			if w.Code != tc.status {
				t.Fatalf("status = %d, want %d; body: %s", w.Code, tc.status, w.Body.String())
			}
			waitForTrail(t, "hotel/"+env.group, 1)
			saw := false
			for _, rec := range logs.all() {
				for k, v := range rec.attrs {
					if strings.Contains(v, "CANARY") {
						t.Fatalf("app log %q attr %s carries the prompt: %q", rec.msg, k, v)
					}
				}
				if tc.line != "" && strings.Contains(rec.msg, tc.line) {
					saw = true
					if !strings.Contains(rec.attrs["error"], "[content]") {
						t.Fatalf("%q does not carry the fenced provider text: %q", tc.line, rec.attrs["error"])
					}
				}
			}
			if tc.line != "" && !saw {
				t.Fatalf("%q was not logged", tc.line)
			}
		})
	}
}

// A multipart request's text fields are fenced: the transcription prompt
// quoted back by the upstream does not reach the row.
func TestContentFence_MultipartPromptEcho(t *testing.T) {
	const prompt = "SIXTH-CANARY multipart private data secret-project-name"
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, "bad multipart: "+err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error": {"message": "rate limit exceeded while processing: `+r.FormValue("prompt")+`"}}`)
	}))
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("model", env.providerName+"/"+env.modelName)
	_ = mw.WriteField("prompt", prompt)
	fw, _ := mw.CreateFormFile("file", "speech.wav")
	_, _ = fw.Write([]byte("RIFFfakewavdata"))
	_ = mw.Close()
	req := env.request("/v1/audio/transcriptions", mw.FormDataContentType(), &buf)
	w := httptest.NewRecorder()
	env.handler.AudioTranscriptions(w, req)
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body: %s", w.Code, w.Body.String())
	}
	attempts := waitForTrailByProvider(t, env.providerID, 1)
	var errMsg string
	if err := testDB.Pool().QueryRow(context.Background(), `SELECT error_message FROM request_logs WHERE provider_id = $1 ORDER BY created_at DESC LIMIT 1`, env.providerID).Scan(&errMsg); err != nil {
		t.Fatalf("read error_message: %v", err)
	}
	if strings.Contains(errMsg, "CANARY") {
		t.Fatalf("error_message carries the multipart prompt: %q", errMsg)
	}
	for _, a := range attempts {
		if strings.Contains(a.Detail, "CANARY") {
			t.Fatalf("attempt detail carries the multipart prompt: %q", a.Detail)
		}
	}
	if !strings.Contains(errMsg, "[content]") && !anyDetailContains(attempts, "[content]") {
		t.Fatalf("nothing on the row shows the fence ran: error_message %q attempts %+v", errMsg, attempts)
	}
}

func anyDetailContains(attempts []attemptRecord, s string) bool {
	for _, a := range attempts {
		if strings.Contains(a.Detail, s) {
			return true
		}
	}
	return false
}
