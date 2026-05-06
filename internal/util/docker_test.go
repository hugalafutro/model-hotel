package util

import "testing"

func TestIsHex(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid lowercase", "abc123", true},
		{"valid uppercase", "ABC123", true},
		{"valid mixed case", "aBcDeF", true},
		{"full hex range", "0123456789abcdefABCDEF", true},
		{"empty string", "", false},
		{"with spaces", "ab cd", false},
		{"with g", "abcg", false},
		{"with special chars", "ff-00", false},
		{"just digits", "123456", true},
		{"just letters", "abcdef", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isHex(tt.input); got != tt.want {
				t.Errorf("isHex(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
