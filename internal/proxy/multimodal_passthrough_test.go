package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/ratelimit"
	"github.com/hugalafutro/model-hotel/internal/settings"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// multimodalTestEnv holds the fixtures for multimodal pass-through tests:
// a fully-wired Handler plus one provider/model pair pointing at a
// caller-supplied upstream.
type multimodalTestEnv struct {
	handler      *Handler
	upstream     *httptest.Server
	providerID   uuid.UUID
	modelUUID    uuid.UUID
	keyHash      string
	providerName string
	modelName    string
}

// newMultimodalEnv builds the standard test environment around the given
// upstream handler: provider + model + virtual key + canonical proxy Handler.
func newMultimodalEnv(t *testing.T, upstreamHandler http.Handler) *multimodalTestEnv {
	t.Helper()
	return newMultimodalEnvWith(t, upstreamHandler, "[]")
}

// newMultimodalEnvWith is the same environment with the model's declared output
// modalities under the test's control.
//
// It exists because a gone-classified refusal on /embeddings only counts against
// a model the catalog says produces embeddings (see modalityRulesOutSurface), so
// a test about embeddings retirement has to describe an embeddings model rather
// than the "[]" every uncatalogued row carries.
func newMultimodalEnvWith(t *testing.T, upstreamHandler http.Handler, outputModalities string) *multimodalTestEnv {
	t.Helper()
	pool := testDB.Pool()
	settingsRepo := settings.NewRepository(pool)
	failoverRepo := failover.NewRepository(pool)
	modelRepo := model.NewRepository(pool)
	providerRepo := provider.NewRepository(pool)
	virtualKeyRepo := virtualkey.NewRepository(pool)
	limiter := ratelimit.NewLimiter(settingsRepo)
	ipLimiter := ratelimit.NewIPLimiter(30, 60, nil, nil)

	upstream := httptest.NewServer(upstreamHandler)
	t.Cleanup(upstream.Close)

	providerName, providerID, modelUUID, modelName := createMultimodalProviderWith(t, upstream.URL, outputModalities)

	virtualKeyName := "mm-key-" + uuid.New().String()[:8]
	keyHash := virtualkey.Hash(virtualKeyName)
	if _, err := virtualKeyRepo.Create(context.Background(), virtualKeyName, keyHash, "mm-"+keyHash[:8], nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create virtual key: %v", err)
	}

	handler := newCanonicalHandler(t, "test-master-key-for-integration", pool, settingsRepo, failoverRepo, modelRepo, providerRepo, virtualKeyRepo, limiter, ipLimiter)

	return &multimodalTestEnv{
		handler:      handler,
		upstream:     upstream,
		providerID:   providerID,
		modelUUID:    modelUUID,
		keyHash:      keyHash,
		providerName: providerName,
		modelName:    modelName,
	}
}

// createMultimodalProvider registers a provider pointing at baseURL and one
// enabled model under it. Returns the generated names/IDs.
func createMultimodalProvider(t *testing.T, baseURL string) (providerName string, providerID, modelUUID uuid.UUID, modelName string) {
	t.Helper()
	return createMultimodalProviderWith(t, baseURL, "[]")
}

// createMultimodalProviderWith is the same, with the model's declared output
// modalities under the caller's control.
func createMultimodalProviderWith(t *testing.T, baseURL, outputModalities string) (providerName string, providerID, modelUUID uuid.UUID, modelName string) {
	t.Helper()
	pool := testDB.Pool()
	providerRepo := provider.NewRepository(pool)
	modelRepo := model.NewRepository(pool)

	keyPair, err := auth.Encrypt("test-api-key", "test-master-key-for-integration")
	if err != nil {
		t.Fatalf("failed to encrypt API key: %v", err)
	}
	providerName = "mm-provider-" + uuid.New().String()[:8]
	createdProvider, err := providerRepo.Create(context.Background(), provider.CreateProviderRequest{
		Name:    providerName,
		BaseURL: baseURL,
		APIKey:  "test-api-key",
	}, keyPair.Ciphertext, keyPair.Nonce, keyPair.Salt)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	providerID = createdProvider.ID

	modelUUID = uuid.New()
	modelName = "mm-model-" + uuid.New().String()[:8]
	testModel := &model.Model{
		ID:               modelUUID,
		ProviderID:       providerID,
		ModelID:          modelName,
		Name:             "Multimodal Test Model",
		Capabilities:     "{}",
		Params:           "{}",
		InputModalities:  "[]",
		OutputModalities: outputModalities,
		Enabled:          true,
		ProviderName:     providerName,
		ProviderEnabled:  true,
	}
	if err := modelRepo.Upsert(context.Background(), testModel); err != nil {
		t.Fatalf("failed to create model: %v", err)
	}
	return providerName, providerID, modelUUID, modelName
}

// multimodalRequest builds an authenticated request against the proxy with
// the virtual-key context values that ProxyKeyMiddleware would normally set.
func (env *multimodalTestEnv) request(path, contentType string, body io.Reader) *http.Request {
	req := httptest.NewRequest("POST", path, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	ctx := context.WithValue(req.Context(), virtualKeyNameKey, "mm-test-key")
	ctx = context.WithValue(ctx, virtualKeyIDKey, uuid.New().String())
	ctx = context.WithValue(ctx, VirtualKeyHashKey, env.keyHash)
	return req.WithContext(ctx)
}

// ---------------------------------------------------------------------------
// Embeddings
// ---------------------------------------------------------------------------

func TestEmbeddings_PassthroughAndModelRewrite(t *testing.T) {
	upstreamBody := `{"object":"list","data":[{"object":"embedding","embedding":[0.1,0.2],"index":0}],"model":"resolved","usage":{"prompt_tokens":8,"total_tokens":8}}`
	var gotPath, gotModel, gotAuth atomic.Value
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		gotAuth.Store(r.Header.Get("Authorization"))
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if m, ok := req["model"].(string); ok {
			gotModel.Store(m)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, upstreamBody)
	}))

	body := fmt.Sprintf(`{"model":"%s/%s","input":["hello","world"],"encoding_format":"float"}`, env.providerName, env.modelName)
	req := env.request("/v1/embeddings", "application/json", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handler.Embeddings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if got := strings.TrimSpace(w.Body.String()); got != upstreamBody {
		t.Errorf("response body not passed through verbatim:\ngot  %s\nwant %s", got, upstreamBody)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if p, _ := gotPath.Load().(string); p != "/embeddings" {
		t.Errorf("upstream path = %q, want /embeddings", p)
	}
	if m, _ := gotModel.Load().(string); m != env.modelName {
		t.Errorf("upstream model = %q, want %q (model must be rewritten)", m, env.modelName)
	}
	if a, _ := gotAuth.Load().(string); a != "Bearer test-api-key" {
		t.Errorf("upstream auth = %q, want Bearer test-api-key", a)
	}
}

func TestEmbeddings_ModelRequired(t *testing.T) {
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called for a request without a model")
		w.WriteHeader(http.StatusOK)
	}))

	req := env.request("/v1/embeddings", "application/json", strings.NewReader(`{"input":"hi"}`))
	w := httptest.NewRecorder()
	env.handler.Embeddings(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "model is required") {
		t.Errorf("body = %q, want model-is-required error", w.Body.String())
	}
}

