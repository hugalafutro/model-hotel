package gemini

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestTranslateSpeechRequest(t *testing.T) {
	body, model, format, err := TranslateSpeechRequest([]byte(`{"model":"gemini-2.5-flash-preview-tts","input":"Say cheerfully: hello","voice":"alloy","speed":1.2,"instructions":"cheerful"}`))
	if err != nil {
		t.Fatalf("TranslateSpeechRequest: %v", err)
	}
	if model != "gemini-2.5-flash-preview-tts" || format != SpeechFormatWAV {
		t.Fatalf("model=%q format=%q, want the model and wav by default", model, format)
	}
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("body not JSON: %v", err)
	}
	contents := m["contents"].([]any)
	part := contents[0].(map[string]any)["parts"].([]any)[0].(map[string]any)
	if part["text"] != "Say cheerfully: hello" {
		t.Errorf("text part = %v", part)
	}
	gc := m["generationConfig"].(map[string]any)
	if mods, _ := json.Marshal(gc["responseModalities"]); string(mods) != `["AUDIO"]` {
		t.Errorf("responseModalities = %s, want AUDIO alone", mods)
	}
	voice := gc["speechConfig"].(map[string]any)["voiceConfig"].(map[string]any)["prebuiltVoiceConfig"].(map[string]any)["voiceName"]
	if voice != "Kore" {
		t.Errorf("voice = %v, want alloy mapped to Kore", voice)
	}
	for _, k := range []string{"speed", "instructions", "model", "responseMimeType"} {
		if _, has := m[k]; has {
			t.Errorf("%s leaked into the native request", k)
		}
	}
}

