package proxy

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/failover"
)

// A music request on a KoboldCpp provider is forwarded verbatim to the server's
// native route at its origin, with the body's model rewritten like every other
// JSON pass-through, and the WAV it answers streams back untouched.
func TestAudioMusic_KoboldCppNativeRoute(t *testing.T) {
	wav := []byte("RIFF\x24\x00\x00\x00WAVEfmt ")
	var gotPath atomic.Value
	var gotBody atomic.Value
	env := newMultimodalEnvTyped(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath.Store(r.URL.Path)
		var buf bytes.Buffer
		_, _ = buf.ReadFrom(r.Body)
		gotBody.Store(buf.String())
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(wav)
	}), `["audio"]`, "koboldcpp", "/v1")

	body := fmt.Sprintf(`{"model":"%s/%s","caption":"upbeat synth pop","lyrics":"[verse]\nla la","duration":10}`, env.providerName, env.modelName)
	req := env.request("/v1/audio/music", "application/json", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handler.AudioMusic(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "audio/wav" {
		t.Errorf("Content-Type = %q, want audio/wav", ct)
	}
	if !bytes.Equal(w.Body.Bytes(), wav) {
		t.Errorf("binary body corrupted: got %q, want %q", w.Body.Bytes(), wav)
	}
	if p, _ := gotPath.Load().(string); p != "/api/extra/music/generate" {
		t.Errorf("upstream path = %q, want /api/extra/music/generate at the origin, not under /v1", p)
	}
	upstream, _ := gotBody.Load().(string)
	if !strings.Contains(upstream, `"model":"`+env.modelName+`"`) || !strings.Contains(upstream, `"caption":"upbeat synth pop"`) || !strings.Contains(upstream, `"duration":10`) {
		t.Errorf("upstream body = %s, want the model rewritten and caption/duration kept", upstream)
	}
}

// A model on any other provider type has no music route, so the request is
// the client's 400 before anything is sent: the caption and lyrics never
// reach a provider that would only 404 them.
func TestAudioMusic_NonKoboldCppProviderIsRefused(t *testing.T) {
	var called atomic.Bool
	env := newMultimodalEnvTyped(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Store(true)
		w.WriteHeader(http.StatusNotFound)
	}), `["audio"]`, "openai", "")

	body := fmt.Sprintf(`{"model":"%s/%s","caption":"a waltz","duration":10}`, env.providerName, env.modelName)
	req := env.request("/v1/audio/music", "application/json", strings.NewReader(body))
	w := httptest.NewRecorder()
	env.handler.AudioMusic(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "KoboldCpp") {
		t.Errorf("error body = %s, want the KoboldCpp-only constraint named", w.Body.String())
	}
	if called.Load() {
		t.Error("upstream was contacted; a non-KoboldCpp provider must never see a music body")
	}
}

// The music family is long-running like image generation and speech: a clip
// takes tens of seconds on a small GPU, longer than the standard budget.
func TestIsLongRunningEndpoint_Music(t *testing.T) {
	if !isLongRunningEndpoint(endpointTypeMusic) {
		t.Fatal("music must be a long-running family")
	}
}

// Through the registered route, with the key in the header: a hotel/ group
// holding a KoboldCpp music model and a model on another provider type sends
// the request to the KoboldCpp alone, and the row it leaves carries the music
// family. The other provider is never contacted, whatever its order in the
// group.
func TestAudioMusic_RoutedMixedGroupKeepsOnlyKoboldCpp(t *testing.T) {
	wav := []byte("RIFF\x24\x00\x00\x00WAVEfmt ")
	var koboldCalls atomic.Int32
	env := newMultimodalEnvTyped(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		koboldCalls.Add(1)
		w.Header().Set("Content-Type", "audio/wav")
		_, _ = w.Write(wav)
	}), `["audio"]`, "koboldcpp", "/v1")
	var otherCalls atomic.Int32
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		otherCalls.Add(1)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(other.Close)
	_, _, otherModelUUID, _ := createMultimodalProviderTyped(t, other.URL, `["audio"]`, "openai")

	groupName := env.modelName
	// The other provider first, so order alone cannot explain the outcome.
	if _, err := failover.NewRepository(testDB.Pool()).UpsertWithConfig(context.Background(), groupName,
		[]uuid.UUID{otherModelUUID, env.modelUUID},
		map[string]bool{otherModelUUID.String(): true, env.modelUUID.String(): true},
		nil, nil, nil, nil); err != nil {
		t.Fatalf("failover group: %v", err)
	}

	r := chi.NewRouter()
	env.handler.Register(r)
	body := fmt.Sprintf(`{"model":"hotel/%s","caption":"a waltz","lyrics":"","duration":10}`, groupName)
	req := httptest.NewRequest(http.MethodPost, "/audio/music", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+env.rawKey)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "audio/wav" || !bytes.Equal(w.Body.Bytes(), wav) {
		t.Fatalf("status %d type %q body %q, want the KoboldCpp WAV", w.Code, w.Header().Get("Content-Type"), w.Body.Bytes())
	}
	if koboldCalls.Load() != 1 || otherCalls.Load() != 0 {
		t.Errorf("KoboldCpp saw %d requests, the other provider %d; want 1 and 0", koboldCalls.Load(), otherCalls.Load())
	}
	if state, status, _, _ := waitForRequestLog(t, env.providerID); state != "completed" || status != http.StatusOK {
		t.Errorf("row state=%q status=%d, want completed 200", state, status)
	}
	var family string
	if err := testDB.Pool().QueryRow(context.Background(),
		`SELECT endpoint_type FROM request_logs WHERE provider_id = $1 ORDER BY created_at DESC LIMIT 1`, env.providerID).Scan(&family); err != nil {
		t.Fatalf("read row: %v", err)
	}
	if family != endpointTypeMusic {
		t.Errorf("endpoint_type = %q, want %q", family, endpointTypeMusic)
	}
}
