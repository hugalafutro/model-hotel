package provider

import "testing"

func TestContains(t *testing.T) {
	tests := []struct {
		name  string
		slice []string
		s     string
		want  bool
	}{
		{"found", []string{"a", "b", "c"}, "b", true},
		{"not found", []string{"a", "b", "c"}, "d", false},
		{"empty slice", []string{}, "a", false},
		{"nil slice", nil, "a", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := contains(tt.slice, tt.s); got != tt.want {
				t.Errorf("contains() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// modalityFromModelsDev
// ---------------------------------------------------------------------------

func TestModalityFromModelsDev(t *testing.T) {
	tests := []struct {
		name string
		mods ModelsDevModalities
		want string
	}{
		{
			name: "video_input",
			mods: ModelsDevModalities{Input: []string{"video"}},
			want: "video",
		},
		{
			name: "video_takes_precedence_over_audio_and_image",
			mods: ModelsDevModalities{Input: []string{"video", "audio", "image"}},
			want: "video",
		},
		{
			name: "audio_and_image",
			mods: ModelsDevModalities{Input: []string{"audio", "image"}},
			want: "multimodal",
		},
		{
			name: "image_only",
			mods: ModelsDevModalities{Input: []string{"image"}},
			want: "vision",
		},
		{
			name: "audio_only",
			mods: ModelsDevModalities{Input: []string{"audio"}},
			want: "audio",
		},
		{
			name: "text_only",
			mods: ModelsDevModalities{Input: []string{"text"}},
			want: "text",
		},
		{
			name: "empty_input",
			mods: ModelsDevModalities{Input: []string{}},
			want: "text",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := modalityFromModelsDev(tt.mods); got != tt.want {
				t.Errorf("modalityFromModelsDev() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestIsNumeric(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"digits", "12345", true},
		{"zero", "0", true},
		{"empty", "", true}, // no non-digit chars, loop doesn't fail
		{"with letters", "123abc", false},
		{"with space", "12 34", false},
		{"negative", "-5", false},
		{"float", "3.14", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNumeric(tt.input); got != tt.want {
				t.Errorf("isNumeric(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestLooksLikeDate(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid_date_with_hyphens", "2024-08-06", true},
		{"valid_date_no_hyphens", "20240806", true},
		{"too_short", "2024-08", false},
		{"not_numeric", "abcdefgh", false},
		{"empty", "", false},
		{"still_numeric", "2024-13-45", true},
		{"seven_chars", "2024080", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := looksLikeDate(tt.input); got != tt.want {
				t.Errorf("looksLikeDate(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
