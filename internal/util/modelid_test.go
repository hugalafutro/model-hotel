package util

import (
	"slices"
	"testing"
)

func TestModelIDSegments(t *testing.T) {
	cases := map[string][]string{
		"THUDM/GLM-4":                  {"thudm", "glm", "4"},
		"glm4:9b":                      {"glm4", "9b"},
		"zai.glm-4.7":                  {"zai", "glm", "4", "7"},
		"nomic_embed text":             {"nomic", "embed", "text"},
		"gemini-2.5-flash-preview-tts": {"gemini", "2", "5", "flash", "preview", "tts"},
		"":                             nil,
		"///":                          nil,
	}
	for id, want := range cases {
		if got := ModelIDSegments(id); !slices.Equal(got, want) {
			t.Errorf("ModelIDSegments(%q) = %v, want %v", id, got, want)
		}
	}
}
