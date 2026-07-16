package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// ---------------------------------------------------------------------------
// Import
// ---------------------------------------------------------------------------

// Import applies an envelope onto this member. With ?dryRun=1 it returns the diff
// without writing. Otherwise it converges this member to the envelope inside a
// single transaction: all-or-nothing.
func (h *ConfigSyncHandler) Import(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	r.Body = http.MaxBytesReader(w, r.Body, maxConfigImportBody)

	var env ConfigEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}
	if env.SchemaVersion != configSchemaVersion {
		writeJSONStatus(w, http.StatusUnprocessableEntity, importResponse{SchemaVersionOK: false})
		return
	}
	if len(env.Config.Providers) == 0 && len(env.Config.VirtualKeys) == 0 &&
		len(env.Config.Settings) == 0 {
		// Structural guard: an envelope with no providers, no virtual keys, and no
		// syncable settings has nothing meaningful to sync (a bare users or
		// failover-groups list cannot stand on its own, since those reference a
		// data plane that isn't here). Applying it would only run the declarative
		// deletes and wipe the member, so refuse rather than write. This is the
		// "obvious mistake" rail; the destructive-wipe rail that can't be dressed
		// around lives in apply() (errWouldWipeProviders), which refuses any
		// envelope whose empty provider list would delete providers this member
		// actually has, whatever settings/keys/users decorate it.
		http.Error(w, "refusing to import an empty config", http.StatusBadRequest)
		return
	}

	// MASTER_KEY guard: prove this member can decrypt an incoming provider key
	// before writing anything. A mismatch means the fleet's keys differ; storing
	// undecryptable ciphertext would silently break the data plane.
	if !h.canDecryptSample(env.Config.Providers) {
		writeJSONStatus(w, http.StatusConflict, importResponse{SchemaVersionOK: true, MasterKeyOK: false})
		return
	}

	diff, err := h.computeDiff(ctx, env)
	if err != nil {
		debuglog.Error("configsync: compute diff", "error", err)
		http.Error(w, "could not read current config", http.StatusInternalServerError)
		return
	}

	if r.URL.Query().Get("dryRun") != "" {
		// A dry run is read-only and is never fenced: Front Desk relies on it to
		// preview the diff before deciding to snapshot and import.
		writeJSON(w, importResponse{SchemaVersionOK: true, MasterKeyOK: true, Applied: false, Diff: diff})
		return
	}

	// Commit fence: a real import may carry Front Desk's monotonic source
	// generation. apply rejects one older than this member last applied, and
	// advances the marker atomically with the write, so an out-of-order push
	// cannot clobber a newer config. The header is absent for an older Front
	// Desk, in which case sourceGen is nil and the import applies unfenced.
	sourceGen := parseSourceGen(r.Header.Get(fleetSourceGenHeader))
	switch err := h.apply(ctx, env, sourceGen); {
	case errors.Is(err, errStaleSourceGen):
		// Benign: a newer generation already won on this member (or an un-versioned
		// push arrived after one had). Report it as a non-applied, non-error outcome
		// so Front Desk does not surface a failure.
		debuglog.Debug("configsync: refused stale import", "source_gen", sourceGenLabel(sourceGen))
		writeJSON(w, importResponse{SchemaVersionOK: true, MasterKeyOK: true, Applied: false, Stale: true, Diff: diff})
		return
	case errors.Is(err, errWouldWipeProviders):
		// The envelope carries no providers but this member has some: applying it
		// would delete every provider (the reported backdoor-wipe vector). Refuse
		// with a 400 so the caller sees a deliberate rejection, not a server error.
		debuglog.Warn("configsync: refused provider-wiping import")
		http.Error(w, "refusing to import a config that would delete every provider on this member", http.StatusBadRequest)
		return
	case err != nil:
		debuglog.Error("configsync: apply import", "error", err)
		http.Error(w, "could not apply config", http.StatusInternalServerError)
		return
	}

	writeJSON(w, importResponse{SchemaVersionOK: true, MasterKeyOK: true, Applied: true, Diff: diff})
}

// parseSourceGen reads the optional fleet source-generation header. It returns
// nil when the header is absent or unparseable, so a malformed or missing value
// degrades to an unfenced import rather than rejecting a legitimate push.
func parseSourceGen(raw string) *int64 {
	if raw == "" {
		return nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		debuglog.Warn("configsync: ignoring unparseable source-generation header", "value", raw)
		return nil
	}
	return &n
}

// sourceGenLabel renders an optional source generation for logs without
// dereferencing a nil (a headerless import has none).
func sourceGenLabel(gen *int64) string {
	if gen == nil {
		return "none"
	}
	return strconv.FormatInt(*gen, 10)
}

// canDecryptSample returns true when there is no encrypted key to check, or the
// first one decrypts under this member's MASTER_KEY.
func (h *ConfigSyncHandler) canDecryptSample(providers []ExportProvider) bool {
	for _, p := range providers {
		if len(p.EncryptedKey) == 0 {
			continue
		}
		if _, err := auth.Decrypt(p.EncryptedKey, p.KeyNonce, p.KeySalt, h.masterKey); err != nil {
			return false
		}
		return true // one good decrypt proves the shared key
	}
	return true // keyless fleet: nothing to verify
}

