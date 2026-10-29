package frontdesk

import (
	"testing"

	"github.com/hugalafutro/model-hotel/internal/util"
)

// TestMaskSecretsAndResolveSecret covers the settings secret round trip: stored
// secrets are masked on the way out, and an echoed mask preserves the stored
// ciphertext while a blank clears it.
func TestMaskSecretsAndResolveSecret(t *testing.T) {
	set := Settings{AlertAppriseTargets: "cipher-a", OidcClientSecret: "cipher-b"}
	maskSecrets(&set)
	if set.AlertAppriseTargets != util.SecretMask || set.OidcClientSecret != util.SecretMask {
		t.Fatalf("masked settings = %+v", set)
	}
	empty := Settings{}
	maskSecrets(&empty)
	if empty.AlertAppriseTargets != "" || empty.OidcClientSecret != "" {
		t.Errorf("an unset secret must stay empty, got %+v", empty)
	}

	if got, err := resolveSecret(util.SecretMask, "cipher-a", "key"); err != nil || got != "cipher-a" {
		t.Errorf("echoed mask = (%q, %v), want the stored ciphertext", got, err)
	}
	if got, err := resolveSecret("", "cipher-a", "key"); err != nil || got != "" {
		t.Errorf("blank = (%q, %v), want it cleared", got, err)
	}
	enc, err := resolveSecret("tgram://tok/chat", "cipher-a", "key")
	if err != nil {
		t.Fatalf("new value: %v", err)
	}
	if enc == "" || enc == "tgram://tok/chat" {
		t.Errorf("a new value must be stored encrypted, got %q", enc)
	}
}