func TestSpeechVoice(t *testing.T) {
	for in, want := range map[string]string{"": "Kore", "alloy": "Kore", "Shimmer": "Zephyr", "onyx": "Charon", "Puck": "Puck", "Sulafat": "Sulafat"} {
		if got := SpeechVoice(in); got != want {
			t.Errorf("SpeechVoice(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSpeechFormat(t *testing.T) {
	for in, want := range map[string]string{"": "wav", "wav": "wav", "WAV": "wav", "pcm": "pcm"} {
		got, err := SpeechFormat(in)
		if err != nil || got != want {
			t.Errorf("SpeechFormat(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"mp3", "opus", "aac", "flac"} {
		_, err := SpeechFormat(in)
		if !errors.Is(err, ErrSpeechFormat) || !strings.Contains(err.Error(), "wav or pcm") {
			t.Errorf("SpeechFormat(%q) = %v, want ErrSpeechFormat naming the formats it takes", in, err)
		}
	}
	if _, _, _, err := TranslateSpeechRequest([]byte(`{"model":"m","input":"hi","response_format":"mp3"}`)); !errors.Is(err, ErrSpeechFormat) {
		t.Errorf("mp3 request = %v, want ErrSpeechFormat", err)
	}
}

func TestTranslateSpeechRequest_Rejects(t *testing.T) {
	for name, body := range map[string]string{"empty input": `{"model":"m","input":"  "}`, "not json": `{"model":`, "no input": `{"model":"m"}`} {
		if _, _, _, err := TranslateSpeechRequest([]byte(body)); err == nil || errors.Is(err, ErrSpeechFormat) {
			t.Errorf("%s: err = %v, want a plain error", name, err)
		}
	}
}

func speechAnswer(pcm []byte, mime string) []byte {
	return []byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"` + mime + `","data":"` + base64.StdEncoding.EncodeToString(pcm) + `"}}],"role":"model"},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":11,"candidatesTokenCount":250,"totalTokenCount":261}}`)
}

func TestBuildSpeechResponse_WAV(t *testing.T) {
	pcm := []byte{1, 0, 2, 0, 3, 0, 4, 0}
	audio, ct, usage, err := BuildSpeechResponse(speechAnswer(pcm, "audio/L16;codec=pcm;rate=24000"), SpeechFormatWAV)
	if err != nil {
		t.Fatalf("BuildSpeechResponse: %v", err)
	}
	if ct != "audio/wav" || len(audio) != 44+len(pcm) {
		t.Fatalf("content type %q, %d bytes; want audio/wav with a 44-byte header", ct, len(audio))
	}
	if string(audio[0:4]) != "RIFF" || string(audio[8:12]) != "WAVE" || string(audio[36:40]) != "data" {
		t.Errorf("header chunks = %q %q %q", audio[0:4], audio[8:12], audio[36:40])
	}
	if got := binary.LittleEndian.Uint32(audio[24:28]); got != 24000 {
		t.Errorf("sample rate = %d, want 24000", got)
	}
	if got := binary.LittleEndian.Uint16(audio[22:24]); got != 1 {
		t.Errorf("channels = %d, want mono", got)
	}
	if got := binary.LittleEndian.Uint16(audio[34:36]); got != 16 {
		t.Errorf("bits per sample = %d, want 16", got)
	}
	if got := binary.LittleEndian.Uint32(audio[40:44]); int(got) != len(pcm) {
		t.Errorf("data size = %d, want %d", got, len(pcm))
	}
	if got := binary.LittleEndian.Uint32(audio[4:8]); int(got) != 36+len(pcm) {
		t.Errorf("riff size = %d, want %d", got, 36+len(pcm))
	}
	if !bytes.Equal(audio[44:], pcm) {
		t.Error("pcm payload not carried")
	}
	if usage.PromptTokens != 11 || usage.CompletionTokens != 250 {
		t.Errorf("usage = %+v, want 11 in, 250 out", usage)
	}
}

func TestBuildSpeechResponse_PCMAndRate(t *testing.T) {
	pcm := []byte{9, 9, 9, 9}
	audio, ct, _, err := BuildSpeechResponse(speechAnswer(pcm, "audio/L16;codec=pcm;rate=16000"), SpeechFormatPCM)
	if err != nil || ct != "audio/pcm" || !bytes.Equal(audio, pcm) {
		t.Fatalf("pcm = %q %q %v, want the bytes as they are", ct, audio, err)
	}
	wav, _, _, err := BuildSpeechResponse(speechAnswer(pcm, "audio/L16;codec=pcm;rate=16000"), SpeechFormatWAV)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.LittleEndian.Uint32(wav[24:28]); got != 16000 {
		t.Errorf("sample rate = %d, want the mime type's 16000", got)
	}
	for mime, want := range map[string]int{"audio/L16": 24000, "audio/L16;rate=abc": 24000, "audio/L16; codec=pcm; rate=48000": 48000} {
		if got := sampleRateOf(mime); got != want {
			t.Errorf("sampleRateOf(%q) = %d, want %d", mime, got, want)
		}
	}
}

func TestBuildSpeechResponse_NoAudio(t *testing.T) {
	for name, body := range map[string]string{
		"text only":   `{"candidates":[{"content":{"parts":[{"text":"I cannot"}]},"finishReason":"STOP"}]}`,
		"empty":       `{"candidates":[]}`,
		"blocked":     `{"promptFeedback":{"blockReason":"SAFETY"}}`,
		"image blob":  `{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"image/png","data":"AAAA"}}]}}]}`,
		"finish said": `{"candidates":[{"content":{"parts":[]},"finishReason":"MAX_TOKENS"}]}`,
	} {
		_, _, _, err := BuildSpeechResponse([]byte(body), SpeechFormatWAV)
		if !errors.Is(err, ErrSpeechNoAudio) {
			t.Errorf("%s: err = %v, want ErrSpeechNoAudio", name, err)
		}
		if name == "blocked" && !strings.Contains(err.Error(), "SAFETY") {
			t.Errorf("blocked: err = %v, want the reason named", err)
		}
		if name == "finish said" && !strings.Contains(err.Error(), "MAX_TOKENS") {
			t.Errorf("finish said: err = %v, want the finish reason named", err)
		}
	}
	if _, _, _, err := BuildSpeechResponse([]byte(`not json`), SpeechFormatWAV); err == nil || errors.Is(err, ErrSpeechNoAudio) {
		t.Errorf("undecodable body: err = %v, want a plain decode error", err)
	}
	if _, _, _, err := BuildSpeechResponse([]byte(`{"candidates":[{"content":{"parts":[{"inlineData":{"mimeType":"audio/L16","data":"!!!"}}]}}]}`), SpeechFormatWAV); err == nil || errors.Is(err, ErrSpeechNoAudio) {
		t.Errorf("bad base64: err = %v, want a plain error", err)
	}
}
