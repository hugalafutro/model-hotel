package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// Strix 2026-09-01 vuln-0002: the masker knew one secret, the attempted
// candidate's, so a relay quoting a different provider row's custom-format
// key left it in the row. The masker now masks every secret the process has
// decrypted.
func TestCredentialMasker_MasksAHeldForeignKey(t *testing.T) {
	const foreign = "custom-key-A-zzzz-11112222-masker"
	const own = "custom-key-B-9999-77776666-masker"
	util.HoldSecret(foreign)
	body := []byte(`{"error":{"message":"relay rejected; upstream said bad api key ` + foreign + ` while ours is ` + own + `"}}`)
	got := string(newCredentialMasker(own).mask(body))
	if strings.Contains(got, foreign) || strings.Contains(got, own) {
		t.Fatalf("a key survived: %q", got)
	}
	if !strings.Contains(got, `bad api key [redacted] while ours is [redacted]`) {
		t.Fatalf("got %q", got)
	}
	// The zero-value masker (no candidate yet) masks the held set too.
	if got := string(credentialMasker{}.maskExact([]byte("quoted " + foreign))); got != "quoted [redacted]" {
		t.Fatalf("zero-value masker: %q", got)
	}
}

// The raw-forwarding writer holds back enough for the longest held key, so a
// foreign key split across two writes is still masked whole.
func TestExactMaskWriter_HoldsBackForTheLongestHeldKey(t *testing.T) {
	const foreign = "custom-key-A-straddle-0123456789abcdefghij" // longer than the candidate's
	const own = "custom-key-B-own-0123"
	util.HoldSecret(foreign)
	var out bytes.Buffer
	w := newExactMaskWriter(&out, newCredentialMasker(own))
	text := "head " + foreign + " mid " + own + " tail"
	cut := len("head ") + len(foreign)/2
	if _, err := w.Write([]byte(text[:cut])); err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(text[cut:])); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != "head [redacted] mid [redacted] tail" {
		t.Fatalf("got %q", got)
	}
}

// The PoC end to end: provider A's key is decrypted by this process (as the
// startup warm and every discovery do), provider B's upstream quotes it in a
// 429, and the row stores neither key.
func TestCredentialMasker_ForeignKeyNeverReachesTheRow(t *testing.T) {
	const foreign = "custom-key-A-flow-11112222-33334444"
	kp, err := auth.Encrypt(foreign, "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if _, err := auth.DecryptCached(kp.Ciphertext, kp.Nonce, kp.Salt, "test-master-key-for-proxy-tests"); err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = io.WriteString(w, `{"error": {"message": "rate limit exceeded while processing: relay rejected; upstream said bad api key `+foreign+`", "type": "rate_limit_error", "code": "rate_limit_exceeded"}}`)
	}))
	defer upstream.Close()
	env := buildReplayEnv(t, upstream)
	body := `{"model": "hotel/` + env.group + `", "messages": [{"role": "user", "content": "hi"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "replay-key")
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.keyHash)
	w := httptest.NewRecorder()
	env.h.ChatCompletions(w, req.WithContext(ctx))
	if w.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d; body: %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), foreign) {
		t.Fatalf("the client response carries the foreign key: %s", w.Body.String())
	}
	modelID := "hotel/" + env.group
	attempts := waitForTrail(t, modelID, 2)
	var errMsg string
	if err := testDB.Pool().QueryRow(context.Background(), `SELECT error_message FROM request_logs WHERE model_id = $1 ORDER BY created_at DESC LIMIT 1`, modelID).Scan(&errMsg); err != nil {
		t.Fatalf("read error_message: %v", err)
	}
	if strings.Contains(errMsg, foreign) {
		t.Fatalf("error_message carries the foreign key: %q", errMsg)
	}
	if !strings.Contains(errMsg, "bad api key [redacted]") {
		t.Fatalf("error_message = %q, want the key redacted in place", errMsg)
	}
	for _, a := range attempts {
		if strings.Contains(a.Detail, foreign) {
			t.Fatalf("attempt %d detail carries the foreign key: %q", a.Attempt, a.Detail)
		}
	}
}
