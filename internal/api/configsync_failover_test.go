package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/failover"
	"github.com/hugalafutro/model-hotel/internal/settings"
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

// Export carries every group, custom and auto, with entries translated from local
// model UUIDs to stable (provider, model_id) refs and enabled flags preserved.
func TestConfigSync_ExportFailoverGroups(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	m1 := seedModel(t, provID, "gpt-4o")
	m2 := seedModel(t, provID, "gpt-4o-mini")
	// Custom group: m2 disabled, to prove entry_enabled survives translation.
	seedFailoverGroup(t, "glm52", []string{m1, m2}, map[string]bool{m2: false}, false)
	// Auto-formed group travels too, flagged, so members take the primary's order.
	seedFailoverGroup(t, "auto-shared", []string{m1, m2}, nil, true)

	env := doExport(t, r)
	if len(env.Config.FailoverGroups) != 2 {
		t.Fatalf("expected both groups exported, got %+v", env.Config.FailoverGroups)
	}
	if a := env.Config.FailoverGroups[0]; a.DisplayModel != "auto-shared" || !a.AutoCreated {
		t.Fatalf("group[0] = %+v, want auto-shared flagged auto", a)
	}
	g := env.Config.FailoverGroups[1]
	if g.DisplayModel != "glm52" || len(g.Entries) != 2 || g.AutoCreated {
		t.Fatalf("group = %+v, want custom glm52 with 2 entries", g)
	}
	if g.Entries[0].ProviderName != "openai" || g.Entries[0].ModelID != "gpt-4o" || !g.Entries[0].Enabled {
		t.Errorf("entry[0] = %+v, want openai/gpt-4o enabled", g.Entries[0])
	}
	if g.Entries[1].ModelID != "gpt-4o-mini" || g.Entries[1].Enabled {
		t.Errorf("entry[1] = %+v, want gpt-4o-mini disabled", g.Entries[1])
	}
}

