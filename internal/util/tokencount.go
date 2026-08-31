package util

// MaxSaneTokenCount is the largest token figure one provider response may
// contribute to metering or the request log. A count is bounded by a context
// window, and no window ever shipped is within 50x of this number, so no
// figure a real provider reports is touched. Past it a value is not a count in
// another spelling; it is a different kind of wrong, and DecodeCounts, which
// tolerates spellings, never judged plausibility. The upstream is configured
// by the operator but its response body is the one input on the metering path
// the gateway does not author, and before this bound a negative member drew a
// key's tokens_used DOWN, a member at 2^31 put the owner's TPM bucket weeks in
// debt, and a plain JSON integer near 2^63 wrapped the charge sum into a
// credit while failing the int4 request-log UPDATE outright. The ceiling sits
// far enough below both column widths that no member, and no per-request sum
// of members, can reach one.
const MaxSaneTokenCount = 100_000_000

// ClampTokenCount folds one provider figure into the range a real count can
// occupy: at least zero, at most MaxSaneTokenCount.
//
// A negative folds to zero rather than rejecting the block: zero is what a
// provider that omitted the member reports, and the proxy's estimate fallback
// then fills it from the sizes the gateway measured itself, which is the
// honest charge for a figure that was never a reading. One definition, here,
// because the proxy and the dashboard's model test both write these figures
// into the same int4 columns.
func ClampTokenCount(n int) int {
	return min(max(n, 0), MaxSaneTokenCount)
}
