package util

import (
	"testing"
	"time"
)

func TestParseDuration(t *testing.T) {
	for _, tc := range []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"24h", 24 * time.Hour, false},
		{"1d", 24 * time.Hour, false},
		{"7d", 7 * 24 * time.Hour, false},
		{"2d12h30m", 60*time.Hour + 30*time.Minute, false},
		{"xd", 0, true},
		{"1d-bogus", 0, true},
		{"fortnightly", 0, true},
		// Day counts past what a time.Duration holds error out instead of
		// wrapping negative, which a reader would floor to its minimum.
		{"106751d", 106751 * 24 * time.Hour, false},
		{"106751d23h", 106751*24*time.Hour + 23*time.Hour, false},
		{"106752d", 0, true},
		{"106751d24h", 0, true},
		{"-106752d", 0, true},
		{"-106751d-24h", 0, true},
	} {
		got, err := ParseDuration(tc.in)
		if (err != nil) != tc.wantErr || got != tc.want {
			t.Errorf("ParseDuration(%q) = %s, %v; want %s, error %v", tc.in, got, err, tc.want, tc.wantErr)
		}
	}
}
