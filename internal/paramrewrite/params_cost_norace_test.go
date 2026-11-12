//go:build !race

package paramrewrite

import (
	"strings"
	"testing"
	"time"
)

// This file is left out of race builds: the test is one goroutine timing pure
// string work, so the race detector has nothing to find, and instrumented it
// takes most of a minute. The plain test job runs it.

// TestParseProviderParamError_CostGrowsLinearly pins the parser's cost to the
// size of the error message. It runs on the request path against an upstream
// 400 body of up to 1 MiB, and a message repeating "messages[N].name" once cost
// a rescan of the rest of the message per repeat: 16 s of one core at 1 MiB.
// Each message repeats something one of the rules looks for, so every rule is
// fed its worst density. The bound is a ratio between two sizes, so it holds on
// a slow machine: four times the message costs about four times as long when
// linear, sixteen when quadratic.
func TestParseProviderParamError_CostGrowsLinearly(t *testing.T) {
	parse := func(unit string, size int) time.Duration {
		body := []byte(`{"error":{"message":"` + strings.Repeat(unit, size/len(unit)) + `"}}`)
		best := time.Duration(1<<63 - 1)
		for range 3 {
			start := time.Now()
			ParseProviderParamError(body)
			best = min(best, time.Since(start))
		}
		return best
	}
	for _, unit := range []string{
		"messages[0].name ",
		"messages[0].name: must be a string ",
		"invalid temperature: only 1 is allowed. ",
		"'reasoning_effort' does not support 'none'. Supported values are: 'low'; ",
		"'stop' 'n' 'seed' ",
		"'temperature';",
		"top_p ",
		"}, {",
		";",
	} {
		small, large := parse(unit, 64<<10), parse(unit, 256<<10)
		if large > 8*small {
			t.Errorf("%q: 256 KiB took %v, 64 KiB took %v: more than 8 times as long for 4 times the message", unit, large, small)
		}
	}
}
