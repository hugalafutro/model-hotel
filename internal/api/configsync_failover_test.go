package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// seedRawFailoverGroup inserts a custom group with the priority_order and
// entry_enabled columns set to arbitrary raw JSON bytes, so a test can model a
// corrupt or hand-edited row that export must reject rather than mis-translate.
func seedRawFailoverGroup(t *testing.T, displayModel, priorityJSON, entryJSON string) {
	t.Helper()
	_, err := apiTestDB.Pool().Exec(context.Background(),
		`INSERT INTO model_failover_groups (display_model, priority_order, entry_enabled, group_enabled, auto_created)
		 VALUES ($1, $2, $3, true, false)`,
		displayModel, []byte(priorityJSON), []byte(entryJSON))
	if err != nil {
		t.Fatalf("seed raw group %s: %v", displayModel, err)
	}
}

// rawExport issues GET /config/export without asserting 200, so a test can check
// the error path.
func rawExport(t *testing.T, r http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/config/export", http.NoBody))
	return rec
}

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

// An entry whose model UUID no longer resolves (the model was deleted after the
// group referenced it) is silently dropped from the export rather than carried as
// a dangling ref.
func TestConfigSync_ExportDropsDeletedModelEntry(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	m1 := seedModel(t, provID, "gpt-4o")
	m2 := seedModel(t, provID, "gpt-4o-mini")
	// A third priority entry points at a model UUID that has no row, as if the
	// model was deleted after the group was built.
	ghost := "00000000-0000-0000-0000-000000000000"
	seedFailoverGroup(t, "glm52", []string{m1, ghost, m2}, nil, false)

	env := doExport(t, r)
	if len(env.Config.FailoverGroups) != 1 {
		t.Fatalf("expected 1 group, got %+v", env.Config.FailoverGroups)
	}
	g := env.Config.FailoverGroups[0]
	if len(g.Entries) != 2 {
		t.Fatalf("expected the dangling entry dropped, got %+v", g.Entries)
	}
	if g.Entries[0].ModelID != "gpt-4o" || g.Entries[1].ModelID != "gpt-4o-mini" {
		t.Errorf("entries = %+v, want gpt-4o then gpt-4o-mini", g.Entries)
	}
}

// A group row whose priority_order JSON is not a string array aborts the export
// with a 500 rather than emitting a half-decoded envelope.
func TestConfigSync_ExportRejectsCorruptPriorityJSON(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	seedRawFailoverGroup(t, "glm52", `"not-an-array"`, `{}`)

	rec := rawExport(t, r)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("export status = %d, want 500; body %s", rec.Code, rec.Body.String())
	}
}

// A group row whose entry_enabled JSON is not an object aborts the export too.
func TestConfigSync_ExportRejectsCorruptEntryEnabledJSON(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	seedRawFailoverGroup(t, "glm52", `["a","b"]`, `"not-an-object"`)

	rec := rawExport(t, r)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("export status = %d, want 500; body %s", rec.Code, rec.Body.String())
	}
}

