package util

import "strings"

// CutDataScheme strips the data: scheme from a URI, which RFC 2397 makes
// case-insensitive ("DATA:" is the same scheme). ok is false for any other
// string, a plain URL included.
func CutDataScheme(u string) (rest string, ok bool) {
	if len(u) >= 5 && strings.EqualFold(u[:5], "data:") {
		return u[5:], true
	}
	return "", false
}
