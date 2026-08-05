package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// modelDisableKind is how a model came to be switched off. The three are kept
// apart on purpose (migration 063) and config-sync must carry exactly one of
// them, so every test here states which it is seeding.
type modelDisableKind int

const (
	// disableByOperator is a hand-written switch-off: config, and the only kind
	// that syncs.
	disableByOperator modelDisableKind = iota
	// disableByDiscovery is the model vanishing from the provider's listing.
	disableByDiscovery
	// disableByTraffic is the proxy retiring a model the provider refused.
	disableByTraffic
)

// seedDisabledModel inserts a model switched off in the given way, and returns
// its UUID.
func seedDisabledModel(t *testing.T, providerID, modelID string, kind modelDisableKind) string {
	t.Helper()
	var (
		manual  = kind == disableByOperator
		retired any
	)
	if kind == disableByTraffic {
		retired = time.Now().UTC()
	}
	var id string
	err := apiTestDB.Pool().QueryRow(context.Background(),
		`INSERT INTO models (provider_id, model_id, enabled, disabled_manually, auto_retired_at)
		 VALUES ($1, $2, false, $3, $4) RETURNING id`,
		providerID, modelID, manual, retired).Scan(&id)
	if err != nil {
		t.Fatalf("seed disabled model %s: %v", modelID, err)
	}
	return id
}

