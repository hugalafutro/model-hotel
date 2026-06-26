package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
)

// seedModel inserts a model under a provider and returns its UUID.
func seedModel(t *testing.T, providerID, modelID string) string {
	t.Helper()
	var id string
	err := apiTestDB.Pool().QueryRow(context.Background(),
		`INSERT INTO models (provider_id, model_id, enabled) VALUES ($1, $2, true) RETURNING id`,
		providerID, modelID).Scan(&id)
	if err != nil {
		t.Fatalf("seed model %s: %v", modelID, err)
	}
	return id
}

// seedFailoverGroup inserts a failover group with the given model-UUID priority
// order. entryEnabled may be nil. autoCreated marks an auto-formed group.
func seedFailoverGroup(t *testing.T, displayModel string, priority []string, entryEnabled map[string]bool, autoCreated bool) {
	t.Helper()
	priorityJSON, _ := json.Marshal(priority)
	if entryEnabled == nil {
		entryEnabled = map[string]bool{}
	}
	entryJSON, _ := json.Marshal(entryEnabled)
	_, err := apiTestDB.Pool().Exec(context.Background(),
		`INSERT INTO model_failover_groups (display_model, priority_order, entry_enabled, group_enabled, auto_created)
		 VALUES ($1, $2, $3, true, $4)`,
		displayModel, priorityJSON, entryJSON, autoCreated)
	if err != nil {
		t.Fatalf("seed failover group %s: %v", displayModel, err)
	}
}

func groupPriority(t *testing.T, displayModel string) ([]string, map[string]bool, bool) {
	t.Helper()
	var priorityJSON, entryJSON []byte
	var autoCreated bool
	err := apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT priority_order, COALESCE(entry_enabled, '{}'), auto_created
		 FROM model_failover_groups WHERE display_model = $1`, displayModel).
		Scan(&priorityJSON, &entryJSON, &autoCreated)
	if err != nil {
		t.Fatalf("read group %s: %v", displayModel, err)
	}
	var priority []string
	_ = json.Unmarshal(priorityJSON, &priority)
	entry := map[string]bool{}
	_ = json.Unmarshal(entryJSON, &entry)
	return priority, entry, autoCreated
}

// Export carries only custom groups, with entries translated from local model
// UUIDs to stable (provider, model_id) refs and enabled flags preserved.
func TestConfigSync_ExportCustomFailoverGroups(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	m1 := seedModel(t, provID, "gpt-4o")
	m2 := seedModel(t, provID, "gpt-4o-mini")
	// Custom group: m2 disabled, to prove entry_enabled survives translation.
	seedFailoverGroup(t, "glm52", []string{m1, m2}, map[string]bool{m2: false}, false)
	// Auto-formed group must NOT be exported (it regenerates per member).
	seedFailoverGroup(t, "auto-shared", []string{m1, m2}, nil, true)

	env := doExport(t, r)
	if len(env.Config.FailoverGroups) != 1 {
		t.Fatalf("expected 1 custom group exported, got %+v", env.Config.FailoverGroups)
	}
	g := env.Config.FailoverGroups[0]
	if g.DisplayModel != "glm52" || len(g.Entries) != 2 {
		t.Fatalf("group = %+v", g)
	}
	if g.Entries[0].ProviderName != "openai" || g.Entries[0].ModelID != "gpt-4o" || !g.Entries[0].Enabled {
		t.Errorf("entry[0] = %+v, want openai/gpt-4o enabled", g.Entries[0])
	}
	if g.Entries[1].ModelID != "gpt-4o-mini" || g.Entries[1].Enabled {
		t.Errorf("entry[1] = %+v, want gpt-4o-mini disabled", g.Entries[1])
	}
}

// Import resolves (provider, model_id) refs back to the replica's own model
// UUIDs, so the group routes correctly despite the UUIDs differing per member.
func TestConfigSync_ImportFailoverGroupTranslatesUUIDs(t *testing.T) {
	cleanConfigTables(t)
	exportRouter := newConfigSyncRouter(t, configSyncMasterKey)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	pm1 := seedModel(t, provID, "gpt-4o")
	pm2 := seedModel(t, provID, "gpt-4o-mini")
	seedFailoverGroup(t, "glm52", []string{pm1, pm2}, map[string]bool{pm2: false}, false)
	env := doExport(t, exportRouter)

	// Fresh replica with DIFFERENT model UUIDs (as if discovery ran locally).
	cleanConfigTables(t)
	rProvID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	rm1 := seedModel(t, rProvID, "gpt-4o")
	rm2 := seedModel(t, rProvID, "gpt-4o-mini")

	rec := doImport(t, newConfigSyncRouter(t, configSyncMasterKey), env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	var resp importResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.Applied || !contains(resp.Diff.FailoverGroups.Added, "glm52") {
		t.Fatalf("diff = %+v", resp.Diff.FailoverGroups)
	}

	priority, entry, autoCreated := groupPriority(t, "glm52")
	if autoCreated {
		t.Error("synced custom group must have auto_created = false")
	}
	if len(priority) != 2 || priority[0] != rm1 || priority[1] != rm2 {
		t.Fatalf("priority = %v, want replica UUIDs [%s %s]", priority, rm1, rm2)
	}
	if v, ok := entry[rm2]; !ok || v {
		t.Errorf("entry_enabled[%s] = %v (ok=%v), want false", rm2, v, ok)
	}
}

// The import runs discovery between committing providers and resolving failover
// groups, so a member that starts with providers but no models still ends up
// with the custom group. The stub stands in for discoverAllProviders, seeding
// the models a real discovery would create.
func TestConfigSync_ImportRunsDiscoveryThenResolvesGroups(t *testing.T) {
	cleanConfigTables(t)
	exportRouter := newConfigSyncRouter(t, configSyncMasterKey)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	pm1 := seedModel(t, provID, "gpt-4o")
	pm2 := seedModel(t, provID, "gpt-4o-mini")
	seedFailoverGroup(t, "glm52", []string{pm1, pm2}, nil, false)
	env := doExport(t, exportRouter)

	// Replica starts with the provider but NO models (discovery has not run).
	cleanConfigTables(t)
	rProvID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)

	// Discovery stub: create the models the group needs, as real discovery would.
	// Records that it ran so we can assert the group resolved because of it.
	discovered := false
	discoverAll := func(ctx context.Context) error {
		discovered = true
		seedModel(t, rProvID, "gpt-4o")
		seedModel(t, rProvID, "gpt-4o-mini")
		return nil
	}

	rec := doImport(t, newConfigSyncRouterWithDiscovery(t, configSyncMasterKey, discoverAll), env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	if !discovered {
		t.Fatal("import did not run discovery")
	}
	// The group resolved against the just-discovered models.
	priority, _, autoCreated := groupPriority(t, "glm52")
	if autoCreated || len(priority) != 2 {
		t.Fatalf("glm52 priority = %v (auto=%v), want 2 resolved entries", priority, autoCreated)
	}
}

// A group is skipped (not created) when fewer than two of its entries resolve
// to models present on the member.
func TestConfigSync_ImportSkipsFailoverGroupWithMissingModels(t *testing.T) {
	cleanConfigTables(t)
	exportRouter := newConfigSyncRouter(t, configSyncMasterKey)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	pm1 := seedModel(t, provID, "gpt-4o")
	pm2 := seedModel(t, provID, "gpt-4o-mini")
	seedFailoverGroup(t, "glm52", []string{pm1, pm2}, nil, false)
	env := doExport(t, exportRouter)

	// Replica only has ONE of the two models, so the group has too few routable
	// entries and must be skipped rather than created half-broken.
	cleanConfigTables(t)
	rProvID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	seedModel(t, rProvID, "gpt-4o") // gpt-4o-mini absent here

	rec := doImport(t, newConfigSyncRouter(t, configSyncMasterKey), env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	var n int
	_ = apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM model_failover_groups WHERE display_model = 'glm52'`).Scan(&n)
	if n != 0 {
		t.Fatalf("group with one resolvable entry should be skipped, found %d", n)
	}
}

