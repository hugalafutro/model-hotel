package util

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ParseDuration parses a duration setting the way a member stores one: a Go
// time.Duration string, optionally led by a "d" day count (1d = 24h0m0s, 1d12h =
// 36h), which Go's time.ParseDuration does not support. Older frontend code
// wrote the day form, and Front Desk reads members' durations with the same
// rules the member applies.
func ParseDuration(s string) (time.Duration, error) {
	days := 0
	if i := strings.IndexByte(s, 'd'); i >= 0 {
		n, err := strconv.Atoi(s[:i])
		if err != nil {
			return 0, fmt.Errorf("invalid day suffix in duration %q: %w", s, err)
		}
		days = n
		s = s[i+1:]
	}
	if s == "" {
		return time.Duration(days) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	return d + time.Duration(days)*24*time.Hour, nil
}
