package frontdesk

import "testing"

func TestVersionSkew(t *testing.T) {
	cases := []struct {
		name            string
		primary, member string
		want            bool
	}{
		{"equal tags", "v1.2.3", "v1.2.3", false},
		{"differ tags", "v1.2.4", "v1.2.3", true},
		{"dev equals dev", "dev", "dev", false},
		{"dev vs tag", "dev", "v1.2.3", true},
		{"tag vs dev", "v1.2.3", "dev", true},
		{"member unknown", "v1.2.3", "", true},
		{"primary unknown", "", "v1.2.3", true},
		{"both unknown", "", "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := versionSkew(c.primary, c.member); got != c.want {
				t.Errorf("versionSkew(%q, %q) = %v, want %v", c.primary, c.member, got, c.want)
			}
		})
	}
}
