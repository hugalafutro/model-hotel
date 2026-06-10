package proxy

import (
	"bytes"
	"context"
	"encoding/json"
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

	providerName, providerID, modelUUID, modelName := createMultimodalProvider(t, upstream.URL)

	virtualKeyName := "mm-key-" + uuid.New().String()[:8]
	keyHash := virtualkey.Hash(virtualKeyName)
	if _, err := virtualKeyRepo.Create(context.Background(), virtualKeyName, keyHash, "mm-"+keyHash[:8], nil, nil, nil, nil); err != nil {
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
		OutputModalities: "[]",
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
		var req map[string]interface{}
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
