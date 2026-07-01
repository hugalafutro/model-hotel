package util

// ShortCommit normalizes a stamped commit SHA to a fixed-length short prefix so
// app_commit reads the same across build paths (a local git SHA vs CI's full
// github.sha). The "unknown" sentinel and any empty value pass through
// unchanged. Both the dashboard API and Front Desk surface app_commit through
// this so the same commit always presents the same prefix.
func ShortCommit(c string) string {
	const shortLen = 12
	if c == "" || c == "unknown" {
		return c
	}
	if len(c) > shortLen {
		return c[:shortLen]
	}
	return c
}
