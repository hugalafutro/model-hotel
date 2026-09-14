package api

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"slices"
	"strconv"

	"github.com/hugalafutro/model-hotel/internal/auth"
	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/util"
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

	// MASTER_KEY guard: prove this member can decrypt every incoming provider key
	// before writing anything. The first key failing means the fleet's keys
	// differ, which Front Desk reports as a MASTER_KEY mismatch; a later key
	// failing after a good one is a corrupt envelope, refused with its own
	// message so the operator is not sent after a mismatch that does not exist.
	// Either way, storing undecryptable ciphertext would silently break the
	// data plane.
	if mismatch, corrupt := h.undecryptableKeys(env.Config.Providers); mismatch {
		writeJSONStatus(w, http.StatusConflict, importResponse{SchemaVersionOK: true, MasterKeyOK: false})
		return
	} else if corrupt != "" {
		msg := fmt.Sprintf("configsync: refusing to import: the key of provider %q does not decrypt under this MASTER_KEY while others do", corrupt)
		debuglog.Warn("configsync: refused import", "error", msg)
		http.Error(w, msg, http.StatusBadRequest)
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
	case errors.Is(err, errWouldWipeVirtualKeys):
		// The envelope carries no virtual keys but this member has some: applying
		// it would delete every credential the member serves. Same 400 as the
		// provider rail, for the same reason.
		debuglog.Warn("configsync: refused key-wiping import")
		http.Error(w, "refusing to import a config that would delete every virtual key on this member", http.StatusBadRequest)
		return
	case errors.Is(err, errWouldWipeProviders):
		// The envelope carries no providers but this member has some: applying it
		// would delete every provider. A 400 so the caller sees a deliberate
		// rejection, not a server error.
		debuglog.Warn("configsync: refused provider-wiping import")
		http.Error(w, "refusing to import a config that would delete every provider on this member", http.StatusBadRequest)
		return
	case slices.ContainsFunc(importRejections, func(e error) bool { return errors.Is(err, e) }):
		// Every one of these means the envelope carries a value the interactive
		// API would reject, which a legitimate primary never exports. The whole
		// envelope is refused with a 400 rather than applied, and the sentinel's
		// own message names the rejection. See the declarations for what each
		// one guards against.
		// A refused base_url is quoted in the error, and a URL can carry a
		// credential in its userinfo; neither the log nor the 400 body gets it.
		msg := util.RedactURLUserinfo(err.Error())
		debuglog.Warn("configsync: refused import", "error", msg)
		http.Error(w, msg, http.StatusBadRequest)
		return
	case err != nil:
		debuglog.Error("configsync: apply import", "error", util.RedactURLUserinfo(err.Error()))
		http.Error(w, "could not apply config", http.StatusInternalServerError)
		return
	}

	writeJSON(w, importResponse{
		SchemaVersionOK: true, MasterKeyOK: true, Applied: true, Diff: diff,
		Incomplete: out.incomplete(), Unapplied: out.SkippedGroups, Partial: out.PartialGroups,
		UnappliedModels: out.UnappliedModels, ModelStateFailed: out.ModelStateErr != nil,
	})
}

// maxSourceGen bounds the generation a member will fence itself on. Front Desk
// counts auto_sync_gen up from zero in a 32-bit column, so anything past that
// range cannot have come from a real primary, and storing it would pin the
// marker where no genuine push can ever reach it again: every later generation
// reads as stale and no API call lowers the marker back.
const maxSourceGen = math.MaxInt32

// parseSourceGen reads the optional fleet source-generation header. It returns
// nil when the header is absent, unparseable, or outside the range a primary
// can produce, so a malformed value degrades to an unfenced import rather than
// rejecting a legitimate push.
func parseSourceGen(raw string) *int64 {
	if raw == "" {
		return nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		debuglog.Warn("configsync: ignoring unparseable source-generation header", "value", raw)
		return nil
	}
	if n < 0 || n > maxSourceGen {
		debuglog.Warn("configsync: ignoring out-of-range source-generation header", "value", raw)
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

// undecryptableKeys checks every encrypted provider key in the envelope against
// this member's MASTER_KEY. mismatch is set when the first keyed provider fails,
// the shape of a fleet whose members hold different keys. corrupt names the
// first provider that fails after another decrypted: one good decrypt proves
// the shared key but not the rest of the envelope, and a payload mixing one
// sound key with corrupt ciphertext used to pass on the first and land
// undecryptable rows that failed at request time and rode this member's own
// export onward. A keyless envelope has nothing to verify.
func (h *ConfigSyncHandler) undecryptableKeys(providers []ExportProvider) (mismatch bool, corrupt string) {
	checked := 0
	for _, p := range providers {
		if len(p.EncryptedKey) == 0 {
			continue
		}
		if _, err := auth.Decrypt(p.EncryptedKey, p.KeyNonce, p.KeySalt, h.masterKey); err != nil {
			if checked == 0 {
				return true, ""
			}
			return false, p.Name
		}
		checked++
	}
	return false, ""
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

	curVKs, err := stringMap(ctx, pool, `SELECT key_hash, name FROM virtual_keys`)
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
