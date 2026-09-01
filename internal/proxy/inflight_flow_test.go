package proxy

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// inflightCountFor reads the live in-flight count and learned limit for one
// provider off the handler's limiter.
func inflightCountFor(h *Handler, providerID uuid.UUID) (limit, inflight int, tracked bool) {
	for _, s := range h.inflight.snapshot() {
		if s.ProviderID == providerID.String() {
			return s.Limit, s.Inflight, true
		}
	}
	return 0, 0, false
}

// Every serve path must return its slot on the response's last byte: a leaked
// count is a slow self-inflicted saturation. Non-streaming and streaming run
// the full ChatCompletions path; the pass-through path is driven at the
// attempt level below.
func TestInflight_SlotReleasedOnServePaths(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Upstream.Close()

	for _, stream := range []bool{false, true} {
		body := fmt.Sprintf(`{"model": "%s/%s", "messages": [{"role": "user", "content": "hello"}], "stream": %v}`, env.ProviderName, env.ModelName, stream)
		req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
		ctx := context.WithValue(req.Context(), virtualKeyNameKey, "test-key")
		ctx = context.WithValue(ctx, VirtualKeyHashKey, env.KeyHash)
		w := httptest.NewRecorder()
		env.Handler.ChatCompletions(w, req.WithContext(ctx))
		if w.Code != http.StatusOK {
			t.Fatalf("stream=%v status = %d, want 200", stream, w.Code)
		}
		if _, inflight, tracked := inflightCountFor(env.Handler, env.ProviderID); !tracked || inflight != 0 {
			t.Errorf("stream=%v inflight after completion = %d (tracked=%v), want 0: the slot must free on the last byte", stream, inflight, tracked)
		}
	}
}

