package proxy

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/model"
	"github.com/hugalafutro/model-hotel/internal/provider"
)

// speechUpstream is a Google-shaped upstream for /v1/audio/speech: it records
// each generateContent request and answers with the audio the test hands it.
type speechUpstream struct {
	mu     sync.Mutex
	paths  []string
	bodies []string
	auth   []string
	answer func(w http.ResponseWriter)
}

func (u *speechUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	raw, _ := io.ReadAll(r.Body)
	u.mu.Lock()
	u.paths = append(u.paths, r.URL.Path)
	u.bodies = append(u.bodies, string(raw))
	u.auth = append(u.auth, r.Header.Get("x-goog-api-key")+"|"+r.Header.Get("Authorization"))
	u.mu.Unlock()
	u.answer(w)
}

func speechAudioAnswer(pcm []byte) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/L16;codec=pcm;rate=24000","data":"%s"}}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":250,"totalTokenCount":261}}`, base64.StdEncoding.EncodeToString(pcm))
	}
}

// A speech request to a Google AI Studio TTS model goes to the native
// generateContent route (the compat base folded up one segment, the model's
// own path, the key in x-goog-api-key), asks for AUDIO with the voice the
// client named mapped to a Gemini one, and comes back as the wav the client
// asked for, built from the PCM the model produced.
func TestAudioSpeech_GeminiNativeRoute(t *testing.T) {
	pcm := []byte{1, 0, 2, 0, 3, 0, 4, 0, 5, 0, 6, 0}
	up := &speechUpstream{answer: speechAudioAnswer(pcm)}
	env := newMultimodalEnvTyped(t, up, `["audio"]`, "google", "/v1beta/openai")

	body := fmt.Sprintf(`{"model":"%s/%s","input":"Say hello","voice":"alloy","response_format":"wav","speed":1.1}`, env.providerName, env.modelName)
	w := httptest.NewRecorder()
	env.handler.AudioSpeech(w, env.request("/v1/audio/speech", "application/json", strings.NewReader(body)))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "audio/wav" {
		t.Errorf("Content-Type = %q, want audio/wav", ct)
	}
	got := w.Body.Bytes()
	if len(got) != 44+len(pcm) || string(got[:4]) != "RIFF" || !bytes.Equal(got[44:], pcm) {
		t.Fatalf("body = %d bytes (%q...), want a 44-byte RIFF header over the %d PCM bytes", len(got), got[:4], len(pcm))
	}
	if rate := binary.LittleEndian.Uint32(got[24:28]); rate != 24000 {
		t.Errorf("sample rate = %d, want 24000", rate)
	}
	if cl := w.Header().Get("Content-Length"); cl != fmt.Sprint(len(got)) {
		t.Errorf("Content-Length = %q, want %d", cl, len(got))
	}

	up.mu.Lock()
	defer up.mu.Unlock()
	if len(up.paths) != 1 || up.paths[0] != "/v1beta/models/"+env.modelName+":generateContent" {
		t.Fatalf("upstream paths = %v, want the native generateContent route under the folded base", up.paths)
	}
	if up.auth[0] != "test-api-key|" {
		t.Errorf("auth headers = %q, want the key in x-goog-api-key and no Bearer", up.auth[0])
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(up.bodies[0]), &sent); err != nil {
		t.Fatalf("upstream body not JSON: %v", err)
	}
	gc := sent["generationConfig"].(map[string]any)
	if mods, _ := json.Marshal(gc["responseModalities"]); string(mods) != `["AUDIO"]` {
		t.Errorf("responseModalities = %s", mods)
	}
	voice := gc["speechConfig"].(map[string]any)["voiceConfig"].(map[string]any)["prebuiltVoiceConfig"].(map[string]any)["voiceName"]
	if voice != "Kore" {
		t.Errorf("voice = %v, want alloy mapped to Kore", voice)
	}
	if strings.Contains(up.bodies[0], `"speed"`) || strings.Contains(up.bodies[0], `"model"`) {
		t.Errorf("OpenAI fields leaked into the native request: %s", up.bodies[0])
	}
}

// pcm hands the model's bytes over as they are, under audio/pcm.
func TestAudioSpeech_GeminiPCM(t *testing.T) {
	pcm := []byte{7, 0, 8, 0}
	env := newMultimodalEnvTyped(t, &speechUpstream{answer: speechAudioAnswer(pcm)}, `["audio"]`, "google", "/v1beta/openai")
	body := fmt.Sprintf(`{"model":"%s/%s","input":"hi","response_format":"pcm"}`, env.providerName, env.modelName)
	w := httptest.NewRecorder()
	env.handler.AudioSpeech(w, env.request("/v1/audio/speech", "application/json", strings.NewReader(body)))
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "audio/pcm" || !bytes.Equal(w.Body.Bytes(), pcm) {
		t.Fatalf("status %d, type %q, body %v; want the raw PCM under audio/pcm", w.Code, w.Header().Get("Content-Type"), w.Body.Bytes())
	}
}

// A format the model cannot produce, on a request no candidate can serve
// otherwise, is the client's 400 before any upstream request: the message
// names what the model does produce.
func TestAudioSpeech_GeminiRefusesCompressedFormats(t *testing.T) {
	up := &speechUpstream{answer: speechAudioAnswer([]byte{0, 0})}
	env := newMultimodalEnvTyped(t, up, `["audio"]`, "google", "/v1beta/openai")
	body := fmt.Sprintf(`{"model":"%s/%s","input":"hi","voice":"alloy","response_format":"mp3"}`, env.providerName, env.modelName)
	w := httptest.NewRecorder()
	env.handler.AudioSpeech(w, env.request("/v1/audio/speech", "application/json", strings.NewReader(body)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body: %s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "wav or pcm") || !strings.Contains(w.Body.String(), "mp3") {
		t.Errorf("body = %s, want the refusal naming mp3 and the formats the model produces", w.Body.String())
	}
	up.mu.Lock()
	defer up.mu.Unlock()
	if len(up.paths) != 0 {
		t.Errorf("upstream saw %v, want nothing", up.paths)
	}
}

// A generateContent answer without an audio part (the model replied in text,
// or the prompt was blocked) is not speech: the attempt fails over, and on
// the last candidate the client gets the gateway's error rather than a
// zero-byte wav.
func TestAudioSpeech_GeminiNoAudioIsAFailedAttempt(t *testing.T) {
	up := &speechUpstream{answer: func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"I cannot say that."}],"role":"model"},"finishReason":"STOP"}]}`)
	}}
	env := newMultimodalEnvTyped(t, up, `["audio"]`, "google", "/v1beta/openai")
	body := fmt.Sprintf(`{"model":"%s/%s","input":"hi"}`, env.providerName, env.modelName)
	w := httptest.NewRecorder()
	env.handler.AudioSpeech(w, env.request("/v1/audio/speech", "application/json", strings.NewReader(body)))
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (body: %s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want the gateway's JSON error and not an audio body", ct)
	}
}

