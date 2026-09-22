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

// EnumToken returns s when it has the shape of a provider's enum value (a
// short identifier: letters, digits, '_', '.', '-', starting with a letter)
// and "unknown" otherwise. It is for the one field of an upstream error a
// gateway may name while leaving the rest out, such as Anthropic's error.type
// or Gemini's promptFeedback.blockReason: real values are identifiers, so a
// relay that puts prose (a quoted prompt) in the field gets nothing into a log.
// Shape rather than a list, so a type the provider adds tomorrow still reads.
func EnumToken(s string) string {
	if s == "" || len(s) > 64 {
		return "unknown"
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case i > 0 && (r >= '0' && r <= '9' || r == '_' || r == '.' || r == '-'):
		default:
			return "unknown"
		}
	}
	return s
}