// A custom group the member already has is reported as Updated (not Added) in the
// import diff, so the operator sees a converge rather than a create.
func TestConfigSync_DiffReportsUpdatedForExistingGroup(t *testing.T) {
	cleanConfigTables(t)
	exportRouter := newConfigSyncRouter(t, configSyncMasterKey)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	pm1 := seedModel(t, provID, "gpt-4o")
	pm2 := seedModel(t, provID, "gpt-4o-mini")
	seedFailoverGroup(t, "glm52", []string{pm1, pm2}, nil, false)
	env := doExport(t, exportRouter)

	// Replica already has its own glm52 custom group over the same models.
	cleanConfigTables(t)
	rProvID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	rm1 := seedModel(t, rProvID, "gpt-4o")
	rm2 := seedModel(t, rProvID, "gpt-4o-mini")
	seedFailoverGroup(t, "glm52", []string{rm1, rm2}, nil, false)

	rec := doImport(t, newConfigSyncRouter(t, configSyncMasterKey), env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	var resp importResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !contains(resp.Diff.FailoverGroups.Updated, "glm52") {
		t.Errorf("expected glm52 in Updated, diff = %+v", resp.Diff.FailoverGroups)
	}
	if contains(resp.Diff.FailoverGroups.Added, "glm52") {
		t.Errorf("glm52 already exists; must not be Added, diff = %+v", resp.Diff.FailoverGroups)
	}
}

// Discovery on import is best-effort: a discovery error is logged and swallowed,
// the import still succeeds, and a group whose models are already present resolves
// from them regardless.
func TestConfigSync_ImportDiscoveryErrorIsBestEffort(t *testing.T) {
	cleanConfigTables(t)
	exportRouter := newConfigSyncRouter(t, configSyncMasterKey)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	pm1 := seedModel(t, provID, "gpt-4o")
	pm2 := seedModel(t, provID, "gpt-4o-mini")
	seedFailoverGroup(t, "glm52", []string{pm1, pm2}, nil, false)
	env := doExport(t, exportRouter)

	// Replica already has the models (a prior discovery), so the group can resolve
	// even though this import's discovery pass fails.
	cleanConfigTables(t)
	rProvID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	seedModel(t, rProvID, "gpt-4o")
	seedModel(t, rProvID, "gpt-4o-mini")

	discoverAll := func(ctx context.Context) error { return errors.New("discovery boom") }

	rec := doImport(t, newConfigSyncRouterWithDiscovery(t, configSyncMasterKey, discoverAll), env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	priority, _, autoCreated := groupPriority(t, "glm52")
	if autoCreated || len(priority) != 2 {
		t.Fatalf("glm52 priority = %v (auto=%v), want 2 entries despite discovery error", priority, autoCreated)
	}
}

// groupExists reports whether a failover group with the given display model is
// present.
func groupExists(t *testing.T, displayModel string) bool {
	t.Helper()
	var n int
	_ = apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM model_failover_groups WHERE display_model = $1`, displayModel).Scan(&n)
	return n > 0
}

// An envelope with NO failover_groups key (a pre-PR primary that never emits the
// field, which decodes to a nil slice) must leave the member's own custom groups
// untouched, so a rolling upgrade does not wipe them on the first sync.
func TestConfigSync_ImportWithAbsentGroupsKeepsExistingCustomGroups(t *testing.T) {
	cleanConfigTables(t)
	exportRouter := newConfigSyncRouter(t, configSyncMasterKey)
	seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	env := doExport(t, exportRouter)
	// Model a pre-PR primary: the field is absent on the wire, i.e. nil here.
	env.Config.FailoverGroups = nil

	// Replica has its own instance-local custom group plus an auto group.
	cleanConfigTables(t)
	rProvID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	rm1 := seedModel(t, rProvID, "gpt-4o")
	rm2 := seedModel(t, rProvID, "gpt-4o-mini")
	seedFailoverGroup(t, "local-custom", []string{rm1, rm2}, nil, false)
	seedFailoverGroup(t, "auto-shared", []string{rm1, rm2}, nil, true)

	rec := doImport(t, newConfigSyncRouter(t, configSyncMasterKey), env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	if !groupExists(t, "local-custom") {
		t.Error("an absent groups section must not wipe the member's own custom group")
	}
	if !groupExists(t, "auto-shared") {
		t.Error("auto-created group must survive regardless")
	}
}

// An envelope with an explicit empty failover_groups array (a current primary
// that genuinely has zero custom groups) must reconcile the member to zero: stale
// custom groups are removed, auto-created groups are left alone.
func TestConfigSync_ImportWithEmptyGroupsReconcilesToZero(t *testing.T) {
	cleanConfigTables(t)
	exportRouter := newConfigSyncRouter(t, configSyncMasterKey)
	seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	env := doExport(t, exportRouter)
	// A current primary with no custom groups still emits the key as [], not absent.
	if env.Config.FailoverGroups == nil {
		t.Fatal("export must emit a non-nil empty groups slice, got nil")
	}

	// Replica has a stale custom group (no longer on the primary) plus an auto group.
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
	if groupExists(t, "stale-custom") {
		t.Error("an explicit empty envelope must delete the stale custom group")
	}
	if !groupExists(t, "auto-shared") {
		t.Error("auto-created group must survive the reconcile")
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
