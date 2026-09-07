package util

import "time"

// MillisSince renders the time elapsed since start in milliseconds with
// sub-millisecond precision. Every request-log duration column in the project
// is written in this unit, so the conversion lives here rather than being
// re-spelled per site: a site that reaches for Duration.Milliseconds() instead
// silently rounds away the fraction the rest of the columns carry.
func MillisSince(start time.Time) float64 {
	return Ms(time.Since(start))
}

// Ms is MillisSince for a duration that has already been measured.
func Ms(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000.0
}
