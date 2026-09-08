package api

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

// An envelope carrying two providers whose names are one name once spaces
// become hyphens is refused whole, with the same 400 every other envelope-content
// refusal answers with. Writing it would leave the member with two rows fighting
// over one routing id, which is exactly what the normalized-name index forbids.
func TestConfigSync_RefusesProvidersCollidingAfterNormalization(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	seedProvider(t, "twin a", "sk-secret", configSyncMasterKey)
	seedProvider(t, "twin b", "sk-secret", configSyncMasterKey)

	env := doExport(t, r)
	if len(env.Config.Providers) != 2 {
		t.Fatalf("export carries %d providers, want 2", len(env.Config.Providers))
	}
	for i := range env.Config.Providers {
		if env.Config.Providers[i].Name == "twin b" {
			env.Config.Providers[i].Name = "twin-a"
		}
	}

	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("import status = %d, want 400; body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, errInvalidSyncedProvider.Error()) {
		t.Errorf("refusal %q does not carry %q", body, errInvalidSyncedProvider.Error())
	}
	for _, want := range []string{"twin a", "twin-a"} {
		if !strings.Contains(body, want) {
			t.Errorf("refusal %q does not name %q", body, want)
		}
	}
	if got := providerNames(t); len(got) != 2 || !got["twin a"] || !got["twin b"] {
		t.Errorf("a refused envelope changed the provider set: %v", got)
	}
}

// validateSyncedProviderNames is the envelope-wide half of the provider
// validation, so it runs over the list rather than a row, and it wraps the
// sentinel the import handler maps to a 400.
func TestValidateSyncedProviderNames(t *testing.T) {
	ok := []ExportProvider{{Name: "alpha"}, {Name: "beta gamma"}, {Name: "beta-gamma-2"}}
	if err := validateSyncedProviderNames(ok); err != nil {
		t.Fatalf("distinct names refused: %v", err)
	}
	err := validateSyncedProviderNames([]ExportProvider{{Name: "alpha"}, {Name: "a b"}, {Name: "a-b"}})
	if err == nil || !errors.Is(err, errInvalidSyncedProvider) {
		t.Fatalf("colliding names not refused with the sentinel: %v", err)
	}
	for _, want := range []string{"a b", "a-b"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not name %q", err.Error(), want)
		}
	}
}

// The collision the envelope cannot see: a provider this member has and the
// envelope does not, whose name is a twin of one the envelope carries. The
// declarative delete that would remove it runs after the upsert, so the insert
// hits the normalized-name index. That is still the envelope carrying what the
// interactive API would refuse, so it is the same 400, not a 500.
func TestConfigSync_RefusesTwinOfALocalProviderAsBadRequest(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	seedProvider(t, "twin-a", "sk-secret", configSyncMasterKey)
	seedProvider(t, "keep", "sk-secret", configSyncMasterKey)

	env := doExport(t, r)
	if _, err := apiTestDB.Pool().Exec(t.Context(),
		`UPDATE providers SET name = 'twin a' WHERE name = 'twin-a'`); err != nil {
		t.Fatalf("rename the local row: %v", err)
	}

	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("import status = %d, want 400; body %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, errInvalidSyncedProvider.Error()) {
		t.Errorf("refusal %q does not carry %q", body, errInvalidSyncedProvider.Error())
	}
	// The phrase the in-transaction branch uses, so this pins the constraint
	// path rather than the envelope-wide check that runs before the transaction.
	if !strings.Contains(body, `"twin-a" is one name with a provider on this member`) {
		t.Errorf("refusal %q is not the one the normalized-name index produces", body)
	}
	if got := providerNames(t); len(got) != 2 || !got["keep"] || !got["twin a"] {
		t.Errorf("a refused envelope changed the provider set: %v", got)
	}
}

// The commit fence answers before the envelope's content is judged. A push a
// newer generation already superseded is the quiet stale outcome even when it
// carries names that would otherwise be refused, so Front Desk sees a skip for a
// push the fence exists to ignore rather than a failed member.
func TestConfigSync_StaleEnvelopeWithCollidingNamesIsStale(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	seedProvider(t, "twin a", "sk-secret", configSyncMasterKey)

	env := doExport(t, r)
	if resp, rec := doImportGen(t, r, env, new(int64(7))); rec.Code != http.StatusOK || !resp.Applied || resp.Stale {
		t.Fatalf("gen=7 import: code=%d applied=%v stale=%v, want 200 applied not-stale", rec.Code, resp.Applied, resp.Stale)
	}

	// "twin-a" is one name with the member's "twin a" once normalized, so this
	// envelope would be refused with a 400 if the fence did not answer first.
	resp, rec := doImportGen(t, r, withExtraProvider(env, "twin-a"), new(int64(6)))
	if rec.Code != http.StatusOK || resp.Applied || !resp.Stale {
		t.Fatalf("stale import with colliding names: code=%d applied=%v stale=%v, want 200 not-applied stale; body %s",
			rec.Code, resp.Applied, resp.Stale, rec.Body.String())
	}
	if got := providerNames(t); len(got) != 1 || !got["twin a"] {
		t.Errorf("a stale envelope changed the provider set: %v", got)
	}
}
