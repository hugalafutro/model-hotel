package util

import "testing"

// TestShortCommit checks the SHA normalization shared by the dashboard API and
// Front Desk: the sentinels pass through, a short value is left alone, a full
// SHA is truncated to 12, and a full and short stamp of the same commit
// normalize identically so app_commit reads the same across build paths.
func TestShortCommit(t *testing.T) {
	full := "7a5eeac6aa758c56432a7dbc8cd059909800e390"
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty passes through", "", ""},
		{"unknown sentinel passes through", "unknown", "unknown"},
		{"full SHA truncated to 12", full, "7a5eeac6aa75"},
		{"short SHA shorter than limit kept", "7a5eeac", "7a5eeac"},
		{"exactly 12 kept", "7a5eeac6aa75", "7a5eeac6aa75"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShortCommit(tc.in); got != tc.want {
				t.Errorf("ShortCommit(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
	if ShortCommit(full) != ShortCommit(full[:12]) {
		t.Errorf("full and short stamps of the same commit diverge: %q vs %q",
			ShortCommit(full), ShortCommit(full[:12]))
	}
}
