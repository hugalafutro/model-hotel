package provider

import (
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/model"
)

func TestDeriveModelClass(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		output  []string
		modelID string
		want    string
	}{
		{"text chat", []string{"text"}, []string{"text"}, "gpt-4o", "chat"},
		{"vision chat", []string{"text", "image"}, []string{"text"}, "gpt-4o", "chat"},
		{"embedding by output", []string{"text"}, []string{"embedding"}, "some-model", "embedding"},
		{"rerank by output", []string{"text"}, []string{"rerank"}, "some-model", "rerank"},
		{"image gen by output", []string{"text"}, []string{"image"}, "grok-2-image", "image"},
		{"video gen by output", []string{"text", "image"}, []string{"video"}, "sora-like", "video"},
		{"tts by output", []string{"text"}, []string{"audio"}, "some-voice", "tts"},
		{"text plus image output stays chat", []string{"text"}, []string{"text", "image"}, "gemini-image", "chat"},
		{"mixed media without text prefers video", nil, []string{"image", "video"}, "media-gen", "video"},
		{"whisper stt tiebreak", []string{"audio"}, []string{"text"}, "whisper-large-v3", "stt"},
		{"transcribe segment stt", []string{"audio"}, []string{"text"}, "gpt-4o-transcribe", "stt"},
		{"audio-input chat is not stt", []string{"text", "audio"}, []string{"text"}, "gpt-4o-audio-preview", "chat"},
		{"empty arrays embed name", nil, nil, "nomic-embed-text", "embedding"},
		{"empty arrays rerank name", nil, nil, "bge-reranker-v2-m3", "rerank"},
		{"empty arrays dall-e name", nil, nil, "dall-e-3", "image"},
		{"empty arrays tts segment", nil, nil, "tts-1", "tts"},
		{"empty arrays gpt tts segment", nil, nil, "gpt-4o-mini-tts", "tts"},
		{"empty arrays whisper name", nil, nil, "whisper-1", "stt"},
		{"empty arrays unknown defaults chat", nil, nil, "llama-3.3-70b", "chat"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DeriveModelClass(tt.input, tt.output, tt.modelID); got != tt.want {
				t.Errorf("DeriveModelClass(%v, %v, %q) = %q, want %q",
					tt.input, tt.output, tt.modelID, got, tt.want)
			}
		})
	}
}

func TestNormalizeModelClassification(t *testing.T) {
	tests := []struct {
		name       string
		in         model.Model
		wantInput  string
		wantOutput string
		wantClass  string
	}{
		{
			name:       "bare model defaults to text chat",
			in:         model.Model{ModelID: "llama-3.3-70b"},
			wantInput:  `["text"]`,
			wantOutput: `["text"]`,
			wantClass:  "chat",
		},
		{
			name: "arrow modality parsed into arrays",
			in: model.Model{
				ModelID:  "some/vision-model",
				Modality: "text+image->text",
			},
			wantInput:  `["text","image"]`,
			wantOutput: `["text"]`,
			wantClass:  "chat",
		},
		{
			name: "arrow modality does not overwrite existing arrays",
			in: model.Model{
				ModelID:         "some/model",
				Modality:        "text->text",
				InputModalities: `["text","image"]`,
			},
			wantInput:  `["text","image"]`,
			wantOutput: `["text"]`,
			wantClass:  "chat",
		},
		{
			name:       "explicit image class kept and arrays filled",
			in:         model.Model{ModelID: "grok-2-image-1212", Modality: "image"},
			wantInput:  `["text"]`,
			wantOutput: `["image"]`,
			wantClass:  "image",
		},
		{
			name:       "explicit rerank class kept",
			in:         model.Model{ModelID: "rerank-english-v3.0", Modality: "rerank"},
			wantInput:  `["text"]`,
			wantOutput: `["rerank"]`,
			wantClass:  "rerank",
		},
		{
			name:       "explicit stt class fills audio input",
			in:         model.Model{ModelID: "whisper-1", Modality: "stt"},
			wantInput:  `["audio"]`,
			wantOutput: `["text"]`,
			wantClass:  "stt",
		},
		{
			name: "vision capability unions into input array",
			in: model.Model{
				ModelID:      "claude-3-5-sonnet",
				Capabilities: `{"vision":true}`,
			},
			wantInput:  `["text","image"]`,
			wantOutput: `["text"]`,
			wantClass:  "chat",
		},
		{
			name: "legacy vision word rederives to chat",
			in: model.Model{
				ModelID:          "claude-3-5-sonnet",
				Modality:         "vision",
				InputModalities:  `["text","image"]`,
				OutputModalities: `["text"]`,
			},
			wantInput:  `["text","image"]`,
			wantOutput: `["text"]`,
			wantClass:  "chat",
		},
		{
			name: "legacy vision word seeds image input when arrays empty",
			in: model.Model{
				ModelID:  "pixtral-12b",
				Modality: "vision",
			},
			wantInput:  `["text","image"]`,
			wantOutput: `["text"]`,
			wantClass:  "chat",
		},
		{
			name: "legacy multimodal word seeds image and audio input",
			in: model.Model{
				ModelID:  "gemini-2.0-flash",
				Modality: "multimodal",
			},
			wantInput:  `["text","image","audio"]`,
			wantOutput: `["text"]`,
			wantClass:  "chat",
		},
		{
			name: "legacy video word is input video not video gen",
			in: model.Model{
				ModelID:  "gemini-1.5-pro",
				Modality: "video",
			},
			wantInput:  `["text","video"]`,
			wantOutput: `["text"]`,
			wantClass:  "chat",
		},
		{
			name: "video-only output classes as video gen",
			in: model.Model{
				ModelID:          "wan-2.2",
				OutputModalities: `["video"]`,
			},
			wantInput:  `["text"]`,
			wantOutput: `["video"]`,
			wantClass:  "video",
		},
		{
			name: "arrays lowercased and deduped in canonical order",
			in: model.Model{
				ModelID:          "some/model",
				InputModalities:  `["IMAGE","Text","image"]`,
				OutputModalities: `["TEXT"]`,
			},
			wantInput:  `["text","image"]`,
			wantOutput: `["text"]`,
			wantClass:  "chat",
		},
		{
			name: "malformed arrays tolerated",
			in: model.Model{
				ModelID:          "some/model",
				InputModalities:  `not-json`,
				OutputModalities: `{"nope":1}`,
			},
			wantInput:  `["text"]`,
			wantOutput: `["text"]`,
			wantClass:  "chat",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := tt.in
			NormalizeModelClassification(&m)
			if m.InputModalities != tt.wantInput {
				t.Errorf("InputModalities = %q, want %q", m.InputModalities, tt.wantInput)
			}
			if m.OutputModalities != tt.wantOutput {
				t.Errorf("OutputModalities = %q, want %q", m.OutputModalities, tt.wantOutput)
			}
			if m.Modality != tt.wantClass {
				t.Errorf("Modality = %q, want %q", m.Modality, tt.wantClass)
			}
		})
	}
}

