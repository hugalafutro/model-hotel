package api

import (
	"context"
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/provider"
)

// Deleting a provider strips its UUID out of virtual_keys.allowed_providers and
// users.allowed_providers, via provider.PruneAllowLists called at each delete
// site. These tests pin the two properties that now mean opposite things and are
// trivial to conflate:
//
//	NULL  -> unrestricted (every provider)
//	'{}'  -> restricted to nothing (deny everything)
//
// Every assertion therefore reads the column back IN SQL and checks NULL-ness
// separately from cardinality. Scanning the array into a []string would not do:
// pgx yields a nil slice for both NULL and '{}', so the exact confusion under
// test would be invisible.

// keyAllowListQuery and userAllowListQuery read the three facts each assertion
// needs without letting a codec collapse them: is the column SQL NULL, how many
// members does it have (-1 standing in for NULL, which has no cardinality), and
// what are they.
const keyAllowListQuery = `
	SELECT allowed_providers IS NULL,
	       coalesce(cardinality(allowed_providers), -1),
	       coalesce(allowed_providers, '{}'::text[])
	  FROM virtual_keys WHERE key_hash = $1`

const userAllowListQuery = `
	SELECT allowed_providers IS NULL,
	       coalesce(cardinality(allowed_providers), -1),
	       coalesce(allowed_providers, '{}'::text[])
	  FROM users WHERE username = $1`

func readAllowList(t *testing.T, query, subject string) (isNull bool, card int, members []string) {
	t.Helper()
	if err := apiTestDB.Pool().QueryRow(context.Background(), query, subject).Scan(&isNull, &card, &members); err != nil {
		// A query failure must fail the test outright. Treating it as "nothing to
		// assert" would make every check below pass green on a missing row.
		t.Fatalf("reading allowed_providers for %q: %v", subject, err)
	}
	return isNull, card, members
}

// seedRestrictedKey inserts a virtual key with the given allow-list. A nil
// allowed encodes as SQL NULL, which is how an unrestricted key is stored.
func seedRestrictedKey(t *testing.T, name, keyHash string, allowed []string) {
	t.Helper()
	if _, err := apiTestDB.Pool().Exec(context.Background(),
		`INSERT INTO virtual_keys (name, key_hash, key_preview, allowed_providers)
		 VALUES ($1, $2, 'sk-...pr', $3)`, name, keyHash, allowed); err != nil {
		t.Fatalf("seed virtual key %s: %v", name, err)
	}
}

// seedCappedUser inserts a user and sets their account cap in a second
// statement, matching how the other config-sync tests write this column.
func seedCappedUser(t *testing.T, username string, providerIDs []string) {
	t.Helper()
	seedUser(t, username, nil, true, nil)
	if _, err := apiTestDB.Pool().Exec(context.Background(),
		`UPDATE users SET allowed_providers = $1 WHERE username = $2`, providerIDs, username); err != nil {
		t.Fatalf("set cap for %s: %v", username, err)
	}
}

// deleteProviderViaRepo removes a provider through the same repository call the
// admin DELETE /api/providers/{id} endpoint uses. Deliberately not raw SQL:
// pruning is Go-side now, so a hand-written DELETE would bypass it entirely and
// these tests would be asserting against a path no user can reach.
func deleteProviderViaRepo(t *testing.T, id string) {
	t.Helper()
	if err := provider.NewRepository(apiTestDB.Pool()).Delete(context.Background(), uuid.MustParse(id)); err != nil {
		t.Fatalf("delete provider %s: %v", id, err)
	}
}

// Deleting one provider out of several must remove exactly that id and leave
// the rest of the restriction standing.
func TestProviderPrune_KeyKeepsSurvivingProviders(t *testing.T) {
	cleanConfigTables(t)

	keep := seedProvider(t, "prune-keep", "sk-a", configSyncMasterKey)
	doomed := seedProvider(t, "prune-doomed", "sk-b", configSyncMasterKey)
	seedRestrictedKey(t, "vk-multi", "hash-prune-multi", []string{keep, doomed})

	deleteProviderViaRepo(t, doomed)

	isNull, card, members := readAllowList(t, keyAllowListQuery, "hash-prune-multi")
	if isNull {
		t.Fatal("ESCALATION: the key's allow-list is now NULL, i.e. unrestricted")
	}
	if card != 1 || members[0] != keep {
		t.Fatalf("allow-list = %v (cardinality %d), want just [%s]", members, card, keep)
	}
}

// Deleting the LAST provider in a key's allow-list must leave '{}', not NULL.
// The two are asserted separately because they now mean opposite things: NULL
// would hand the key every provider, which is the escalation this whole branch
// exists to close.
func TestProviderPrune_EmptiedKeyAllowListIsEmptyNotNull(t *testing.T) {
	cleanConfigTables(t)

	seedProvider(t, "prune-bystander", "sk-a", configSyncMasterKey)
	doomed := seedProvider(t, "prune-doomed", "sk-b", configSyncMasterKey)
	seedRestrictedKey(t, "vk-solo", "hash-prune-solo", []string{doomed})

	deleteProviderViaRepo(t, doomed)

	isNull, card, _ := readAllowList(t, keyAllowListQuery, "hash-prune-solo")
	if isNull {
		t.Fatal("ESCALATION: pruning the last entry produced NULL; the key is now unrestricted")
	}
	if card != 0 {
		t.Fatalf("allow-list cardinality = %d, want 0", card)
	}
}

