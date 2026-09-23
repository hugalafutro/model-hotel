package util

import "testing"

// A listed value reads unchanged; anything else reads "unknown", including a
// short identifier-shaped echo ("PIN2468") that a shape check would pass and
// that is below the request-content fence's window as well.
func TestKnownToken(t *testing.T) {
	known := map[string]bool{"overloaded_error": true, "SAFETY": true}
	for in, want := range map[string]string{
		"overloaded_error":       "overloaded_error",
		"SAFETY":                 "SAFETY",
		"PIN2468":                "unknown",
		"":                       "unknown",
		"the user asked about X": "unknown",
		"safety":                 "unknown",
	} {
		if got := KnownToken(in, known); got != want {
			t.Errorf("KnownToken(%q) = %q, want %q", in, got, want)
		}
	}
}
