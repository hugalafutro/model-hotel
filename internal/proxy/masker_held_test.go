package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// Strix 2026-09-01 vuln-0002: the masker knew one secret, the attempted
// candidate's, so a relay quoting a different provider row's custom-format
// key left it in the row. Error text now masks every held provider key.
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
	if got := string(credentialMasker{}.mask([]byte("quoted " + foreign))); got != "quoted [redacted]" {
		t.Fatalf("zero-value masker: %q", got)
	}
}

// Content keeps to the candidate's own key: a held foreign key is not
// rewritten out of an answer, and neither is a placeholder key held for a
// keyless local server.
func TestCredentialMasker_ContentKeepsToTheCandidate(t *testing.T) {
	const foreign = "custom-key-A-content-11112222-aaaa"
	util.HoldSecret(foreign)
	util.HoldSecret("not-needed")
	m := newCredentialMasker("custom-key-B-content-77776666-bbbb")
	answer := []byte("The API is open, so a key is not-needed; and here is " + foreign + " verbatim, plus custom-key-B-content-77776666-bbbb")
	got := string(m.maskExact(answer))
	if !strings.Contains(got, "not-needed") || !strings.Contains(got, foreign) {
		t.Fatalf("content lost a held value: %q", got)
	}
	if strings.Contains(got, "custom-key-B-content") {
		t.Fatalf("the candidate's own key survived in content: %q", got)
	}
	var out bytes.Buffer
	w := newExactMaskWriter(&out, m)
	_, _ = w.Write([]byte("raw " + foreign + " here"))
	_ = w.Flush()
	if out.String() != "raw "+foreign+" here" {
		t.Fatalf("the raw writer rewrote a held foreign key out of content: %q", out.String())
	}
}

// The union is masked longest first whichever side a key came from: a
// candidate key that is a prefix of a held key must not leave the held key's
// tail behind.
func TestCredentialMasker_HeldPrefixOrder(t *testing.T) {
	const short = "sk-prefixcase-abcdefgh"
	const long = short + "IJKLMNOPQRSTUVWX"
	util.HoldSecret(long)
	util.HoldSecret(short)
	got := string(newCredentialMasker(short).mask([]byte("upstream said: bad key " + long)))
	if got != "upstream said: bad key [redacted]" {
		t.Fatalf("got %q", got)
	}
	if got := util.MaskCredential(short, "upstream said: bad key "+long); got != "upstream said: bad key [redacted]" {
		t.Fatalf("util: got %q", got)
	}
}

// The PoC end to end from the provider table: provider A holds a custom key
// and is disabled (never called, never decrypted for a request); the seed
// registers it; provider B's upstream quotes it in a 429; the row stores
// neither key.
func TestCredentialMasker_ForeignKeyNeverReachesTheRow(t *testing.T) {
	const foreign = "custom-key-A-flow-11112222-33334444"
	const master = "test-master-key-for-proxy-tests"
	kp, err := auth.Encrypt(foreign, master)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	repo := provider.NewRepository(testDB.Pool())
	pa, err := repo.Create(context.Background(), provider.CreateProviderRequest{Name: "disabled-a-" + uuid.New().String()[:8], BaseURL: "http://disabled-a.upstream.test", APIKey: foreign}, kp.Ciphertext, kp.Nonce, kp.Salt)
	if err != nil {
		t.Fatalf("create provider A: %v", err)
	}
	if _, err := testDB.Pool().Exec(context.Background(), `UPDATE providers SET enabled = false WHERE id = $1`, pa.ID); err != nil {
		t.Fatalf("disable provider A: %v", err)
	}
	if held, _ := provider.HoldKeys(context.Background(), repo, master); held == 0 {
		t.Fatal("the seed held no keys")
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
