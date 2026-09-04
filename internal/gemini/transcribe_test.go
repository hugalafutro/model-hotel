package gemini

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestTranslateTranscriptionRequest(t *testing.T) {
	audio := []byte("RIFFfakewav")
	body, format, err := TranslateTranscriptionRequest(TranscriptionRequest{Audio: audio, FileName: "speech.WAV", ContentType: "application/octet-stream"})
	if err != nil {
		t.Fatalf("TranslateTranscriptionRequest: %v", err)
	}
	if format != TranscriptionFormatJSON {
		t.Errorf("format = %q, want json by default", format)
	}
	var sent map[string]any
	if err := json.Unmarshal(body, &sent); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	parts := sent["contents"].([]any)[0].(map[string]any)["parts"].([]any)
	if len(parts) != 2 {
		t.Fatalf("parts = %v, want audio + instruction", parts)
	}
	inline := parts[0].(map[string]any)["inlineData"].(map[string]any)
	if inline["mimeType"] != "audio/wav" || inline["data"] != base64.StdEncoding.EncodeToString(audio) {
		t.Errorf("inlineData = %v", inline)
	}
	if parts[1].(map[string]any)["text"] != defaultTranscriptionPrompt {
		t.Errorf("instruction = %v", parts[1])
	}
	if _, ok := sent["generationConfig"]; ok {
		t.Error("generationConfig present; a transcription asks for nothing but text")
	}

	// A client prompt replaces the default instruction; text format rides
	// through; the container comes from the content type when the name
	// gives none.
	body, format, err = TranslateTranscriptionRequest(TranscriptionRequest{Audio: audio, FileName: "clip", ContentType: "audio/ogg; codecs=opus", Prompt: "Names: Prague.", ResponseFormat: "text"})
	if err != nil || format != TranscriptionFormatText {
		t.Fatalf("text format: err=%v format=%q", err, format)
	}
	if !strings.Contains(string(body), `"mimeType":"audio/ogg"`) || !strings.Contains(string(body), `"text":"Names: Prague."`) {
		t.Errorf("body = %s", body)
	}
}

func TestTranslateTranscriptionRequest_Dedicated(t *testing.T) {
	body, _, err := TranslateTranscriptionRequest(TranscriptionRequest{Audio: []byte("x"), FileName: "a.wav", Dedicated: true})
	if err != nil {
		t.Fatalf("dedicated: %v", err)
	}
	if strings.Contains(string(body), `"text"`) {
		t.Errorf("dedicated model got an instruction: %s", body)
	}
	body, _, err = TranslateTranscriptionRequest(TranscriptionRequest{Audio: []byte("x"), FileName: "a.wav", Dedicated: true, Prompt: "Names: Prague."})
	if err != nil || !strings.Contains(string(body), `"text":"Names: Prague."`) {
		t.Errorf("dedicated with client prompt: err=%v body=%s", err, body)
	}
}

func TestTranslateTranscriptionRequest_Refusals(t *testing.T) {
	audio := []byte("x")
	if _, _, err := TranslateTranscriptionRequest(TranscriptionRequest{Audio: audio, FileName: "a.wav", ResponseFormat: "srt"}); !errors.Is(err, ErrTranscriptionFormat) {
		t.Errorf("srt: err = %v, want ErrTranscriptionFormat", err)
	}
	if _, _, err := TranslateTranscriptionRequest(TranscriptionRequest{FileName: "a.wav"}); err == nil || !strings.Contains(err.Error(), "no audio") {
		t.Errorf("empty upload: err = %v", err)
	}
	if _, _, err := TranslateTranscriptionRequest(TranscriptionRequest{Audio: audio, FileName: "blob", ContentType: "application/octet-stream"}); err == nil || !strings.Contains(err.Error(), "audio container") {
		t.Errorf("unknown container: err = %v", err)
	}
}

func TestBuildTranscriptionResponse(t *testing.T) {
	body := []byte(`{"candidates":[{"content":{"parts":[{"text":"thinking","thought":true},{"text":"The pass phrase is "},{"text":"orange elephant seven.\n"}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":66,"candidatesTokenCount":9,"totalTokenCount":75}}`)
	out, ct, usage, err := BuildTranscriptionResponse(body, TranscriptionFormatJSON)
	if err != nil {
		t.Fatalf("json: %v", err)
	}
	if ct != "application/json" || string(out) != `{"text":"The pass phrase is orange elephant seven."}` {
		t.Errorf("json: ct=%q out=%s", ct, out)
	}
	if usage.PromptTokens != 66 || usage.CompletionTokens != 9 {
		t.Errorf("usage = %+v", usage)
	}
	out, ct, _, err = BuildTranscriptionResponse(body, TranscriptionFormatText)
	if err != nil || !strings.HasPrefix(ct, "text/plain") || string(out) != "The pass phrase is orange elephant seven." {
		t.Errorf("text: err=%v ct=%q out=%s", err, ct, out)
	}
}

// gemini-3.5-transcribe answers with an audioTranscription part and no
// candidatesTokenCount, as observed live.
func TestBuildTranscriptionResponse_DedicatedShape(t *testing.T) {
	body := []byte(`{"candidates":[{"content":{"parts":[{"audioTranscription":{"text":"The pass phrase is orange elephant seven."}}],"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":66,"totalTokenCount":66},"modelVersion":"gemini-3.5-transcribe"}`)
	out, _, usage, err := BuildTranscriptionResponse(body, TranscriptionFormatJSON)
	if err != nil {
		t.Fatalf("dedicated: %v", err)
	}
	if string(out) != `{"text":"The pass phrase is orange elephant seven."}` || usage.PromptTokens != 66 || usage.CompletionTokens != 0 {
		t.Errorf("out=%s usage=%+v", out, usage)
	}
}

func TestBuildTranscriptionResponse_NoText(t *testing.T) {
	cases := map[string]string{
		"empty answer": `{"candidates":[{"content":{"parts":[],"role":"model"},"finishReason":"MAX_TOKENS"}]}`,
		"blocked":      `{"promptFeedback":{"blockReason":"SAFETY"}}`,
	}
	for name, body := range cases {
		if _, _, _, err := BuildTranscriptionResponse([]byte(body), TranscriptionFormatJSON); !errors.Is(err, ErrTranscriptionNoText) {
			t.Errorf("%s: err = %v, want ErrTranscriptionNoText", name, err)
		}
	}
	if _, _, _, err := BuildTranscriptionResponse([]byte(`not json`), TranscriptionFormatJSON); err == nil || errors.Is(err, ErrTranscriptionNoText) {
		t.Errorf("garbage: err = %v, want a plain decode error", err)
	}
}
