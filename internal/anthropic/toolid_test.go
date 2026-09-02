package anthropic

import (
	"bytes"
	"encoding/base64"
	"regexp"
	"strings"
	"testing"
)

// toolUseIDAlphabet is the id alphabet Anthropic accepts on a tool_use block.
var toolUseIDAlphabet = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

func TestSignedToolUseID_RoundTrip(t *testing.T) {
	const sig = "CqkBAXLB+/w=/Zm9v" // not padded base64, so carried as text; has the characters a tool_use id may not carry
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

// A real signature is padded base64 (928 characters on gemini-3.1-pro); it
// is carried as the bytes it encodes and put back through the same encoding,
// so the id grows by the marker and a third less than the text form would.
func TestSignedToolUseID_Base64SignatureCarriedAsBytes(t *testing.T) {
	raw := make([]byte, 696)
	for i := range raw {
		raw[i] = byte(i*7 + 3)
	}
	sig := base64.StdEncoding.EncodeToString(raw)
	if len(sig) != 928 {
		t.Fatalf("fixture signature is %d chars, want 928", len(sig))
	}
	signed := signedToolUseID("call_abc_0", sig)
	if !toolUseIDAlphabet.MatchString(signed) {
		t.Fatalf("signed id leaves the tool_use id alphabet")
	}
	if want := len("call_abc_0") + len(thoughtSigMarker) + 1 + 928; len(signed) != want {
		t.Errorf("signed id is %d chars, want %d (the bytes re-encoded, not the text)", len(signed), want)
	}
	id, got := splitToolUseID(signed)
	if id != "call_abc_0" || got != sig {
		t.Fatalf("split = (%q, %d chars, equal=%v), want the id and the signature byte for byte", id, len(got), got == sig)
	}
	// Unpadded base64 is not the byte form: it would not come back
	// byte-exact, so it rides as text.
	unpadded := strings.TrimRight(sig, "=")
	if _, got := splitToolUseID(signedToolUseID("c", unpadded)); got != unpadded {
		t.Errorf("unpadded signature came back as %q", got)
	}
}

func TestStripSignedToolUseIDs(t *testing.T) {
	signed := signedToolUseID("toolu_01", "sig")
	body := []byte(`{"model":"claude","max_tokens":5,"messages":[` +
		`{"role":"user","content":"hi"},` +
		`{"role":"assistant","content":[{"type":"text","text":"ok","cache_control":{"type":"ephemeral"}},{"type":"tool_use","id":"` + signed + `","name":"f","input":{"a":1}}]},` +
		`{"role":"user","content":[{"type":"tool_result","tool_use_id":"` + signed + `","content":"done"},{"type":"tool_result","tool_use_id":"toolu_02","content":"x"}]}]}`)
	out := StripSignedToolUseIDs(body)
	if bytes.Contains(out, []byte(thoughtSigMarker)) {
		t.Fatalf("suffix survived: %s", out)
	}
	if !bytes.Contains(out, []byte(`"id":"toolu_01"`)) || !bytes.Contains(out, []byte(`"tool_use_id":"toolu_01"`)) || !bytes.Contains(out, []byte(`"tool_use_id":"toolu_02"`)) {
		t.Errorf("ids not paired after the strip: %s", out)
	}
	if !bytes.Contains(out, []byte(`"cache_control":{"type":"ephemeral"}`)) || !bytes.Contains(out, []byte(`"max_tokens":5`)) {
		t.Errorf("unrelated members lost: %s", out)
	}
	for _, same := range []string{
		`{"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":[{"type":"tool_use","id":"toolu_9","name":"f","input":{}}]}]}`,
		`not json`,
		`{"messages":"nope"}`,
		// A content array holding a non-object is not the Messages shape:
		// the message is left as it came.
		`{"messages":[{"role":"assistant","content":["text",{"type":"tool_use","id":"toolu_1` + thoughtSigMarker + `tc2ln","name":"f","input":{}}]}]}`,
	} {
		if got := StripSignedToolUseIDs([]byte(same)); string(got) != same {
			t.Errorf("body without a signed id was rewritten: %s -> %s", same, got)
		}
	}
}