func TestNormalizeModelClassification_ExplicitClassSyncsCaps(t *testing.T) {
	// An image-editing generation model accepts image input; the vision flag
	// must be set even though the explicit class short-circuits derivation.
	m := model.Model{
		ModelID:         "qwen-image",
		Modality:        "image",
		InputModalities: `["text","image"]`,
		Capabilities:    `{"vision":false,"streaming":false}`,
	}
	NormalizeModelClassification(&m)
	if m.Modality != "image" {
		t.Errorf("Modality = %q, want image", m.Modality)
	}
	if !containsSubstring(m.Capabilities, `"vision":true`) {
		t.Errorf("Capabilities = %s, want vision:true from image input", m.Capabilities)
	}
	if !containsSubstring(m.Capabilities, `"streaming":false`) {
		t.Errorf("Capabilities = %s, streaming flag must be preserved", m.Capabilities)
	}
}

func TestNormalizeModelClassification_CapsSyncFromArrays(t *testing.T) {
	m := model.Model{
		ModelID:         "gpt-4o-audio-preview",
		InputModalities: `["text","audio"]`,
		Capabilities:    `{"streaming":true}`,
	}
	NormalizeModelClassification(&m)
	caps := m.Capabilities
	for _, want := range []string{`"audio_input":true`, `"streaming":true`} {
		if !containsSubstring(caps, want) {
			t.Errorf("Capabilities = %s, missing %s", caps, want)
		}
	}
	if containsSubstring(caps, `"vision":true`) {
		t.Errorf("Capabilities = %s, unexpected vision flag", caps)
	}
}

func TestNormalizeModels_Batch(t *testing.T) {
	models := []*model.Model{
		{ModelID: "llama-3.3-70b"},
		{ModelID: "nomic-embed-text"},
	}
	NormalizeModels(models)
	if models[0].Modality != "chat" {
		t.Errorf("models[0].Modality = %q, want chat", models[0].Modality)
	}
	if models[1].Modality != "embedding" {
		t.Errorf("models[1].Modality = %q, want embedding", models[1].Modality)
	}
	if models[1].OutputModalities != `["embedding"]` {
		t.Errorf("models[1].OutputModalities = %q, want [\"embedding\"]", models[1].OutputModalities)
	}
}

func containsSubstring(s, sub string) bool {
	return strings.Contains(s, sub)
}
