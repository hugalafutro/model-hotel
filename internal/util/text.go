package util

import (
	"strings"
	"unicode/utf8"
)

// TruncateRunes caps s at n runes, cutting on a rune boundary so the result
// stays valid UTF-8, and marks the cut with an ellipsis. Strings already within
// the cap are returned untouched without allocating a rune slice.
func TruncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n]) + "…"
}

// TruncateBytes cuts s to at most maxBytes bytes without splitting a rune,
// dropping any invalid sequences the wire delivered. A byte-level cut could
// split a multi-byte rune and Postgres refuses invalid UTF-8, which would turn
// a valid request from a long-header client into a 500.
func TruncateBytes(s string, maxBytes int) string {
	s = strings.ToValidUTF8(s, "")
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// CollapseSpace folds every run of whitespace into a single space and trims the
// ends, so a multi-line provider message reads as one log line.
func CollapseSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