// The pass-through attempt settles its slot too, on success and on a failed
// upstream alike.
func TestInflight_PassthroughAttemptSettlesSlot(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}]}`)
	}))
	defer srv.Close()
	h.upstreamTransport = dialToTestServer(t, srv)

	m := &model.Model{ID: uuid.New(), ModelID: "text-embedding-3-small", InputModalities: `["text"]`, OutputModalities: `["embedding"]`}
	cand := goneCandidateAt(m, "P", "http://p.example")
	st := &requestState{
		startTime: time.Now(), reqModel: "text-embedding-3-small",
		endpointPath:    "/embeddings",
		bodyBytes:       []byte(`{"model":"text-embedding-3-small","input":"hi"}`),
		failoverTimeout: 30 * time.Second,
		inflightEnabled: true,
		logData:         &requestLogData{modelID: "text-embedding-3-small", endpointType: endpointTypeEmbeddings},
	}
	out := h.attemptPassthroughCandidate(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/embeddings", http.NoBody), st, cand, 0, 1)
	if out != outcomeServed {
		t.Fatalf("outcome = %v, want served", out)
	}
	if _, inflight, tracked := inflightCountFor(h, cand.provider.ID); !tracked || inflight != 0 {
		t.Errorf("inflight after pass-through = %d (tracked=%v), want 0", inflight, tracked)
	}
}

// The operator's max_in_flight is a hard admission gate: with the only slot
// held, a request waits for it rather than failing, and a slot that never
// frees inside the bounded wait answers the honest saturated 429.
func TestChatCompletions_MaxInFlightCeiling(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Upstream.Close()
	h := env.Handler
	ctx := context.Background()

	one := 1
	if _, err := h.providerRepo.Update(ctx, env.ProviderID, provider.UpdateProviderRequest{MaxInFlight: provider.OptionalInt{Set: true, Value: &one}}, nil, nil, nil); err != nil {
		t.Fatalf("failed to set max_in_flight: %v", err)
	}

	t.Run("waits for the held slot and serves", func(t *testing.T) {
		if !h.inflight.tryAcquire(env.ProviderID, 1) {
			t.Fatal("setup: slot not acquired")
		}
		go func() {
			time.Sleep(50 * time.Millisecond)
			h.inflight.release(env.ProviderID, true, 0, 0)
		}()
		w := chatRequest(t, env)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 once the slot freed; body: %s", w.Code, w.Body.String())
		}
	})

	t.Run("a slot that never frees answers 429 with Retry-After", func(t *testing.T) {
		if err := h.settingsRepo.Set(ctx, "rate_limit_saturation_max_wait", "150ms"); err != nil {
			t.Fatalf("failed to shrink the wait: %v", err)
		}
		defer func() { _ = h.settingsRepo.Set(ctx, "rate_limit_saturation_max_wait", "60s") }()
		if !h.inflight.tryAcquire(env.ProviderID, 1) {
			t.Fatal("setup: slot not acquired")
		}
		defer h.inflight.release(env.ProviderID, true, 0, 0)

		w := chatRequest(t, env)
		if w.Code != http.StatusTooManyRequests {
			t.Fatalf("status = %d, want 429 for a provider at its ceiling; body: %s", w.Code, w.Body.String())
		}
		if w.Header().Get("Retry-After") == "" {
			t.Error("expected a Retry-After on the at-ceiling response")
		}
	})
}

// The scaled 2026-08-31 replay with the phrase table DELETED: a one-slot
// provider answering an unrecognisable 429 when busy, a healthy sibling behind
// it. The behavioural rules alone (recent-success fallback + the cut) must
// keep every request served and the busy provider's circuit closed — the
// spec's universality requirement: phrases are accelerators, never
// prerequisites.
func TestChatCompletions_Replay_PhraseTableEmptied(t *testing.T) {
	saved := rateLimitPhrases
	rateLimitPhrases = nil
	t.Cleanup(func() { rateLimitPhrases = saved })

	// One upstream serving both provider hosts, telling them apart by Host.
	var busySlots atomic.Int32
	var p1Rejected atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.Host, "one-slot") {
			if busySlots.Add(1) > 1 {
				busySlots.Add(-1)
				p1Rejected.Add(1)
				w.WriteHeader(http.StatusTooManyRequests)
				// Deliberately opaque: no phrase, no Retry-After.
				_, _ = io.WriteString(w, `{"error":{"code":"E_OPAQUE","message":"request denied"}}`)
				return
			}
			defer busySlots.Add(-1)
			time.Sleep(20 * time.Millisecond)
		}
		var reqBody map[string]any
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
		_, _ = io.WriteString(w, chatCompletionJSON(reqBody["model"].(string)))
	}))
	defer upstream.Close()

	env := buildReplayEnv(t, upstream)

	// Wave 0, sequential: the one-slot provider serves once, stamping the
	// recent success the fallback needs (exactly how a live fleet would have
	// served it before the burst).
	if w := replayRequest(t, env); w.Code != http.StatusOK {
		t.Fatalf("warm-up status = %d; body: %s", w.Code, w.Body.String())
	}

	// Three waves of four concurrent requests: the shape of the incident.
	var non200 atomic.Int32
	for range 3 {
		var wg sync.WaitGroup
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if w := replayRequest(t, env); w.Code != http.StatusOK {
					non200.Add(1)
					t.Errorf("request answered %d; body: %s", w.Code, w.Body.String())
				}
			}()
		}
		wg.Wait()
	}

	if got := non200.Load(); got != 0 {
		t.Fatalf("%d requests failed; the 2026-08-31 shape must produce zero client-visible errors", got)
	}
	if state := env.h.circuitBreaker.GetState(env.p1ID, "shared-model"); state == failover.StateOpen {
		t.Errorf("the one-slot provider's circuit is open: busy 429s were charged as failures (rejections drawn: %d)", p1Rejected.Load())
	}
}

type replayEnv struct {
	h       *Handler
	p1ID    uuid.UUID
	group   string
	keyHash string
}

// buildReplayEnv builds a two-provider failover group in priority order
// (one-slot first, healthy second) routed to the caller's upstream.
func buildReplayEnv(t *testing.T, upstream *httptest.Server) *replayEnv {
	t.Helper()
	pool := testDB.Pool()
	providerRepo := provider.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)

	keyPair, err := auth.Encrypt("k", "test-master-key-for-proxy-tests")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	suffix := uuid.New().String()[:8]
	p1, err := providerRepo.Create(context.Background(), provider.CreateProviderRequest{Name: "one-slot-" + suffix, BaseURL: "http://one-slot.upstream.test", APIKey: "k"}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("create p1: %v", err)
	}
	p2, err := providerRepo.Create(context.Background(), provider.CreateProviderRequest{Name: "healthy-" + suffix, BaseURL: "http://healthy.upstream.test", APIKey: "k"}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("create p2: %v", err)
	}
	mkModel := func(pid uuid.UUID, pname string) *model.Model {
		m := &model.Model{
			ID: uuid.New(), ProviderID: pid, ModelID: "shared-model", Name: "Shared",
			Capabilities: "{}", Params: "{}", Modality: "chat",
			InputModalities: `["text"]`, OutputModalities: `["text"]`,
			Enabled: true, ProviderName: pname, ProviderEnabled: true,
		}
		if err := modelRepo.Upsert(context.Background(), m); err != nil {
			t.Fatalf("upsert model: %v", err)
		}
		return m
	}
	m1 := mkModel(p1.ID, p1.Name)
	m2 := mkModel(p2.ID, p2.Name)

	group := "replay-" + suffix
	if _, err := failoverRepo.UpsertWithConfig(context.Background(), group, []uuid.UUID{m1.ID, m2.ID},
		map[string]bool{m1.ID.String(): true, m2.ID.String(): true}, nil, nil, nil, nil); err != nil {
		t.Fatalf("create group: %v", err)
	}

	keyName := "replay-key-" + suffix
	keyHash := virtualkey.Hash(keyName)
	if _, err := virtualKeyRepo.Create(context.Background(), keyName, keyHash, "sk-rep...", nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("create vk: %v", err)
	}

	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	provider.InvalidateProviderCache()
	target := upstream.Listener.Addr().String()
	h.upstreamTransport = &http.Transport{
		DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, target)
		},
	}
	return &replayEnv{h: h, p1ID: p1.ID, group: group, keyHash: keyHash}
}

func replayRequest(t *testing.T, env *replayEnv) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"model": "hotel/` + env.group + `", "messages": [{"role": "user", "content": "hi"}], "stream": false}`
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(body))
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "replay-key")
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.keyHash)
	w := httptest.NewRecorder()
	env.h.ChatCompletions(w, req.WithContext(ctx))
	return w
}

