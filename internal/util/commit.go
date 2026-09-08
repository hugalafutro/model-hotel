package util

// UnstampedCommit is the value app_commit carries when the build was not
// stamped with a git SHA. Both binaries default their buildCommit to it and
// ShortCommit passes it through unchanged.
const UnstampedCommit = "unknown"

// ShortCommit normalizes a stamped commit SHA to a fixed-length short prefix so
// app_commit reads the same across build paths (a local git SHA vs CI's full
// github.sha). The UnstampedCommit sentinel and any empty value pass through
// unchanged. Both the dashboard API and Front Desk surface app_commit through
// this so the same commit always presents the same prefix.
func ShortCommit(c string) string {
	const shortLen = 12
	if c == "" || c == UnstampedCommit {
		return c
	}
	if len(c) > shortLen {
		return c[:shortLen]
	}
	return c
}
