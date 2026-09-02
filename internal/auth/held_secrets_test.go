package auth

import (
	"slices"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/util"
)

// Both decrypt paths hand the plaintext to the credential mask's held set,
// so a key decrypted for any purpose is masked wherever it is quoted.
func TestDecryptHoldsTheSecret(t *testing.T) {
	const master = "test-master-key-for-held-secrets"
	for _, tc := range []struct {
		name   string
		secret string
		via    func(kp *KeyPair) (string, error)
	}{
		{"Decrypt", "custom-key-decrypt-path-0011223344", func(kp *KeyPair) (string, error) {
			return Decrypt(kp.Ciphertext, kp.Nonce, kp.Salt, master)
		}},
		{"DecryptCached", "custom-key-cached-path-5566778899", func(kp *KeyPair) (string, error) {
			return DecryptCached(kp.Ciphertext, kp.Nonce, kp.Salt, master)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if slices.Contains(util.HeldSecrets(), tc.secret) {
				t.Fatal("secret held before it was ever decrypted")
			}
			kp, err := Encrypt(tc.secret, master)
			if err != nil {
				t.Fatalf("encrypt: %v", err)
			}
			got, err := tc.via(kp)
			if err != nil || got != tc.secret {
				t.Fatalf("decrypt = %q, %v", got, err)
			}
			if !slices.Contains(util.HeldSecrets(), tc.secret) {
				t.Fatal("decrypted secret not held")
			}
		})
	}
}
