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
		{"code-only output is chat", []string{"text"}, []string{"code"}, "deepseek-coder", "chat"},
		{"code plus image output stays chat", []string{"text"}, []string{"code", "image"}, "coder-with-diagrams", "chat"},
		{"mixed media without text prefers video", nil, []string{"image", "video"}, "media-gen", "video"},
		{"whisper stt tiebreak", []string{"audio"}, []string{"text"}, "whisper-large-v3", "stt"},
		{"transcribe segment stt", []string{"audio"}, []string{"text"}, "gpt-4o-transcribe", "stt"},
		{"audio-input chat is not stt", []string{"text", "audio"}, []string{"text"}, "gpt-4o-audio-preview", "chat"},
		{"enriched audio chat is not stt", []string{"text", "image", "audio"}, []string{"text"}, "gpt-4o-audio-preview", "chat"},
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

// The gpt-4o transcription models inherit the full modality set of their chat
// namesakes from models.dev enrichment, so their input arrays are
// indistinguishable from an audio chat model's. Only the ID names them as
// transcribers. Rows mirror the live catalogue of one provider.
func TestDeriveModelClass_TranscriptionModels(t *testing.T) {
	tests := []struct {
		modelID string
		input   []string
		want    string
	}{
		{"gpt-4o-mini-transcribe", []string{"text", "image", "audio"}, "stt"},
		{"gpt-4o-transcribe", []string{"text", "image", "audio"}, "stt"},
		{"gpt-4o-transcribe-diarize", []string{"audio"}, "stt"},
		{"whisper-1", []string{"audio"}, "stt"},
		{"gpt-4o-audio-preview", []string{"text", "image", "audio"}, "chat"},
	}
	for _, tt := range tests {
		t.Run(tt.modelID, func(t *testing.T) {
			if got := DeriveModelClass(tt.input, []string{"text"}, tt.modelID); got != tt.want {
				t.Errorf("DeriveModelClass(%v, [text], %q) = %q, want %q",
					tt.input, tt.modelID, got, tt.want)
			}
		})
	}
}

// The name heuristic is authoritative once audio input is present: an
// audio-input model whose ID carries a whole "whisper" or "transcribe" segment
// is a transcriber even when it also reports text input, because that is the
// exact shape models.dev enrichment produces. The heuristic reaches no further
// than that — it is consulted only for text-or-code output, so an audio-output
// or rerank-output model with the same name in it is decided by its arrays.
func TestDeriveModelClass_NameHeuristicBoundary(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		output  []string
		modelID string
		want    string
	}{
		{"text and audio in with transcriber name", []string{"text", "audio"}, []string{"text"}, "some-whisper-chat", "stt"},
		{"audio output wins over the name", []string{"text"}, []string{"audio"}, "tts-whisper", "tts"},
		{"rerank output wins over the name", []string{"text"}, []string{"rerank"}, "whisper-reranker-v1", "rerank"},
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

// A transcription model reclassified out of "chat" must not keep claiming the
// image input its chat namesake reported.
func TestNormalizeModelClassification_EnrichedTranscribeInput(t *testing.T) {
	m := model.Model{
		ModelID:          "gpt-4o-transcribe",
		InputModalities:  `["text","image","audio"]`,
		OutputModalities: `["text"]`,
		Capabilities:     `{"streaming":true,"vision":true}`,
	}
	NormalizeModelClassification(&m)
	if m.Modality != "stt" {
		t.Errorf("Modality = %q, want stt", m.Modality)
	}
	if m.InputModalities != `["audio"]` {
		t.Errorf("InputModalities = %q, want [\"audio\"]", m.InputModalities)
	}
	if m.OutputModalities != `["text"]` {
		t.Errorf("OutputModalities = %q, want [\"text\"]", m.OutputModalities)
	}
	if !containsSubstring(m.Capabilities, `"vision":false`) {
		t.Errorf("Capabilities = %s, want the inherited vision flag cleared", m.Capabilities)
	}
	for _, want := range []string{`"audio_input":true`, `"streaming":true`} {
		if !containsSubstring(m.Capabilities, want) {
			t.Errorf("Capabilities = %s, missing %s", m.Capabilities, want)
		}
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
			name: "code output kept and derives chat",
			in: model.Model{
				ModelID:          "openrouter/some-coder",
				OutputModalities: `["code"]`,
			},
			wantInput:  `["text"]`,
			wantOutput: `["code"]`,
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

func TestNormalizeModelClassification_PDFCapSync(t *testing.T) {
	// Arrays are the source of truth: pdf input implies the pdf_upload flag
	// (models.dev-enriched rows carry pdf only in the arrays)...
	m := model.Model{
		ModelID:         "claude-sonnet-4-6",
		InputModalities: `["text","pdf"]`,
		Capabilities:    `{"streaming":true}`,
	}
	NormalizeModelClassification(&m)
	if !containsSubstring(m.Capabilities, `"pdf_upload":true`) {
		t.Errorf("Capabilities = %s, want pdf_upload:true from pdf input", m.Capabilities)
	}
	// ...and the reverse: a catalog pdf_upload flag seeds the input array.
	m2 := model.Model{
		ModelID:      "claude-sonnet-4-6",
		Capabilities: `{"pdf_upload":true}`,
	}
	NormalizeModelClassification(&m2)
	if m2.InputModalities != `["text","pdf"]` {
		t.Errorf("InputModalities = %q, want %q", m2.InputModalities, `["text","pdf"]`)
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

// models.dev enrichment describes an embedding or reranking model as text in,
// text out, so the name has to break the tie the way it does for transcribers:
// left to the arrays alone those models classed as chat and were offered a
// chat request the provider refuses.
func TestDeriveModelClass_TextOutputYieldsToAnEmbeddingOrRerankName(t *testing.T) {
	for _, tc := range []struct{ id, want string }{
		{"text-embedding-3-small", "embedding"},
		{"text-embedding-ada-002", "embedding"},
		{"embedding-gemma", "embedding"},
		{"rerank-v3.5", "rerank"},
		{"gpt-4o", "chat"},
		{"gpt-5.6-luna", "chat"},
		// A bare "embed" token with a stated text output is a discovery that
		// knows better (an Ollama completion model named after its tutor).
		{"llama3-embed-tutor", "chat"},
	} {
		if got := DeriveModelClass([]string{"text"}, []string{"text"}, tc.id); got != tc.want {
			t.Errorf("DeriveModelClass(text->text, %q) = %q, want %q", tc.id, got, tc.want)
		}
	}
	// An explicit embedding output still wins on its own, whatever the name.
	if got := DeriveModelClass([]string{"text"}, []string{"embedding"}, "mystery-model"); got != "embedding" {
		t.Errorf("explicit embedding output = %q, want embedding", got)
	}
}
