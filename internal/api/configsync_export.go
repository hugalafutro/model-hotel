package api

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"slices"
	"time"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// ---------------------------------------------------------------------------
// Export
// ---------------------------------------------------------------------------

// Export returns this member's full config envelope so Front Desk can replicate
// it onto the fleet.
func (h *ConfigSyncHandler) Export(w http.ResponseWriter, r *http.Request) {
	env, err := h.buildEnvelope(r.Context())
	if err != nil {
		debuglog.Error("configsync: build export envelope", "error", err)
		http.Error(w, "could not export config", http.StatusInternalServerError)
		return
	}
	writeJSON(w, env)
}

// Version returns a stable content hash of this member's syncable config, so
// Front Desk's auto-sync poller can cheaply detect that the primary's config
// changed without pulling and diffing the full export every tick. The hash
// covers only the Config payload (providers, virtual keys, syncable settings,
// custom failover groups, users), never the volatile envelope fields
// (exported_at), so
// it changes if and only if a synced entity changed. Same auth as Export.
func (h *ConfigSyncHandler) Version(w http.ResponseWriter, r *http.Request) {
	env, err := h.buildEnvelope(r.Context())
	if err != nil {
		debuglog.Error("configsync: build version envelope", "error", err)
		http.Error(w, "could not read config", http.StatusInternalServerError)
		return
	}
	// Marshal only the Config payload. Every list is ordered by a column a unique
	// index makes total, so no two rows tie and fall back to physical row order:
	// providers by name, failover groups by display_model, users by username,
	// virtual keys by (created_at, key_hash), disabled models by (provider,
	// model_id). encoding/json key-sorts the settings map. The bytes are therefore
	// deterministic for an unchanged config, and two members holding the same config
	// hash identically.
	payload, err := json.Marshal(env.Config)
	if err != nil {
		debuglog.Error("configsync: marshal config for version", "error", err)
		http.Error(w, "could not read config", http.StatusInternalServerError)
		return
	}
	sum := sha256.Sum256(payload)
	writeJSON(w, map[string]string{"version": hex.EncodeToString(sum[:])})
}

// buildEnvelope reads this member's full config (providers with key ciphertext,
// virtual keys with provider-name-translated allow-lists, syncable settings) into
// an envelope. Any read failure aborts with the underlying error.
func (h *ConfigSyncHandler) buildEnvelope(ctx context.Context) (ConfigEnvelope, error) {
	pool := h.db.Pool()
	idToName, err := h.providerIDToName(ctx, pool)
	if err != nil {
		return ConfigEnvelope{}, err
	}
	providers, err := exportProviders(ctx, pool)
	if err != nil {
		return ConfigEnvelope{}, err
	}
	vks, err := exportVirtualKeys(ctx, pool, idToName)
	if err != nil {
		return ConfigEnvelope{}, err
	}
	set, err := exportSettings(ctx, pool)
	if err != nil {
		return ConfigEnvelope{}, err
	}
	refByUUID, err := modelRefByUUID(ctx, pool)
	if err != nil {
		return ConfigEnvelope{}, err
	}
	groups, err := exportFailoverGroups(ctx, pool, refByUUID)
	if err != nil {
		return ConfigEnvelope{}, err
	}
	users, err := exportUsers(ctx, pool, idToName)
	if err != nil {
		return ConfigEnvelope{}, err
	}
	disabled, err := exportDisabledModels(ctx, pool)
	if err != nil {
		return ConfigEnvelope{}, err
	}
	return ConfigEnvelope{
		SchemaVersion: configSchemaVersion,
		AppVersion:    h.appVersion,
		ExportedAt:    time.Now().UTC(),
		Config: ConfigPayload{
			Providers: providers, VirtualKeys: vks, Settings: set, FailoverGroups: groups,
			Users: users, DisabledModels: disabled,
		},
	}, nil
}

