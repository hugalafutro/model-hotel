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
