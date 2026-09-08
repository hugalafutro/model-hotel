package provider

import (
	"net/http"
	"testing"
)

// TestPruneQuotaBreakers_DropsRemovedKeepsLive covers the map hygiene a
// long-lived service needs: one instance now serves every poll pass for the
// life of the process, so a provider deleted from the database would otherwise
// keep its circuit state forever.
func TestPruneQuotaBreakers_DropsRemovedKeepsLive(t *testing.T) {
	d := NewDiscoveryService(nil, nil)
	kept := d.getOrCreateCircuit("live-provider")
	d.getOrCreateCircuit("removed-provider")

	live := map[string]bool{"live-provider": true}
	d.PruneQuotaBreakers(func(id string) bool { return live[id] })

	if _, ok := d.quotaBreaker.Load("removed-provider"); ok {
		t.Error("a provider outside the live set must lose its circuit state")
	}
	got, ok := d.quotaBreaker.Load("live-provider")
	if !ok {
		t.Fatal("a live provider must keep its circuit state")
	}
	if got != kept {
		t.Error("a live provider's circuit must be the same state, not a fresh one")
	}
}

// TestPruneQuotaBreakers_NilPredicateKeepsEverything pins the fail-safe: a
// caller with no provider list must not be read as "no provider is live".
func TestPruneQuotaBreakers_NilPredicateKeepsEverything(t *testing.T) {
	d := NewDiscoveryService(nil, nil)
	d.getOrCreateCircuit("some-provider")

	d.PruneQuotaBreakers(nil)

	if _, ok := d.quotaBreaker.Load("some-provider"); !ok {
		t.Error("a nil predicate must leave the breaker map alone")
	}
}

// TestClose_Idempotent verifies shutdown can call Close more than once, and
// that a service built with a nil HTTP client does not panic on it.
func TestClose_Idempotent(t *testing.T) {
	d := NewDiscoveryService(nil, nil)
	d.Close()
	d.Close()

	NewDiscoveryServiceWithHTTPClient(nil).Close()
	NewDiscoveryServiceWithHTTPClient(&http.Client{}).Close()
}
