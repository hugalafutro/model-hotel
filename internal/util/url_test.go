package util

import "testing"

func TestSanitizeBaseURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"trailing slash", "https://api.example.com/", "https://api.example.com"},
		{"no trailing slash", "https://api.example.com", "https://api.example.com"},
		{"double trailing slash", "https://api.example.com//", "https://api.example.com/"},
		{"empty", "", ""},
		{"just slash", "/", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeBaseURL(tc.raw)
			if got != tc.want {
				t.Errorf("SanitizeBaseURL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestSplitAndTrim(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  []string
	}{
		{"empty", "", nil},
		{"single", "hello", []string{"hello"}},
		{"comma separated", "a, b, c", []string{"a", "b", "c"}},
		{"with empty parts", "a,,b", []string{"a", "b"}},
		{"spaces only", "   ", nil},
		{"mixed", " a , , b ", []string{"a", "b"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SplitAndTrim(tc.value)
			if len(got) != len(tc.want) {
				t.Fatalf("SplitAndTrim(%q) = %v (len=%d), want %v (len=%d)", tc.value, got, len(got), tc.want, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("SplitAndTrim(%q)[%d] = %q, want %q", tc.value, i, got[i], tc.want[i])
				}
			}
		})
	}
}
func TestSanitizeAPIURL(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"trailing slash and /v1", "https://api.example.com/v1/", "https://api.example.com"},
		{"trailing /v1 no slash", "https://api.example.com/v1", "https://api.example.com"},
		{"trailing slash no /v1", "https://api.example.com/", "https://api.example.com"},
		{"no trailing slash or /v1", "https://api.example.com", "https://api.example.com"},
		{"double trailing slash /v1", "https://api.example.com/v1//", "https://api.example.com/v1/"},
		{"empty", "", ""},
		{"/v1 in path not suffix", "https://api.example.com/v1/models", "https://api.example.com/v1/models"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := SanitizeAPIURL(tc.raw)
			if got != tc.want {
				t.Errorf("SanitizeAPIURL(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}
