package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// readFlagReader wraps a reader so a test can assert whether the body was ever
// read. remapMiniMaxBusinessError must NOT consume the body of responses it
// leaves untouched (non-minimax providers, streaming SSE), so a flipped flag on
// those paths is a bug.
type readFlagReader struct {
	r    io.Reader
	read bool
}

func (rr *readFlagReader) Read(p []byte) (int, error) {
	rr.read = true
	return rr.r.Read(p)
}

func (rr *readFlagReader) Close() error { return nil }

func minimaxTestResp(status int, contentType, body string) (*http.Response, *readFlagReader) {
	rr := &readFlagReader{r: strings.NewReader(body)}
	h := make(http.Header)
	if contentType != "" {
		h.Set("Content-Type", contentType)
	}
	return &http.Response{StatusCode: status, Header: h, Body: rr}, rr
}

// 1. Non-minimax provider type is a no-op: status stays 200 and the body is
// never touched (so the real streaming/non-streaming handlers get it intact).
func TestRemapMiniMaxBusinessError_NonMiniMaxUntouched(t *testing.T) {
	resp, rr := minimaxTestResp(http.StatusOK, "application/json",
		`{"base_resp":{"status_code":1008,"status_msg":"insufficient balance"}}`)
	out := remapMiniMaxBusinessError("openai", "some-provider", resp)
	if out != resp {
		t.Fatalf("expected same response pointer returned")
	}
	if out.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (untouched)", out.StatusCode)
	}
	if rr.read {
		t.Errorf("body was consumed for a non-minimax provider")
	}
}

// 2. A minimax streaming (SSE) 200 is left untouched and its body is not read;
// base_resp errors only appear in the non-streaming JSON envelope.
func TestRemapMiniMaxBusinessError_StreamingUntouched(t *testing.T) {
	resp, rr := minimaxTestResp(http.StatusOK, "text/event-stream",
		`data: {"choices":[{"delta":{"content":"hi"}}]}`)
	out := remapMiniMaxBusinessError("minimax", "mm", resp)
	if out.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200 (SSE untouched)", out.StatusCode)
	}
	if rr.read {
		t.Errorf("body was consumed for a streaming response")
	}
}

// 3. A minimax non-streaming 200 whose base_resp reports insufficient balance
// (1008) is remapped to 429, and the original body is restored so downstream
// error-forwarding still sees the message.
func TestRemapMiniMaxBusinessError_InsufficientBalanceTo429(t *testing.T) {
	resp, _ := minimaxTestResp(http.StatusOK, "application/json",
		`{"base_resp":{"status_code":1008,"status_msg":"insufficient balance"}}`)
	out := remapMiniMaxBusinessError("minimax", "mm", resp)
	if out.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", out.StatusCode)
	}
	body, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatalf("read restored body: %v", err)
	}
	if !strings.Contains(string(body), "insufficient balance") {
		t.Errorf("restored body missing message: %s", body)
	}
}

// 4. Auth rejection (1004) maps to 401; an unmapped business code (1013) maps to
// the generic failover-eligible 502.
func TestRemapMiniMaxBusinessError_AuthAndUnmapped(t *testing.T) {
	authResp, _ := minimaxTestResp(http.StatusOK, "application/json",
		`{"base_resp":{"status_code":1004,"status_msg":"invalid api key"}}`)
	if got := remapMiniMaxBusinessError("minimax", "mm", authResp).StatusCode; got != http.StatusUnauthorized {
		t.Errorf("1004 -> %d, want 401", got)
	}

	unmappedResp, _ := minimaxTestResp(http.StatusOK, "application/json",
		`{"base_resp":{"status_code":1013,"status_msg":"internal error"}}`)
	if got := remapMiniMaxBusinessError("minimax", "mm", unmappedResp).StatusCode; got != http.StatusBadGateway {
		t.Errorf("1013 -> %d, want 502", got)
	}
}

// 5. A genuine success (base_resp.status_code == 0) stays 200 and the body reads
// back byte-for-byte identical.
func TestRemapMiniMaxBusinessError_SuccessPassthrough(t *testing.T) {
	payload := `{"id":"x","choices":[{"message":{"content":"ok"}}],"base_resp":{"status_code":0,"status_msg":""}}`
	resp, _ := minimaxTestResp(http.StatusOK, "application/json", payload)
	out := remapMiniMaxBusinessError("minimax", "mm", resp)
	if out.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (success)", out.StatusCode)
	}
	body, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatalf("read restored body: %v", err)
	}
	if string(body) != payload {
		t.Errorf("body altered:\n got %s\nwant %s", body, payload)
	}
}

// 6. An unparseable body leaves the status at 200 and restores the bytes.
func TestRemapMiniMaxBusinessError_InvalidJSONPassthrough(t *testing.T) {
	resp, _ := minimaxTestResp(http.StatusOK, "application/json", `not json`)
	out := remapMiniMaxBusinessError("minimax", "mm", resp)
	if out.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (invalid JSON)", out.StatusCode)
	}
	body, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatalf("read restored body: %v", err)
	}
	if string(body) != "not json" {
		t.Errorf("body altered: %s", body)
	}
}