// The same two cases for the per-user account cap (migration 065's column).
func TestProviderPrune_UserCapKeepsSurvivingProviders(t *testing.T) {
	cleanConfigTables(t)

	keep := seedProvider(t, "prune-keep", "sk-a", configSyncMasterKey)
	doomed := seedProvider(t, "prune-doomed", "sk-b", configSyncMasterKey)
	seedCappedUser(t, "capped-multi", []string{keep, doomed})

	deleteProviderViaRepo(t, doomed)

	isNull, card, members := readAllowList(t, userAllowListQuery, "capped-multi")
	if isNull {
		t.Fatal("ESCALATION: the account cap is now NULL, i.e. uncapped")
	}
	if card != 1 || members[0] != keep {
		t.Fatalf("cap = %v (cardinality %d), want just [%s]", members, card, keep)
	}
}

func TestProviderPrune_EmptiedUserCapIsEmptyNotNull(t *testing.T) {
	cleanConfigTables(t)

	seedProvider(t, "prune-bystander", "sk-a", configSyncMasterKey)
	doomed := seedProvider(t, "prune-doomed", "sk-b", configSyncMasterKey)
	seedCappedUser(t, "capped-solo", []string{doomed})

	deleteProviderViaRepo(t, doomed)

	isNull, card, _ := readAllowList(t, userAllowListQuery, "capped-solo")
	if isNull {
		t.Fatal("ESCALATION: pruning the last entry produced NULL; the account is now uncapped")
	}
	if card != 0 {
		t.Fatalf("cap cardinality = %d, want 0", card)
	}
}

// The other direction, and the one that would be a silent disaster: a key or
// account that was never restricted must stay NULL. The prune's WHERE clause is
// an array-overlap test, and NULL && anything is NULL rather than true, so an
// unrestricted row is never selected and an open key must not come out of a
// provider delete restricted to nothing.
func TestProviderPrune_UnrestrictedRowsStayNull(t *testing.T) {
	cleanConfigTables(t)

	seedProvider(t, "prune-bystander", "sk-a", configSyncMasterKey)
	doomed := seedProvider(t, "prune-doomed", "sk-b", configSyncMasterKey)
	seedRestrictedKey(t, "vk-open", "hash-prune-open", nil)
	seedUser(t, "uncapped", nil, true, nil)

	deleteProviderViaRepo(t, doomed)

	keyNull, keyCard, _ := readAllowList(t, keyAllowListQuery, "hash-prune-open")
	if !keyNull {
		t.Fatalf("LOCKOUT: an unrestricted key was given an allow-list (cardinality %d)", keyCard)
	}
	userNull, userCard, _ := readAllowList(t, userAllowListQuery, "uncapped")
	if !userNull {
		t.Fatalf("LOCKOUT: an uncapped account was given a cap (cardinality %d)", userCard)
	}
}

// The second delete path, and the one easiest to forget: config-sync's
// declarative replace drops providers in ONE bulk statement
// (`DELETE FROM providers WHERE name <> ALL($1) RETURNING id`, in
// configsync_apply.go), inside the import transaction. It has to prune the whole
// returned set, not just one id, and it is a separate call site from the admin
// delete, so nothing about the repository test above covers it.
//
// Two keys, because they prove different halves:
//
//   - "both" ends up holding the surviving id alone. The import also rewrites
//     this key (its names resolve to the survivor), so on its own it cannot tell
//     the prune from the upsert.
//   - "solo" is the unconfounded one. Its only name is the deleted provider, so
//     upsertVirtualKeys resolves nothing and SKIPS the row entirely rather than
//     writing NULL over a restriction. Nothing else in the import touches it, so
//     its stored value afterwards is the prune's work and nothing else. Without
//     the prune it would still hold the dangling UUID.
func TestProviderPrune_ConfigSyncBulkDeletePrunes(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	keep := seedProvider(t, "prune-keep", "sk-a", configSyncMasterKey)
	doomed := seedProvider(t, "prune-doomed", "sk-b", configSyncMasterKey)
	seedRestrictedKey(t, "vk-both", "hash-sync-both", []string{keep, doomed})
	seedRestrictedKey(t, "vk-solo", "hash-sync-solo", []string{doomed})

	// Export while both providers exist, then drop one from the envelope: that is
	// exactly the shape a primary produces after an operator deletes a provider
	// there, and it is what drives the member's declarative delete.
	env := doExport(t, r)
	env.Config.Providers = slices.DeleteFunc(env.Config.Providers, func(p ExportProvider) bool {
		return p.Name == "prune-doomed"
	})
	if len(env.Config.Providers) != 1 {
		t.Fatalf("envelope should carry exactly the surviving provider, got %d", len(env.Config.Providers))
	}

	if rec := doImport(t, r, env, ""); rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}

	// The provider really is gone, so the assertions below are about pruning and
	// not about a delete that silently did nothing.
	var stillThere bool
	if err := apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM providers WHERE id = $1)`, doomed).Scan(&stillThere); err != nil {
		t.Fatalf("checking the deleted provider: %v", err)
	}
	if stillThere {
		t.Fatal("the declarative replace did not delete the provider")
	}

	isNull, card, members := readAllowList(t, keyAllowListQuery, "hash-sync-both")
	if isNull {
		t.Fatal("ESCALATION: the multi-provider key became unrestricted")
	}
	if card != 1 || members[0] != keep {
		t.Fatalf("multi-provider key allow-list = %v (cardinality %d), want just [%s]", members, card, keep)
	}

	isNull, card, members = readAllowList(t, keyAllowListQuery, "hash-sync-solo")
	if isNull {
		t.Fatal("ESCALATION: the single-provider key became unrestricted")
	}
	if card != 0 {
		t.Fatalf("skipped key allow-list = %v (cardinality %d), want 0: the bulk delete did not fire the trigger", members, card)
	}
}
