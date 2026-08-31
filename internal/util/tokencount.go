package util

// MaxSaneTokenCount is the largest token figure one provider response may
// contribute to metering or the request log. Past it a value is not a count in
// another spelling; it is a different kind of wrong, and DecodeCounts, which
// tolerates spellings, never judged plausibility. The upstream is configured
// by the operator but its response body is the one input on the metering path
// the gateway does not author, and before this bound a negative member drew a
// key's tokens_used DOWN, a member at 2^31 put the owner's TPM bucket weeks in
// debt, and a plain JSON integer near 2^63 wrapped the charge sum into a
// credit while failing the int4 request-log UPDATE outright.
//
// The number is chosen from what a clamped charge COSTS, not from column
// width. A charge lands in the TPM bucket as debt that drains at tpm/60 per
// second, so the ceiling is how long one bogus response can hold an owner's
// keys at 429: ten million against the default 60k TPM is under three hours,
// where a hundred million was more than a day. It still sits ~9x above the
// largest context window in the catalogs (1,050,000), which bounds any figure
// a provider can honestly report for a request it actually served, and far
// enough below int4 that no member, and no per-request sum of members, can
// reach the column limit.
//
// It bounds the blast radius rather than removing it: debt still accumulates
// across responses, which is the limiter's own contract and a separate fix.
const MaxSaneTokenCount = 10_000_000

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
