package util

import (
	"sort"
	"sync"
)

// Held secrets: every secret this process has decrypted, kept for the exact
// layer of the credential mask.
//
// The exact layer used to know one secret at a time, the key of the provider
// whose response was being masked. That is the wrong scope for the threat it
// exists for. A relay or aggregator in front of the operator's other vendor
// accounts echoes the rejection it received upstream, and that rejection can
// quote the key of a different provider row, in a custom format the shape
// layer cannot recognise; the masker then had exactly the one credential it
// did not need. The gateway holds every one of those keys, so the exact layer
// masks all of them: everything the process has ever decrypted is registered
// here at the moment of decryption (internal/auth), and every exact pass
// unions the secrets its caller names with this set.
//
// Registration is by value and never expires. A rotated key stays held until
// the process restarts, which is right: a body quoting the old key is still
// quoting a credential. The set is bounded by the number of distinct secrets
// decrypted, a few dozen on a large deployment, and a pass costs one substring
// search per held secret over a body that is already bounded.
//
// Secrets that are not provider keys (a TOTP seed, an OIDC client secret) are
// held as well, since the same decryption serves them. Masking them from a
// body is always right, and none of them can appear in a body a client sends,
// so the exact pass over client-bound content cannot false-positive on them
// short of a user typing the operator's own secret.

var (
	heldMu   sync.RWMutex
	heldSet  = map[string]struct{}{}
	heldList []string // longest first, so a secret that is a prefix of another is masked whole
)

// HoldSecret registers a decrypted secret with the exact layer. Values under
// CredentialMinLen are ignored, for the reason given there.
func HoldSecret(secret string) {
	if len(secret) < CredentialMinLen {
		return
	}
	heldMu.Lock()
	defer heldMu.Unlock()
	if _, ok := heldSet[secret]; ok {
		return
	}
	heldSet[secret] = struct{}{}
	list := make([]string, 0, len(heldSet))
	for s := range heldSet {
		list = append(list, s)
	}
	sort.Slice(list, func(i, j int) bool {
		if len(list[i]) != len(list[j]) {
			return len(list[i]) > len(list[j])
		}
		return list[i] < list[j]
	})
	heldList = list
}

// HeldSecrets returns the held set, longest first. The slice is shared and
// must not be modified.
func HeldSecrets() []string {
	heldMu.RLock()
	defer heldMu.RUnlock()
	return heldList
}

// withHeld returns the caller's secrets followed by every held secret, the
// list every exact pass runs. The caller's come first so a caller that lists
// a superset before its subset keeps that order.
func withHeld(secrets []string) []string {
	held := HeldSecrets()
	if len(held) == 0 {
		return secrets
	}
	out := make([]string, 0, len(secrets)+len(held))
	out = append(out, secrets...)
	return append(out, held...)
}