// The usage the adapter read off the answer meters the request, in place of
// the estimate the binary path would otherwise fall back to.
func TestServeStreamedPassthrough_AdapterUsageWins(t *testing.T) {
	h := newIntegrationHandler()
	t.Cleanup(func() { stopUnitHandler(h) })
	vkRepo := &mockVirtualKeyRepo{}
	h.virtualKeyRepo = vkRepo
	logData := &requestLogData{
		id:              uuid.New().String(),
		modelID:         "gemini-2.5-flash-preview-tts",
		endpointType:    endpointTypeTTS,
		virtualKeyName:  "test-key",
		virtualKeyID:    "00000000-0000-0000-0000-000000000001",
		state:           "streaming",
		promptTextBytes: 400,
	}
	st := &requestState{startTime: time.Now(), logData: logData, vkHash: "test-hash", passthroughUsage: &passthroughUsage{prompt: 11, completion: 250}}
	h.insertRequestLogAsync(logData)
	time.Sleep(20 * time.Millisecond)
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"audio/wav"}}, Body: io.NopCloser(strings.NewReader("RIFF....WAVE"))}
	h.serveStreamedPassthrough(httptest.NewRecorder(), httptest.NewRequest("POST", "/v1/audio/speech", http.NoBody), st, modelCandidate{
		model:    &model.Model{ID: uuid.New(), ModelID: "gemini-2.5-flash-preview-tts"},
		provider: &provider.Provider{ID: uuid.New(), Name: "test-provider"},
	}, resp, "audio/wav", false, 1, 10.0)
	if got := singleAddTokens(t, vkRepo); got != 261 {
		t.Errorf("charged %d tokens, want the answer's 11 + 250 rather than the 100-token estimate", got)
	}
}
