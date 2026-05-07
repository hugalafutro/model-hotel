package provider

import (
	"testing"
)

func TestContainsString(t *testing.T) {
	tests := []struct {
		name   string
		slice  []string
		target string
		want   bool
	}{
		{"found", []string{"a", "b", "c"}, "b", true},
		{"not found", []string{"a", "b", "c"}, "d", false},
		{"empty slice", []string{}, "a", false},
		{"nil slice", nil, "a", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsString(tt.slice, tt.target); got != tt.want {
				t.Errorf("containsString() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsGoogleToolCallingModel(t *testing.T) {
	tests := []struct {
		name    string
		modelID string
		want    bool
	}{
		{"gemini pro", "gemini-2.0-flash", true},
		{"embedding excluded", "text-embedding-004", false},
		{"imagen excluded", "imagen-3.0", false},
		{"veo excluded", "veo-2.0", false},
		{"lyria excluded", "lyria-2.0", false},
		{"aqa excluded", "aqa-model", false},
		{"tts excluded", "tts-1", false},
		{"live excluded", "gemini-live", false},
		{"case insensitive", "Text-Embedding-004", false},
		{"regular model", "gemma-2b", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGoogleToolCallingModel(tt.modelID); got != tt.want {
				t.Errorf("isGoogleToolCallingModel(%q) = %v, want %v", tt.modelID, got, tt.want)
			}
		})
	}
}

func TestIsGoogleVisionModel(t *testing.T) {
	tests := []struct {
		name    string
		modelID string
		want    bool
	}{
		{"gemini-2 yes", "gemini-2.0-flash", true},
		{"gemini-3 yes", "gemini-3.0-pro", true},
		{"gemma yes", "gemma-3-4b", true},
		{"embedding no", "text-embedding-004", false},
		{"tts no", "tts-1", false},
		{"live no", "gemini-live-2.0", false},
		{"random model no", "palm-2", false},
		{"case insensitive", "GEMINI-2.0-FLASH", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGoogleVisionModel(tt.modelID); got != tt.want {
				t.Errorf("isGoogleVisionModel(%q) = %v, want %v", tt.modelID, got, tt.want)
			}
		})
	}
}

func TestToNativeBaseURL(t *testing.T) {
	tests := []struct {
		name     string
		proxyURL string
		want     string
	}{
		{"no suffix", "https://example.com", "https://example.com"},
		{"trailing slash", "https://example.com/", "https://example.com"},
		{"openai suffix", "https://example.com/openai", "https://example.com"},
		{"openai with trailing slash", "https://example.com/openai/", "https://example.com"},
		{"openai in path", "https://example.com/api/openai", "https://example.com/api"},
		{"no openai", "https://example.com/v1beta", "https://example.com/v1beta"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := toNativeBaseURL(tt.proxyURL); got != tt.want {
				t.Errorf("toNativeBaseURL(%q) = %v, want %v", tt.proxyURL, got, tt.want)
			}
		})
	}
}

func TestIsRelevantGoogleModel(t *testing.T) {
	tests := []struct {
		name string
		gm   GoogleModel
		want bool
	}{
		{
			name: "generateContent",
			gm:   GoogleModel{SupportedGenerationMethods: []string{"generateContent"}},
			want: true,
		},
		{
			name: "embedContent",
			gm:   GoogleModel{SupportedGenerationMethods: []string{"embedContent"}},
			want: true,
		},
		{
			name: "both methods",
			gm:   GoogleModel{SupportedGenerationMethods: []string{"generateContent", "embedContent"}},
			want: true,
		},
		{
			name: "irrelevant method",
			gm:   GoogleModel{SupportedGenerationMethods: []string{"someOtherMethod"}},
			want: false,
		},
		{
			name: "empty methods",
			gm:   GoogleModel{SupportedGenerationMethods: []string{}},
			want: false,
		},
		{
			name: "nil methods",
			gm:   GoogleModel{SupportedGenerationMethods: nil},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isRelevantGoogleModel(tt.gm); got != tt.want {
				t.Errorf("isRelevantGoogleModel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsGoogleStructuredOutputModel(t *testing.T) {
	tests := []struct {
		name    string
		modelID string
		want    bool
	}{
		{"tool calling model", "gemini-2.0-flash", true},
		{"embedding excluded", "text-embedding-004", false},
		{"imagen excluded", "imagen-3.0", false},
		{"veo excluded", "veo-2.0", false},
		{"lyria excluded", "lyria-2.0", false},
		{"aqa excluded", "aqa-model", false},
		{"tts excluded", "tts-1", false},
		{"live excluded", "gemini-live", false},
		{"case insensitive", "Text-Embedding-004", false},
		{"regular model", "gemma-2b", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGoogleStructuredOutputModel(tt.modelID); got != tt.want {
				t.Errorf("isGoogleStructuredOutputModel(%q) = %v, want %v", tt.modelID, got, tt.want)
			}
		})
	}
}

func TestIsGoogleImageGenModel(t *testing.T) {
	tests := []struct {
		name    string
		modelID string
		want    bool
	}{
		{"image in name", "imagen-3.0", true},
		{"banana in name", "banana-dev-v1", true},
		{"image case insensitive", "IMAGE-2.0", true},
		{"banana case insensitive", "BANANA-PRO", true},
		{"no match", "gemini-2.0-flash", false},
		{"partial match", "my-image-model", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGoogleImageGenModel(tt.modelID); got != tt.want {
				t.Errorf("isGoogleImageGenModel(%q) = %v, want %v", tt.modelID, got, tt.want)
			}
		})
	}
}

func TestIsGoogleAudioModel(t *testing.T) {
	tests := []struct {
		name    string
		modelID string
		want    bool
	}{
		{"tts in name", "tts-1", true},
		{"live in name", "gemini-live-2.0", true},
		{"native-audio in name", "native-audio-1.0", true},
		{"tts case insensitive", "TTS-1-HD", true},
		{"live case insensitive", "LIVE-STREAM", true},
		{"no match", "gemini-2.0-flash", false},
		{"partial match", "my-tts-model", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGoogleAudioModel(tt.modelID); got != tt.want {
				t.Errorf("isGoogleAudioModel(%q) = %v, want %v", tt.modelID, got, tt.want)
			}
		})
	}
}

func TestIsGoogleEmbeddingModel(t *testing.T) {
	tests := []struct {
		name    string
		modelID string
		want    bool
	}{
		{"embedding in name", "text-embedding-004", true},
		{"embedding case insensitive", "TEXT-EMBEDDING-004", true},
		{"no match", "gemini-2.0-flash", false},
		{"partial match", "my-embedding-model", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isGoogleEmbeddingModel(tt.modelID); got != tt.want {
				t.Errorf("isGoogleEmbeddingModel(%q) = %v, want %v", tt.modelID, got, tt.want)
			}
		})
	}
}
