package util

import "testing"

func TestPlural(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "models"},
		{1, "model"},
		{2, "models"},
		{-1, "models"},
	}
	for _, c := range cases {
		if got := Plural(c.n, "model", "models"); got != c.want {
			t.Errorf("Plural(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestCount(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{0, "0 models"},
		{1, "1 model"},
		{5, "5 models"},
	}
	for _, c := range cases {
		if got := Count(c.n, "model", "models"); got != c.want {
			t.Errorf("Count(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}