func TestEmbeddings_FailoverToNextProvider(t *testing.T) {
	var badCalls, goodCalls atomic.Int32
	envBad := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		badCalls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	goodUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		goodCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[],"usage":{"prompt_tokens":2,"total_tokens":2}}`)
	}))
	t.Cleanup(goodUpstream.Close)
	_, _, goodModelUUID, _ := createMultimodalProvider(t, goodUpstream.URL)

	// Failover group: bad provider first, good provider second.
	groupName := envBad.modelName
	failoverRepo := failover.NewRepository(testDB.Pool())
	if _, err := failoverRepo.UpsertWithConfig(context.Background(), groupName,
		[]uuid.UUID{envBad.modelUUID, goodModelUUID},
		map[string]bool{envBad.modelUUID.String(): true, goodModelUUID.String(): true},
		nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}

	body := fmt.Sprintf(`{"model":"hotel/%s","input":"hi"}`, groupName)
	req := envBad.request("/v1/embeddings", "application/json", strings.NewReader(body))
	w := httptest.NewRecorder()
	envBad.handler.Embeddings(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 after failover (body: %s)", w.Code, w.Body.String())
	}
	if badCalls.Load() != 1 {
		t.Errorf("bad provider calls = %d, want 1", badCalls.Load())
	}
	if goodCalls.Load() != 1 {
		t.Errorf("good provider calls = %d, want 1", goodCalls.Load())
	}
	if !strings.Contains(w.Body.String(), `"object":"list"`) {
		t.Errorf("body = %q, want the good provider's response", w.Body.String())
	}
}

// TestEmbeddings_FailoverStillRecordsTheGoneSignal pins that failing over does
// not throw the retirement evidence away.
//
// A retired model usually answers 404, which is failover-eligible, so the branch
// that hands the request to the next candidate is the branch a dead model's
// refusals actually take. It drained the body and moved on without classifying
// it, which meant a model sitting anywhere but LAST in a failover group accrued
// no strikes at all: only the final candidate reaches forwardUpstreamError,
// where the strike is recorded. attemptCandidate has classified on the way out
// for chat since the feature was written; the pass-through loop is the same
// request path for embeddings, which is now an auto-retirable family.
//
// Delete this and traffic-driven retirement quietly stops working for exactly
// the models that have somewhere to fail over to.
func TestEmbeddings_FailoverStillRecordsTheGoneSignal(t *testing.T) {
	// The upstream handler needs the model's generated name to refuse it BY
	// NAME: classifyUpstreamError only reads a gone-phrase as a retirement when
	// the body names the model the request asked for.
	var goneModelName atomic.Value
	envGone := newMultimodalEnvWith(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusNotFound)
		name, _ := goneModelName.Load().(string)
		_, _ = fmt.Fprintf(w, "{\"error\":{\"message\":\"The model `%s` does not exist\"}}", name)
	}), `["embedding"]`)
	goneModelName.Store(envGone.modelName)

	goodUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"object":"embedding","embedding":[0.1],"index":0}]}`)
	}))
	t.Cleanup(goodUpstream.Close)
	_, _, goodModelUUID, _ := createMultimodalProvider(t, goodUpstream.URL)

	groupName := envGone.modelName
	failoverRepo := failover.NewRepository(testDB.Pool())
	if _, err := failoverRepo.UpsertWithConfig(context.Background(), groupName,
		[]uuid.UUID{envGone.modelUUID, goodModelUUID},
		map[string]bool{envGone.modelUUID.String(): true, goodModelUUID.String(): true},
		nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}

	body := fmt.Sprintf(`{"model":"hotel/%s","input":"hi"}`, groupName)
	req := envGone.request("/v1/embeddings", "application/json", strings.NewReader(body))
	w := httptest.NewRecorder()
	envGone.handler.Embeddings(w, req)

	// The client is served by the second candidate, exactly as before: recording
	// the signal must not change what the request returns.
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 after failover (body: %s)", w.Code, w.Body.String())
	}

	// The strike itself is recorded synchronously — only the disable is detached
	// — so this is the state the request left behind, not a race.
	raw, ok := envGone.handler.goneStrikes.Load(goneStreakKey{model: envGone.modelUUID, endpoint: probeEmbeddingsEndpoint})
	if !ok {
		t.Fatal("the refused model accrued no strike, so it can never be auto-retired from a failover group")
	}
	streak, ok := raw.(*goneStreak)
	if !ok {
		t.Fatalf("unexpected streak type %T", raw)
	}
	if n := streak.count(); n != 1 {
		t.Errorf("streak = %d, want exactly 1 strike for one refusal", n)
	}
}

// TestEmbeddings_ASuccessClearsTheGoneStrikes pins the success half of
// traffic-driven retirement on the pass-through path.
//
// The strike is only half a signal. What makes three strikes mean anything is
// that they have to be CONSECUTIVE: a success in between resets the count, so a
// retirement is drawn from a run of failures rather than from three unrelated
// ones. The chat loop has always done that; the pass-through loop recorded
// strikes and never cleared them, so for embeddings — the one pass-through
// family that can be auto-retired — nothing but the 30-minute window bounded
// them. A model serving thousands of requests an hour that drew three scattered
// 404s in half an hour reached the threshold and spent a probe on a model that
// was demonstrably working the whole time.
func TestEmbeddings_ASuccessClearsTheGoneStrikes(t *testing.T) {
	var goneModelName atomic.Value
	var refuse atomic.Bool
	refuse.Store(true)
	env := newMultimodalEnvWith(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if refuse.Load() {
			w.WriteHeader(http.StatusNotFound)
			name, _ := goneModelName.Load().(string)
			_, _ = fmt.Fprintf(w, "{\"error\":{\"message\":\"The model `%s` does not exist\"}}", name)
			return
		}
		_, _ = io.WriteString(w, `{"object":"list","data":[{"object":"embedding","embedding":[0.1],"index":0}]}`)
	}), `["embedding"]`)
	goneModelName.Store(env.modelName)

	body := fmt.Sprintf(`{"model":"%s/%s","input":"hi"}`, env.providerName, env.modelName)
	embed := func() int {
		req := env.request("/v1/embeddings", "application/json", strings.NewReader(body))
		w := httptest.NewRecorder()
		env.handler.Embeddings(w, req)
		return w.Code
	}

	if code := embed(); code != http.StatusNotFound {
		t.Fatalf("status = %d, want the provider's 404", code)
	}
	raw, ok := env.handler.goneStrikes.Load(goneStreakKey{model: env.modelUUID, endpoint: probeEmbeddingsEndpoint})
	if !ok {
		t.Fatal("the refusal accrued no strike")
	}
	streak, ok := raw.(*goneStreak)
	if !ok {
		t.Fatalf("unexpected streak type %T", raw)
	}
	if n := streak.count(); n != 1 {
		t.Fatalf("streak = %d, want 1 after one refusal", n)
	}

	// The same model now answers. That is the evidence a strike is measured
	// against, and it must reset the count.
	refuse.Store(false)
	if code := embed(); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if n := streak.count(); n != 0 {
		t.Fatalf("streak = %d, want 0 after the model answered", n)
	}
}

