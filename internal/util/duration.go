package util

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// ParseDuration parses a duration setting the way a member stores one: a Go
// time.Duration string, optionally led by a "d" day count (1d = 24h0m0s, 1d12h =
// 36h), which Go's time.ParseDuration does not support. Older frontend code
// wrote the day form, and Front Desk reads members' durations with the same
// rules the member applies. A day count or total that does not fit a
// time.Duration is an error rather than a silently wrapped value.
func ParseDuration(s string) (time.Duration, error) {
	in := s
	days := 0
	if i := strings.IndexByte(s, 'd'); i >= 0 {
		n, err := strconv.Atoi(s[:i])
		if err != nil {
			return 0, fmt.Errorf("invalid day suffix in duration %q: %w", s, err)
		}
		if maxDays := math.MaxInt64 / int64(24*time.Hour); int64(n) > maxDays || int64(n) < -maxDays {
			return 0, fmt.Errorf("day count in duration %q out of range", s)
		}
		days = n
		s = s[i+1:]
	}
	dayPart := time.Duration(days) * 24 * time.Hour
	if s == "" {
		return dayPart, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	if (dayPart > 0 && d > math.MaxInt64-dayPart) || (dayPart < 0 && d < math.MinInt64-dayPart) {
		return 0, fmt.Errorf("duration %q out of range", in)
	}
	return d + dayPart, nil
}
