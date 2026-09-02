package provider

import (
	"context"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
)

// HoldKeys registers every provider key in the table with the credential
// mask's held set (util.HoldSecret), whatever the row's enabled or
// autodiscovery state. The mask exists for a relay that quotes the key of a
// different provider row in its error body, and the row it quotes is, if
// anything, more likely to be one the operator has disabled (a key being
// rotated, a provider under suspicion) than one in use; so coverage cannot
// depend on a key having been decrypted for a request, which a disabled
// provider never is. Called at startup and after every import; the create
// and update handlers register the plaintext they have in hand directly.
//
// Decryption goes through the key cache, so a key already warmed costs a map
// read and a cold one costs one Argon2 derivation; a large table is seconds
// of CPU, which is why the callers run this off the request path.
func HoldKeys(ctx context.Context, repo *Repository, masterKey string) (held, failed int) {
	providers, err := repo.List(ctx)
	if err != nil {
		debuglog.Error("provider: held keys: list failed", "error", err)
		return 0, 0
	}
	for _, p := range providers {
		if len(p.EncryptedKey) == 0 {
			continue
		}
		key, err := auth.DecryptCached(p.EncryptedKey, p.KeyNonce, p.KeySalt, masterKey)
		if err != nil {
			failed++
			continue
		}
		util.HoldSecret(key)
		held++
	}
	if failed > 0 {
		debuglog.Warn("provider: held keys: some keys did not decrypt", "held", held, "failed", failed)
	}
	return held, failed
}