// Declarative replace removes a custom group absent from the envelope but never
// touches auto-created groups (those regenerate from discovery).
func TestConfigSync_ImportDeletesAbsentCustomGroupsButKeepsAuto(t *testing.T) {
	cleanConfigTables(t)
	exportRouter := newConfigSyncRouter(t, configSyncMasterKey)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	pm1 := seedModel(t, provID, "gpt-4o")
	pm2 := seedModel(t, provID, "gpt-4o-mini")
	seedFailoverGroup(t, "glm52", []string{pm1, pm2}, nil, false)
	env := doExport(t, exportRouter)

	// Replica has the synced models plus a stale custom group and an auto group
	// that are NOT in the envelope.
	cleanConfigTables(t)
	rProvID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	rm1 := seedModel(t, rProvID, "gpt-4o")
	rm2 := seedModel(t, rProvID, "gpt-4o-mini")
	seedFailoverGroup(t, "stale-custom", []string{rm1, rm2}, nil, false)
	seedFailoverGroup(t, "auto-shared", []string{rm1, rm2}, nil, true)

	rec := doImport(t, newConfigSyncRouter(t, configSyncMasterKey), env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}

	exists := func(name string) bool {
		var n int
		_ = apiTestDB.Pool().QueryRow(context.Background(),
			`SELECT count(*) FROM model_failover_groups WHERE display_model = $1`, name).Scan(&n)
		return n > 0
	}
	if exists("stale-custom") {
		t.Error("stale custom group absent from envelope should be deleted")
	}
	if !exists("auto-shared") {
		t.Error("auto-created group must survive a config sync")
	}
	if !exists("glm52") {
		t.Error("synced custom group should be present")
	}
}
