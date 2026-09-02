package api

import (
	"net/http"
	"slices"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/util"
)

// An import seeds the credential mask's held set with every imported key: a
// fleet member receives providers it has never decrypted, and the seed runs
// before the cache invalidation that makes the new rows routable.
func TestConfigSync_ImportHoldsEveryProviderKey(t *testing.T) {
	const key = "custom-key-imported-row-0011223344"
	cleanConfigTables(t)
	r := newConfigSyncRouter(t, configSyncMasterKey)
	seedProvider(t, "prov-held", key, configSyncMasterKey)
	env := doExport(t, r)
	if slices.Contains(util.HeldSecrets(), key) {
		t.Fatal("key held before any import or create; the export's sample decrypt must not register")
	}
	cleanConfigTables(t)
	rec := doImport(t, r, env, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("import status = %d; body %s", rec.Code, rec.Body.String())
	}
	if !slices.Contains(util.HeldSecrets(), key) {
		t.Fatal("imported provider key not held for the credential mask")
	}
}