// TestEmbeddings_ResponsesThatCarryNothingKeepTheGoneStrikes is the other half
// of the test above: which 2xx counts as the model answering.
//
// A status is a promise; bytes are the evidence. All three cases below are
// responses the pass-through path is perfectly happy with and none of them
// carried an answer, so none may reset a strike count. Crediting any of them
// would let a provider — or a CDN in front of one — that intermittently returns
// an empty success keep a retired model from ever reaching three CONSECUTIVE
// strikes: it would never be nominated, never probed, and never retired, which
// is exactly the state the strike machinery exists to escape.
//
// The three cover both commit points. The first two take the buffered JSON
// branch (a body that dies mid-read, and a clean 200 that carries nothing), the
// third takes the streamed branch, and the point of running them together is
// that the two branches must answer the same way.
func TestEmbeddings_ResponsesThatCarryNothingKeepTheGoneStrikes(t *testing.T) {
	cases := []struct {
		name       string
		answer     func(t *testing.T, w http.ResponseWriter)
		wantStatus int
	}{
		{
			// 200 headers, a promise of 1000 bytes, and then the connection
			// dies nine bytes in. The breaker already records this as a
			// provider failure and the client gets a 502.
			name: "body dies mid-read",
			answer: func(t *testing.T, w http.ResponseWriter) {
				hj, ok := w.(http.Hijacker)
				if !ok {
					t.Error("test server does not support hijacking")
					return
				}
				conn, buf, err := hj.Hijack()
				if err != nil {
					t.Errorf("hijack: %v", err)
					return
				}
				_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 1000\r\n\r\n{\"object\":")
				_ = buf.Flush()
				_ = conn.Close()
			},
			wantStatus: http.StatusBadGateway,
		},
		{
			// A clean, complete, entirely contentless 200. The read succeeds,
			// which is what makes this the easiest of the three to credit by
			// accident.
			name: "empty json 200",
			answer: func(_ *testing.T, w http.ResponseWriter) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusOK)
			},
			wantStatus: http.StatusOK,
		},
		{
			// A well-formed embeddings response carrying no embedding. The
			// probe already refuses to credit this shape; the traffic path has
			// to agree, or an aggregator alternating gone-shaped 404s with an
			// empty 200 resets the count on every other request and the model is
			// never nominated.
			name: "empty embeddings payload",
			answer: func(_ *testing.T, w http.ResponseWriter) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"object":"list","data":[]}`)
			},
			wantStatus: http.StatusOK,
		},
		{
			// The same empty payload, unlabelled. Which twin runs was decided by
			// the content type alone, so an aggregator or CDN that drops the
			// header sent an embeddings answer to the streamed path — which
			// commits on the first byte and cannot judge what it never holds, so
			// eleven bytes cleared the streak through the one door that skips
			// passthroughAnswered.
			name: "empty embeddings payload, unlabelled",
			answer: func(_ *testing.T, w http.ResponseWriter) {
				w.WriteHeader(http.StatusOK)
				_, _ = io.WriteString(w, `{"object":"list","data":[]}`)
			},
			wantStatus: http.StatusOK,
		},
		{
			// A 204 is a legitimate HTTP success and belongs in the breaker's
			// ledger, because the provider is plainly alive. It still says
			// nothing about whether this MODEL is served.
			name: "204 no content",
			answer: func(_ *testing.T, w http.ResponseWriter) {
				w.WriteHeader(http.StatusNoContent)
			},
			wantStatus: http.StatusNoContent,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var goneModelName atomic.Value
			var refuse atomic.Bool
			refuse.Store(true)
			env := newMultimodalEnvWith(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if refuse.Load() {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusNotFound)
					name, _ := goneModelName.Load().(string)
					_, _ = fmt.Fprintf(w, "{\"error\":{\"message\":\"The model `%s` does not exist\"}}", name)
					return
				}
				tc.answer(t, w)
			}), `["embedding"]`)
			goneModelName.Store(env.modelName)

			body := fmt.Sprintf(`{"model":"%s/%s","input":"hi"}`, env.providerName, env.modelName)
			embed := func() int {
				req := env.request("/v1/embeddings", "application/json", strings.NewReader(body))
				w := httptest.NewRecorder()
				env.handler.Embeddings(w, req)
				return w.Code
			}

			if code := embed(); code != http.StatusNotFound {
				t.Fatalf("status = %d, want the provider's 404", code)
			}
			raw, ok := env.handler.goneStrikes.Load(goneStreakKey{model: env.modelUUID, endpoint: probeEmbeddingsEndpoint})
			if !ok {
				t.Fatal("the refusal accrued no strike")
			}
			streak, ok := raw.(*goneStreak)
			if !ok {
				t.Fatalf("unexpected streak type %T", raw)
			}

			refuse.Store(false)
			if code := embed(); code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", code, tc.wantStatus)
			}
			if n := streak.count(); n != 1 {
				t.Fatalf("streak = %d, want the strike kept: a response that carried nothing is not the model answering", n)
			}
		})
	}
}

// TestAttemptPassthroughCandidate_AMiniMaxRefusalInsideA200KeepsTheStreak pins
// that the pass-through loop normalises MiniMax's 200-wrapped errors like every
// other attempt path does.
//
// MiniMax reports rate limits, an exhausted plan balance and auth failures
// inside an HTTP 200 envelope. attemptCandidate, probeStreamingCandidate and
// probeModel all remap them before judging anything; this loop did not, and
// until the retirement work that only cost a spurious breaker success. Now the
// 2xx branch clears the model's gone-strike streak, so a refusal wrapped in a
// 200 was recorded as the model answering and reset the consecutive count a
// retirement depends on.
func TestAttemptPassthroughCandidate_AMiniMaxRefusalInsideA200KeepsTheStreak(t *testing.T) {
	h := newIntegrationHandler()
	defer stopUnitHandler(h)

	// A 200 whose envelope says rate-limited, which remaps to 429.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"base_resp":{"status_code":1002,"status_msg":"rate limit exceeded"}}`)
	}))
	defer srv.Close()
	// The hostname decides the provider dialect, so the transport dials the test
	// server regardless of it. Plain http, because that dial hands back a TCP
	// connection to an httptest server that speaks no TLS.
	h.upstreamTransport = dialToTestServer(t, srv)

	m := &model.Model{ID: uuid.New(), ModelID: "MiniMax-Text-01", InputModalities: `["text"]`, OutputModalities: `["embedding"]`}
	cand := goneCandidateAt(m, "MiniMax", "http://api.minimax.io/v1")

	// One real refusal, so there is a streak for the 200 to clear.
	h.noteModelGone(cand, endpointTypeEmbeddings)
	streak := goneStreakFor(t, h, m.ID, probeEmbeddingsEndpoint)

	st := &requestState{
		startTime:       time.Now(),
		reqModel:        "MiniMax-Text-01",
		endpointPath:    "/embeddings",
		bodyBytes:       []byte(`{"model":"MiniMax-Text-01","input":"hi"}`),
		failoverTimeout: 30 * time.Second,
		logData:         &requestLogData{modelID: "MiniMax-Text-01", endpointType: endpointTypeEmbeddings},
	}
	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/v1/embeddings", http.NoBody)

	if got := h.attemptPassthroughCandidate(w, r, st, cand, 0, 1); got != outcomeFatal {
		t.Fatalf("outcome = %v, want a terminal error for a remapped 429", got)
	}
	if n := streak.count(); n != 1 {
		t.Fatalf("streak = %d, want the strike kept: a refusal inside a 200 is not the model answering", n)
	}
}

