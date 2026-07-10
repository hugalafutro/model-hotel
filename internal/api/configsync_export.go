package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
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
	// Marshal only the Config payload: providers come out ORDER BY name, virtual
	// keys ORDER BY created_at, failover groups ORDER BY display_model, and the
	// settings map is key-sorted by encoding/json, so the bytes are deterministic
	// for an unchanged config and the hash is stable across calls.
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
	users, err := exportUsers(ctx, pool)
	if err != nil {
		return ConfigEnvelope{}, err
	}
	return ConfigEnvelope{
		SchemaVersion: configSchemaVersion,
		AppVersion:    h.appVersion,
		ExportedAt:    time.Now().UTC(),
		Config: ConfigPayload{
			Providers: providers, VirtualKeys: vks, Settings: set, FailoverGroups: groups,
			Users: users,
		},
	}, nil
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

func exportVirtualKeys(ctx context.Context, q querier, idToName map[string]string) ([]ExportVK, error) {
	rows, err := q.Query(ctx, `
		SELECT vk.name, vk.key_hash, vk.key_preview, vk.rate_limit_rps, vk.rate_limit_burst, vk.rate_limit_tpm,
		       vk.allowed_providers, vk.strip_reasoning, u.username
		FROM virtual_keys vk LEFT JOIN users u ON u.id = vk.owner_user_id
		ORDER BY vk.created_at`)
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
		// Translate instance-local provider UUIDs to names; drop any that no
		// longer resolve. A key whose entire allow-list is stale exports an empty
		// AllowedProviderNames; import refuses to widen it to all-allowed and
		// skips it instead (see upsertVirtualKeys).
		for _, id := range allowedIDs {
			if name, ok := idToName[id]; ok {
				v.AllowedProviderNames = append(v.AllowedProviderNames, name)
			}
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// exportUsers carries every dashboard user account, keyed by username. The
// argon2id password hash rides verbatim so a member can authenticate the same
// credentials; grants and role port as-is (no instance-local IDs involved).
func exportUsers(ctx context.Context, q querier) ([]ExportUser, error) {
	rows, err := q.Query(ctx, `
		SELECT username, display_name, email, password_hash, role, grants, enabled,
		       rate_limit_rps, rate_limit_burst, rate_limit_tpm
		FROM users ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ExportUser{}
	for rows.Next() {
		var u ExportUser
		if err := rows.Scan(&u.Username, &u.DisplayName, &u.Email, &u.PasswordHash,
			&u.Role, &u.Grants, &u.Enabled,
			&u.RateLimitRPS, &u.RateLimitBurst, &u.RateLimitTPM); err != nil {
			return nil, err
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
