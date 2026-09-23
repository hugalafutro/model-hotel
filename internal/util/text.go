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

// KnownToken returns s when it is one of a provider's documented enum values
// and "unknown" otherwise. It is for the one field of an upstream error a
// gateway names while leaving the rest out, such as Anthropic's error.type or
// Gemini's blockReason and finishReason: the text reaches logs and the request
// row, and a relay is free to put anything in the field. A shape check was not
// enough: a short identifier-shaped value ("PIN2468") passes one, and an echo
// that short is below the request-content fence's window too. A value the
// provider adds later reads "unknown" until it is listed, which costs only a
// diagnostic.
func KnownToken(s string, known map[string]bool) string {
	if known[s] {
		return s
	}
	return "unknown"
}
