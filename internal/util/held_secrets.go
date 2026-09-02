package util

import (
	"sort"
	"sync"
)

// Held secrets: every provider key the gateway holds, kept for the exact
// layer of the credential mask over error text and log lines.
//
// The exact layer used to know one secret at a time, the key of the provider
// whose response was being masked. That is the wrong scope for the threat it
// exists for. A relay or aggregator in front of the operator's other vendor
// accounts echoes the rejection it received upstream, and that rejection can
// quote the key of a different provider row, in a custom format the shape
// layer cannot recognise; the masker then had exactly the one credential it
// did not need. The gateway holds every one of those keys, so the exact layer
// over error text masks all of them: the set is seeded from the provider
// table at startup and after every import (provider.HoldKeys), the create and
// update handlers add the plaintext they have in hand, and every exact pass
// over error or log text unions the secrets its caller names with this set.
//
// Only error and log text. The exact pass a client's content goes through
// (a streamed answer, a success body) keeps to the attempted provider's own
// key: no provider quotes another provider's key inside an answer, and a
// placeholder key an operator typed for a keyless local server ("not-needed")
// is a plain word that must not be rewritten out of every answer the gateway
// serves. The length floor here is the only filter, for the same reason: a
// placeholder masked out of an error message costs nothing.
//
// Registration is by value and never expires. A rotated key stays held until
// the process restarts, which is right: a body quoting the old key is still
// quoting a credential. The set is bounded by the number of distinct keys, a
// few dozen on a large deployment, and a pass costs one substring search per
// held key over an error body that is already bounded.

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

// withHeld returns the caller's secrets and every held secret, deduplicated
// and longest first, the list every exact pass over error text runs. Length
// order is what keeps a key that is a prefix of another from leaving the
// longer one's tail behind, whichever side of the union each came from; it
// also preserves the "superset before subset" order a discovery caller
// relies on ("Bearer X" before "X"), since the superset is the longer.
func withHeld(secrets []string) []string {
	held := HeldSecrets()
	if len(held) == 0 && len(secrets) <= 1 {
		return secrets
	}
	out := make([]string, 0, len(secrets)+len(held))
	seen := make(map[string]bool, len(secrets)+len(held))
	for _, s := range append(append([]string{}, secrets...), held...) {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	return out
}
