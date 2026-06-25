package admin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetHashPersistsAndNormalizes covers the HA token-hash sync path: SetHash
// accepts either a sha256:<hex> or a bare 64-hex hash, persists it to the
// admin-token file, takes effect immediately, and rejects malformed input.
func TestSetHashPersistsAndNormalizes(t *testing.T) {
	tmp := t.TempDir()
	mgr, _, err := New(tmp, "")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	hexA := strings.Repeat("a", 64)
	if err := mgr.SetHash("sha256:" + hexA); err != nil {
		t.Fatalf("SetHash(prefixed): %v", err)
	}
	if got := mgr.Hash(); got != "sha256:"+hexA {
		t.Errorf("Hash() = %q, want sha256:%s", got, hexA)
	}
	// Persisted to disk in sha256:<hex> form.
	data, err := os.ReadFile(filepath.Join(tmp, "admin-token"))
	if err != nil {
		t.Fatalf("read admin-token file: %v", err)
	}
	if string(data) != "sha256:"+hexA {
		t.Errorf("admin-token file = %q, want sha256:%s", string(data), hexA)
	}

	// A bare 64-hex hash is normalized to the same form.
	hexB := strings.Repeat("b", 64)
	if err := mgr.SetHash(hexB); err != nil {
		t.Fatalf("SetHash(bare): %v", err)
	}
	if got := mgr.Hash(); got != "sha256:"+hexB {
		t.Errorf("Hash() after bare set = %q, want sha256:%s", got, hexB)
	}
	// Setting a new hash clears any plaintext token from this boot.
	if mgr.Validate(strings.Repeat("c", 32)) {
		t.Error("an arbitrary token should not validate after SetHash")
	}

	// Malformed values are rejected and leave the live hash untouched.
	for _, bad := range []string{"", "notahash", "sha256:short", strings.Repeat("z", 64)} {
		if err := mgr.SetHash(bad); err == nil {
			t.Errorf("SetHash(%q) = nil, want an error", bad)
		}
	}
	if got := mgr.Hash(); got != "sha256:"+hexB {
		t.Errorf("live hash changed after a rejected SetHash: %q", got)
	}
}