// A MiniMax business error dressed as a 200 must not count as a clean
// completion: the slot rides the body only after the status remap, so the
// remapped saturated 429 cuts the window instead of growing it.
func TestInflight_MiniMaxEnvelopeIsNotACleanCompletion(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"base_resp":{"status_code":1002,"status_msg":"rate limit exceeded"}}`)
	}))
	defer srv.Close()
	h.upstreamTransport = dialToTestServer(t, srv)

	m := &model.Model{ID: uuid.New(), ModelID: "MiniMax-Text-01", InputModalities: `["text"]`, OutputModalities: `["embedding"]`}
	cand := goneCandidateAt(m, "MiniMax", "http://api.minimax.io/v1")
	st := &requestState{
		startTime: time.Now(), reqModel: "MiniMax-Text-01",
		endpointPath:    "/embeddings",
		bodyBytes:       []byte(`{"model":"MiniMax-Text-01","input":"hi"}`),
		failoverTimeout: 30 * time.Second,
		inflightEnabled: true,
		logData:         &requestLogData{modelID: "MiniMax-Text-01", endpointType: endpointTypeEmbeddings},
	}
	_ = h.attemptPassthroughCandidate(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/embeddings", http.NoBody), st, cand, 0, 1)

	limit, inflight, tracked := inflightCountFor(h, cand.provider.ID)
	if !tracked || inflight != 0 {
		t.Fatalf("inflight = %d (tracked=%v), want 0", inflight, tracked)
	}
	if limit != 1 {
		t.Errorf("limit = %d, want the cut to 1: a refusal inside a 200 grew the window instead", limit)
	}
}

// A hedged race whose every candidate sits at its in-flight window must wait
// for the first freed slot and serve there, exactly as the sequential loop
// does: turning hedging on must never make a merely-busy group fail faster.
func TestRunHedgedStreaming_AllBusyWaitsForASlot(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n\n")
	}))
	t.Cleanup(srv.Close)
	h.upstreamTransport = dialToTestServer(t, srv)

	one := 1
	mkCand := func(name string) modelCandidate {
		return modelCandidate{
			model:    &model.Model{ID: uuid.New(), ModelID: "m-" + name},
			provider: &provider.Provider{ID: uuid.New(), Name: name, BaseURL: "http://" + name + ".upstream.test", MaxInFlight: &one},
		}
	}
	cands := []modelCandidate{mkCand("a"), mkCand("b")}
	for _, c := range cands {
		if !h.inflight.tryAcquire(c.provider.ID, 1) {
			t.Fatal("setup: slot not acquired")
		}
	}
	t.Cleanup(func() {
		for _, c := range cands {
			h.inflight.release(c.provider.ID, true, 0, 0)
		}
	})

	// Every probe reports busy, as the real probe does at a full window.
	hh := newHedgeHarness([]fakeProbeSpec{
		{reqErr: reqError{Kind: KindProviderSaturated, Attempt: 0, Provider: "a", Detail: "at in-flight limit"}, busy: true},
		{reqErr: reqError{Kind: KindProviderSaturated, Attempt: 1, Provider: "b", Detail: "at in-flight limit"}, busy: true},
	})
	st, _ := newHedgeState(5 * time.Millisecond)
	st.inflightEnabled = true
	st.bodyBytes = []byte(`{"model":"test-model","messages":[{"role":"user","content":"hi"}],"stream":true}`)

	// Candidate b's slot frees while the orchestrator would otherwise be
	// writing the all-busy error.
	go func() {
		time.Sleep(30 * time.Millisecond)
		h.inflight.release(cands[1].provider.ID, true, 0, 0)
	}()

	w := runHedge(context.Background(), h, hh, st, cands)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want the freed slot to serve the stream; body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "data:") {
		t.Errorf("body %q is not the served stream", w.Body.String())
	}
}