// TestChatCompletions_MiniMaxBusinessErrorFailsOver proves the seam: a failover
// group whose first candidate is a minimax-typed provider returning a real HTTP
// 200 with base_resp.status_code 1008 (exhausted Token Plan balance) fails over
// to the second candidate, which serves a genuine completion. Without the remap
// the proxy would forward the empty 200 and never try the backup.
//
// Mirrors the type-pinned transport-rewrite pattern in gemini_egress_test.go:
// provider base URLs keep real hostnames (so DetectProviderType classifies the
// first as "minimax"), while a bare transport's DialContext routes every TCP
// connection to a single fake upstream that branches on the request Host.
func TestChatCompletions_MiniMaxBusinessErrorFailsOver(t *testing.T) {
	pool := testDB.Pool()
	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if strings.Contains(r.Host, "minimax") {
			// MiniMax "HTTP 200 business error": exhausted Token Plan balance.
			_ = json.NewEncoder(w).Encode(map[string]any{
				"base_resp": map[string]any{"status_code": 1008, "status_msg": "insufficient balance"},
			})
			return
		}
		// Backup candidate: a genuine completion.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-backup",
			"object":  "chat.completion",
			"created": 1780272000,
			"model":   "backup-model",
			"choices": []map[string]any{
				{"index": 0, "message": map[string]any{"role": "assistant", "content": "served by backup"}, "finish_reason": "stop"},
			},
			"usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 7, "total_tokens": 12},
		})
	}))
	defer upstream.Close()

	keyPair1, _ := auth.Encrypt("key1", "test-master-key-for-integration")
	keyPair2, _ := auth.Encrypt("key2", "test-master-key-for-integration")

	provider1Name := "minimax-provider-" + uuid.New().String()[:8]
	provider1, err := providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    provider1Name,
		BaseURL: "http://api.minimax.io",
		APIKey:  "key1",
	}, keyPair1.Ciphertext, keyPair1.Nonce, keyPair1.Salt)
	if err != nil {
		t.Fatalf("create minimax provider: %v", err)
	}

	provider2Name := "backup-provider-" + uuid.New().String()[:8]
	provider2, err := providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    provider2Name,
		BaseURL: "http://backup.upstream.test",
		APIKey:  "key2",
	}, keyPair2.Ciphertext, keyPair2.Nonce, keyPair2.Salt)
	if err != nil {
		t.Fatalf("create backup provider: %v", err)
	}
	// Sanity: the pinned types must be what the seam keys on.
	if got := provider.DetectProviderType("http://api.minimax.io"); got != "minimax" {
		t.Fatalf("provider1 detects as %q, want minimax", got)
	}
	if got := provider.DetectProviderType("http://backup.upstream.test"); got == "minimax" {
		t.Fatalf("provider2 must not detect as minimax")
	}

	model1 := &model.Model{
		ID: uuid.New(), ProviderID: provider1.ID, ModelID: "shared-model", Name: "MiniMax Shared",
		Capabilities: "{}", Params: "{}", Modality: "chat",
		InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Enabled: true, ProviderName: provider1Name, ProviderEnabled: true,
	}
	model2 := &model.Model{
		ID: uuid.New(), ProviderID: provider2.ID, ModelID: "shared-model", Name: "Backup Shared",
		Capabilities: "{}", Params: "{}", Modality: "chat",
		InputModalities: `["text"]`, OutputModalities: `["text"]`,
		Enabled: true, ProviderName: provider2Name, ProviderEnabled: true,
	}
	if err := modelRepo.Upsert(context.Background(), model1); err != nil {
		t.Fatalf("upsert model1: %v", err)
	}
	if err := modelRepo.Upsert(context.Background(), model2); err != nil {
		t.Fatalf("upsert model2: %v", err)
	}

	groupName := "minimax-failover-" + uuid.New().String()[:8]
	if _, err := failoverRepo.UpsertWithConfig(context.Background(), groupName,
		[]uuid.UUID{model1.ID, model2.ID},
		map[string]bool{model1.ID.String(): true, model2.ID.String(): true},
		nil, nil, nil, nil); err != nil {
		t.Fatalf("create failover group: %v", err)
	}

	virtualKey, _ := virtualKeyRepo.Create(context.Background(), "test-key", virtualkey.Hash("mm-vk-failover"), "sk-tes...", nil, nil, nil, nil, nil, nil)
	defer func() { _ = virtualKeyRepo.Delete(context.Background(), virtualKey.ID) }()

	handler := newCanonicalHandler(t, "test-master-key-for-integration", pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, limiter, ipLimiter)

	// Route every upstream TCP connection to the single fake server, regardless
	// of the provider's real base-URL host.
	provider.InvalidateProviderCache()
	target := upstream.Listener.Addr().String()
	handler.upstreamTransport = &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, target)
		},
	}
	cb := handler.circuitBreaker

	body := `{"model": "hotel/` + groupName + `", "messages": [{"role": "user", "content": "hi"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, virtualKey.ID.String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, virtualkey.Hash("mm-vk-failover"))
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 after failover, got %d (body=%s)", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response: %v (body=%s)", err, w.Body.String())
	}
	if resp["model"] != "backup-model" {
		t.Errorf("expected backup completion, got model=%v", resp["model"])
	}
	choices, _ := resp["choices"].([]any)
	if len(choices) == 0 {
		t.Fatalf("no choices in response: %s", w.Body.String())
	}
	content := choices[0].(map[string]any)["message"].(map[string]any)["content"]
	if content != "served by backup" {
		t.Errorf("content = %v, want 'served by backup'", content)
	}

	// The remapped 429 must have recorded a breaker failure against the minimax
	// candidate (failover_on_rate_limit defaults true).
	if f, ok := cbConsecutiveFails(cb, provider1.ID); !ok || f != 1 {
		t.Errorf("minimax candidate: expected consecutiveFails=1, got %d (seen=%v)", f, ok)
	}
}
