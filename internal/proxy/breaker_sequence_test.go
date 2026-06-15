package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/config"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// cbConsecutiveFails returns the recorded consecutive-failure count for a
// provider as observed via the circuit breaker's public Status(), plus whether
// the breaker has any circuit at all for that provider. A circuit only exists
// once a CB method (IsOpen/RecordFailure/RecordSuccess/GetState) has touched
// the provider, so `ok` doubles as "the breaker saw this provider".
func cbConsecutiveFails(cb *failover.CircuitBreaker, id uuid.UUID) (fails int, ok bool) {
	for _, s := range cb.Status() {
		if s.ProviderID == id.String() {
			return s.ConsecutiveFails, true
		}
	}
	return 0, false
}

// TestChatCompletions_CircuitBreakerSequence_FailoverThen200 pins the circuit
// breaker call sequence for a 2-candidate run where the first provider returns a
// failover-eligible 5xx and the second returns 200. This is the §10 safety-net
// test for the ChatCompletions/failover refactor: the CB calls (a RecordFailure
// on the 5xx provider, then a RecordSuccess on the 200 provider) are the easiest
// thing to silently break when the loop body is decomposed.
//
// To make the RecordSuccess observable (it resets consecutiveFails to 0, which
// is also the default), the success provider is pre-seeded with 2 failures
// (still Closed, since threshold is 5). After the request:
//   - provider1 must show consecutiveFails == 1  (exactly one RecordFailure)
//   - provider2 must show consecutiveFails == 0  (the seeded 2 were reset by the
//     RecordSuccess) and remain Closed
func TestChatCompletions_CircuitBreakerSequence_FailoverThen200(t *testing.T) {
	pool := testDB.Pool()
	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	// First provider returns 500 (failover-eligible 5xx → RecordFailure).
	upstream1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"message": "internal error", "type": "server_error"},
		})
	}))
	defer upstream1.Close()

	// Second provider succeeds (200 non-streaming → RecordSuccess).
	upstream2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-test", "object": "chat.completion", "created": time.Now().Unix(),
			"model": "success-model",
			"choices": []map[string]any{
				{"index": 0, "message": map[string]any{"role": "assistant", "content": "success"}, "finish_reason": "stop"},
			},
			"usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 7, "total_tokens": 12},
		})
	}))
	defer upstream2.Close()

	keyPair1, _ := auth.Encrypt("key1", "test-master-key-for-integration")
	keyPair2, _ := auth.Encrypt("key2", "test-master-key-for-integration")

	provider1Name := "cbseq-fail-" + uuid.New().String()[:8]
	provider1, _ := providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name: provider1Name, BaseURL: upstream1.URL, APIKey: "key1",
	}, keyPair1.Ciphertext, keyPair1.Nonce, keyPair1.Salt)

	provider2Name := "cbseq-ok-" + uuid.New().String()[:8]
	provider2, _ := providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name: provider2Name, BaseURL: upstream2.URL, APIKey: "key2",
	}, keyPair2.Ciphertext, keyPair2.Nonce, keyPair2.Salt)

	mkModel := func(id, provID uuid.UUID, provName string) *model.Model {
		return &model.Model{
			ID: id, ProviderID: provID, ModelID: "shared-model", Name: "Shared",
			Capabilities: "{}", Params: "{}", Modality: "chat",
			InputModalities: "[\"text\"]", OutputModalities: "[\"text\"]",
			Enabled: true, ProviderName: provName, ProviderEnabled: true,
		}
	}
	model1 := mkModel(uuid.New(), provider1.ID, provider1Name)
	model2 := mkModel(uuid.New(), provider2.ID, provider2Name)
	_ = modelRepo.Upsert(context.Background(), model1)
	_ = modelRepo.Upsert(context.Background(), model2)

	groupName := "cbseq-group-" + uuid.New().String()[:8]
	if _, err := failoverRepo.UpsertWithConfig(context.Background(), groupName,
		[]uuid.UUID{model1.ID, model2.ID},
		map[string]bool{model1.ID.String(): true, model2.ID.String(): true},
		nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}

	virtualKey, _ := virtualKeyRepo.Create(context.Background(), "test-key", virtualkey.Hash("cbseq-vk"), "sk-tes...", nil, nil, nil, nil, nil)
	defer func() { _ = virtualKeyRepo.Delete(context.Background(), virtualKey.ID) }()

	cb := failover.NewCircuitBreaker(settingsRepo)
	handler := &Handler{
		cfg:            &config.Config{MasterKey: "test-master-key-for-integration"},
		settingsRepo:   settingsRepo,
		failoverRepo:   failoverRepo,
		modelRepo:      modelRepo,
		providerRepo:   providerRepo,
		virtualKeyRepo: WrapVirtualKeyRepo(virtualKeyRepo),
		rateLimiter:    limiter,
		tpmLimiter:     ratelimit.NewTPMLimiter(settingsRepo),
		ipLimiter:      ipLimiter,
		circuitBreaker: cb,
		dbPool:         pool,
		upstreamTransport: &http.Transport{
			DialContext:           NewSafeDialer(append(config.KnownProviderHosts(), "127.0.0.1"), nil).DialContext,
			ResponseHeaderTimeout: 120 * time.Second,
			IdleConnTimeout:       120 * time.Second,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   20,
		},
		safeDialer: NewSafeDialer(nil, nil),
	}

	// Pre-seed the success provider with 2 failures so the expected
	// RecordSuccess (reset to 0) is distinguishable from "never called".
	// 2 < threshold (5), so the circuit stays Closed and the provider remains
	// a valid candidate, in group order, after provider1.
	cb.RecordFailure(provider2.ID, provider2Name)
	cb.RecordFailure(provider2.ID, provider2Name)
	if f, _ := cbConsecutiveFails(cb, provider2.ID); f != 2 {
		t.Fatalf("pre-seed sanity: expected provider2 consecutiveFails=2, got %d", f)
	}

	body := `{"model": "hotel/` + groupName + `", "messages": [{"role": "user", "content": "hi"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, virtualKey.ID.String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, virtualkey.Hash("cbseq-vk"))
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 after failover, got %d (body=%s)", w.Code, w.Body.String())
	}

	// provider1: exactly one RecordFailure for the 5xx.
	if f, ok := cbConsecutiveFails(cb, provider1.ID); !ok || f != 1 {
		t.Errorf("provider1: expected consecutiveFails=1 (one RecordFailure on 5xx), got %d (seen=%v)", f, ok)
	}
	// provider2: RecordSuccess reset the seeded failures to 0, still Closed.
	if f, ok := cbConsecutiveFails(cb, provider2.ID); !ok || f != 0 {
		t.Errorf("provider2: expected consecutiveFails=0 (RecordSuccess reset), got %d (seen=%v)", f, ok)
	}
	if st := cb.GetState(provider2.ID); st != failover.StateClosed {
		t.Errorf("provider2: expected StateClosed after success, got %s", st)
	}
}

// TestChatCompletions_CircuitBreakerSequence_400RetryThen200 pins the circuit
// breaker behaviour for the 400 param-stripping auto-retry path: a single
// candidate returns a param-rejection 400, the proxy strips the offending param
// and retries, and the retry returns 200. The CB contract here is a single
// RecordSuccess (the 400 itself records nothing — it is consumed inside the
// retry block). The success provider is pre-seeded with 2 failures to make the
// reset observable.
func TestChatCompletions_CircuitBreakerSequence_400RetryThen200(t *testing.T) {
	env := newTestProxyHandler(t)
	handler := env.Handler
	upstream := env.Upstream
	defer upstream.Close()

	var calls atomic.Int32
	upstream.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if n == 1 {
			// First call: reject top_p with a parseable param error.
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"message": "Unsupported parameter: `top_p` is not supported"},
			})
			return
		}
		// Retry: succeed.
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "chatcmpl-test", "object": "chat.completion", "created": time.Now().Unix(),
			"model": "retry-model",
			"choices": []map[string]any{
				{"index": 0, "message": map[string]any{"role": "assistant", "content": "ok"}, "finish_reason": "stop"},
			},
			"usage": map[string]any{"prompt_tokens": 5, "completion_tokens": 7, "total_tokens": 12},
		})
	})

	cb := handler.circuitBreaker
	// Pre-seed 2 failures (< threshold 5 → still Closed) so the expected
	// RecordSuccess on the 200 retry is observable as a reset to 0.
	cb.RecordFailure(env.ProviderID, env.ProviderName)
	cb.RecordFailure(env.ProviderID, env.ProviderName)

	body := `{"model": "` + env.ProviderName + `/` + env.ModelName + `", "stream": false, "messages": [{"role": "user", "content": "hi"}], "top_p": 0.9}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handler.ChatCompletions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 after 400-retry, got %d (body=%s)", w.Code, w.Body.String())
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected upstream to be called twice (400 then retry 200), got %d", got)
	}
	// The 400 records nothing; the 200 retry records a success → seeded 2 reset to 0.
	if f, ok := cbConsecutiveFails(cb, env.ProviderID); !ok || f != 0 {
		t.Errorf("expected consecutiveFails=0 after 400-retry success, got %d (seen=%v)", f, ok)
	}
	if st := cb.GetState(env.ProviderID); st != failover.StateClosed {
		t.Errorf("expected StateClosed after 400-retry success, got %s", st)
	}
}
