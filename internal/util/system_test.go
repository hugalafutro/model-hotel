package util

import "testing"

func TestParseInt(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int64
	}{
		{"zero", "0", 0},
		{"positive", "12345", 12345},
		{"empty", "", 0},
		{"non-numeric", "abc", 0},
		{"mixed", "123abc", 123},
		{"leading spaces", "  42", 0},
		{"negative sign", "-5", 0},
		{"large number", "9999999999", 9999999999},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseInt(tc.input)
			if err != nil {
				t.Errorf("ParseInt(%q) returned error: %v", tc.input, err)
			}
			if got != tc.want {
				t.Errorf("ParseInt(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}