// TestEmbeddings_AnOversizedAnswerStillClearsTheGoneStrikes pins the one case
// where the buffered pass-through must NOT ask what the body contains.
//
// Past passthroughJSONBufferCap the buffered body is a prefix and the remainder
// is streamed, so a content check would be parsing truncated JSON — which never
// parses, so a provider that had just produced megabytes would read as having
// answered with nothing. A batch embeddings call clears 8 MiB at around 140
// inputs of 3072 dimensions, which is ordinary document-indexing traffic: the
// success side of the streak would be permanently dead on that workload, and a
// live model would be nominated and probed over and over.
func TestEmbeddings_AnOversizedAnswerStillClearsTheGoneStrikes(t *testing.T) {
	var goneModelName atomic.Value
	var refuse atomic.Bool
	refuse.Store(true)
	// A real embeddings answer, past the buffer cap.
	oversized := `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[` +
		strings.Repeat("0.1,", (passthroughJSONBufferCap/4)+16) + `0.2]}]}`
	env := newMultimodalEnvWith(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if refuse.Load() {
			w.WriteHeader(http.StatusNotFound)
			name, _ := goneModelName.Load().(string)
			_, _ = fmt.Fprintf(w, "{\"error\":{\"message\":\"The model `%s` does not exist\"}}", name)
			return
		}
		_, _ = io.WriteString(w, oversized)
	}), `["embedding"]`)
	goneModelName.Store(env.modelName)

	body := fmt.Sprintf(`{"model":"%s/%s","input":"hi"}`, env.providerName, env.modelName)
	embed := func() int {
		req := env.request("/v1/embeddings", "application/json", strings.NewReader(body))
		w := httptest.NewRecorder()
		env.handler.Embeddings(w, req)
		return w.Code
	}

	if code := embed(); code != http.StatusNotFound {
		t.Fatalf("status = %d, want the provider's 404", code)
	}
	raw, ok := env.handler.goneStrikes.Load(goneStreakKey{model: env.modelUUID, endpoint: probeEmbeddingsEndpoint})
	if !ok {
		t.Fatal("the refusal accrued no strike")
	}
	streak, ok := raw.(*goneStreak)
	if !ok {
		t.Fatalf("unexpected streak type %T", raw)
	}

	refuse.Store(false)
	if code := embed(); code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if n := streak.count(); n != 0 {
		t.Fatalf("streak = %d, want 0: a provider that produced %d bytes answered", n, len(oversized))
	}
}

func TestEmbeddings_UpstreamErrorReturnsOpenAIError(t *testing.T) {
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":{"message":"input too long","type":"invalid_request_error"}}`)
	}))

	body := fmt.Sprintf(`{"model":"%s/%s","input":"hi"}`, env.providerName, env.modelName)
	req := env.request("/v1/embeddings", "application/json", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handler.Embeddings(w, req)

	// Single candidate + non-failover-eligible 400: a generic OpenAI error is
	// returned (the upstream body goes to the request log, not the client).
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	var errResp struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil || errResp.Error == nil {
		t.Fatalf("error response is not OpenAI-shaped JSON: %q", w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Rerank
// ---------------------------------------------------------------------------

func TestRerank_PassthroughAndModelRewrite(t *testing.T) {
	upstreamBody := `{"model":"resolved","results":[{"index":1,"relevance_score":0.98},{"index":0,"relevance_score":0.12}],"usage":{"total_tokens":17}}`
	var gotPath, gotModel, gotAuth atomic.Value
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		gotAuth.Store(r.Header.Get("Authorization"))
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		if m, ok := req["model"].(string); ok {
			gotModel.Store(m)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, upstreamBody)
	}))

	body := fmt.Sprintf(`{"model":"%s/%s","query":"what is a capybara","documents":["a rodent","a river"],"top_n":2}`, env.providerName, env.modelName)
	req := env.request("/v1/rerank", "application/json", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handler.Rerank(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if got := strings.TrimSpace(w.Body.String()); got != upstreamBody {
		t.Errorf("response body not passed through verbatim:\ngot  %s\nwant %s", got, upstreamBody)
	}
	if p, _ := gotPath.Load().(string); p != "/rerank" {
		t.Errorf("upstream path = %q, want /rerank", p)
	}
	if m, _ := gotModel.Load().(string); m != env.modelName {
		t.Errorf("upstream model = %q, want %q (model must be rewritten)", m, env.modelName)
	}
	if a, _ := gotAuth.Load().(string); a != "Bearer test-api-key" {
		t.Errorf("upstream auth = %q, want Bearer test-api-key", a)
	}
}

func TestRerank_ModelRequired(t *testing.T) {
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called for a request without a model")
		w.WriteHeader(http.StatusOK)
	}))

	req := env.request("/v1/rerank", "application/json", strings.NewReader(`{"query":"q","documents":["d"]}`))
	w := httptest.NewRecorder()
	env.handler.Rerank(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "model is required") {
		t.Errorf("body = %q, want model-is-required error", w.Body.String())
	}
}

