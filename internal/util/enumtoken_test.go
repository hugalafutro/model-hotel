package util

import (
	"strings"
	"testing"
)

// A provider's enum value reads unchanged; anything with the shape of prose,
// the one way a relay could put a quoted prompt in the field, reads "unknown".
func TestEnumToken(t *testing.T) {
	for in, want := range map[string]string{
		"overloaded_error":       "overloaded_error",
		"rate_limit_error":       "rate_limit_error",
		"SAFETY":                 "SAFETY",
		"PROHIBITED_CONTENT":     "PROHIBITED_CONTENT",
		"a.b-c_1":                "a.b-c_1",
		"":                       "unknown",
		"the user asked about X": "unknown",
		"1starts_with_digit":     "unknown",
		"error:with:colons":      "unknown",
		strings.Repeat("a", 65):  "unknown",
		"ünïcode":                "unknown",
	} {
		if got := EnumToken(in); got != want {
			t.Errorf("EnumToken(%q) = %q, want %q", in, got, want)
		}
	}
}
