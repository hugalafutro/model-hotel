package util

import "testing"

func TestCutDataScheme(t *testing.T) {
	for _, tc := range []struct {
		in, rest string
		ok       bool
	}{
		{"data:image/png;base64,AAAA", "image/png;base64,AAAA", true},
		{"DATA:image/png;base64,AAAA", "image/png;base64,AAAA", true},
		{"Data:,", ",", true},
		{"data:", "", true},
		{"https://example.com/a.png", "", false},
		{"dat", "", false},
		{"", "", false},
	} {
		rest, ok := CutDataScheme(tc.in)
		if ok != tc.ok || rest != tc.rest {
			t.Errorf("CutDataScheme(%q) = (%q, %v), want (%q, %v)", tc.in, rest, ok, tc.rest, tc.ok)
		}
	}
}