func TestRerank_FailoverToNextProvider(t *testing.T) {
	var badCalls, goodCalls atomic.Int32
	envBad := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		badCalls.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	goodUpstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		goodCalls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"results":[{"index":0,"relevance_score":0.5}],"usage":{"total_tokens":3}}`)
	}))
	t.Cleanup(goodUpstream.Close)
	_, _, goodModelUUID, _ := createMultimodalProvider(t, goodUpstream.URL)

	// Failover group: bad provider first, good provider second.
	groupName := envBad.modelName
	failoverRepo := failover.NewRepository(testDB.Pool())
	if _, err := failoverRepo.UpsertWithConfig(context.Background(), groupName,
		[]uuid.UUID{envBad.modelUUID, goodModelUUID},
		map[string]bool{envBad.modelUUID.String(): true, goodModelUUID.String(): true},
		nil, nil, nil, nil); err != nil {
		t.Fatalf("failed to create failover group: %v", err)
	}

	body := fmt.Sprintf(`{"model":"hotel/%s","query":"q","documents":["d1","d2"]}`, groupName)
	req := envBad.request("/v1/rerank", "application/json", strings.NewReader(body))
	w := httptest.NewRecorder()
	envBad.handler.Rerank(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 after failover (body: %s)", w.Code, w.Body.String())
	}
	if badCalls.Load() != 1 {
		t.Errorf("bad provider calls = %d, want 1", badCalls.Load())
	}
	if goodCalls.Load() != 1 {
		t.Errorf("good provider calls = %d, want 1", goodCalls.Load())
	}
	if !strings.Contains(w.Body.String(), `"relevance_score"`) {
		t.Errorf("body = %q, want the good provider's response", w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Audio speech (binary passthrough)
// ---------------------------------------------------------------------------

func TestAudioSpeech_BinaryPassthrough(t *testing.T) {
	audio := []byte{0xFF, 0xFB, 0x90, 0x00, 0xDE, 0xAD, 0xBE, 0xEF}
	var gotPath atomic.Value
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		w.Header().Set("Content-Type", "audio/mpeg")
		_, _ = w.Write(audio)
	}))

	body := fmt.Sprintf(`{"model":"%s/%s","input":"hello","voice":"alloy"}`, env.providerName, env.modelName)
	req := env.request("/v1/audio/speech", "application/json", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handler.AudioSpeech(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "audio/mpeg" {
		t.Errorf("Content-Type = %q, want audio/mpeg", ct)
	}
	if !bytes.Equal(w.Body.Bytes(), audio) {
		t.Errorf("binary body corrupted: got %v, want %v", w.Body.Bytes(), audio)
	}
	if p, _ := gotPath.Load().(string); p != "/audio/speech" {
		t.Errorf("upstream path = %q, want /audio/speech", p)
	}
}

// ---------------------------------------------------------------------------
// Image generations (JSON + SSE streaming passthrough)
// ---------------------------------------------------------------------------

func TestImageGenerations_JSONPassthrough(t *testing.T) {
	upstreamBody := `{"created":1713833628,"data":[{"b64_json":"aW1n"}],"usage":{"input_tokens":50,"output_tokens":100,"total_tokens":150}}`
	var gotPath atomic.Value
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, upstreamBody)
	}))

	body := fmt.Sprintf(`{"model":"%s/%s","prompt":"a cat","size":"1024x1024"}`, env.providerName, env.modelName)
	req := env.request("/v1/images/generations", "application/json", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handler.ImageGenerations(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if got := strings.TrimSpace(w.Body.String()); got != upstreamBody {
		t.Errorf("response not passed through verbatim:\ngot  %s\nwant %s", got, upstreamBody)
	}
	if p, _ := gotPath.Load().(string); p != "/images/generations" {
		t.Errorf("upstream path = %q, want /images/generations", p)
	}
}

func TestImageGenerations_SSEPassthrough(t *testing.T) {
	sse := "event: image_generation.partial_image\ndata: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"cGFydA==\"}\n\nevent: image_generation.completed\ndata: {\"type\":\"image_generation.completed\",\"b64_json\":\"ZnVsbA==\"}\n\n"
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse)
	}))

	body := fmt.Sprintf(`{"model":"%s/%s","prompt":"a cat","stream":true,"partial_images":1}`, env.providerName, env.modelName)
	req := env.request("/v1/images/generations", "application/json", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handler.ImageGenerations(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}
	if w.Body.String() != sse {
		t.Errorf("SSE stream not passed through verbatim:\ngot  %q\nwant %q", w.Body.String(), sse)
	}
	if !w.Flushed {
		t.Error("expected streamed response to be flushed")
	}
}

// A pass-through SSE stream that carries a mid-stream error frame quoting the
// operator's provider key must reach the client masked, exactly as the chat
// streaming paths do; content events and framing stay byte-identical.
func TestImageGenerations_SSEPassthroughMasksErrorFrame(t *testing.T) {
	const planted = "sk-proj-STANDARDKEY1234567890abcdef1234567890"
	partial := "event: image_generation.partial_image\ndata: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"cGFydA==\"}\n\n"
	errFrame := "event: error\ndata:{\"type\":\"error\",\"error\":{\"message\":\"billing key " + planted + " is invalid\"}}\n\n"
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, partial+errFrame)
	}))

	body := fmt.Sprintf(`{"model":"%s/%s","prompt":"a cat","stream":true,"partial_images":1}`, env.providerName, env.modelName)
	req := env.request("/v1/images/generations", "application/json", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handler.ImageGenerations(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	got := w.Body.String()
	if strings.Contains(got, planted) {
		t.Fatalf("operator credential reached the client through the SSE passthrough:\n%s", got)
	}
	want := partial + "event: error\ndata: {\"type\":\"error\",\"error\":{\"message\":\"billing key [redacted] is invalid\"}}\n\n"
	if got != want {
		t.Errorf("masked stream mismatch:\ngot  %q\nwant %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// Audio transcriptions (multipart)
// ---------------------------------------------------------------------------

// buildUploadBody builds a client-side multipart upload with the given model
// value and a small fake audio file.
func buildUploadBody(t *testing.T, modelValue string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if modelValue != "" {
		if err := mw.WriteField("model", modelValue); err != nil {
			t.Fatalf("WriteField: %v", err)
		}
	}
	if err := mw.WriteField("language", "en"); err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	fw, err := mw.CreateFormFile("file", "speech.wav")
	if err != nil {
		t.Fatalf("CreateFormFile: %v", err)
	}
	if _, err := fw.Write([]byte("RIFFfakewavdata")); err != nil {
		t.Fatalf("file write: %v", err)
	}
	if err := mw.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return &buf, mw.FormDataContentType()
}

func TestAudioTranscriptions_MultipartPassthrough(t *testing.T) {
	upstreamBody := `{"text":"hello world","usage":{"input_tokens":14,"output_tokens":3,"total_tokens":17}}`
	var gotPath, gotModel, gotFile, gotFilename, gotLanguage atomic.Value
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, "bad multipart: "+err.Error(), http.StatusBadRequest)
			return
		}
		gotModel.Store(r.FormValue("model"))
		gotLanguage.Store(r.FormValue("language"))
		file, hdr, err := r.FormFile("file")
		if err != nil {
			http.Error(w, "missing file: "+err.Error(), http.StatusBadRequest)
			return
		}
		defer file.Close()
		data, _ := io.ReadAll(file)
		gotFile.Store(string(data))
		gotFilename.Store(hdr.Filename)
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, upstreamBody)
	}))

	upload, contentType := buildUploadBody(t, env.providerName+"/"+env.modelName)
	req := env.request("/v1/audio/transcriptions", contentType, upload)
	w := httptest.NewRecorder()
	env.handler.AudioTranscriptions(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if got := strings.TrimSpace(w.Body.String()); got != upstreamBody {
		t.Errorf("response not passed through verbatim:\ngot  %s\nwant %s", got, upstreamBody)
	}
	if p, _ := gotPath.Load().(string); p != "/audio/transcriptions" {
		t.Errorf("upstream path = %q, want /audio/transcriptions", p)
	}
	if m, _ := gotModel.Load().(string); m != env.modelName {
		t.Errorf("upstream model = %q, want %q (model must be rewritten)", m, env.modelName)
	}
	if f, _ := gotFile.Load().(string); f != "RIFFfakewavdata" {
		t.Errorf("file bytes corrupted: %q", f)
	}
	if fn, _ := gotFilename.Load().(string); fn != "speech.wav" {
		t.Errorf("filename = %q, want speech.wav", fn)
	}
	if l, _ := gotLanguage.Load().(string); l != "en" {
		t.Errorf("language field = %q, want en", l)
	}
}

func TestAudioTranscriptions_RejectsNonMultipart(t *testing.T) {
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called for a non-multipart request")
		w.WriteHeader(http.StatusOK)
	}))

	req := env.request("/v1/audio/transcriptions", "application/json", strings.NewReader(`{"model":"x"}`))
	w := httptest.NewRecorder()
	env.handler.AudioTranscriptions(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "multipart/form-data") {
		t.Errorf("body = %q, want multipart content-type error", w.Body.String())
	}
}

func TestAudioTranscriptions_ModelRequired(t *testing.T) {
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called without a model")
		w.WriteHeader(http.StatusOK)
	}))

	upload, contentType := buildUploadBody(t, "")
	req := env.request("/v1/audio/transcriptions", contentType, upload)
	w := httptest.NewRecorder()
	env.handler.AudioTranscriptions(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "model is required") {
		t.Errorf("body = %q, want model-is-required error", w.Body.String())
	}
}

func TestAudioSpeech_SSEUsageMetered(t *testing.T) {
	// Streaming TTS/STT responses carry usage on the final SSE event; the
	// pass-through must scrape it and meter the virtual key.
	sse := "data: {\"type\":\"speech.audio.delta\",\"audio\":\"cGFydA==\"}\n\ndata: {\"type\":\"speech.audio.done\",\"usage\":{\"input_tokens\":12,\"output_tokens\":34,\"total_tokens\":46}}\n\n"
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse)
	}))

	body := fmt.Sprintf(`{"model":"%s/%s","input":"hello","voice":"alloy","stream_format":"sse"}`, env.providerName, env.modelName)
	req := env.request("/v1/audio/speech", "application/json", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handler.AudioSpeech(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if w.Body.String() != sse {
		t.Errorf("SSE stream not passed through verbatim:\ngot  %q\nwant %q", w.Body.String(), sse)
	}

	// recordTokenUsage runs synchronously before the handler returns.
	vkRepo := virtualkey.NewRepository(testDB.Pool())
	vk, err := vkRepo.FindByKeyHash(context.Background(), env.keyHash)
	if err != nil {
		t.Fatalf("FindByKeyHash: %v", err)
	}
	if vk.TokensUsed != 46 {
		t.Errorf("tokens_used = %d, want 46 (12 input + 34 output)", vk.TokensUsed)
	}
}

func TestAudioSpeech_FirstByteFailureReturns502(t *testing.T) {
	// A 200 whose body dies before the first byte must NOT be served: the
	// client gets a clean OpenAI 502 (headers were not committed) and the
	// circuit breaker records a failure instead of a success.
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("test server does not support hijacking")
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}
		// Promise a body, send none, drop the connection.
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: audio/mpeg\r\nContent-Length: 1000\r\n\r\n")
		_ = buf.Flush()
		_ = conn.Close()
	}))

	body := fmt.Sprintf(`{"model":"%s/%s","input":"hello","voice":"alloy"}`, env.providerName, env.modelName)
	req := env.request("/v1/audio/speech", "application/json", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handler.AudioSpeech(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body: %s)", w.Code, w.Body.String())
	}
	var errResp struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil || errResp.Error == nil {
		t.Fatalf("expected OpenAI-shaped error JSON, got %q", w.Body.String())
	}
	if fails, seen := cbConsecutiveFails(env.handler.circuitBreaker, env.providerID); !seen || fails != 1 {
		t.Errorf("expected one breaker failure recorded, got seen=%v fails=%d", seen, fails)
	}
}

func TestImageGenerations_OversizedJSONStreamsThrough(t *testing.T) {
	// JSON bodies beyond the usage-extraction cap must still pass through
	// verbatim (memory-bounded streaming), with usage extraction skipped.
	hugePayload := strings.Repeat("A", passthroughJSONBufferCap+4096)
	upstreamBody := `{"created":1,"data":[{"b64_json":"` + hugePayload + `"}],"usage":{"input_tokens":5,"output_tokens":9}}`
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, upstreamBody)
	}))

	body := fmt.Sprintf(`{"model":"%s/%s","prompt":"a cat"}`, env.providerName, env.modelName)
	req := env.request("/v1/images/generations", "application/json", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handler.ImageGenerations(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	got := w.Body.String()
	if len(got) != len(upstreamBody) {
		t.Fatalf("body length = %d, want %d (oversized body must stream through whole)", len(got), len(upstreamBody))
	}
	if got[:64] != upstreamBody[:64] || got[len(got)-64:] != upstreamBody[len(upstreamBody)-64:] {
		t.Error("oversized body corrupted in passthrough")
	}
}

func TestLoadFailoverConfig_LongRunningGetsExtendedBudget(t *testing.T) {
	h := newIntegrationHandler()
	req := httptest.NewRequest("POST", "/v1/images/generations", http.NoBody)

	base := &requestState{startTime: time.Now()}
	h.loadFailoverConfig(req, base)

	long := &requestState{startTime: time.Now(), longRunning: true}
	h.loadFailoverConfig(req, long)

	if long.failoverTimeout != base.failoverTimeout*10 {
		t.Errorf("longRunning timeout = %v, want 10x base %v", long.failoverTimeout, base.failoverTimeout)
	}
}

func TestAudioSpeech_EmptyBody200Returns502(t *testing.T) {
	// A 200 with a genuinely empty body breaks the binary/SSE content
	// contract: the provider must record a breaker failure and the client
	// must get a clean 502, not an empty "success".
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.WriteHeader(http.StatusOK)
	}))

	body := fmt.Sprintf(`{"model":"%s/%s","input":"hello","voice":"alloy"}`, env.providerName, env.modelName)
	req := env.request("/v1/audio/speech", "application/json", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handler.AudioSpeech(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 for empty 200 body (body: %s)", w.Code, w.Body.String())
	}
	if fails, seen := cbConsecutiveFails(env.handler.circuitBreaker, env.providerID); !seen || fails != 1 {
		t.Errorf("expected one breaker failure recorded, got seen=%v fails=%d", seen, fails)
	}
}

func TestEmbeddings_JSONBodyReadFailureReturns502(t *testing.T) {
	// A JSON 200 whose body dies mid-read must produce a 502 (headers were
	// not committed) and a breaker failure, not a success.
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("test server does not support hijacking")
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 1000\r\n\r\n{\"object\":")
		_ = buf.Flush()
		_ = conn.Close()
	}))

	body := fmt.Sprintf(`{"model":"%s/%s","input":"hi"}`, env.providerName, env.modelName)
	req := env.request("/v1/embeddings", "application/json", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handler.Embeddings(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body: %s)", w.Code, w.Body.String())
	}
	if fails, seen := cbConsecutiveFails(env.handler.circuitBreaker, env.providerID); !seen || fails != 1 {
		t.Errorf("expected one breaker failure recorded, got seen=%v fails=%d", seen, fails)
	}
}

func TestAudioSpeech_MidStreamFailureAfterCommit(t *testing.T) {
	// Once the first byte committed (200 sent to the client), a mid-stream
	// upstream death cannot be retried: the partial bytes reach the client
	// and the breaker keeps the commit-point success.
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("test server does not support hijacking")
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Type: audio/mpeg\r\nContent-Length: 1000\r\n\r\n0123456789")
		_ = buf.Flush()
		_ = conn.Close()
	}))

	body := fmt.Sprintf(`{"model":"%s/%s","input":"hello","voice":"alloy"}`, env.providerName, env.modelName)
	req := env.request("/v1/audio/speech", "application/json", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handler.AudioSpeech(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (already committed)", w.Code)
	}
	if got := w.Body.String(); got != "0123456789" {
		t.Errorf("partial body = %q, want the 10 bytes delivered before the failure", got)
	}
	if fails, seen := cbConsecutiveFails(env.handler.circuitBreaker, env.providerID); !seen || fails != 0 {
		t.Errorf("expected breaker success (commit point reached), got seen=%v fails=%d", seen, fails)
	}
}

func TestEmbeddings_UnknownProviderReturns404(t *testing.T) {
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called for an unresolvable model")
		w.WriteHeader(http.StatusOK)
	}))

	req := env.request("/v1/embeddings", "application/json", strings.NewReader(`{"model":"no-such-provider/embed-1","input":"hi"}`))
	w := httptest.NewRecorder()
	env.handler.Embeddings(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (body: %s)", w.Code, w.Body.String())
	}
}

func TestEmbeddings_UpstreamConnectFailureReturns502(t *testing.T) {
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	// Kill the upstream so the dial fails and the (single-candidate)
	// failover loop exhausts.
	env.upstream.Close()

	body := fmt.Sprintf(`{"model":"%s/%s","input":"hi"}`, env.providerName, env.modelName)
	req := env.request("/v1/embeddings", "application/json", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handler.Embeddings(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (body: %s)", w.Code, w.Body.String())
	}
}

func TestAudioTranscriptions_MalformedFormReturns400(t *testing.T) {
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called for a malformed form")
		w.WriteHeader(http.StatusOK)
	}))

	// Declared boundary with a malformed part header (no colon).
	body := "--xyz\r\nno-colon-header-line\r\n\r\nhi\r\n--xyz--\r\n"
	req := env.request("/v1/audio/transcriptions", "multipart/form-data; boundary=xyz", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handler.AudioTranscriptions(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "invalid multipart form") {
		t.Errorf("body = %q, want invalid-multipart error", w.Body.String())
	}
}

func TestAudioSpeech_MissingContentTypeDefaultsToOctetStream(t *testing.T) {
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("test server does not support hijacking")
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack: %v", err)
		}
		// Raw response without a Content-Type header.
		_, _ = buf.WriteString("HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\nMP3!")
		_ = buf.Flush()
		_ = conn.Close()
	}))

	body := fmt.Sprintf(`{"model":"%s/%s","input":"hello","voice":"alloy"}`, env.providerName, env.modelName)
	req := env.request("/v1/audio/speech", "application/json", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handler.AudioSpeech(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/octet-stream" {
		t.Errorf("Content-Type = %q, want application/octet-stream default", ct)
	}
	if w.Body.String() != "MP3!" {
		t.Errorf("body = %q, want MP3!", w.Body.String())
	}
}

func TestAudioSpeech_ContentDispositionForwarded(t *testing.T) {
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "audio/mpeg")
		w.Header().Set("Content-Disposition", `attachment; filename="speech.mp3"`)
		_, _ = w.Write([]byte{0xFF, 0xFB})
	}))

	body := fmt.Sprintf(`{"model":"%s/%s","input":"hello","voice":"alloy"}`, env.providerName, env.modelName)
	req := env.request("/v1/audio/speech", "application/json", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handler.AudioSpeech(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if cd := w.Header().Get("Content-Disposition"); cd != `attachment; filename="speech.mp3"` {
		t.Errorf("Content-Disposition = %q, want it forwarded on success", cd)
	}
}

func TestEmbeddings_RequestCreationFailureReturns502(t *testing.T) {
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called when request creation fails")
		w.WriteHeader(http.StatusOK)
	}))

	origNewReq := newRequestWithContext
	defer func() { newRequestWithContext = origNewReq }()
	newRequestWithContext = func(_ context.Context, _, _ string, _ io.Reader) (*http.Request, error) {
		return nil, fmt.Errorf("simulated request creation failure")
	}

	body := fmt.Sprintf(`{"model":"%s/%s","input":"hi"}`, env.providerName, env.modelName)
	req := env.request("/v1/embeddings", "application/json", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handler.Embeddings(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (body: %s)", w.Code, w.Body.String())
	}
}

// failingReader errors on the first read, simulating a client that aborts
// mid-upload.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, fmt.Errorf("simulated client abort")
}

func TestAudioTranscriptions_BodyReadFailureReturns400(t *testing.T) {
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("upstream must not be called when the upload aborts")
		w.WriteHeader(http.StatusOK)
	}))

	req := env.request("/v1/audio/transcriptions", "multipart/form-data; boundary=xyz", failingReader{})
	w := httptest.NewRecorder()
	env.handler.AudioTranscriptions(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if !strings.Contains(w.Body.String(), "failed to read request body") {
		t.Errorf("body = %q, want read-failure error", w.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Audio translations / image edits / variations (same multipart pipeline,
// verify the endpoint path routing)
// ---------------------------------------------------------------------------

func TestMultipartEndpoints_UpstreamPaths(t *testing.T) {
	cases := []struct {
		name     string
		call     func(h *Handler, w http.ResponseWriter, r *http.Request)
		wantPath string
	}{
		{"translations", func(h *Handler, w http.ResponseWriter, r *http.Request) { h.AudioTranslations(w, r) }, "/audio/translations"},
		{"image edits", func(h *Handler, w http.ResponseWriter, r *http.Request) { h.ImageEdits(w, r) }, "/images/edits"},
		{"image variations", func(h *Handler, w http.ResponseWriter, r *http.Request) { h.ImageVariations(w, r) }, "/images/variations"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var gotPath atomic.Value
			env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotPath.Store(r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"ok":true}`)
			}))

			upload, contentType := buildUploadBody(t, env.providerName+"/"+env.modelName)
			req := env.request("/v1"+tc.wantPath, contentType, upload)
			w := httptest.NewRecorder()
			tc.call(env.handler, w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
			}
			if p, _ := gotPath.Load().(string); p != tc.wantPath {
				t.Errorf("upstream path = %q, want %q", p, tc.wantPath)
			}
		})
	}
}

