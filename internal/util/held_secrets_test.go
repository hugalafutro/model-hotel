package util

import (
	"strings"
	"testing"
)

// The registry keeps every decrypted secret and the exact pass masks all of
// them, whichever secrets the caller names.
func TestHeldSecrets_ExactPassMasksEveryHeldSecret(t *testing.T) {
	const foreign = "custom-key-A-zzzz-11112222-heldtest"
	HoldSecret(foreign)
	HoldSecret(foreign) // idempotent
	HoldSecret("short") // under CredentialMinLen: ignored
	found := 0
	for _, s := range HeldSecrets() {
		if s == foreign {
			found++
		}
		if s == "short" {
			t.Fatal("a value under the minimum length was held")
		}
	}
	if found != 1 {
		t.Fatalf("held %d copies of the secret, want exactly one", found)
	}
	body := `relay rejected; upstream said bad api key ` + foreign + ` while ours is custom-key-B-9999-77776666-own`
	got := MaskCredential("custom-key-B-9999-77776666-own", body)
	if strings.Contains(got, foreign) || strings.Contains(got, "77776666") {
		t.Fatalf("a held foreign key survived the exact pass: %q", got)
	}
	if got != "relay rejected; upstream said bad api key [redacted] while ours is [redacted]" {
		t.Fatalf("got %q", got)
	}
	// A caller naming no secret at all still gets the held set.
	if got := MaskCredentials(nil, "quoted "+foreign); got != "quoted [redacted]" {
		t.Fatalf("no-secret caller: %q", got)
	}
}

// Longest first: a secret that is a prefix of another must not leave the
// longer one's tail behind, whichever side of the union each came from.
func TestHeldSecrets_LongestFirst(t *testing.T) {
	HoldSecret("prefix-secret-value")
	HoldSecret("prefix-secret-value-with-a-longer-tail")
	got := MaskCredentials(nil, "saw prefix-secret-value-with-a-longer-tail here")
	if got != "saw [redacted] here" {
		t.Fatalf("got %q", got)
	}
	// The caller names the short one; the held set has the long one.
	if got := MaskCredential("prefix-secret-value", "saw prefix-secret-value-with-a-longer-tail here"); got != "saw [redacted] here" {
		t.Fatalf("caller prefix left the held tail: %q", got)
	}
	// The caller names the long one; the held set has the short one.
	HoldSecret("held-short-prefix")
	if got := MaskCredential("held-short-prefix-and-its-longer-form", "saw held-short-prefix-and-its-longer-form here"); got != "saw [redacted] here" {
		t.Fatalf("held prefix left the caller's tail: %q", got)
	}
	list := HeldSecrets()
	for i := 1; i < len(list); i++ {
		if len(list[i]) > len(list[i-1]) {
			t.Fatalf("held list not longest first at %d: %q after %q", i, list[i], list[i-1])
		}
	}
}

// The bounded form's tail strip covers held secrets too: a held key cut by
// the bound leaves no recognisable head.
func TestHeldSecrets_BoundedTailStrip(t *testing.T) {
	const foreign = "custom-key-tail-strip-0123456789abcdef"
	HoldSecret(foreign)
	body := strings.Repeat("x", 40) + foreign
	got := MaskCredentialsBounded(nil, body, 52)
	if strings.Contains(got, "custom-key-tail-strip-0") {
		t.Fatalf("the head of a held key survived the cut: %q", got)
	}
	if !strings.Contains(got, "[redacted]") {
		t.Fatalf("got %q", got)
	}
}
