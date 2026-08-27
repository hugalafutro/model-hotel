package util

import "time"

// TrustedAge measures how long ago t happened and reports whether that
// measurement can be trusted.
//
// It exists because `now.Sub(t) < window` is wrong for any timestamp the
// current process did not just take. A future t yields a NEGATIVE duration,
// which is below every positive threshold, so the thing being aged reads as
// "still fresh" forever and "too old" never. Nothing errors; the check simply
// stops working. That bug has been found repeatedly in this codebase, always on
// a timestamp that crossed a boundary: parsed from RFC3339, read back from a
// database row, or received from a peer.
//
// Timestamps that stay inside one process do NOT need this. A time.Time from
// time.Now() carries a monotonic reading, and Sub uses it whenever both
// operands have one, so an in-process duration is immune to wall-clock steps.
// Note that .UTC(), .Round() and .Truncate() strip that reading.
//
// A future stamp is refused outright rather than tolerated up to some skew
// window, matching the two checks that already implement this rule by hand
// (internal/ratelimit.fleetDivisor and the Front Desk backup listing). Every
// caller here reads a stamp its own host wrote, and every writer rounds
// downward - RFC3339 truncates to whole seconds, UnixNano and timestamptz are
// exact - so no amount of ordinary clock jitter can legitimately place one in
// the future. Only a backwards step of the clock can, and a tolerance would do
// nothing but extend each caller's freshness window by its own width during
// exactly that fault.
//
// (The quota repository clamps on WRITE with a two-minute tolerance instead.
// That is a genuinely different problem - the same snapshot is redistributed
// across hosts every 60s and relies on skip-if-newer to make repeats free, so
// rewriting near-now stamps there breaks deduplication. Do not copy its
// tolerance to a read-side check.)
//
// The zero Time is likewise untrusted; callers that need to tell "never
// happened" from "happened long ago" must test IsZero themselves. In practice
// each one already has a separate signal for it (a found flag, a haveSync
// flag, or a nil pointer).
func TrustedAge(now, t time.Time) (time.Duration, bool) {
	if t.IsZero() {
		return 0, false
	}
	age := now.Sub(t)
	if age < 0 {
		return 0, false
	}
	return age, true
}