// sseErrorMaskWriter must be framing-transparent: lines split across writes
// are reassembled before the mask decides, CRLF survives, non-error data lines
// (including key-shaped noise inside base64 image payloads) are untouched, and
// an unterminated trailing error frame is still masked on Flush.
func TestSSEErrorMaskWriter(t *testing.T) {
	const planted = "sk-proj-STANDARDKEY1234567890abcdef1234567890"
	cases := []struct {
		name   string
		writes []string
		want   string
	}{
		{
			name:   "error frame split across writes",
			writes: []string{"event: error\ndata: {\"error\":{\"message\":\"key " + planted[:10], planted[10:] + " bad\"}}\n\n"},
			want:   "event: error\ndata: {\"error\":{\"message\":\"key [redacted] bad\"}}\n\n",
		},
		{
			name:   "crlf framing preserved",
			writes: []string{"data:{\"error\":{\"message\":\"" + planted + "\"}}\r\n\r\n"},
			want:   "data: {\"error\":{\"message\":\"[redacted]\"}}\r\n\r\n",
		},
		{
			name:   "content frame with key-shaped text untouched",
			writes: []string{"data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"/" + planted + "\"}\n\n"},
			want:   "data: {\"type\":\"image_generation.partial_image\",\"b64_json\":\"/" + planted + "\"}\n\n",
		},
		{
			name:   "error null is not an error frame",
			writes: []string{"data: {\"error\":null,\"text\":\"" + planted + "\"}\n\n"},
			want:   "data: {\"error\":null,\"text\":\"" + planted + "\"}\n\n",
		},
		{
			name:   "unterminated trailing error frame masked on flush",
			writes: []string{"data: {\"error\":{\"message\":\"" + planted + "\"}}"},
			want:   "data: {\"error\":{\"message\":\"[redacted]\"}}",
		},
		{
			name:   "error frame without a credential keeps its original framing",
			writes: []string{"data:{\"error\":{\"message\":\"rate limited\"}}\n\n"},
			want:   "data:{\"error\":{\"message\":\"rate limited\"}}\n\n",
		},
		{
			name:   "error object split across data lines is joined before masking",
			writes: []string{"event: error\ndata: {\"type\":\"error\",\n", "data: \"error\":{\"message\":\"key " + planted + " bad\"}}\n\n"},
			want:   "event: error\ndata: {\"type\":\"error\",\ndata: \"error\":{\"message\":\"key [redacted] bad\"}}\n\n",
		},
		{
			name:   "id and retry lines survive a masked event",
			writes: []string{"id: 7\ndata: {\"error\":{\"message\":\"" + planted + "\"}}\nretry: 100\n\n"},
			want:   "id: 7\nretry: 100\ndata: {\"error\":{\"message\":\"[redacted]\"}}\n\n",
		},
		{
			name:   "oversized event passes through raw including its remainder",
			writes: []string{"data: " + strings.Repeat("A", sseErrorMaskEventCap) + "\n", "data: {\"error\":{\"message\":\"" + planted + "\"}}\n\n"},
			want:   "data: " + strings.Repeat("A", sseErrorMaskEventCap) + "\ndata: {\"error\":{\"message\":\"" + planted + "\"}}\n\n",
		},
		{
			name:   "raw mode forwards unterminated chunks until the delimiter",
			writes: []string{"data: " + strings.Repeat("A", sseErrorMaskEventCap) + "\n", "data: tail", "\n\n", "data: {\"error\":{\"message\":\"" + planted + "\"}}\n\n"},
			want:   "data: " + strings.Repeat("A", sseErrorMaskEventCap) + "\ndata: tail\n\ndata: {\"error\":{\"message\":\"[redacted]\"}}\n\n",
		},
		{
			name:   "oversized unterminated line passes through raw",
			writes: []string{"data: " + strings.Repeat("A", sseErrorMaskEventCap), planted + "\n"},
			want:   "data: " + strings.Repeat("A", sseErrorMaskEventCap) + planted + "\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			m := newSSEErrorMaskWriter(&out)
			for _, w := range tc.writes {
				n, err := m.Write([]byte(w))
				if err != nil || n != len(w) {
					t.Fatalf("Write = %d, %v; want %d, nil", n, err, len(w))
				}
			}
			if err := m.Flush(); err != nil {
				t.Fatalf("Flush: %v", err)
			}
			if out.String() != tc.want {
				t.Errorf("got  %q\nwant %q", out.String(), tc.want)
			}
		})
	}

	t.Run("write error reports only fully delivered input", func(t *testing.T) {
		// The client takes a prefix of the second event and dies. The first
		// event was delivered in full; the second was not, so the count the
		// caller logs as bytes delivered must stop at the event boundary.
		m := newSSEErrorMaskWriter(&prefixFailingWriter{accept: 1})
		in := "data: x\n\ndata: y\n\n"
		n, err := m.Write([]byte(in))
		if err == nil {
			t.Fatal("expected the underlying write error")
		}
		if want := len("data: x\n\n"); n != want {
			t.Errorf("delivered = %d, want the first event only (%d)", n, want)
		}
	})

	t.Run("spill and raw-mode write errors surface", func(t *testing.T) {
		big := []byte("data: " + strings.Repeat("A", sseErrorMaskEventCap) + "\n")
		m := newSSEErrorMaskWriter(&prefixFailingWriter{})
		if n, err := m.Write(big); err == nil || n != 0 {
			t.Fatalf("spill onto a dead client: got n=%d err=%v, want 0 and an error", n, err)
		}

		m = newSSEErrorMaskWriter(&prefixFailingWriter{accept: 1})
		if n, err := m.Write(big); err != nil || n != len(big) {
			t.Fatalf("spill: got n=%d err=%v, want %d and nil", n, err, len(big))
		}
		if n, err := m.Write([]byte("data: more\n")); err == nil || n != 0 {
			t.Fatalf("raw-mode line onto a dead client: got n=%d err=%v, want 0 and an error", n, err)
		}
	})

	t.Run("flush error surfaces", func(t *testing.T) {
		m := newSSEErrorMaskWriter(&prefixFailingWriter{})
		if _, err := m.Write([]byte("data: tail")); err != nil {
			t.Fatalf("buffering a partial line must not touch the client: %v", err)
		}
		if err := m.Flush(); err == nil {
			t.Fatal("expected Flush to surface the write error")
		}
	})
}