// exportDisabledModels lists the models the operator switched off by hand, by
// stable (provider, model_id) ref.
//
// disabled_manually is the only one of the three disable kinds that is operator
// intent. A model discovery disabled (both flags false) or the proxy retired from
// traffic (auto_retired_at set) is this member's own evidence about what its
// provider served it, and carrying either would spread one member's provider
// trouble to the whole fleet. Migration 063 has the full three-way distinction.
//
// It carries two things: the disables this member has applied to its own model
// rows, and the ones it acknowledged but has no model for
// (keyFleetUnappliedModelDisables). Both are the same operator intent, and
// exporting only the first would leave a member that lacks one of the primary's
// models unable to ever reproduce the primary's list, so the two would hash
// differently on every pass forever. The union converges instead, and costs
// nothing in routing: a member cannot serve a model it does not have.
//
// Ordered by (provider name, model_id), which is unique per member, so the list is
// total and two members holding the same disables hash identically. Deduplicated,
// because a model discovered since the acknowledgement appears in both halves
// until the next import clears it from the second.
func exportDisabledModels(ctx context.Context, q querier) ([]ExportModelRef, error) {
	rows, err := q.Query(ctx, `
		SELECT p.name, m.model_id
		FROM models m JOIN providers p ON m.provider_id = p.id
		WHERE m.disabled_manually = true`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[ExportModelRef]bool{}
	out := []ExportModelRef{}
	for rows.Next() {
		var ref ExportModelRef
		if err := rows.Scan(&ref.ProviderName, &ref.ModelID); err != nil {
			return nil, err
		}
		if !seen[ref] {
			seen[ref] = true
			out = append(out, ref)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	acked, err := readUnappliedModelDisables(ctx, q)
	if err != nil {
		return nil, err
	}
	// Only refs this member still has no model for. Once one appears the
	// acknowledgement must stop standing in for it, or the export would keep
	// claiming a disable while the model sits enabled here: the hashes would agree
	// across a real routing difference, which is the exact failure this whole
	// comparison exists to catch. Dropping it instead makes this member's hash
	// differ until a sync applies the disable for real.
	if len(acked) > 0 {
		missing, err := filterModelsAbsentHere(ctx, q, acked)
		if err != nil {
			return nil, err
		}
		for _, ref := range missing {
			if !seen[ref] {
				seen[ref] = true
				out = append(out, ref)
			}
		}
	}
	slices.SortFunc(out, func(a, b ExportModelRef) int {
		if c := cmp.Compare(a.ProviderName, b.ProviderName); c != 0 {
			return c
		}
		return cmp.Compare(a.ModelID, b.ModelID)
	})
	return out, nil
}

// filterModelsAbsentHere returns the refs that resolve to no model on this member.
// A ref that does resolve is dropped: whatever its state, the row is what the
// export must describe, either through the disabled_manually list or by differing
// until a sync sets it.
func filterModelsAbsentHere(ctx context.Context, q querier, refs []ExportModelRef) ([]ExportModelRef, error) {
	providers := make([]string, len(refs))
	modelIDs := make([]string, len(refs))
	for i, ref := range refs {
		providers[i] = ref.ProviderName
		modelIDs[i] = ref.ModelID
	}
	rows, err := q.Query(ctx, `
		SELECT p.name, m.model_id
		  FROM models m JOIN providers p ON m.provider_id = p.id
		 WHERE EXISTS (SELECT * FROM unnest($1::text[], $2::text[]) AS w(provider_name, model_id)
		                WHERE w.provider_name = p.name AND w.model_id = m.model_id)`,
		providers, modelIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	present := map[ExportModelRef]bool{}
	for rows.Next() {
		var ref ExportModelRef
		if err := rows.Scan(&ref.ProviderName, &ref.ModelID); err != nil {
			return nil, err
		}
		present[ref] = true
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]ExportModelRef, 0, len(refs))
	for _, ref := range refs {
		if !present[ref] {
			out = append(out, ref)
		}
	}
	return out, nil
}

// readUnappliedModelDisables reads the disable intent this member acknowledged but
// could not apply. A missing key is the normal state (nothing outstanding); an
// unparseable one is treated the same way rather than failing the export, since a
// corrupt instance-local marker must not take this member's config sync down. The
// next import rewrites it.
func readUnappliedModelDisables(ctx context.Context, q querier) ([]ExportModelRef, error) {
	rows, err := q.Query(ctx, `SELECT value FROM settings WHERE key = $1`, keyFleetUnappliedModelDisables)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var raw string
	if !rows.Next() {
		return nil, rows.Err() // no marker: nothing outstanding
	}
	if err := rows.Scan(&raw); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var refs []ExportModelRef
	if err := json.Unmarshal([]byte(raw), &refs); err != nil {
		debuglog.Warn("configsync: unparseable unapplied model-disable marker; ignoring", "error", err)
		return nil, nil
	}
	return refs, nil
}

// modelRef is the stable cross-member identity of a model: the provider's name
// plus the provider-scoped model_id. (provider_id, model_id) is unique per
// member, but the UUIDs differ, so failover entries travel by this pair.
type modelRef struct {
	provider string
	modelID  string
}

// modelRefByUUID maps each local model UUID to its stable (provider, model_id)
// ref, for translating a failover group's UUID entries out on export.
func modelRefByUUID(ctx context.Context, q querier) (map[string]modelRef, error) {
	rows, err := q.Query(ctx,
		`SELECT m.id, p.name, m.model_id FROM models m JOIN providers p ON m.provider_id = p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]modelRef{}
	for rows.Next() {
		var id, provider, modelID string
		if err := rows.Scan(&id, &provider, &modelID); err != nil {
			return nil, err
		}
		out[id] = modelRef{provider: provider, modelID: modelID}
	}
	return out, rows.Err()
}

// exportFailoverGroups reads every CUSTOM (auto_created = false) failover group
// and carries each as ordered (provider, model_id) entry refs. Auto-created
// groups are skipped: they regenerate identically on each member. An entry whose
// model UUID no longer resolves (model deleted) is dropped; the group is still
// exported so the importer can decide whether enough entries survive.
func exportFailoverGroups(ctx context.Context, q querier, refByUUID map[string]modelRef) ([]ExportFailoverGroup, error) {
	// description is COALESCEd because the main app's failover Upsert lists the
	// column with a *string value, so a nil description writes a SQL NULL (the
	// column DEFAULT '' only applies when the column is omitted). Every other read
	// in the failover package COALESCEs it; without this, a custom group with a
	// NULL description fails the Scan into g.Description and kills the whole export.
	rows, err := q.Query(ctx, `
		SELECT display_model, display_name, COALESCE(description, ''), COALESCE(group_enabled, true),
		       priority_order, COALESCE(entry_enabled, '{}')
		FROM model_failover_groups WHERE auto_created = false ORDER BY display_model`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExportFailoverGroup{}
	for rows.Next() {
		var g ExportFailoverGroup
		// priority_order / entry_enabled are JSONB stored as marshaled JSON, so
		// scan into bytes and unmarshal (matching failover.GetByModel).
		var priorityJSON, entryEnabledJSON []byte
		if err := rows.Scan(&g.DisplayModel, &g.DisplayName, &g.Description, &g.GroupEnabled,
			&priorityJSON, &entryEnabledJSON); err != nil {
			return nil, err
		}
		var priority []string
		if err := json.Unmarshal(priorityJSON, &priority); err != nil {
			return nil, err
		}
		entryEnabled := map[string]bool{}
		if len(entryEnabledJSON) > 0 {
			if err := json.Unmarshal(entryEnabledJSON, &entryEnabled); err != nil {
				return nil, err
			}
		}
		for _, uuidStr := range priority {
			ref, ok := refByUUID[uuidStr]
			if !ok {
				continue // model deleted since the group referenced it
			}
			// entry_enabled absence means enabled (matches proxy/enabledEntryIDs).
			enabled := true
			if v, ok := entryEnabled[uuidStr]; ok {
				enabled = v
			}
			g.Entries = append(g.Entries, ExportFailoverEntry{
				ProviderName: ref.provider, ModelID: ref.modelID, Enabled: enabled,
			})
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// providerIDToName maps provider UUID (text) -> name for translating a virtual
// key's allowed_providers list out of instance-local IDs.
func (h *ConfigSyncHandler) providerIDToName(ctx context.Context, q querier) (map[string]string, error) {
	rows, err := q.Query(ctx, `SELECT id, name FROM providers`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		out[id] = name
	}
	return out, rows.Err()
}

func exportProviders(ctx context.Context, q querier) ([]ExportProvider, error) {
	rows, err := q.Query(ctx, `
		SELECT name, base_url, encrypted_key, key_nonce, key_salt, masked_key, enabled, autodiscovery_enabled
		FROM providers ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExportProvider{}
	for rows.Next() {
		var p ExportProvider
		if err := rows.Scan(&p.Name, &p.BaseURL, &p.EncryptedKey, &p.KeyNonce, &p.KeySalt,
			&p.MaskedKey, &p.Enabled, &p.AutodiscoveryEnabled); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// exportVirtualKeys lists every virtual key by (created_at, key_hash). The
// tiebreaker is load-bearing: an import writes every key in one transaction, so a
// whole fleet's keys share a created_at, and without a second ordering column tied
// rows come back in physical row order. A row rewrite on either side would then
// reorder byte-identical config and change its hash, which Front Desk reads as a
// member that has not converged. key_hash is unique, so the order is total.
func exportVirtualKeys(ctx context.Context, q querier, idToName map[string]string) ([]ExportVK, error) {
	rows, err := q.Query(ctx, `
		SELECT vk.name, vk.key_hash, vk.key_preview, vk.rate_limit_rps, vk.rate_limit_burst, vk.rate_limit_tpm,
		       vk.allowed_providers, vk.strip_reasoning, u.username
		FROM virtual_keys vk LEFT JOIN users u ON u.id = vk.owner_user_id
		ORDER BY vk.created_at, vk.key_hash`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExportVK{}
	for rows.Next() {
		var v ExportVK
		var allowedIDs []string
		if err := rows.Scan(&v.Name, &v.KeyHash, &v.KeyPreview, &v.RateLimitRPS, &v.RateLimitBurst,
			&v.RateLimitTPM, &allowedIDs, &v.StripReasoning, &v.OwnerUsername); err != nil {
			return nil, err
		}
		// Translate instance-local provider UUIDs to names, dropping any that no
		// longer resolve. Restriction PRESENCE comes from the column being
		// non-NULL, not from how many names survive translation: a key whose
		// providers were all deleted stays restricted (present but empty) so the
		// import can tell it apart from an unrestricted key and refuse to widen
		// it (see upsertVirtualKeys).
		if allowedIDs != nil {
			names := []string{}
			for _, id := range allowedIDs {
				if name, ok := idToName[id]; ok {
					names = append(names, name)
				}
			}
			v.AllowedProviderNames = &names
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// exportUsers carries every dashboard user account, keyed by username. The
// argon2id password hash rides verbatim so a member can authenticate the same
// credentials; grants and role port as-is (no instance-local IDs involved).
// idToName translates the account provider cap out of instance-local UUIDs, the
// same way exportVirtualKeys translates a key's.
func exportUsers(ctx context.Context, q querier, idToName map[string]string) ([]ExportUser, error) {
	rows, err := q.Query(ctx, `
		SELECT username, display_name, email, password_hash, role, grants, enabled,
		       rate_limit_rps, rate_limit_burst, rate_limit_tpm, allowed_providers
		FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExportUser{}
	for rows.Next() {
		var u ExportUser
		var allowedIDs []string
		if err := rows.Scan(&u.Username, &u.DisplayName, &u.Email, &u.PasswordHash,
			&u.Role, &u.Grants, &u.Enabled,
			&u.RateLimitRPS, &u.RateLimitBurst, &u.RateLimitTPM, &allowedIDs); err != nil {
			return nil, err
		}
		// Same nullness-driven rule as exportVirtualKeys: cap PRESENCE comes from
		// the column being non-NULL, never from how many names survive
		// translation. An account whose capped providers were all deleted stays
		// capped (present but empty) so the import can tell it apart from an
		// uncapped account instead of widening it.
		if allowedIDs != nil {
			names := []string{}
			for _, id := range allowedIDs {
				if name, ok := idToName[id]; ok {
					names = append(names, name)
				}
			}
			u.AllowedProviderNames = &names
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func exportSettings(ctx context.Context, q querier) (map[string]string, error) {
	keys := syncableSettingKeys()
	rows, err := q.Query(ctx, `SELECT key, value FROM settings WHERE key = ANY($1)`, keys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, val string
		if err := rows.Scan(&k, &val); err != nil {
			return nil, err
		}
		out[k] = val
	}
	return out, rows.Err()
}