// A custom group whose description column is NULL (the main app's failover Upsert
// writes a SQL NULL when the caller passes a nil description) must still export:
// the query COALESCEs description, so the Scan into the string field does not fail
// and take the whole member's config-sync down with it.
func TestConfigSync_ExportToleratesNullGroupDescription(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	m1 := seedModel(t, provID, "gpt-4o")
	seedFailoverGroup(t, "glm52", []string{m1}, nil, false)
	// Force the description to a genuine SQL NULL, mimicking a group created via the
	// main-app Upsert path with no description.
	if _, err := apiTestDB.Pool().Exec(context.Background(),
		`UPDATE model_failover_groups SET description = NULL WHERE display_model = $1`, "glm52"); err != nil {
		t.Fatalf("null out description: %v", err)
	}

	env := doExport(t, r)
	if len(env.Config.FailoverGroups) != 1 {
		t.Fatalf("expected 1 custom group exported, got %+v", env.Config.FailoverGroups)
	}
	if g := env.Config.FailoverGroups[0]; g.Description != "" {
		t.Errorf("NULL description should export as empty string, got %q", g.Description)
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

// The dry-run diff must match what apply actually does for failover groups: an
// absent field (nil, a pre-PR primary) performs no removals, while an explicit
// empty array reconciles the member to zero and so does report removals. Otherwise
// the preview would warn an operator mid-rolling-upgrade of deletions that the
// apply never performs.
func TestConfigSync_DiffMatchesApplyForAbsentVsEmptyGroups(t *testing.T) {
	seedReplicaWithCustomGroup := func() {
		cleanConfigTables(t)
		rProvID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
		rm1 := seedModel(t, rProvID, "gpt-4o")
		rm2 := seedModel(t, rProvID, "gpt-4o-mini")
		seedFailoverGroup(t, "local-custom", []string{rm1, rm2}, nil, false)
	}
	dryRunDiff := func(env ConfigEnvelope) entityDiff {
		t.Helper()
		rec := doImport(t, newConfigSyncRouter(t, configSyncMasterKey), env, "?dryRun=1")
		if rec.Code != http.StatusOK {
			t.Fatalf("dryRun status = %d, body %s", rec.Code, rec.Body.String())
		}
		var resp importResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode dryRun response: %v", err)
		}
		if resp.Applied {
			t.Fatal("dryRun must not apply")
		}
		return resp.Diff.FailoverGroups
	}
	// A virtual key keeps each envelope non-empty (import refuses a structurally
	// empty config) without introducing any custom groups of its own.
	withVK := func(groups []ExportFailoverGroup) ConfigEnvelope {
		return ConfigEnvelope{
			SchemaVersion: configSchemaVersion,
			Config: ConfigPayload{
				VirtualKeys:    []ExportVK{{Name: "vk", KeyHash: "h", KeyPreview: "p"}},
				FailoverGroups: groups,
			},
		}
	}

	seedReplicaWithCustomGroup()
	if got := dryRunDiff(withVK(nil)); contains(got.Removed, "local-custom") {
		t.Errorf("absent groups must report no removals, diff = %+v", got)
	}

	seedReplicaWithCustomGroup()
	if got := dryRunDiff(withVK([]ExportFailoverGroup{})); !contains(got.Removed, "local-custom") {
		t.Errorf("explicit empty groups must report the stale group as removed, diff = %+v", got)
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

// setGroupAutoDisabled models this member's OWN discovery having auto-disabled a
// group: group_enabled = false plus the migration-062 provenance stamp that
// makes it a discovery claim rather than an operator choice.
func setGroupAutoDisabled(t *testing.T, displayModel string) {
	t.Helper()
	tag, err := apiTestDB.Pool().Exec(context.Background(),
		`UPDATE model_failover_groups
		    SET group_enabled = false, auto_disabled_at = now()
		  WHERE display_model = $1`, displayModel)
	if err != nil {
		t.Fatalf("auto-disable group %s: %v", displayModel, err)
	}
	// A silently zero-row UPDATE would leave the group ENABLED and unstamped,
	// which would make the preserve-half of the test below pass for the wrong
	// reason on step 2 and fail confusingly on step 1.
	if tag.RowsAffected() != 1 {
		t.Fatalf("auto-disable group %s affected %d rows, want 1", displayModel, tag.RowsAffected())
	}
}

// groupAutoDisabledAt reads the provenance stamp back, nil when it is SQL NULL.
func groupAutoDisabledAt(t *testing.T, displayModel string) *time.Time {
	t.Helper()
	var at *time.Time
	if err := apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT auto_disabled_at FROM model_failover_groups WHERE display_model = $1`,
		displayModel).Scan(&at); err != nil {
		t.Fatalf("read auto_disabled_at for %s: %v", displayModel, err)
	}
	return at
}

// A config-sync import must not erase a stamp this member's own discovery
// earned. Clearing it is a ONE-WAY door: revalidateCustomGroups skips groups
// that are already disabled, so nothing ever re-stamps a disabled group, and a
// member whose hotel/<model> routing is dead would go silent about it forever.
//
// Both directions in one test, because the rule is a CASE on the imported flag
// and either half alone would pass under a blanket clear or a blanket keep:
//
//   - imported group_enabled = false: that is the PRIMARY's group state and says
//     nothing about this member's routable membership, so the local stamp stays.
//   - imported group_enabled = true: fleet-level operator intent that does
//     contradict a local auto-disable, and the only direction that is genuinely
//     self-healing, since an ENABLED group is no longer skipped by revalidation
//     and gets re-disabled and re-stamped on the next scan if it really is short
//     of routable members here.
func TestConfigSync_ImportPreservesLocalAutoDisableStamp(t *testing.T) {
	cleanConfigTables(t)
	exportRouter := newConfigSyncRouter(t, configSyncMasterKey)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	pm1 := seedModel(t, provID, "gpt-4o")
	pm2 := seedModel(t, provID, "gpt-4o-mini")
	seedFailoverGroup(t, "glm52", []string{pm1, pm2}, nil, false)
	env := doExport(t, exportRouter)
	if len(env.Config.FailoverGroups) != 1 {
		t.Fatalf("expected 1 exported group, got %+v", env.Config.FailoverGroups)
	}

	// A member carrying a live discovery claim on the same group.
	seedMemberWithClaim := func() {
		cleanConfigTables(t)
		rProvID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
		rm1 := seedModel(t, rProvID, "gpt-4o")
		rm2 := seedModel(t, rProvID, "gpt-4o-mini")
		seedFailoverGroup(t, "glm52", []string{rm1, rm2}, nil, false)
		setGroupAutoDisabled(t, "glm52")
		if groupAutoDisabledAt(t, "glm52") == nil {
			t.Fatal("seed anchor: the member must start with a stamped claim")
		}
	}

	seedMemberWithClaim()
	env.Config.FailoverGroups[0].GroupEnabled = false
	if rec := doImport(t, newConfigSyncRouter(t, configSyncMasterKey), env, ""); rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	if groupAutoDisabledAt(t, "glm52") == nil {
		t.Error("an import that leaves the group disabled erased the local discovery stamp; nothing can ever re-stamp a disabled group, so that claim is gone permanently")
	}

	seedMemberWithClaim()
	env.Config.FailoverGroups[0].GroupEnabled = true
	if rec := doImport(t, newConfigSyncRouter(t, configSyncMasterKey), env, ""); rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	if at := groupAutoDisabledAt(t, "glm52"); at != nil {
		t.Errorf("an import that re-enables the group must clear the stamp (operator intent, and revalidation re-stamps it if still undersized), got %v", at)
	}
}

// The post-commit steps run on their own budget, not the caller's. Front Desk's
// import client gives up after 120s while discovery on a fresh member routinely
// eats most of that, so by the time the group build starts the request context
// can already be dead. The core config is committed at that point, so the build
// must still run: inheriting the cancellation drops every custom group while the
// member answers 200 / Applied: true.
//
// The synced marker is asserted alongside the group build because it is stamped
// from the same detached context: a narrower detach that covered only the build
// would keep the group assertions green while silently leaving the member's
// dashboard reporting it was never synced.
func TestConfigSync_FailoverGroupsSurviveCancelledRequestContext(t *testing.T) {
	cleanConfigTables(t)
	sr := settings.NewRepository(apiTestDB.Pool())
	h := NewConfigSyncHandler(apiTestDB, sr, configSyncMasterKey, "v-test", nil, nil)

	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	seedModel(t, provID, "gpt-4o")
	seedModel(t, provID, "gpt-4o-mini")

	groups := []ExportFailoverGroup{{
		DisplayModel: "survivor",
		GroupEnabled: true,
		Entries: []ExportFailoverEntry{
			{ProviderName: "openai", ModelID: "gpt-4o", Enabled: true},
			{ProviderName: "openai", ModelID: "gpt-4o-mini", Enabled: true},
		},
	}}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Front Desk hung up before the build began

	out := h.postImportRefresh(ctx, ConfigEnvelope{Config: ConfigPayload{FailoverGroups: groups}}, nil, nil, nil)

	if out.GroupApplyErr != nil {
		t.Fatalf("group apply must not inherit the request cancellation: %v", out.GroupApplyErr)
	}
	if len(out.SkippedGroups) != 0 {
		t.Fatalf("SkippedGroups = %v, want none", out.SkippedGroups)
	}
	if !groupExists(t, "survivor") {
		t.Fatal("group was not written")
	}
	got := sr.GetWithDefault(context.Background(), keyFleetConfigSyncedAt, "")
	if got == "" {
		t.Fatal("synced marker not stamped: it must not inherit the request cancellation either")
	}
	if _, err := time.Parse(time.RFC3339, got); err != nil {
		t.Errorf("synced marker = %q, not RFC3339: %v", got, err)
	}
}

// An EXPIRED DEADLINE on the caller's context must not reach the group build
// either, which is the distinct half of the cancellation case above: the build
// derives its own budget with context.WithTimeout, and a child of an already
// expired parent is born expired.
//
// This is the shape a per-refresh ceiling would create. Discovery detaches every
// provider under its own timeout and never consults this context, so an aggregate
// deadline cannot shorten the sweep it is meant to bound; it can only expire while
// discovery runs long and then fail the one step that has to happen. The member
// would answer applied-but-incomplete with no groups built, and be re-pushed
// forever, restarting the same long discovery each time.
func TestConfigSync_FailoverGroupsSurviveExpiredRequestDeadline(t *testing.T) {
	cleanConfigTables(t)
	sr := settings.NewRepository(apiTestDB.Pool())
	h := NewConfigSyncHandler(apiTestDB, sr, configSyncMasterKey, "v-test", nil, nil)

	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	seedModel(t, provID, "gpt-4o")
	seedModel(t, provID, "gpt-4o-mini")

	groups := []ExportFailoverGroup{{
		DisplayModel: "survivor",
		GroupEnabled: true,
		Entries: []ExportFailoverEntry{
			{ProviderName: "openai", ModelID: "gpt-4o", Enabled: true},
			{ProviderName: "openai", ModelID: "gpt-4o-mini", Enabled: true},
		},
	}}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Hour))
	defer cancel()
	if ctx.Err() == nil {
		t.Fatal("test setup: the context was expected to be already expired")
	}

	out := h.postImportRefresh(ctx, ConfigEnvelope{Config: ConfigPayload{FailoverGroups: groups}}, nil, nil, nil)

	if out.GroupApplyErr != nil {
		t.Fatalf("group apply inherited the expired deadline: %v", out.GroupApplyErr)
	}
	if out.incomplete() {
		t.Error("an expired caller deadline made the import report incomplete")
	}
	if !groupExists(t, "survivor") {
		t.Fatal("group was not written")
	}
}

// upsertFailoverGroups reports the DisplayModel of a group it skips for too few
// resolvable entries, and that group never also lands in Partial: a group with no
// entries it can use is Skipped, never Partial, so the two lists stay disjoint.
func TestConfigSync_UpsertReportsSkippedGroups(t *testing.T) {
	cleanConfigTables(t)
	tx, err := apiTestDB.Pool().Begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	groups := []ExportFailoverGroup{{
		DisplayModel: "ghost-group",
		GroupEnabled: true,
		Entries: []ExportFailoverEntry{
			{ProviderName: "NoSuchProvider", ModelID: "no-such-model-a", Enabled: true},
			{ProviderName: "NoSuchProvider", ModelID: "no-such-model-b", Enabled: true},
		},
	}}

	res, err := upsertFailoverGroups(context.Background(), tx, groups)
	if err != nil {
		t.Fatalf("upsertFailoverGroups: %v", err)
	}
	if len(res.Skipped) != 1 || res.Skipped[0] != "ghost-group" {
		t.Fatalf("Skipped = %v, want [ghost-group]", res.Skipped)
	}
	if len(res.Partial) != 0 {
		t.Fatalf("Partial = %v, want none: a group below the two-entry floor is skipped, never partial", res.Partial)
	}
}

// A group that resolves two of the three entries the primary sent is built, with
// the two it has, and reported as partial rather than skipped: this member fails
// over across fewer providers for that model, which is a real difference from the
// primary and not a group it failed to build.
func TestConfigSync_UpsertReportsPartialGroups(t *testing.T) {
	cleanConfigTables(t)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	seedModel(t, provID, "gpt-4o")
	seedModel(t, provID, "gpt-4o-mini")

	tx, err := apiTestDB.Pool().Begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	groups := []ExportFailoverGroup{{
		DisplayModel: "short-group",
		GroupEnabled: true,
		Entries: []ExportFailoverEntry{
			{ProviderName: "openai", ModelID: "gpt-4o", Enabled: true},
			{ProviderName: "openai", ModelID: "gpt-4o-mini", Enabled: true},
			{ProviderName: "openai", ModelID: "gpt-4o-absent", Enabled: true},
		},
	}}

	res, err := upsertFailoverGroups(context.Background(), tx, groups)
	if err != nil {
		t.Fatalf("upsertFailoverGroups: %v", err)
	}
	if len(res.Partial) != 1 || res.Partial[0] != "short-group" {
		t.Fatalf("Partial = %v, want [short-group]", res.Partial)
	}
	if len(res.Skipped) != 0 {
		t.Fatalf("Skipped = %v, want none: the group was written, just with fewer entries", res.Skipped)
	}
	var priorityJSON []byte
	if err := tx.QueryRow(context.Background(),
		`SELECT priority_order FROM model_failover_groups WHERE display_model = 'short-group'`).
		Scan(&priorityJSON); err != nil {
		t.Fatalf("a partial group must still be written: %v", err)
	}
	var priority []string
	if err := json.Unmarshal(priorityJSON, &priority); err != nil {
		t.Fatalf("decode priority_order: %v", err)
	}
	if len(priority) != 2 {
		t.Fatalf("priority_order = %v, want the 2 entries this member could resolve", priority)
	}
}

// A fully resolving group is neither skipped nor partial: the member holds
// exactly what the primary sent.
func TestConfigSync_UpsertReportsNothingForAFullGroup(t *testing.T) {
	cleanConfigTables(t)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	seedModel(t, provID, "gpt-4o")
	seedModel(t, provID, "gpt-4o-mini")

	tx, err := apiTestDB.Pool().Begin(context.Background())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	groups := []ExportFailoverGroup{{
		DisplayModel: "whole-group",
		GroupEnabled: true,
		Entries: []ExportFailoverEntry{
			{ProviderName: "openai", ModelID: "gpt-4o", Enabled: true},
			{ProviderName: "openai", ModelID: "gpt-4o-mini", Enabled: true},
		},
	}}

	res, err := upsertFailoverGroups(context.Background(), tx, groups)
	if err != nil {
		t.Fatalf("upsertFailoverGroups: %v", err)
	}
	if len(res.Skipped) != 0 || len(res.Partial) != 0 {
		t.Fatalf("result = %+v, want both lists empty for a fully resolving group", res)
	}
}

// ghostGroupEnvelope builds an otherwise-minimal envelope (a virtual key keeps
// it non-empty) carrying one custom failover group whose entries name a
// provider absent from the member, so upsertFailoverGroups skips it.
func ghostGroupEnvelope() ConfigEnvelope {
	return ConfigEnvelope{
		SchemaVersion: configSchemaVersion,
		Config: ConfigPayload{
			VirtualKeys: []ExportVK{{Name: "vk", KeyHash: "h", KeyPreview: "p"}},
			FailoverGroups: []ExportFailoverGroup{{
				DisplayModel: "ghost-group",
				GroupEnabled: true,
				Entries: []ExportFailoverEntry{
					{ProviderName: "NoSuchProvider", ModelID: "no-such-model-a", Enabled: true},
					{ProviderName: "NoSuchProvider", ModelID: "no-such-model-b", Enabled: true},
				},
			}},
		},
	}
}

// An import that commits but cannot build one of its custom failover groups
// still answers Applied: true (the core config is durable) and additionally
// reports Incomplete plus the group's DisplayModel in Unapplied, so a caller
// like Front Desk can tell a full apply from one that left a group unbuilt.
func TestConfigSync_ImportReportsIncompleteOnSkippedGroup(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	rec := doImport(t, r, ghostGroupEnvelope(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}

	var got importResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Applied {
		t.Fatal("Applied = false, want true (the core config committed)")
	}
	if !got.Incomplete {
		t.Fatal("Incomplete = false, want true")
	}
	if len(got.Unapplied) != 1 || got.Unapplied[0] != "ghost-group" {
		t.Fatalf("Unapplied = %v, want [ghost-group]", got.Unapplied)
	}
}

// A clean import (nothing skipped) must omit incomplete/unapplied from the
// wire entirely, not emit them as false/null, so an older Front Desk that
// only checks Applied sees the same shape it always has.
func TestConfigSync_ImportOmitsIncompleteWhenFullyApplied(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	env := ConfigEnvelope{
		SchemaVersion: configSchemaVersion,
		Config: ConfigPayload{
			VirtualKeys: []ExportVK{{Name: "vk", KeyHash: "h", KeyPreview: "p"}},
		},
	}

	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(`"incomplete"`)) {
		t.Fatalf("clean import must omit incomplete, got %s", rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(`"unapplied"`)) {
		t.Fatalf("clean import must omit unapplied, got %s", rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(`"partial"`)) {
		t.Fatalf("clean import must omit partial, got %s", rec.Body.String())
	}
}

// A member holding fewer of a group's models than the primary builds that group
// with what it has and names it in Partial. The apply is NOT incomplete: the
// member did everything it was asked. It is still diverged, which the config
// hash establishes; Partial only tells the operator which group is short.
func TestConfigSync_ImportReportsPartialGroup(t *testing.T) {
	cleanConfigTables(t)
	exportRouter := newConfigSyncRouter(t, configSyncMasterKey)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	pm1 := seedModel(t, provID, "gpt-4o")
	pm2 := seedModel(t, provID, "gpt-4o-mini")
	pm3 := seedModel(t, provID, "gpt-4o-nano")
	seedFailoverGroup(t, "glm52", []string{pm1, pm2, pm3}, nil, false)
	env := doExport(t, exportRouter)

	// Replica has two of the three models, so the group builds short rather than
	// falling below the two-entry floor.
	cleanConfigTables(t)
	rProvID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	seedModel(t, rProvID, "gpt-4o")
	seedModel(t, rProvID, "gpt-4o-mini") // gpt-4o-nano absent here

	rec := doImport(t, newConfigSyncRouter(t, configSyncMasterKey), env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	var got importResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Applied {
		t.Fatal("Applied = false, want true")
	}
	if len(got.Partial) != 1 || got.Partial[0] != "glm52" {
		t.Fatalf("Partial = %v, want [glm52]", got.Partial)
	}
	if len(got.Unapplied) != 0 {
		t.Fatalf("Unapplied = %v, want none: the group was built", got.Unapplied)
	}
	if got.Incomplete {
		t.Fatal("Incomplete = true for a partial build; want false: the member applied everything it was asked to")
	}
	priority, _, _ := groupPriority(t, "glm52")
	if len(priority) != 2 {
		t.Fatalf("priority = %v, want the 2 entries this member could resolve", priority)
	}
}

// seedSharedModel gives two providers the same model_id, which is what forms an
// auto group named after the model's leaf. Returns the two model UUIDs in the
// order the providers were given.
func seedSharedModel(t *testing.T, modelID string, providerIDs ...string) []string {
	t.Helper()
	ids := make([]string, 0, len(providerIDs))
	for _, p := range providerIDs {
		ids = append(ids, seedModel(t, p, modelID))
	}
	return ids
}

// An auto group travels with the flag set, entries in the primary's order.
func TestConfigSync_ExportCarriesAutoGroupsFlagged(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	openai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	azure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	m := seedSharedModel(t, "gpt-4o", openai, azure)
	// Azure first: the order the operator chose, not provider creation order.
	seedFailoverGroup(t, "gpt-4o", []string{m[1], m[0]}, nil, true)

	env := doExport(t, r)
	if len(env.Config.FailoverGroups) != 1 {
		t.Fatalf("groups = %+v, want the auto group", env.Config.FailoverGroups)
	}
	g := env.Config.FailoverGroups[0]
	if !g.AutoCreated || g.DisplayModel != "gpt-4o" || len(g.Entries) != 2 {
		t.Fatalf("group = %+v, want auto gpt-4o with 2 entries", g)
	}
	if g.Entries[0].ProviderName != "azure" || g.Entries[1].ProviderName != "openai" {
		t.Errorf("entries = %+v, want azure before openai", g.Entries)
	}
}

// A member takes the primary's auto group as an auto group, in the primary's
// order, resolved to its own model UUIDs, and the dry-run diff counts it.
func TestConfigSync_ImportAutoGroupKeepsFlagAndOrder(t *testing.T) {
	cleanConfigTables(t)
	openai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	azure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	m := seedSharedModel(t, "gpt-4o", openai, azure)
	seedFailoverGroup(t, "gpt-4o", []string{m[1], m[0]}, map[string]bool{m[0]: false}, true)
	env := doExport(t, newConfigSyncRouter(t, configSyncMasterKey))

	// Member: same providers, different model UUIDs, and its own discovery already
	// formed the group in creation order.
	cleanConfigTables(t)
	rOpenai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	rAzure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	rm := seedSharedModel(t, "gpt-4o", rOpenai, rAzure)
	seedFailoverGroup(t, "gpt-4o", []string{rm[0], rm[1]}, nil, true)

	rec := doImport(t, newConfigSyncRouter(t, configSyncMasterKey), env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	var resp importResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !contains(resp.Diff.FailoverGroups.Updated, "gpt-4o") {
		t.Errorf("diff = %+v, want the auto group counted as updated", resp.Diff.FailoverGroups)
	}
	priority, entry, autoCreated := groupPriority(t, "gpt-4o")
	if !autoCreated {
		t.Error("synced auto group must stay auto_created = true")
	}
	if len(priority) != 2 || priority[0] != rm[1] || priority[1] != rm[0] {
		t.Fatalf("priority = %v, want the primary's order [azure openai] = [%s %s]", priority, rm[1], rm[0])
	}
	if v, ok := entry[rm[0]]; !ok || v {
		t.Errorf("entry_enabled[openai] = %v (ok=%v), want false", v, ok)
	}
}

// An auto group this member cannot fill is the member's own discovery's business:
// it is neither built short nor reported, because a report would raise
// config.sync_incomplete for a gap that is not an operator error.
func TestConfigSync_ImportShortAutoGroupIsSilent(t *testing.T) {
	cleanConfigTables(t)
	openai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	azure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	groq := seedProvider(t, "groq", "sk-secret", configSyncMasterKey)
	three := seedSharedModel(t, "gpt-4o", openai, azure, groq)
	seedFailoverGroup(t, "gpt-4o", three, nil, true)
	two := seedSharedModel(t, "o3", openai, azure)
	seedFailoverGroup(t, "o3", two, nil, true)
	env := doExport(t, newConfigSyncRouter(t, configSyncMasterKey))

	// Member holds gpt-4o on two of the three providers and o3 on one.
	cleanConfigTables(t)
	rOpenai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	rAzure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	seedProvider(t, "groq", "sk-secret", configSyncMasterKey)
	seedSharedModel(t, "gpt-4o", rOpenai, rAzure)
	seedModel(t, rOpenai, "o3")

	rec := doImport(t, newConfigSyncRouter(t, configSyncMasterKey), env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	var resp importResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Incomplete || len(resp.Unapplied) != 0 || len(resp.Partial) != 0 {
		t.Errorf("resp = incomplete=%v unapplied=%v partial=%v, want nothing reported for auto groups",
			resp.Incomplete, resp.Unapplied, resp.Partial)
	}
	if priority, _, _ := groupPriority(t, "gpt-4o"); len(priority) != 2 {
		t.Errorf("gpt-4o priority = %v, want the two resolvable entries", priority)
	}
	var n int
	_ = apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM model_failover_groups WHERE display_model = 'o3'`).Scan(&n)
	if n != 0 {
		t.Error("o3 resolved to one entry and must not be built")
	}
	// The echo still carries o3 as sent, so this member's hash can equal the
	// primary's despite the gap.
	setFleetPrimaryMarker(t, false)
	var echoed *ExportFailoverGroup
	for _, g := range doExport(t, newConfigSyncRouter(t, configSyncMasterKey)).Config.FailoverGroups {
		if g.DisplayModel == "o3" {
			echoed = &g
		}
	}
	if echoed == nil || len(echoed.Entries) != 2 || !echoed.AutoCreated {
		t.Errorf("echoed o3 = %+v, want the primary's two-entry auto group", echoed)
	}
}

// The member's own discovery keeps the imported order and appends what it finds.
func TestConfigSync_ImportedAutoOrderSurvivesLocalDiscoverySync(t *testing.T) {
	cleanConfigTables(t)
	openai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	azure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	m := seedSharedModel(t, "gpt-4o", openai, azure)
	seedFailoverGroup(t, "gpt-4o", []string{m[1], m[0]}, nil, true)
	env := doExport(t, newConfigSyncRouter(t, configSyncMasterKey))

	cleanConfigTables(t)
	rOpenai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	rAzure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	rm := seedSharedModel(t, "gpt-4o", rOpenai, rAzure)
	rec := doImport(t, newConfigSyncRouter(t, configSyncMasterKey), env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}

	// A third provider appears on the member only; its scan re-forms the group.
	groq := seedProvider(t, "groq", "sk-secret", configSyncMasterKey)
	extra := seedModel(t, groq, "gpt-4o")
	if _, err := failover.NewRepository(apiTestDB.Pool()).SyncAllModels(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	priority, _, autoCreated := groupPriority(t, "gpt-4o")
	want := []string{rm[1], rm[0], extra}
	if !autoCreated || !slices.Equal(priority, want) {
		t.Fatalf("priority = %v auto=%v, want %v with the imported order kept and the new model last", priority, autoCreated, want)
	}
}

// A member's export echoes the auto groups it was last sent, so its config hash
// equals the primary's; its own discovery changing an auto group drops the echo
// (the hash then differs, and Front Desk re-imports), and the next import puts it
// back while keeping what discovery found. Promoted, it exports its own rows.
func TestConfigSync_MemberEchoesPrimaryAutoGroups(t *testing.T) {
	cleanConfigTables(t)
	openai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	azure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	m := seedSharedModel(t, "gpt-4o", openai, azure)
	seedFailoverGroup(t, "gpt-4o", []string{m[1], m[0]}, nil, true)
	c := seedSharedModel(t, "o3", openai, azure)
	seedFailoverGroup(t, "mine", c, nil, false)
	rp := newConfigSyncRouter(t, configSyncMasterKey)
	env := doExport(t, rp)
	primaryVersion := doVersion(t, rp)

	cleanConfigTables(t)
	rOpenai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	rAzure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	rm := seedSharedModel(t, "gpt-4o", rOpenai, rAzure)
	seedSharedModel(t, "o3", rOpenai, rAzure)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	if rec := doImport(t, r, env, ""); rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	setFleetPrimaryMarker(t, false)
	want, _ := json.Marshal(env.Config.FailoverGroups)
	if got, _ := json.Marshal(doExport(t, r).Config.FailoverGroups); !bytes.Equal(got, want) {
		t.Errorf("after import, failover groups = %s\nwant the primary's list verbatim: %s", got, want)
	}
	if v := doVersion(t, r); v != primaryVersion {
		t.Errorf("after import, version = %s, want the primary's %s", v, primaryVersion)
	}

	// The member's discovery then forms groups the primary has not sent (gpt-5
	// and o3 both share two providers here). Nothing the primary sent changed, so
	// the echo stays and the member still hashes as converged.
	seedSharedModel(t, "gpt-5", rOpenai, rAzure)
	if _, err := failover.NewRepository(apiTestDB.Pool()).SyncAllModels(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if v := doVersion(t, r); v != primaryVersion {
		t.Errorf("after a scan that only formed member-only groups, version = %s, want the primary's %s", v, primaryVersion)
	}
	// A change to a group the primary DID send is a different matter: its azure
	// model goes away here, the scan deletes the undersized group, and the echo
	// would now hide that, so it is dropped and the member shows its own rows.
	if _, err := apiTestDB.Pool().Exec(context.Background(), `UPDATE models SET enabled = false WHERE id = $1`, rm[1]); err != nil {
		t.Fatalf("disable model: %v", err)
	}
	if _, err := failover.NewRepository(apiTestDB.Pool()).SyncAllModels(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if got, _ := json.Marshal(doExport(t, r).Config.FailoverGroups); bytes.Equal(got, want) {
		t.Fatal("after a scan that deleted an echoed auto group, the export must show the member's own rows, not the stale echo")
	}
	if names := groupNames(doExport(t, r).Config.FailoverGroups); !slices.Equal(names, []string{"gpt-5", "mine", "o3"}) {
		t.Fatalf("own rows = %v, want gpt-5 mine o3", names)
	}
	// The model comes back and the scan re-forms gpt-4o in creation order.
	if _, err := apiTestDB.Pool().Exec(context.Background(), `UPDATE models SET enabled = true WHERE id = $1`, rm[1]); err != nil {
		t.Fatalf("enable model: %v", err)
	}
	if _, err := failover.NewRepository(apiTestDB.Pool()).SyncAllModels(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if p, _, _ := groupPriority(t, "gpt-4o"); !slices.Equal(p, []string{rm[0], rm[1]}) {
		t.Fatalf("re-formed gpt-4o = %v, want creation order %v", p, []string{rm[0], rm[1]})
	}

	// Front Desk sees the mismatch and imports again: the echo is back, the
	// member-only groups survive, and the hashes meet.
	if rec := doImport(t, r, env, ""); rec.Code != http.StatusOK {
		t.Fatalf("re-import status = %d, body %s", rec.Code, rec.Body.String())
	}
	if got, _ := json.Marshal(doExport(t, r).Config.FailoverGroups); !bytes.Equal(got, want) {
		t.Errorf("after re-import, failover groups = %s\nwant the primary's list verbatim: %s", got, want)
	}
	if v := doVersion(t, r); v != primaryVersion {
		t.Errorf("after re-import, version = %s, want the primary's %s", v, primaryVersion)
	}
	for _, name := range []string{"gpt-5", "o3"} {
		if p, _, auto := groupPriority(t, name); len(p) != 2 || !auto {
			t.Errorf("member-only auto group %s = %v auto=%v, want kept intact", name, p, auto)
		}
	}
	if p, _, _ := groupPriority(t, "gpt-4o"); !slices.Equal(p, []string{rm[1], rm[0]}) {
		t.Errorf("re-imported gpt-4o = %v, want the primary's order %v", p, []string{rm[1], rm[0]})
	}

	setFleetPrimaryMarker(t, true)
	asPrimary := doExport(t, r).Config.FailoverGroups
	if names := groupNames(asPrimary); !slices.Equal(names, []string{"gpt-4o", "gpt-5", "mine", "o3"}) {
		t.Fatalf("as the primary, groups = %v, want its own rows", names)
	}
}

// groupNames lists exported groups by display_model, in export order.
func groupNames(groups []ExportFailoverGroup) []string {
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.DisplayModel)
	}
	return out
}

// The echo is rewritten by every import that carries the field and left alone by
// one that omits it, mirroring how apply treats the groups themselves.
func TestConfigSync_AutoGroupEchoFollowsTheEnvelope(t *testing.T) {
	cleanConfigTables(t)
	openai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	azure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	m := seedSharedModel(t, "gpt-4o", openai, azure)
	seedFailoverGroup(t, "gpt-4o", m, nil, true)
	env := doExport(t, newConfigSyncRouter(t, configSyncMasterKey))

	cleanConfigTables(t)
	rOpenai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	rAzure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	seedSharedModel(t, "gpt-4o", rOpenai, rAzure)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	if rec := doImport(t, r, env, ""); rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	setFleetPrimaryMarker(t, false)

	absent := env
	absent.Config.FailoverGroups = nil
	if rec := doImport(t, r, absent, ""); rec.Code != http.StatusOK {
		t.Fatalf("import (absent) status = %d, body %s", rec.Code, rec.Body.String())
	}
	if got := doExport(t, r).Config.FailoverGroups; len(got) != 1 || !got[0].AutoCreated {
		t.Fatalf("after an envelope without the field, groups = %+v, want the echo kept", got)
	}

	empty := env
	empty.Config.FailoverGroups = []ExportFailoverGroup{}
	if rec := doImport(t, r, empty, ""); rec.Code != http.StatusOK {
		t.Fatalf("import (empty) status = %d, body %s", rec.Code, rec.Body.String())
	}
	if got := doExport(t, r).Config.FailoverGroups; len(got) != 0 {
		t.Fatalf("after an empty list, groups = %+v, want the echo emptied too", got)
	}
}

// A corrupt echo marker must not take the member's export down with it: the
// export falls back to the member's own rows and the next import rewrites it.
func TestConfigSync_CorruptAutoGroupEchoFallsBackToOwnRows(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	openai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	azure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	seedFailoverGroup(t, "gpt-4o", seedSharedModel(t, "gpt-4o", openai, azure), nil, true)
	if _, err := apiTestDB.Pool().Exec(context.Background(),
		`INSERT INTO settings (key, value, updated_at) VALUES ($1, 'not json', now())`,
		keyFleetAutoFailoverGroups); err != nil {
		t.Fatalf("seed marker: %v", err)
	}
	setFleetPrimaryMarker(t, false)
	if got := doExport(t, r).Config.FailoverGroups; len(got) != 1 || got[0].DisplayModel != "gpt-4o" {
		t.Fatalf("groups = %+v, want the member's own auto group", got)
	}
}

// Read failures on the export path surface as errors rather than as a shorter
// list: a member whose export silently dropped its groups would hash as one that
// holds none, and be re-synced to that. Both reads exportFailoverGroups makes
// after its Query returned are driven to fail through the one-connection seam.
func TestConfigSync_ExportFailoverGroupsReadFailures(t *testing.T) {
	cleanConfigTables(t)
	openai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	azure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	seedFailoverGroup(t, "gpt-4o", seedSharedModel(t, "gpt-4o", openai, azure), nil, true)
	ctx := context.Background()

	// rows.Err(): warm the prepared statement, then lock the groups table.
	pool, lockGroups := lockedReadDB(t, "model_failover_groups")
	if _, err := exportFailoverGroups(ctx, pool.Pool(), map[string]ExportModelRef{}); err != nil {
		t.Fatalf("warm-up: %v", err)
	}
	unlock := lockGroups()
	_, err := exportFailoverGroups(ctx, pool.Pool(), map[string]ExportModelRef{})
	unlock()
	if err == nil {
		t.Fatal("locked groups table: want an error from rows.Err(), got none")
	}

	// The echo marker read: lock settings while the groups table is free.
	_, lockSettings := lockedReadDB(t, "settings")
	unlock = lockSettings()
	_, err = exportFailoverGroups(ctx, pool.Pool(), map[string]ExportModelRef{})
	unlock()
	if err == nil {
		t.Fatal("locked settings table: want the marker read to fail the export, got none")
	}
}

// The echo commits with the rows it certifies: when its write fails, the group
// rows from the same import are rolled back too, so rows and echo can never come
// from different imports.
func TestConfigSync_AutoGroupEchoCommitsWithTheGroups(t *testing.T) {
	cleanConfigTables(t)
	openai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	azure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	seedSharedModel(t, "gpt-4o", openai, azure)
	h := NewConfigSyncHandler(apiTestDB, settings.NewRepository(apiTestDB.Pool()), configSyncMasterKey, "v-test", nil, nil)
	_, lockSettings := lockedReadDB(t, "settings")

	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	unlock := lockSettings()
	_, err := h.applyFailoverGroups(ctx, []ExportFailoverGroup{{
		DisplayModel: "mine", GroupEnabled: true,
		Entries: []ExportFailoverEntry{{ProviderName: "openai", ModelID: "gpt-4o", Enabled: true}, {ProviderName: "azure", ModelID: "gpt-4o", Enabled: true}},
	}}, true, nil)
	unlock()
	if err == nil {
		t.Fatal("apply with the settings table locked: want an error, got none")
	}
	var n int
	_ = apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM model_failover_groups WHERE display_model = 'mine'`).Scan(&n)
	if n != 0 {
		t.Error("group row committed although the echo write failed")
	}
}

// A member custom group that carries the name of a primary auto group is not kept
// by that name: display_model is unique per instance, and the echo already
// exports the name as the primary's auto group. Here the auto group is short on
// the member, so the row is deleted rather than converted.
func TestConfigSync_ImportDropsMemberCustomGroupNamedLikeAPrimaryAutoGroup(t *testing.T) {
	cleanConfigTables(t)
	openai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	azure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	seedFailoverGroup(t, "gpt-4o", seedSharedModel(t, "gpt-4o", openai, azure), nil, true)
	env := doExport(t, newConfigSyncRouter(t, configSyncMasterKey))

	cleanConfigTables(t)
	rOpenai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	rAzure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	seedModel(t, rOpenai, "gpt-4o") // azure does not list it here: the auto group is short
	seedFailoverGroup(t, "gpt-4o", seedSharedModel(t, "o3", rOpenai, rAzure), nil, false)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	if rec := doImport(t, r, env, ""); rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	var n int
	_ = apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM model_failover_groups WHERE display_model = 'gpt-4o'`).Scan(&n)
	if n != 0 {
		t.Error("member custom group named like the primary's auto group must be removed")
	}
	setFleetPrimaryMarker(t, false)
	got := doExport(t, r).Config.FailoverGroups
	if len(got) != 1 || got[0].DisplayModel != "gpt-4o" || !got[0].AutoCreated {
		t.Fatalf("export = %+v, want exactly the echoed auto group", got)
	}
}

// The dry-run diff never reports an auto group as removed: apply never deletes
// one, and a removal that will not happen would mislead the operator.
func TestConfigSync_DiffNeverReportsAnAutoGroupRemoved(t *testing.T) {
	cleanConfigTables(t)
	seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	env := doExport(t, newConfigSyncRouter(t, configSyncMasterKey))
	env.Config.FailoverGroups = []ExportFailoverGroup{}

	cleanConfigTables(t)
	rOpenai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	rAzure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	seedFailoverGroup(t, "gpt-4o", seedSharedModel(t, "gpt-4o", rOpenai, rAzure), nil, true)
	seedFailoverGroup(t, "stale", seedSharedModel(t, "o3", rOpenai, rAzure), nil, false)
	rec := doImport(t, newConfigSyncRouter(t, configSyncMasterKey), env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	var resp importResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if !slices.Equal(resp.Diff.FailoverGroups.Removed, []string{"stale"}) {
		t.Errorf("removed = %v, want only the stale custom group", resp.Diff.FailoverGroups.Removed)
	}
	if p, _, auto := groupPriority(t, "gpt-4o"); len(p) != 2 || !auto {
		t.Errorf("auto group after an empty list = %v auto=%v, want untouched", p, auto)
	}
}

// An entry only this member's discovery knows (a provider listing the model for
// this key alone) survives the import after the primary's entries, toggle and all:
// the order discovery would produce itself, so the next scan changes nothing.
func TestConfigSync_ImportKeepsMemberOnlyAutoEntries(t *testing.T) {
	cleanConfigTables(t)
	openai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	azure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	seedProvider(t, "groq", "sk-secret", configSyncMasterKey)
	m := seedSharedModel(t, "gpt-4o", openai, azure)
	seedFailoverGroup(t, "gpt-4o", []string{m[1], m[0]}, nil, true)
	env := doExport(t, newConfigSyncRouter(t, configSyncMasterKey))

	cleanConfigTables(t)
	rOpenai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	rAzure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	rGroq := seedProvider(t, "groq", "sk-secret", configSyncMasterKey)
	rm := seedSharedModel(t, "gpt-4o", rOpenai, rAzure, rGroq)
	seedFailoverGroup(t, "gpt-4o", rm, map[string]bool{rm[2]: false}, true)
	rec := doImport(t, newConfigSyncRouter(t, configSyncMasterKey), env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	var resp importResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Partial) != 0 {
		t.Errorf("partial = %v, want none for an auto group", resp.Partial)
	}
	priority, entry, _ := groupPriority(t, "gpt-4o")
	if want := []string{rm[1], rm[0], rm[2]}; !slices.Equal(priority, want) {
		t.Fatalf("priority = %v, want the primary's order then the member-only entry: %v", priority, want)
	}
	if v, ok := entry[rm[2]]; !ok || v {
		t.Errorf("entry_enabled[groq] = %v (ok=%v), want the member's toggle kept (false)", v, ok)
	}
	if _, err := failover.NewRepository(apiTestDB.Pool()).SyncAllModels(context.Background()); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if after, _, _ := groupPriority(t, "gpt-4o"); !slices.Equal(after, priority) {
		t.Errorf("a scan after the import changed the order: %v -> %v", priority, after)
	}
	var echo int
	_ = apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM settings WHERE key = $1`, keyFleetAutoFailoverGroups).Scan(&echo)
	if echo != 1 {
		t.Error("the scan after the import must leave the echo in place: nothing it found was news")
	}
}

// Auto groups count enabled models only, as discovery does; a disabled one would
// be pruned by the next scan, which would drop the echo and re-import forever.
// Custom groups keep resolving disabled models (the router skips them, the UI
// greys them).
func TestConfigSync_ImportSkipsDisabledModelsInAutoGroupsOnly(t *testing.T) {
	cleanConfigTables(t)
	openai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	azure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	m := seedSharedModel(t, "gpt-4o", openai, azure)
	seedFailoverGroup(t, "gpt-4o", m, nil, true)
	seedFailoverGroup(t, "mine", m, nil, false)
	env := doExport(t, newConfigSyncRouter(t, configSyncMasterKey))

	cleanConfigTables(t)
	rOpenai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	rAzure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	rm := seedSharedModel(t, "gpt-4o", rOpenai, rAzure)
	if _, err := apiTestDB.Pool().Exec(context.Background(), `UPDATE models SET enabled = false WHERE id = $1`, rm[1]); err != nil {
		t.Fatalf("disable model: %v", err)
	}
	rec := doImport(t, newConfigSyncRouter(t, configSyncMasterKey), env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	var n int
	_ = apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT count(*) FROM model_failover_groups WHERE display_model = 'gpt-4o'`).Scan(&n)
	if n != 0 {
		t.Error("auto group with one enabled model must not be built")
	}
	if p, _, _ := groupPriority(t, "mine"); len(p) != 2 {
		t.Errorf("custom group = %v, want both models, disabled one included", p)
	}
}

// An operator editing or deleting an auto group on a member drops the echo, so
// the member's export shows the change and Front Desk syncs the primary's back.
func TestFailoverGroup_EditOrDeleteOfAnAutoGroupDropsTheFleetEcho(t *testing.T) {
	cleanConfigTables(t)
	h := newIntegrationFailoverHandler()
	openai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	azure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	m := seedSharedModel(t, "gpt-4o", openai, azure)
	seedFailoverGroup(t, "gpt-4o", m, nil, true)
	seedEcho := func() {
		if _, err := apiTestDB.Pool().Exec(context.Background(),
			`INSERT INTO settings (key, value, updated_at) VALUES ($1, '[]', now())
			 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, keyFleetAutoFailoverGroups); err != nil {
			t.Fatalf("seed echo: %v", err)
		}
	}
	echoRows := func() int {
		var n int
		_ = apiTestDB.Pool().QueryRow(context.Background(),
			`SELECT count(*) FROM settings WHERE key = $1`, keyFleetAutoFailoverGroups).Scan(&n)
		return n
	}
	// By SQL, not the repository: the process-wide group cache may still hold a
	// "gpt-4o" from an earlier test with a different id.
	var groupID string
	if err := apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT id FROM model_failover_groups WHERE display_model = 'gpt-4o'`).Scan(&groupID); err != nil {
		t.Fatalf("get group: %v", err)
	}
	failover.InvalidateFailoverCache()

	seedEcho()
	req, w := newChiRequest(http.MethodPut, "/failover-groups/"+groupID,
		strings.NewReader(`{"priority_order":["`+m[1]+`","`+m[0]+`"],"entry_enabled":{"`+m[0]+`":true,"`+m[1]+`":true}}`))
	req = setChiURLParam(req, "id", groupID)
	h.Update(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update status = %d body = %s", w.Code, w.Body.String())
	}
	if echoRows() != 0 {
		t.Error("reordering an auto group must drop the fleet echo")
	}

	seedEcho()
	req, w = newChiRequest(http.MethodDelete, "/failover-groups/"+groupID, http.NoBody)
	req = setChiURLParam(req, "id", groupID)
	h.Delete(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d body = %s", w.Code, w.Body.String())
	}
	if echoRows() != 0 {
		t.Error("deleting an auto group must drop the fleet echo")
	}

	// A custom group is not the echo's business: editing one leaves it alone.
	seedFailoverGroup(t, "mine", m, nil, false)
	if err := apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT id FROM model_failover_groups WHERE display_model = 'mine'`).Scan(&groupID); err != nil {
		t.Fatalf("get custom group: %v", err)
	}
	failover.InvalidateFailoverCache()
	seedEcho()
	req, w = newChiRequest(http.MethodPut, "/failover-groups/"+groupID,
		strings.NewReader(`{"priority_order":["`+m[1]+`","`+m[0]+`"],"entry_enabled":{"`+m[0]+`":true,"`+m[1]+`":true}}`))
	req = setChiURLParam(req, "id", groupID)
	h.Update(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("update custom status = %d body = %s", w.Code, w.Body.String())
	}
	if echoRows() != 1 {
		t.Error("editing a custom group must leave the fleet echo alone")
	}
	req, w = newChiRequest(http.MethodDelete, "/failover-groups/"+groupID, http.NoBody)
	req = setChiURLParam(req, "id", groupID)
	h.Delete(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete custom status = %d body = %s", w.Code, w.Body.String())
	}
	if echoRows() != 1 {
		t.Error("deleting a custom group must leave the fleet echo alone")
	}
}

// A member auto row whose stored order is not a JSON array is overwritten by the
// primary's entries: discovery cannot read such a row either, so the import is
// the only repair it gets, and failing on it would re-import forever.
func TestConfigSync_ImportOverwritesCorruptMemberAutoGroup(t *testing.T) {
	cleanConfigTables(t)
	openai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	azure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	seedFailoverGroup(t, "gpt-4o", seedSharedModel(t, "gpt-4o", openai, azure), nil, true)
	env := doExport(t, newConfigSyncRouter(t, configSyncMasterKey))

	cleanConfigTables(t)
	rOpenai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	rAzure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	rm := seedSharedModel(t, "gpt-4o", rOpenai, rAzure)
	if _, err := apiTestDB.Pool().Exec(context.Background(),
		`INSERT INTO model_failover_groups (display_model, priority_order, entry_enabled, group_enabled, auto_created)
		 VALUES ('gpt-4o', '"nope"'::jsonb, '{}'::jsonb, true, true)`); err != nil {
		t.Fatalf("seed corrupt row: %v", err)
	}
	rec := doImport(t, newConfigSyncRouter(t, configSyncMasterKey), env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	var resp importResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Incomplete {
		t.Errorf("resp = %+v, want a complete import", resp)
	}
	if priority, _, _ := groupPriority(t, "gpt-4o"); !slices.Equal(priority, rm) {
		t.Errorf("priority = %v, want the primary's entries %v over the corrupt row", priority, rm)
	}
}

// An import whose post-import discovery failed withholds the echo: the auto
// groups it could not resolve were skipped without a report, and an echo would
// certify that gap. The member's own rows stay in its hash, so Front Desk keeps
// it amber and the re-push reruns discovery.
func TestConfigSync_ImportWithFailedDiscoveryWithholdsTheEcho(t *testing.T) {
	cleanConfigTables(t)
	openai := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	azure := seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	seedFailoverGroup(t, "gpt-4o", seedSharedModel(t, "gpt-4o", openai, azure), nil, true)
	env := doExport(t, newConfigSyncRouter(t, configSyncMasterKey))

	cleanConfigTables(t)
	seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	seedProvider(t, "azure", "sk-secret", configSyncMasterKey)
	failing := func(context.Context) error { return errors.New("provider timed out") }
	r := newConfigSyncRouterWithDiscovery(t, configSyncMasterKey, failing)
	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	setFleetPrimaryMarker(t, false)
	if got := doExport(t, r).Config.FailoverGroups; len(got) != 0 {
		t.Fatalf("export after a failed discovery = %+v, want the member's own (empty) rows, not an echo", got)
	}
}
