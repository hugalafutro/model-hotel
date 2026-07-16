package paramrewrite

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// decodeBody reads and JSON-decodes an upstream request body.
func decodeBody(t *testing.T, r *http.Request) map[string]interface{} {
	t.Helper()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	var m map[string]interface{}
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("unmarshal body %q: %v", b, err)
	}
	return m
}

func jsonHeaders(req *http.Request) { req.Header.Set("Content-Type", "application/json") }

// TestSelfHeal_MaxTokensRename is the regression test for the broken "Test
// model" button: an OpenAI gpt-5/o-series model rejects max_tokens with a
// rename directive; the executor must rename to max_completion_tokens (keeping
// the value) and retry, ending in a 200.
func TestSelfHeal_MaxTokensRename(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		body := decodeBody(t, r)
		if _, hasOld := body["max_tokens"]; hasOld {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead.","type":"invalid_request_error"}}`)
			return
		}
		v, ok := body["max_completion_tokens"]
		if !ok {
			t.Errorf("retry missing max_completion_tokens: %v", body)
		}
		if v != float64(10) {
			t.Errorf("value not preserved across rename: got %v want 10", v)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"Hi"}}]}`)
	}))
	defer srv.Close()

	base := []byte(`{"model":"gpt-5.4","messages":[{"role":"user","content":"hi"}],"max_tokens":10}`)
	resp, err := SelfHealChatCompletion(context.Background(), srv.Client(), srv.URL, "openai", "gpt-5.4", base, jsonHeaders)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("status = %d, want 200 (body %s)", resp.StatusCode, b)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2 (one 400 + one healed retry)", attempts)
	}
}

// TestSelfHeal_RejectedParamStripped covers the strip path: a provider rejects
// a sampling param by name; the executor strips it and retries to a 200.
func TestSelfHeal_RejectedParamStripped(t *testing.T) {
	var attempts int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		body := decodeBody(t, r)
		if _, has := body["temperature"]; has {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":{"message":"Unsupported value: `+"`temperature`"+` is not supported."}}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"Hi"}}]}`)
	}))
	defer srv.Close()

	base := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}],"temperature":0.7}`)
	resp, err := SelfHealChatCompletion(context.Background(), srv.Client(), srv.URL, "openai", "m", base, jsonHeaders)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
}

// TestSelfHeal_NonParam400NoRetry ensures a 400 that is not a param error is
// returned as-is with its body intact and does not trigger a second request.
func TestSelfHeal_NonParam400NoRetry(t *testing.T) {
	var attempts int
	const msg = `{"error":{"message":"You exceeded your quota."}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, msg)
	}))
	defer srv.Close()

	base := []byte(`{"model":"m","messages":[{"role":"user","content":"hi"}]}`)
	resp, err := SelfHealChatCompletion(context.Background(), srv.Client(), srv.URL, "openai", "m", base, jsonHeaders)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if attempts != 1 {
		t.Fatalf("attempts = %d, want 1 (no retry on non-param 400)", attempts)
	}
	b, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(b), "exceeded your quota") {
		t.Fatalf("original 400 body not preserved: %s", b)
	}
}