// prefixFailingWriter accepts the first `accept` downstream writes in full,
// then writes a 3-byte prefix of the next one and fails.
type prefixFailingWriter struct{ accept, calls int }

func (w *prefixFailingWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls <= w.accept {
		return len(p), nil
	}
	return min(3, len(p)), errors.New("client gone")
}

// One logical error object may span several `data:` lines of one event; the
// route-level masking must reassemble it before judging, or the second
// fragment carries the credential through.
func TestImageGenerations_SSEPassthroughMasksSplitErrorFrame(t *testing.T) {
	const planted = "sk-proj-STANDARDKEY1234567890abcdef1234567890"
	sse := "event: error\ndata: {\"type\":\"error\",\ndata: \"error\":{\"message\":\"billing key " + planted + " is invalid\"}}\n\n"
	env := newMultimodalEnv(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, sse)
	}))

	body := fmt.Sprintf(`{"model":"%s/%s","prompt":"a cat","stream":true,"partial_images":1}`, env.providerName, env.modelName)
	req := env.request("/v1/images/generations", "application/json", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handler.ImageGenerations(w, req)

	got := w.Body.String()
	if strings.Contains(got, planted) {
		t.Fatalf("operator credential reached the client through a split error frame:\n%s", got)
	}
	want := "event: error\ndata: {\"type\":\"error\",\ndata: \"error\":{\"message\":\"billing key [redacted] is invalid\"}}\n\n"
	if got != want {
		t.Errorf("masked stream mismatch:\ngot  %q\nwant %q", got, want)
	}
}
