package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// transcriptionAnswer is a generateContent answer carrying a transcript.
func transcriptionAnswer(text string) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"candidates":[{"content":{"parts":[{"text":%q}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":66,"candidatesTokenCount":9,"totalTokenCount":75}}`, text)
	}
}

// buildTranscriptionForm builds an OpenAI transcription form with the given
// extra fields beside the model and the upload.
func buildTranscriptionForm(t *testing.T, modelValue string, fields map[string]string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	if err := mw.WriteField("model", modelValue); err != nil {
		t.Fatalf("WriteField: %v", err)
	}
	for k, v := range fields {
		if err := mw.WriteField(k, v); err != nil {
			t.Fatalf("WriteField %s: %v", k, err)
		}
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

// A transcription request to a Google AI Studio model goes to the native
// generateContent route (the compat base folded up one segment, the model's
// own path, the key in x-goog-api-key), carries the upload as an inlineData
// audio part beside the client's prompt, and comes back as OpenAI's
// transcription JSON built from the text the model produced, metered from
// the usage the answer carried.
func TestAudioTranscriptions_GeminiNativeRoute(t *testing.T) {
	up := &speechUpstream{answer: transcriptionAnswer("The pass phrase is orange elephant seven.")}
	env := newMultimodalEnvTyped(t, up, `["text"]`, "google", "/v1beta/openai")

	form, contentType := buildTranscriptionForm(t, env.providerName+"/"+env.modelName, map[string]string{"language": "en", "prompt": "Names: Prague."})
	w := httptest.NewRecorder()
	env.handler.AudioTranscriptions(w, env.request("/v1/audio/transcriptions", contentType, form))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if got := strings.TrimSpace(w.Body.String()); got != `{"text":"The pass phrase is orange elephant seven."}` {
		t.Errorf("body = %s", got)
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
	parts := sent["contents"].([]any)[0].(map[string]any)["parts"].([]any)
	if len(parts) != 2 {
		t.Fatalf("parts = %v, want audio + prompt", parts)
	}
	inline := parts[0].(map[string]any)["inlineData"].(map[string]any)
	if inline["mimeType"] != "audio/wav" || inline["data"] != "UklGRmZha2V3YXZkYXRh" {
		t.Errorf("inlineData = %v", inline)
	}
	if parts[1].(map[string]any)["text"] != "Names: Prague." {
		t.Errorf("prompt part = %v", parts[1])
	}
	if strings.Contains(up.bodies[0], `"language"`) || strings.Contains(up.bodies[0], `"model"`) {
		t.Errorf("form fields leaked into the native request: %s", up.bodies[0])
	}
}

// text delivers the transcript as plain text.
func TestAudioTranscriptions_GeminiTextFormat(t *testing.T) {
	env := newMultimodalEnvTyped(t, &speechUpstream{answer: transcriptionAnswer("hello there")}, `["text"]`, "google", "/v1beta/openai")
	form, contentType := buildTranscriptionForm(t, env.providerName+"/"+env.modelName, map[string]string{"response_format": "text"})
	w := httptest.NewRecorder()
	env.handler.AudioTranscriptions(w, env.request("/v1/audio/transcriptions", contentType, form))
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") || w.Body.String() != "hello there" {
		t.Fatalf("status %d, type %q, body %q; want the transcript as text/plain", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
}

// A format the adapter cannot produce, on a request no candidate can serve
// otherwise, is the client's 400 before any upstream request.
func TestAudioTranscriptions_GeminiRefusesTimestampedFormats(t *testing.T) {
	up := &speechUpstream{answer: transcriptionAnswer("x")}
	env := newMultimodalEnvTyped(t, up, `["text"]`, "google", "/v1beta/openai")
	form, contentType := buildTranscriptionForm(t, env.providerName+"/"+env.modelName, map[string]string{"response_format": "srt"})
	w := httptest.NewRecorder()
	env.handler.AudioTranscriptions(w, env.request("/v1/audio/transcriptions", contentType, form))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "json or text") {
		t.Fatalf("status %d body %s; want a 400 naming the formats the adapter produces", w.Code, w.Body.String())
	}
	up.mu.Lock()
	defer up.mu.Unlock()
	if len(up.paths) != 0 {
		t.Errorf("upstream contacted: %v", up.paths)
	}
}

// An answer without text is untranslatable: with no other candidate the
// request fails, and nothing is served as a transcript.
func TestAudioTranscriptions_GeminiEmptyAnswerFails(t *testing.T) {
	env := newMultimodalEnvTyped(t, &speechUpstream{answer: func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"candidates":[{"content":{"parts":[],"role":"model"},"finishReason":"STOP"}]}`)
	}}, `["text"]`, "google", "/v1beta/openai")
	form, contentType := buildTranscriptionForm(t, env.providerName+"/"+env.modelName, nil)
	w := httptest.NewRecorder()
	env.handler.AudioTranscriptions(w, env.request("/v1/audio/transcriptions", contentType, form))
	if w.Code == http.StatusOK {
		t.Fatalf("status 200 with body %s; want the request to fail", w.Body.String())
	}
}

// A Google model whose discovered input modalities name no audio keeps the
// pass-through: the form goes to the compat route as it is.
func TestAudioTranscriptions_GeminiTextOnlyModelStaysPassthrough(t *testing.T) {
	up := &speechUpstream{answer: func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"text":"passed through"}`)
	}}
	env := newMultimodalEnvTyped(t, up, `["text"]`, "google", "/v1beta/openai")
	if _, err := testDB.Pool().Exec(context.Background(), `UPDATE models SET input_modalities = '["text","image"]' WHERE id = $1`, env.modelUUID); err != nil {
		t.Fatalf("update model: %v", err)
	}
	model.InvalidateModelCache()

	form, contentType := buildTranscriptionForm(t, env.providerName+"/"+env.modelName, nil)
	w := httptest.NewRecorder()
	env.handler.AudioTranscriptions(w, env.request("/v1/audio/transcriptions", contentType, form))
	if w.Code != http.StatusOK || strings.TrimSpace(w.Body.String()) != `{"text":"passed through"}` {
		t.Fatalf("status %d body %s; want the compat answer verbatim", w.Code, w.Body.String())
	}
	up.mu.Lock()
	defer up.mu.Unlock()
	if len(up.paths) != 1 || up.paths[0] != "/v1beta/openai/audio/transcriptions" {
		t.Errorf("upstream paths = %v, want the compat transcription route", up.paths)
	}
}
