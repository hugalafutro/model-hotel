package debuglog

import "testing"

func TestScopeEnabled(t *testing.T) {
	origDebug, origScopes := globalDebug, enabledScopes
	t.Cleanup(func() { globalDebug, enabledScopes = origDebug, origScopes })

	globalDebug, enabledScopes = false, nil
	if !ScopeEnabled("proxy") {
		t.Error("no scopes configured: every scope passes")
	}
	globalDebug, enabledScopes = false, map[string]bool{"failover": true}
	if ScopeEnabled("proxy") {
		t.Error("an unlisted scope must be reported filtered")
	}
	if !ScopeEnabled("Failover") {
		t.Error("a listed scope matches case-insensitively")
	}
	globalDebug = true
	if !ScopeEnabled("proxy") {
		t.Error("global Debug enables every scope")
	}
}
