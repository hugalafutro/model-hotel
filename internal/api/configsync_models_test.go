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

// A disable landing on a model this member's proxy already retired from traffic
// must not destroy that provenance. The model is switched off either way, so the
// retirement stamp has nothing to contradict, and it is this member's own evidence
// that its provider refuses the model.
//
// Losing it is not cosmetic: on a later re-enable at the primary, the enable pass
// would put a model the provider is still refusing back into routing here, where
// it fails until three more refusals re-retire it, alerting each cycle.
func TestConfigSync_DisableKeepsAMembersTrafficRetirement(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	id := seedDisabledModel(t, provID, "gpt-4o", disableByTraffic)

	env := disabledModelsEnvelope([]ExportModelRef{{ProviderName: "openai", ModelID: "gpt-4o"}})
	env.Config.Providers = doExport(t, r).Config.Providers

	if rec := doImport(t, r, env, ""); rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}

	enabled, manual, retired := modelState(t, id)
	if enabled || !manual {
		t.Errorf("model state = enabled %v, disabled_manually %v; the primary's disable must still apply", enabled, manual)
	}
	if retired == nil {
		t.Error("auto_retired_at was cleared by a disable, destroying this member's own evidence about its provider")
	}
}

// The same for an operator's discovery-claim dismissal on the member: a disable
// arriving from the primary must not resurrect a claim they already dismissed.
func TestConfigSync_DisableKeepsAMembersDiscoveryDismissal(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	id := seedModel(t, provID, "gpt-4o")
	if _, err := apiTestDB.Pool().Exec(context.Background(),
		`UPDATE models SET discovery_dismissed_at = now() WHERE id = $1`, id); err != nil {
		t.Fatalf("stamp dismissal: %v", err)
	}

	env := disabledModelsEnvelope([]ExportModelRef{{ProviderName: "openai", ModelID: "gpt-4o"}})
	env.Config.Providers = doExport(t, r).Config.Providers

	if rec := doImport(t, r, env, ""); rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}

	var dismissed *time.Time
	if err := apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT discovery_dismissed_at FROM models WHERE id = $1`, id).Scan(&dismissed); err != nil {
		t.Fatalf("read dismissal: %v", err)
	}
	if dismissed == nil {
		t.Error("discovery_dismissed_at was cleared by a disable, resurrecting a claim the operator dismissed here")
	}
}

// TestConfigSync_UnappliableDisableStillConverges is the whole point of
// acknowledging intent. A member that has no model for one of the primary's
// disables cannot put it in a list derived from its own rows, so before this its
// hash differed from the primary's on every pass forever: a permanent amber badge
// and a full re-import, with the member-side discovery it runs, every ten minutes
// for good. Convergence is what this fleet work is for, so the member records the
// intent it cannot apply and exports it alongside what it did apply.
func TestConfigSync_UnappliableDisableStillConverges(t *testing.T) {
	cleanConfigTables(t)
	primary := newConfigSyncRouter(t, configSyncMasterKey)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	seedModel(t, provID, "gpt-5")
	seedDisabledModel(t, provID, "gpt-4o", disableByOperator) // the member will not have this one
	primaryHash := doVersion(t, primary)
	env := doExport(t, primary)

	// The member holds only gpt-5.
	cleanConfigTables(t)
	mProvID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	seedModel(t, mProvID, "gpt-5")
	member := newConfigSyncRouter(t, configSyncMasterKey)

	rec := doImport(t, member, env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d, body %s", rec.Code, rec.Body.String())
	}
	var got importResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.UnappliedModels) != 1 || got.UnappliedModels[0] != "openai/gpt-4o" {
		t.Fatalf("UnappliedModels = %v, want [openai/gpt-4o]", got.UnappliedModels)
	}

	if h := doVersion(t, member); h != primaryHash {
		t.Errorf("member hash = %s, primary = %s; a member that cannot hold one of the primary's models must still converge",
			h[:12], primaryHash[:12])
	}
}

// TestConfigSync_AcknowledgedDisableIsAppliedOnceTheModelArrives: the
// acknowledgement is intent held open, not a way to look converged forever. Once
// discovery creates the model, the next import disables it for real and the
// acknowledgement is dropped, so the exported list still describes the member.
func TestConfigSync_AcknowledgedDisableIsAppliedOnceTheModelArrives(t *testing.T) {
	cleanConfigTables(t)
	primary := newConfigSyncRouter(t, configSyncMasterKey)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	seedModel(t, provID, "gpt-5")
	seedDisabledModel(t, provID, "gpt-4o", disableByOperator)
	env := doExport(t, primary)

	cleanConfigTables(t)
	mProvID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	seedModel(t, mProvID, "gpt-5")
	member := newConfigSyncRouter(t, configSyncMasterKey)
	if rec := doImport(t, member, env, ""); rec.Code != http.StatusOK {
		t.Fatalf("first import: %d %s", rec.Code, rec.Body.String())
	}
	if got := unappliedMarker(t); len(got) != 1 {
		t.Fatalf("acknowledged refs = %v, want exactly the one it could not apply", got)
	}

	// Discovery finds the model, then the next sync lands.
	id := seedModel(t, currentProviderID(t, "openai"), "gpt-4o")
	if rec := doImport(t, member, env, ""); rec.Code != http.StatusOK {
		t.Fatalf("second import: %d %s", rec.Code, rec.Body.String())
	}

	if enabled, manual, _ := modelState(t, id); enabled || !manual {
		t.Errorf("model state = enabled %v, manual %v; the held intent must be applied once the model exists", enabled, manual)
	}
	if got := unappliedMarker(t); len(got) != 0 {
		t.Errorf("acknowledged refs = %v, want none: the disable is applied for real now", got)
	}
}

// TestConfigSync_AcknowledgedDisableIsDroppedWhenThePrimaryReEnables: the other
// way out. The operator switching the model back on clears the acknowledgement
// too, so the member stops exporting intent that no longer exists.
func TestConfigSync_AcknowledgedDisableIsDroppedWhenThePrimaryReEnables(t *testing.T) {
	cleanConfigTables(t)
	primary := newConfigSyncRouter(t, configSyncMasterKey)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	seedModel(t, provID, "gpt-5")
	seedDisabledModel(t, provID, "gpt-4o", disableByOperator)
	disabledEnv := doExport(t, primary)

	cleanConfigTables(t)
	mProvID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	seedModel(t, mProvID, "gpt-5")
	member := newConfigSyncRouter(t, configSyncMasterKey)
	if rec := doImport(t, member, disabledEnv, ""); rec.Code != http.StatusOK {
		t.Fatalf("first import: %d %s", rec.Code, rec.Body.String())
	}
	if got := unappliedMarker(t); len(got) != 1 {
		t.Fatalf("acknowledged refs = %v, want one", got)
	}

	// The primary re-enables it: the envelope now carries no disables at all.
	reEnabled := disabledEnv
	reEnabled.Config.DisabledModels = []ExportModelRef{}
	if rec := doImport(t, member, reEnabled, ""); rec.Code != http.StatusOK {
		t.Fatalf("second import: %d %s", rec.Code, rec.Body.String())
	}

	if got := unappliedMarker(t); len(got) != 0 {
		t.Errorf("acknowledged refs = %v, want none once the primary re-enabled the model", got)
	}
	env := doExport(t, member)
	if len(env.Config.DisabledModels) != 0 {
		t.Errorf("exported disables = %v, want none", env.Config.DisabledModels)
	}
}

// unappliedMarker reads the acknowledged-intent marker this member holds.
func unappliedMarker(t *testing.T) []ExportModelRef {
	t.Helper()
	var raw string
	err := apiTestDB.Pool().QueryRow(context.Background(),
		`SELECT value FROM settings WHERE key = $1`, keyFleetUnappliedModelDisables).Scan(&raw)
	if err != nil {
		return nil
	}
	var refs []ExportModelRef
	if err := json.Unmarshal([]byte(raw), &refs); err != nil {
		t.Fatalf("unparseable marker %q: %v", raw, err)
	}
	return refs
}

// TestConfigSync_AcknowledgementStopsMaskingOnceTheModelExists: the acknowledged
// list stands in for a model this member does not have, and must stop the instant
// it does. Otherwise the export keeps claiming the disable while the model sits
// enabled here, the member's hash matches the primary's across a real routing
// difference, and nothing ever pushes the disable that would fix it. That is the
// precise failure this whole hash comparison exists to catch, so it must not be
// reintroduced by the mechanism that makes the fleet converge.
func TestConfigSync_AcknowledgementStopsMaskingOnceTheModelExists(t *testing.T) {
	cleanConfigTables(t)
	primary := newConfigSyncRouter(t, configSyncMasterKey)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	seedModel(t, provID, "gpt-5")
	seedDisabledModel(t, provID, "gpt-4o", disableByOperator)
	primaryHash := doVersion(t, primary)
	env := doExport(t, primary)

	cleanConfigTables(t)
	seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	seedModel(t, currentProviderID(t, "openai"), "gpt-5")
	member := newConfigSyncRouter(t, configSyncMasterKey)
	if rec := doImport(t, member, env, ""); rec.Code != http.StatusOK {
		t.Fatalf("import: %d %s", rec.Code, rec.Body.String())
	}
	if doVersion(t, member) != primaryHash {
		t.Fatal("member did not converge on the acknowledged disable")
	}

	// Discovery creates the model, enabled. The member now genuinely routes
	// differently from the primary, so its hash must say so.
	seedModel(t, currentProviderID(t, "openai"), "gpt-4o")

	if h := doVersion(t, member); h == primaryHash {
		t.Error("the member still hashes as converged while serving a model the primary has switched off")
	}
	env2 := doExport(t, member)
	for _, ref := range env2.Config.DisabledModels {
		if ref.ModelID == "gpt-4o" {
			t.Error("the export still claims a disable for a model that now exists here and is enabled")
		}
	}
}

// TestConfigSync_UnparseableAcknowledgementIsIgnored: the acknowledgement marker
// is instance-local state, not something an operator edits, but a corrupt one must
// not take this member's config sync down with it. It is ignored, and the next
// import rewrites it.
func TestConfigSync_UnparseableAcknowledgementIsIgnored(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	provID := seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	seedDisabledModel(t, provID, "gpt-4o", disableByOperator)
	if _, err := apiTestDB.Pool().Exec(context.Background(),
		`INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
		 ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`,
		keyFleetUnappliedModelDisables, "{not json"); err != nil {
		t.Fatalf("seed corrupt marker: %v", err)
	}

	env := doExport(t, r)

	if len(env.Config.DisabledModels) != 1 || env.Config.DisabledModels[0].ModelID != "gpt-4o" {
		t.Errorf("disabled models = %+v, want just the applied one; a corrupt marker must not break the export",
			env.Config.DisabledModels)
	}
}

// TestConfigSync_ExportFailsLoudlyWhenDisablesCannotBeRead: the export is
// all-or-nothing. A member that cannot read its own per-model state must answer
// with an error rather than a config envelope that silently omits it, which Front
// Desk would otherwise replicate onto the rest of the fleet.
func TestConfigSync_ExportFailsLoudlyWhenDisablesCannotBeRead(t *testing.T) {
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	seedProvider(t, "openai", "sk-secret", configSyncMasterKey)
	ctx := context.Background()
	if _, err := apiTestDB.Pool().Exec(ctx, `ALTER TABLE models RENAME COLUMN disabled_manually TO dm_broken`); err != nil {
		t.Fatalf("break models: %v", err)
	}
	t.Cleanup(func() {
		if _, err := apiTestDB.Pool().Exec(ctx, `ALTER TABLE models RENAME COLUMN dm_broken TO disabled_manually`); err != nil {
			t.Fatalf("restore models: %v", err)
		}
	})

	if rec := rawExport(t, r); rec.Code != http.StatusInternalServerError {
		t.Errorf("export status = %d, want 500 when the per-model state cannot be read", rec.Code)
	}
}