// computeDiff classifies each entity as added (new to this member), updated
// (present on both), or removed (here but not in the envelope).
func (h *ConfigSyncHandler) computeDiff(ctx context.Context, env ConfigEnvelope) (configDiff, error) {
	pool := h.db.Pool()
	var d configDiff

	curProviders, err := nameSet(ctx, pool, `SELECT name FROM providers`)
	if err != nil {
		return d, err
	}
	wantProviders := map[string]struct{}{}
	for _, p := range env.Config.Providers {
		wantProviders[p.Name] = struct{}{}
		if _, ok := curProviders[p.Name]; ok {
			d.Providers.Updated = append(d.Providers.Updated, p.Name)
		} else {
			d.Providers.Added = append(d.Providers.Added, p.Name)
		}
	}
	for name := range curProviders {
		if _, ok := wantProviders[name]; !ok {
			d.Providers.Removed = append(d.Providers.Removed, name)
		}
	}

	curVKs, err := hashToName(ctx, pool, `SELECT key_hash, name FROM virtual_keys`)
	if err != nil {
		return d, err
	}
	wantVKs := map[string]struct{}{}
	for _, v := range env.Config.VirtualKeys {
		wantVKs[v.KeyHash] = struct{}{}
		if _, ok := curVKs[v.KeyHash]; ok {
			d.VirtualKeys.Updated = append(d.VirtualKeys.Updated, v.Name)
		} else {
			d.VirtualKeys.Added = append(d.VirtualKeys.Added, v.Name)
		}
	}
	for hash, name := range curVKs {
		if _, ok := wantVKs[hash]; !ok {
			d.VirtualKeys.Removed = append(d.VirtualKeys.Removed, name)
		}
	}

	curSettings, err := nameSet(ctx, pool, `SELECT key FROM settings`)
	if err != nil {
		return d, err
	}
	for k := range env.Config.Settings {
		if !isSyncableSetting(k) {
			continue
		}
		if _, ok := curSettings[k]; ok {
			d.Settings.Updated = append(d.Settings.Updated, k)
		} else {
			d.Settings.Added = append(d.Settings.Added, k)
		}
	}
	// A syncable setting present here but not on the primary is removed (the
	// replica falls back to the built-in default), mirroring providers/VKs.
	for k := range curSettings {
		if !isSyncableSetting(k) {
			continue
		}
		if _, ok := env.Config.Settings[k]; !ok {
			d.Settings.Removed = append(d.Settings.Removed, k)
		}
	}

	// Custom failover groups, scoped to auto_created = false to match the apply
	// side (auto groups regenerate per member and are never synced). The counts
	// reflect intent: a group the importer later skips for too few resolvable
	// entries on this member still shows as added/updated here.
	curGroups, err := nameSet(ctx, pool, `SELECT display_model FROM model_failover_groups WHERE auto_created = false`)
	if err != nil {
		return d, err
	}
	wantGroups := map[string]struct{}{}
	for _, g := range env.Config.FailoverGroups {
		wantGroups[g.DisplayModel] = struct{}{}
		if _, ok := curGroups[g.DisplayModel]; ok {
			d.FailoverGroups.Updated = append(d.FailoverGroups.Updated, g.DisplayModel)
		} else {
			d.FailoverGroups.Added = append(d.FailoverGroups.Added, g.DisplayModel)
		}
	}
	// Mirror applyFailoverGroups exactly: a nil slice means the field was absent (a
	// pre-PR primary), which apply leaves untouched, so report no removals here. An
	// explicit empty array reconciles to zero, so its removals are real. Reporting
	// removals for a nil slice would scare an operator mid-rolling-upgrade with
	// deletions the apply never performs.
	if env.Config.FailoverGroups != nil {
		for name := range curGroups {
			if _, ok := wantGroups[name]; !ok {
				d.FailoverGroups.Removed = append(d.FailoverGroups.Removed, name)
			}
		}
	}

	// Users, keyed by username, with the same nil-guard as failover groups: a
	// nil slice means the envelope predates the field and apply leaves users
	// alone, so report no removals either.
	curUsers, err := nameSet(ctx, pool, `SELECT username FROM users`)
	if err != nil {
		return d, err
	}
	wantUsers := map[string]struct{}{}
	for _, u := range env.Config.Users {
		wantUsers[u.Username] = struct{}{}
		if _, ok := curUsers[u.Username]; ok {
			d.Users.Updated = append(d.Users.Updated, u.Username)
		} else {
			d.Users.Added = append(d.Users.Added, u.Username)
		}
	}
	if env.Config.Users != nil {
		for name := range curUsers {
			if _, ok := wantUsers[name]; !ok {
				d.Users.Removed = append(d.Users.Removed, name)
			}
		}
	}
	return d, nil
}
