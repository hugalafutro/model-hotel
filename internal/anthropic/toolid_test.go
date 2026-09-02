package anthropic

import (
	"regexp"
	"testing"
)

// toolUseIDAlphabet is the id alphabet Anthropic accepts on a tool_use block.
var toolUseIDAlphabet = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func TestSignedToolUseID_RoundTrip(t *testing.T) {
	const sig = "CqkBAXLB+/w=/Zm9v" // base64 with the characters a tool_use id may not carry
	signed := signedToolUseID("call_abc_0", sig)
	if signed == "call_abc_0" {
		t.Fatal("signature was not carried")
	}
	if !toolUseIDAlphabet.MatchString(signed) {
		t.Fatalf("signed id %q leaves the tool_use id alphabet", signed)
	}
	id, got := splitToolUseID(signed)
	if id != "call_abc_0" || got != sig {
		t.Fatalf("split = (%q, %q), want the id and signature back", id, got)
	}
}

func TestSignedToolUseID_Unsigned(t *testing.T) {
	if got := signedToolUseID("call_1", ""); got != "call_1" {
		t.Fatalf("unsigned id = %q, want it untouched", got)
	}
	for _, id := range []string{"call_1", "toolu_01abc", "", "call_1" + thoughtSigMarker + "not base64url!", "call_1" + thoughtSigMarker} {
		base, sig := splitToolUseID(id)
		if base != id || sig != "" {
			t.Errorf("splitToolUseID(%q) = (%q, %q), want the id as it came and no signature", id, base, sig)
		}
	}
}

// An upstream id that itself contains the marker keeps it: the last marker
// is the carrier's.
func TestSignedToolUseID_MarkerInsideUpstreamID(t *testing.T) {
	odd := "call" + thoughtSigMarker + "x"
	base, sig := splitToolUseID(signedToolUseID(odd, "s"))
	if base != odd || sig != "s" {
		t.Fatalf("split = (%q, %q), want (%q, s)", base, sig, odd)
	}
}
