package quota

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

// nestedObjectPayload builds `{"n":{"n":...{"<leaf>":1}}}` with `levels` "n"
// wrappers, so the leaf key sits at path depth levels+1. Used to place a
// difference precisely on either side of the depth cap.
func nestedObjectPayload(levels int, leaf string) json.RawMessage {
	var sb strings.Builder
	for range levels {
		sb.WriteString(`{"n":`)
	}
	fmt.Fprintf(&sb, `{%q:1}`, leaf)
	sb.WriteString(strings.Repeat("}", levels))
	return json.RawMessage(sb.String())
}

// wideObjectPayload builds a flat object with `keys` zero-padded members, the
// last of which is named `lastKey` instead of its positional name. Sorted key
// order makes truncation at the path cap deterministic and testable.
func wideObjectPayload(keys int, lastKey string) json.RawMessage {
	m := make(map[string]int, keys)
	for i := range keys {
		m[fmt.Sprintf("k%03d", i)] = i
	}
	if lastKey != "" {
		delete(m, fmt.Sprintf("k%03d", keys-1))
		m[lastKey] = keys - 1
	}
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return b
}

func mustFingerprint(t *testing.T, payload json.RawMessage) string {
	t.Helper()
	fp, ok := Fingerprint(payload)
	if !ok {
		t.Fatalf("Fingerprint(%s) returned ok=false, want a fingerprint", payload)
	}
	if fp == "" {
		t.Fatalf("Fingerprint(%s) returned ok=true with an empty digest", payload)
	}
	return fp
}

// TestFingerprintIgnoresValueChanges is the property the whole drift watch
// rests on: quota counters move on every poll, and a fingerprint that moved
// with them would alert continuously. Only the key-path set may matter.
func TestFingerprintIgnoresValueChanges(t *testing.T) {
	before := json.RawMessage(`{"data":{"limits":[{"type":"TOKENS_LIMIT","unit":3,"remaining":81234,"nextResetTime":1753600000000}]}}`)
	after := json.RawMessage(`{"data":{"limits":[{"type":"TOKENS_LIMIT","unit":6,"remaining":0,"nextResetTime":1753999999000}]}}`)

	if a, b := mustFingerprint(t, before), mustFingerprint(t, after); a != b {
		t.Errorf("value-only change moved the fingerprint: %s vs %s", a, b)
	}
}

// TestFingerprintIgnoresArrayLength verifies a provider adding a model to the
// plan (one more array element of the same shape) does not read as drift.
func TestFingerprintIgnoresArrayLength(t *testing.T) {
	one := json.RawMessage(`{"model_remains":[{"end_time":1,"current_interval_status":1}]}`)
	three := json.RawMessage(`{"model_remains":[{"end_time":1,"current_interval_status":1},{"end_time":2,"current_interval_status":1},{"end_time":3,"current_interval_status":3}]}`)

	if a, b := mustFingerprint(t, one), mustFingerprint(t, three); a != b {
		t.Errorf("array length moved the fingerprint: %s (1 element) vs %s (3 elements)", a, b)
	}
}

// TestFingerprintChangesOnAddedTopLevelKey covers the NeuralWatt overage-credit
// case: the provider starts sending a new billing field alongside the old ones.
func TestFingerprintChangesOnAddedTopLevelKey(t *testing.T) {
	before := json.RawMessage(`{"dedup_power":12,"period_end":"2026-08-01T00:00:00Z"}`)
	after := json.RawMessage(`{"dedup_power":12,"period_end":"2026-08-01T00:00:00Z","overage_credits":{"balance":5}}`)

	if a, b := mustFingerprint(t, before), mustFingerprint(t, after); a == b {
		t.Errorf("an added top-level key must move the fingerprint, both were %s", a)
	}
}

// TestFingerprintChangesOnRemovedNestedKey covers the Ollama Cloud case: a
// subscription-period field disappears when billing moves to usage credits.
func TestFingerprintChangesOnRemovedNestedKey(t *testing.T) {
	before := json.RawMessage(`{"account":{"plan":"pro","subscription":{"period_end":"2026-08-01T00:00:00Z","seats":1}}}`)
	after := json.RawMessage(`{"account":{"plan":"pro","subscription":{"seats":1}}}`)

	if a, b := mustFingerprint(t, before), mustFingerprint(t, after); a == b {
		t.Errorf("a removed nested key must move the fingerprint, both were %s", a)
	}
}

