package provider

import (
	"context"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// Every keyed row is held, enabled or not; a keyless row is skipped; a row
// that does not decrypt is counted and does not stop the walk.
func TestHoldKeys(t *testing.T) {
	const master = "test-master-key-for-held-keys"
	repo := NewRepository(testDB.Pool())
	mk := func(name, key, masterKey string) *Provider {
		t.Helper()
		var ct, nonce, salt []byte
		if key != "" {
			kp, err := auth.Encrypt(key, masterKey)
			if err != nil {
				t.Fatalf("encrypt: %v", err)
			}
			ct, nonce, salt = kp.Ciphertext, kp.Nonce, kp.Salt
		}
		p, err := repo.Create(context.Background(), CreateProviderRequest{Name: name + "-" + uuid.New().String()[:8], BaseURL: "https://" + name + ".example.test/v1", APIKey: key, ProviderType: "custom"}, ct, nonce, salt)
		if err != nil {
			t.Fatalf("create %s: %v", name, err)
		}
		return p
	}
	const disabledKey = "custom-key-disabled-row-0011223344"
	const enabledKey = "custom-key-enabled-row-5566778899"
	pd := mk("held-disabled", disabledKey, master)
	if _, err := testDB.Pool().Exec(context.Background(), `UPDATE providers SET enabled = false WHERE id = $1`, pd.ID); err != nil {
		t.Fatalf("disable: %v", err)
	}
	mk("held-enabled", enabledKey, master)
	mk("held-keyless", "", master)
	mk("held-other-master", "custom-key-other-master-aabbccdd", "a-different-master-key-entirely")

	held, failed := HoldKeys(context.Background(), repo, master)
	if held < 2 || failed < 1 {
		t.Fatalf("held %d failed %d, want at least the two rows under this master held and the foreign-master row failed", held, failed)
	}
	for _, key := range []string{disabledKey, enabledKey} {
		if !slices.Contains(util.HeldSecrets(), key) {
			t.Fatalf("%q not held", key)
		}
	}
	if slices.Contains(util.HeldSecrets(), "custom-key-other-master-aabbccdd") {
		t.Fatal("a key that did not decrypt was held")
	}
}