// modelState reads the three flags that decide whether a model routes and who
// decided it.
func modelState(t *testing.T, modelUUID string) (enabled, manual bool, retired *time.Time) {
	t.Helper()
	err := apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT enabled, disabled_manually, auto_retired_at FROM models WHERE id = $1`,
		modelUUID).Scan(&enabled, &manual, &retired)
	if err != nil {
		t.Fatalf("read model state: %v", err)
	}
	return enabled, manual, retired
}

// disabledModelsEnvelope wraps a disabled-model list in an otherwise minimal
// envelope. A virtual key keeps the payload non-empty, matching ghostGroupEnvelope.
func disabledModelsEnvelope(refs []ExportModelRef) ConfigEnvelope {
	return ConfigEnvelope{
		SchemaVersion: configSchemaVersion,
		Config: ConfigPayload{
			VirtualKeys:    []ExportVK{{Name: "vk", KeyHash: "h", KeyPreview: "p"}},
			DisabledModels: refs,
		},
	}
}

// Export carries the operator's disables and nothing else. Discovery's disable
// and the proxy's traffic retirement are this member's own evidence about what
// its provider served it, so syncing them would spread one member's provider
// trouble across the fleet.
func TestConfigSync_ExportCarriesOnlyOperatorDisables(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	seedModel(t, provID, "gpt-5")                                // enabled
	seedDisabledModel(t, provID, "gpt-4o", disableByOperator)    // syncs
	seedDisabledModel(t, provID, "gpt-3.5", disableByDiscovery)  // must not
	seedDisabledModel(t, provID, "gpt-legacy", disableByTraffic) // must not

	env := doExport(t, r)

	if len(env.Config.DisabledModels) != 1 {
		t.Fatalf("disabled models = %+v, want only the operator's", env.Config.DisabledModels)
	}
	if got := env.Config.DisabledModels[0]; got.ProviderName != "openai" || got.ModelID != "gpt-4o" {
		t.Errorf("disabled model = %+v, want openai/gpt-4o", got)
	}
}

// The list is ordered by (provider, model_id), which is unique per member, so two
// members holding the same disables serialise identically and hash the same.
func TestConfigSync_ExportOrdersDisabledModels(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)

	zeta := seedProvider(t, "zeta", "sk-z", configSyncMasterKey)
	alpha := seedProvider(t, "alpha", "sk-a", configSyncMasterKey)
	seedDisabledModel(t, zeta, "m-b", disableByOperator)
	seedDisabledModel(t, alpha, "m-b", disableByOperator)
	seedDisabledModel(t, alpha, "m-a", disableByOperator)

	env := doExport(t, r)

	want := []ExportModelRef{
		{ProviderName: "alpha", ModelID: "m-a"},
		{ProviderName: "alpha", ModelID: "m-b"},
		{ProviderName: "zeta", ModelID: "m-b"},
	}
	if len(env.Config.DisabledModels) != len(want) {
		t.Fatalf("disabled models = %+v, want %+v", env.Config.DisabledModels, want)
	}
	for i, ref := range want {
		if env.Config.DisabledModels[i] != ref {
			t.Errorf("disabled model[%d] = %+v, want %+v", i, env.Config.DisabledModels[i], ref)
		}
	}
}

// A disable the primary holds is applied here as the operator's own, so nothing
// automatic can undo it later.
func TestConfigSync_ImportAppliesAnOperatorDisable(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	id := seedModel(t, provID, "gpt-4o")

	env := disabledModelsEnvelope([]ExportModelRef{{ProviderName: "openai", ModelID: "gpt-4o"}})
	// The provider must survive the declarative replace, or its models cascade away.
	env.Config.Providers = doExport(t, r).Config.Providers

	if rec := doImport(t, r, env, ""); rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}

	enabled, manual, _ := modelState(t, id)
	if enabled || !manual {
		t.Errorf("model state = enabled %v, disabled_manually %v; want off by the operator", enabled, manual)
	}
}

// A disable the primary no longer holds is lifted here, exactly as the operator's
// own re-enable would: the traffic retirement is cleared alongside, because a
// hand-written enabled flag supersedes what the proxy concluded.
func TestConfigSync_ImportLiftsADisableThePrimaryDropped(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	id := seedDisabledModel(t, provID, "gpt-4o", disableByOperator)
	if _, err := apiTestDB.Pool().Exec(context.Background(),
		`UPDATE models SET auto_retired_at = now() WHERE id = $1`, id); err != nil {
		t.Fatalf("stamp retirement: %v", err)
	}

	env := disabledModelsEnvelope([]ExportModelRef{}) // primary has none
	env.Config.Providers = doExport(t, r).Config.Providers

	if rec := doImport(t, r, env, ""); rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}

	enabled, manual, retired := modelState(t, id)
	if !enabled || manual {
		t.Errorf("model state = enabled %v, disabled_manually %v; want back on", enabled, manual)
	}
	if retired != nil {
		t.Error("auto_retired_at survived an operator re-enable; nothing automatic may outlive a hand-written flag")
	}
}

// The primary's list says nothing about what THIS member's provider served it, so
// a model discovery disabled or the proxy retired must survive an import that does
// not name it. Reviving those would put models the provider is refusing here back
// into routing on every pass.
func TestConfigSync_ImportDoesNotReviveAMembersOwnDisables(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	byDiscovery := seedDisabledModel(t, provID, "gpt-3.5", disableByDiscovery)
	byTraffic := seedDisabledModel(t, provID, "gpt-legacy", disableByTraffic)

	env := disabledModelsEnvelope([]ExportModelRef{})
	env.Config.Providers = doExport(t, r).Config.Providers

	if rec := doImport(t, r, env, ""); rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}

	if enabled, _, _ := modelState(t, byDiscovery); enabled {
		t.Error("a discovery-disabled model was revived by a sync that never mentioned it")
	}
	enabled, _, retired := modelState(t, byTraffic)
	if enabled {
		t.Error("a traffic-retired model was revived by a sync that never mentioned it")
	}
	if retired == nil {
		t.Error("the traffic retirement stamp was cleared, so the next scan would revive the model")
	}
}

// An envelope from a primary that predates this field carries no disabled_models
// key at all, which decodes to nil. That must leave this member's per-model state
// alone rather than reading as "the primary has none, switch everything on":
// during a rolling upgrade the first sync would otherwise re-enable every model
// the operator had switched off.
func TestConfigSync_ImportWithAbsentDisabledModelsChangesNothing(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	id := seedDisabledModel(t, provID, "gpt-4o", disableByOperator)

	env := disabledModelsEnvelope(nil) // field absent, not empty
	env.Config.Providers = doExport(t, r).Config.Providers

	if rec := doImport(t, r, env, ""); rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}

	if enabled, manual, _ := modelState(t, id); enabled || !manual {
		t.Errorf("model state = enabled %v, disabled_manually %v; an older primary's envelope must change neither",
			enabled, manual)
	}
}

// A disable naming a model this member does not have is reported, not failed. The
// member cannot route to a model it lacks, so nothing is mis-served; what the
// report explains is the config hash difference that will keep the member flagged
// until it discovers the model.
func TestConfigSync_ImportReportsAnUnappliedModelDisable(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	seedModel(t, provID, "gpt-5") // a different model: the named one is absent

	env := disabledModelsEnvelope([]ExportModelRef{{ProviderName: "openai", ModelID: "gpt-4o"}})
	env.Config.Providers = doExport(t, r).Config.Providers

	rec := doImport(t, r, env, "")
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
	if got.Incomplete {
		t.Error("Incomplete = true; a disable with no model to apply is not a failure to apply")
	}
	if len(got.UnappliedModels) != 1 || got.UnappliedModels[0] != "openai/gpt-4o" {
		t.Fatalf("UnappliedModels = %v, want [openai/gpt-4o]", got.UnappliedModels)
	}
}

// A clean import must omit the field entirely rather than emit null, so an older
// Front Desk sees the shape it always has.
func TestConfigSync_ImportOmitsUnappliedModelsWhenFullyApplied(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	seedModel(t, provID, "gpt-4o")

	env := disabledModelsEnvelope([]ExportModelRef{{ProviderName: "openai", ModelID: "gpt-4o"}})
	env.Config.Providers = doExport(t, r).Config.Providers

	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	if bytes.Contains(rec.Body.Bytes(), []byte(`"unapplied_models"`)) {
		t.Fatalf("a fully applied import must omit unapplied_models, got %s", rec.Body.String())
	}
}

// The whole point of syncing per-model disables: the config hash has to move with
// them. Before this, two members could report identical hashes while routing
// differently, because one had a model switched off and the other did not.
func TestConfigSync_ADisabledModelChangesTheConfigHash(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	id := seedModel(t, provID, "gpt-4o")

	before := doVersion(t, r)
	if _, err := apiTestDB.Pool().Exec(context.Background(),
		`UPDATE models SET enabled = false, disabled_manually = true WHERE id = $1`, id); err != nil {
		t.Fatalf("disable model: %v", err)
	}
	after := doVersion(t, r)

	if after == before {
		t.Fatal("the config hash did not move when a model was switched off; identical hashes would not mean identical routing")
	}

	// And a per-member disable must NOT move it: the hash is the convergence
	// criterion, so folding in evidence that legitimately differs per member would
	// leave the fleet permanently unable to agree.
	if _, err := apiTestDB.Pool().Exec(context.Background(),
		`UPDATE models SET disabled_manually = false, auto_retired_at = now() WHERE id = $1`, id); err != nil {
		t.Fatalf("retire model: %v", err)
	}
	if got := doVersion(t, r); got != before {
		t.Error("the config hash moved for a traffic retirement, which is per-member evidence, not config")
	}
}

// The reconcile runs after discovery, so a disable can land on a model the member
// only just learned about. Ordering it before discovery would leave every model on
// a fresh member enabled until the following sync pass.
func TestConfigSync_ImportRunsDiscoveryThenAppliesDisables(t *testing.T) {
	cleanConfigTables(t)
	seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	exportRouter := newConfigSyncRouter(t, configSyncMasterKey)
	env := disabledModelsEnvelope([]ExportModelRef{{ProviderName: "openai", ModelID: "gpt-4o"}})
	env.Config.Providers = doExport(t, exportRouter).Config.Providers

	// The member has no such model until "discovery" creates it mid-import.
	var discovered string
	r := newConfigSyncRouterWithDiscovery(t, configSyncMasterKey, func(context.Context) error {
		var id string
		err := apiTestDB.Pool().QueryRow(context.Background(),
			`INSERT INTO models (provider_id, model_id, enabled) VALUES ($1, $2, true) RETURNING id`,
			currentProviderID(t, "openai"), "gpt-4o").Scan(&id)
		if err != nil {
			return err
		}
		discovered = id
		return nil
	})

	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	var got importResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.UnappliedModels) != 0 {
		t.Fatalf("UnappliedModels = %v, want none: discovery created the model before the reconcile ran", got.UnappliedModels)
	}
	if discovered == "" {
		t.Fatal("the injected discovery never ran")
	}
	if enabled, manual, _ := modelState(t, discovered); enabled || !manual {
		t.Errorf("model state = enabled %v, disabled_manually %v; want the disable applied to the freshly discovered model",
			enabled, manual)
	}
}

// currentProviderID reads a provider's UUID by name, for a caller that runs after
// the import's declarative provider replace has re-created the row.
func currentProviderID(t *testing.T, name string) string {
	t.Helper()
	var id string
	if err := apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT id FROM providers WHERE name = $1`, name).Scan(&id); err != nil {
		t.Fatalf("read provider %s: %v", name, err)
	}
	return id
}

// A per-model disable reconcile that fails outright is a failure to apply, unlike
// one that merely has no model to land on: the member is left routing to models
// the primary has switched off. It must therefore report Incomplete, so Front Desk
// retries rather than recording the member as done.
//
// The failure is forced with a model_id carrying a NUL byte, which no legitimate
// primary exports and Postgres refuses outright. Any error from the reconcile takes
// the same path; what matters is that the import still commits its core config and
// still reports the shortfall.
func TestConfigSync_ImportReportsAFailedModelReconcile(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	seedProvider(t, "openai", "sk-secret", configSyncMasterKey)

	env := disabledModelsEnvelope([]ExportModelRef{{ProviderName: "openai", ModelID: "gpt\x004o"}})
	env.Config.Providers = doExport(t, r).Config.Providers

	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	var got importResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Applied {
		t.Fatal("Applied = false, want true: the core config committed before the reconcile ran")
	}
	if !got.Incomplete {
		t.Error("Incomplete = false; a reconcile that failed leaves the member routing differently from the primary")
	}
}