// TestFingerprintRejectsUnusablePayloads verifies every payload that carries no
// key paths is refused outright rather than collapsing into one shared
// "fingerprint of nothing", which every such payload would then match.
func TestFingerprintRejectsUnusablePayloads(t *testing.T) {
	cases := map[string]json.RawMessage{
		"nil":              nil,
		"empty":            json.RawMessage(``),
		"json null":        json.RawMessage(`null`),
		"empty array":      json.RawMessage(`[]`),
		"empty object":     json.RawMessage(`{}`),
		"top-level array":  json.RawMessage(`[{"a":1}]`),
		"top-level string": json.RawMessage(`"quota exhausted"`),
		"top-level number": json.RawMessage(`42`),
		"malformed":        json.RawMessage(`{"a":`),
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			fp, ok := Fingerprint(payload)
			if ok {
				t.Errorf("want ok=false, got fingerprint %q", fp)
			}
			if fp != "" {
				t.Errorf("a refused payload must return an empty digest, got %q", fp)
			}
		})
	}
}

// TestFingerprintDepthCapHidesDeeperDifferences pins the depth cap on both
// sides: a difference at the deepest recorded level must still move the
// fingerprint, and the same difference one level below the cap must not. An
// assertion that only checked "does not panic" would pass for an unbounded
// walk, which is the thing being prevented.
func TestFingerprintDepthCapHidesDeeperDifferences(t *testing.T) {
	// Leaf sits at depth maxPathDepth: inside the cap, so it is recorded.
	atCapA := nestedObjectPayload(maxPathDepth-1, "alpha")
	atCapB := nestedObjectPayload(maxPathDepth-1, "beta")
	if a, b := mustFingerprint(t, atCapA), mustFingerprint(t, atCapB); a == b {
		t.Errorf("a difference at depth %d is inside the cap and must be visible, both were %s", maxPathDepth, a)
	}

	// Leaf sits at depth maxPathDepth+2: past the cap, so it is invisible.
	pastCapA := nestedObjectPayload(maxPathDepth+1, "alpha")
	pastCapB := nestedObjectPayload(maxPathDepth+1, "beta")
	if a, b := mustFingerprint(t, pastCapA), mustFingerprint(t, pastCapB); a != b {
		t.Errorf("a difference below the depth cap must be invisible: %s vs %s", a, b)
	}

	// Two payloads nested to different absurd depths truncate to the same
	// recorded prefix, so the walk is bounded rather than merely guarded.
	if a, b := mustFingerprint(t, nestedObjectPayload(60, "x")), mustFingerprint(t, nestedObjectPayload(400, "x")); a != b {
		t.Errorf("payloads nested past the cap must truncate to the same fingerprint: %s vs %s", a, b)
	}
}

// TestFingerprintPathCapTruncatesDeterministically verifies the total-path cap
// both bounds the work and stays stable: Go map iteration order is randomized
// per range, so a truncating walk that did not sort keys would return a
// different fingerprint on nearly every call and alert forever.
func TestFingerprintPathCapTruncatesDeterministically(t *testing.T) {
	wide := wideObjectPayload(maxPaths+100, "")

	first := mustFingerprint(t, wide)
	for i := range 25 {
		if got := mustFingerprint(t, wide); got != first {
			t.Fatalf("call %d returned %s, want the stable %s", i, got, first)
		}
	}

	// A key sorted past the cap is dropped, so renaming it cannot move the
	// fingerprint.
	if got := mustFingerprint(t, wideObjectPayload(maxPaths+100, "zzz_beyond_cap")); got != first {
		t.Errorf("a key beyond the path cap must not move the fingerprint: %s vs %s", got, first)
	}

	// A key sorted inside the cap is kept, so renaming it must move it.
	kept := wideObjectPayload(maxPaths+100, "")
	var m map[string]int
	if err := json.Unmarshal(kept, &m); err != nil {
		t.Fatalf("unmarshal wide payload: %v", err)
	}
	delete(m, "k000")
	m["a000"] = 0
	renamed, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal renamed payload: %v", err)
	}
	if got := mustFingerprint(t, renamed); got == first {
		t.Errorf("a key inside the path cap must move the fingerprint, both were %s", got)
	}
}

// TestFingerprintPathsMatchesFingerprint verifies the two entry points cannot
// disagree: the drift detector diffs SchemaPaths output but compares digests,
// so a digest that was not derived from exactly those paths would report an
// added/removed list belonging to a different payload.
func TestFingerprintPathsMatchesFingerprint(t *testing.T) {
	payload := json.RawMessage(`{"data":{"limits":[{"type":"TOKENS_LIMIT","remaining":3}]},"ok":true}`)

	paths, ok := SchemaPaths(payload)
	if !ok {
		t.Fatal("SchemaPaths returned ok=false for a well-formed object payload")
	}
	want := []string{"data", "data.limits", "data.limits[]", "data.limits[].remaining", "data.limits[].type", "ok"}
	if len(paths) != len(want) {
		t.Fatalf("got paths %v, want %v", paths, want)
	}
	for i, w := range want {
		if paths[i] != w {
			t.Fatalf("got paths %v, want %v", paths, want)
		}
	}
	if got := FingerprintPaths(paths); got != mustFingerprint(t, payload) {
		t.Errorf("FingerprintPaths(SchemaPaths(p)) = %s, want Fingerprint(p) = %s", got, mustFingerprint(t, payload))
	}
}
