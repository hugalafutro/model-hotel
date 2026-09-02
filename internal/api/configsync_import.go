package api

import (
	"context"
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
	var env ConfigEnvelope
	if !decodeJSONLimit(w, r, maxConfigImportBody, &env) {
		return
	}
	if env.SchemaVersion != configSchemaVersion {
		writeJSONStatus(w, http.StatusUnprocessableEntity, importResponse{SchemaVersionOK: false})
		return
	}
	if len(env.Config.Providers) == 0 && len(env.Config.VirtualKeys) == 0 &&
		len(env.Config.Settings) == 0 {
		// Structural guard: an envelope with no providers, no virtual keys, and no
		// syncable settings has nothing to sync (a bare users or failover-groups list
		// references a data plane that is not here). Applying it would only run the
		// declarative deletes and wipe the member. This is the "obvious mistake"
		// rail; the rail that cannot be dressed around is errWouldWipeProviders in
		// apply(), which refuses any envelope whose empty provider list would delete
		// providers this member has.
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
		// A dry run is read-only and never fenced: Front Desk uses it to preview the
		// diff before deciding to snapshot and import.
		writeJSON(w, importResponse{SchemaVersionOK: true, MasterKeyOK: true, Applied: false, Diff: diff})
		return
	}

	// Commit fence: a real import may carry Front Desk's monotonic source
	// generation. apply rejects one older than this member last applied and
	// advances the marker atomically with the write, so an out-of-order push cannot
	// clobber a newer config. An absent header leaves sourceGen nil and the import
	// applies unfenced.
	sourceGen := parseSourceGen(r.Header.Get(fleetSourceGenHeader))
	out, err := h.apply(ctx, env, sourceGen)
	switch {
	case errors.Is(err, errStaleSourceGen):
		// A newer generation already won on this member (or an un-versioned push
		// arrived after one had). Reported as a non-applied, non-error outcome so
		// Front Desk does not surface a failure.
		debuglog.Debug("configsync: refused stale import", "source_gen", sourceGenLabel(sourceGen))
		writeJSON(w, importResponse{SchemaVersionOK: true, MasterKeyOK: true, Applied: false, Stale: true, Diff: diff})
		return
	case errors.Is(err, errWouldWipeProviders):
		// The envelope carries no providers but this member has some: applying it
		// would delete every provider. A 400 so the caller sees a deliberate
		// rejection, not a server error.
		debuglog.Warn("configsync: refused provider-wiping import")
		http.Error(w, "refusing to import a config that would delete every provider on this member", http.StatusBadRequest)
		return
	case errors.Is(err, errInvalidSyncedURL):
		// A syncable url-typed setting failed the same netguard validation the
		// interactive settings endpoint enforces. A legitimate primary never exports
		// such a value, so a 400 surfaces the poisoned or corrupt envelope to Front
		// Desk rather than applying it.
		debuglog.Warn("configsync: refused import with invalid URL setting", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, errInvalidSyncedSettingBound):
		// A syncable numeric setting arrived below the minimum the interactive
		// settings endpoint enforces. The limiter floors are the dangerous ones: a
		// negative rate_limit_ip_burst denies every request from every client IP.
		debuglog.Warn("configsync: refused import with out-of-range setting", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, errInvalidSyncedPasswordHash):
		// A user in the envelope carries a password_hash login could never verify. A
		// legitimate primary only exports hashes it computed, so the envelope is
		// refused rather than writing an account that cannot log in.
		debuglog.Warn("configsync: refused import with a malformed password hash", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, errInvalidSyncedProvider):
		// A provider in the envelope carries a value the interactive API rejects (a
		// max_in_flight the runtime would read as "no ceiling"). A legitimate primary
		// never exports one, so the whole envelope is refused rather than stored and
		// shipped on to every member.
		debuglog.Warn("configsync: refused import with an invalid provider", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, errInvalidSyncedRateLimit):
		// A virtual key or user in the envelope carries a rate limit the interactive
		// API rejects: a negative TPM imports as "no cap" and a negative burst
		// rejects every request on that key. A legitimate primary never exports one,
		// so the whole envelope is refused.
		debuglog.Warn("configsync: refused import with invalid rate limit", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	case errors.Is(err, errUnresolvableUserProviders):
		// A capped account naming providers absent here would import as unrestricted,
		// so the envelope is refused rather than the account widened. Only the
		// non-empty-but-unresolvable case reaches this: after the declarative
		// provider replace every name a legitimate primary exported resolves, so it
		// means a corrupt or tampered envelope. A cap the primary itself resolves to
		// nothing rides through as an empty array instead.
		debuglog.Warn("configsync: refused import with an unresolvable user provider cap", "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	case err != nil:
		debuglog.Error("configsync: apply import", "error", err)
		http.Error(w, "could not apply config", http.StatusInternalServerError)
		return
	}

	writeJSON(w, importResponse{
		SchemaVersionOK: true, MasterKeyOK: true, Applied: true, Diff: diff,
		Incomplete: out.incomplete(), Unapplied: out.SkippedGroups, Partial: out.PartialGroups,
		UnappliedModels: out.UnappliedModels, ModelStateFailed: out.ModelStateErr != nil,
	})
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

// diffKeyed classifies items against the member's current rows: a key present on
// both sides is updated, a new key added, and a current row whose key no item
// carries removed. keyLabel returns an item's identity key and the label the diff
// reports (virtual keys are keyed by hash but reported by name).
// includeRemovals=false suppresses the removed bucket, for envelope fields whose
// nil form the apply side leaves untouched.
func diffKeyed[T any](cur map[string]string, items []T, keyLabel func(T) (key, label string), includeRemovals bool) entityDiff {
	var d entityDiff
	want := make(map[string]struct{}, len(items))
	for _, it := range items {
		key, label := keyLabel(it)
		want[key] = struct{}{}
		if _, ok := cur[key]; ok {
			d.Updated = append(d.Updated, label)
		} else {
			d.Added = append(d.Added, label)
		}
	}
	if !includeRemovals {
		return d
	}
	for key, label := range cur {
		if _, ok := want[key]; !ok {
			d.Removed = append(d.Removed, label)
		}
	}
	return d
}

// identLabels widens a name set to the key->label form diffKeyed takes, with
// each name labelling itself.
func identLabels(set map[string]struct{}) map[string]string {
	out := make(map[string]string, len(set))
	for k := range set {
		out[k] = k
	}
	return out
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
	d.Providers = diffKeyed(identLabels(curProviders), env.Config.Providers,
		func(p ExportProvider) (string, string) { return p.Name, p.Name }, true)

	curVKs, err := hashToName(ctx, pool, `SELECT key_hash, name FROM virtual_keys`)
	if err != nil {
		return d, err
	}
	d.VirtualKeys = diffKeyed(curVKs, env.Config.VirtualKeys,
		func(v ExportVK) (string, string) { return v.KeyHash, v.Name }, true)

	curSettings, err := nameSet(ctx, pool, `SELECT key FROM settings`)
	if err != nil {
		return d, err
	}
	// Only syncable keys participate on either side. A syncable setting present
	// here but not on the primary is removed (the replica falls back to the
	// built-in default), mirroring providers/VKs.
	syncableWant := make([]string, 0, len(env.Config.Settings))
	for k := range env.Config.Settings {
		if isSyncableSetting(k) {
			syncableWant = append(syncableWant, k)
		}
	}
	curSyncable := map[string]string{}
	for k := range curSettings {
		if isSyncableSetting(k) {
			curSyncable[k] = k
		}
	}
	d.Settings = diffKeyed(curSyncable, syncableWant,
		func(k string) (string, string) { return k, k }, true)

	// Custom failover groups, scoped to auto_created = false to match the apply
	// side (auto groups regenerate per member and are never synced). The counts
	// reflect intent: a group the importer later skips for too few resolvable
	// entries on this member still shows as added/updated here.
	//
	// Removals mirror applyFailoverGroups: a nil slice means the field was absent,
	// which apply leaves untouched, so report no removals. An explicit empty array
	// reconciles to zero, so its removals are real.
	curGroups, err := nameSet(ctx, pool, `SELECT display_model FROM model_failover_groups WHERE auto_created = false`)
	if err != nil {
		return d, err
	}
	d.FailoverGroups = diffKeyed(identLabels(curGroups), env.Config.FailoverGroups,
		func(g ExportFailoverGroup) (string, string) { return g.DisplayModel, g.DisplayModel },
		env.Config.FailoverGroups != nil)

	// Users, keyed by username, with the same nil-guard as failover groups: a nil
	// slice means the envelope omits the field and apply leaves users alone, so
	// report no removals either.
	curUsers, err := nameSet(ctx, pool, `SELECT username FROM users`)
	if err != nil {
		return d, err
	}
	d.Users = diffKeyed(identLabels(curUsers), env.Config.Users,
		func(u ExportUser) (string, string) { return u.Username, u.Username },
		env.Config.Users != nil)

	return d, nil
}
