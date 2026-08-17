package provider

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
	"github.com/hugalafutro/model-hotel/internal/model"
)

// Provider represents an LLM provider configuration.
type Provider struct {
	ID                   uuid.UUID  `json:"id"`
	Name                 string     `json:"name"`
	BaseURL              string     `json:"base_url"`
	ProviderType         string     `json:"provider_type"`
	EncryptedKey         []byte     `json:"-"`
	KeyNonce             []byte     `json:"-"`
	KeySalt              []byte     `json:"-"`
	MaskedKey            *string    `json:"masked_key"`
	Enabled              bool       `json:"enabled"`
	AutodiscoveryEnabled bool       `json:"autodiscovery_enabled"`
	ScheduledDisableOn   *time.Time `json:"scheduled_disable_on"`
	LastDiscoveredAt     *time.Time `json:"last_discovered_at"`
	LastUsedAt           *time.Time `json:"last_used_at"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// CreateProviderRequest is the request body for creating a provider.
type CreateProviderRequest struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	// ProviderType is the vendor/API family the operator picked in the add
	// dialog. It is stored as given and never re-derived from the URL.
	ProviderType string `json:"provider_type"`
	APIKey       string `json:"api_key"`
}

// UpdateProviderRequest is the request body for updating a provider.
type UpdateProviderRequest struct {
	Name    *string `json:"name"`
	BaseURL *string `json:"base_url"`
	// ProviderType corrects a provider's type. It is not something to change
	// casually, but a row backfilled from the old URL rules can carry a type
	// its operator never chose (a self-hosted server on a non-default port was
	// filed as generic OpenAI), and re-adding the provider would cascade away
	// its models. A new self-hosted type is confirmed by probing, exactly as on
	// create.
	ProviderType         *string      `json:"provider_type"`
	APIKey               *string      `json:"api_key"`
	Enabled              *bool        `json:"enabled"`
	AutodiscoveryEnabled *bool        `json:"autodiscovery_enabled"`
	ScheduledDisableOn   OptionalDate `json:"scheduled_disable_on"`
}

// OptionalDate is a JSON field with three states an ordinary pointer cannot
// express: absent (keep the stored value), null (clear it), and a value.
type OptionalDate struct {
	Set   bool
	Value *string // nil with Set means an explicit null
}

// UnmarshalJSON is only invoked when the field is present, which is what makes
// Set a presence flag.
func (o *OptionalDate) UnmarshalJSON(b []byte) error {
	o.Set = true
	if string(b) == "null" {
		o.Value = nil
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	o.Value = &s
	return nil
}

// ProviderResponse is the response body for provider operations.
//
//nolint:revive // stutter is acceptable: ProviderResponse is a domain concept
type ProviderResponse struct {
	ID                   uuid.UUID  `json:"id"`
	Name                 string     `json:"name"`
	BaseURL              string     `json:"base_url"`
	ProviderType         string     `json:"provider_type"`
	MaskedKey            string     `json:"masked_key"`
	Enabled              bool       `json:"enabled"`
	AutodiscoveryEnabled bool       `json:"autodiscovery_enabled"`
	ScheduledDisableOn   *string    `json:"scheduled_disable_on"`
	LastDiscoveredAt     *time.Time `json:"last_discovered_at"`
	LastUsedAt           *time.Time `json:"last_used_at"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	ModelCount           int        `json:"model_count"`
	TotalTokens          int        `json:"total_tokens"`
}

// Repository manages provider CRUD operations.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a new provider repository.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Create creates a new provider with the given request and encrypted key material.
//
//nolint:gocritic // same-type params are clearer with separate names
func (r *Repository) Create(ctx context.Context, req CreateProviderRequest, encryptedKey []byte, keyNonce []byte, keySalt []byte) (*Provider, error) {
	mk := MaskAPIKey(req.APIKey)
	query := `
		INSERT INTO providers (name, base_url, provider_type, encrypted_key, key_nonce, key_salt, masked_key, enabled, autodiscovery_enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7, true, true)
		RETURNING ` + providerColumns

	p, err := scanProvider(r.pool.QueryRow(ctx, query, req.Name, req.BaseURL, req.ProviderType, encryptedKey, keyNonce, keySalt, mk))
	if err != nil {
		debuglog.Error("provider: create failed", "name", req.Name, "error", err)
		return nil, err
	}

	cacheProvider(p)
	return p, nil
}

const providerColumns = `id, name, base_url, provider_type, encrypted_key, key_nonce, key_salt, masked_key, enabled, autodiscovery_enabled, scheduled_disable_on, last_discovered_at, last_used_at, created_at, updated_at`

// scanner is satisfied by pgx.Row and pgx.Rows.
type scanner interface{ Scan(dest ...any) error }

// scanProvider scans a single row into a Provider using the providerColumns order.
func scanProvider(row scanner) (*Provider, error) {
	var p Provider
	err := row.Scan(
		&p.ID, &p.Name, &p.BaseURL, &p.ProviderType, &p.EncryptedKey, &p.KeyNonce, &p.KeySalt, &p.MaskedKey, &p.Enabled, &p.AutodiscoveryEnabled,
		&p.ScheduledDisableOn, &p.LastDiscoveredAt, &p.LastUsedAt, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// List returns all providers ordered by creation date.
func (r *Repository) List(ctx context.Context) ([]*Provider, error) {
	query := `SELECT ` + providerColumns + ` FROM providers ORDER BY created_at DESC`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		debuglog.Error("provider: list query failed", "error", err)
		return nil, err
	}
	defer rows.Close()

	var providers []*Provider
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			debuglog.Error("provider: list scan failed", "error", err)
			return nil, err
		}
		providers = append(providers, p)
	}

	return providers, nil
}

// Get retrieves a provider by ID.
func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*Provider, error) {
	if p, ok := GetCachedByID(id); ok {
		return p, nil
	}

	query := `SELECT ` + providerColumns + ` FROM providers WHERE id = $1`

	p, err := scanProvider(r.pool.QueryRow(ctx, query, id))
	if err != nil {
		return nil, err
	}

	cacheProvider(p)
	return p, nil
}

// GetByIDs retrieves multiple providers by their IDs.
func (r *Repository) GetByIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]*Provider, error) {
	result := make(map[uuid.UUID]*Provider, len(ids))

	if len(ids) == 0 {
		return result, nil
	}

	var uncachedIDs []uuid.UUID
	for _, id := range ids {
		if p, ok := GetCachedByID(id); ok {
			result[id] = p
		} else {
			uncachedIDs = append(uncachedIDs, id)
		}
	}

	if len(uncachedIDs) == 0 {
		return result, nil
	}

	query := `SELECT ` + providerColumns + ` FROM providers WHERE id = ANY($1)`

	rows, err := r.pool.Query(ctx, query, uncachedIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			return nil, err
		}
		cacheProvider(p)
		result[p.ID] = p
	}

	return result, rows.Err()
}

// GetByName retrieves a provider by name (with normalization fallback).
func (r *Repository) GetByName(ctx context.Context, name string) (*Provider, error) {
	if p, ok := GetCachedByName(name); ok {
		return p, nil
	}

	query := `SELECT ` + providerColumns + ` FROM providers WHERE name = $1`

	p, err := scanProvider(r.pool.QueryRow(ctx, query, name))
	if err == nil {
		cacheProvider(p)
		return p, nil
	}

	normalized := NormalizeName(name)
	normalizedQuery := `SELECT ` + providerColumns + ` FROM providers WHERE REPLACE(name, ' ', '-') = $1`
	p, err = scanProvider(r.pool.QueryRow(ctx, normalizedQuery, normalized))
	if err != nil {
		return nil, err
	}

	cacheProvider(p)
	return p, nil
}

// Update updates a provider's fields.
//
//nolint:gocritic // same-type params are clearer with separate names
func (r *Repository) Update(ctx context.Context, id uuid.UUID, req UpdateProviderRequest, encryptedKey []byte, keyNonce []byte, keySalt []byte) (*Provider, error) {
	var maskedKey *string
	if req.APIKey != nil {
		mk := MaskAPIKey(*req.APIKey)
		maskedKey = &mk
	}

	query := `
		UPDATE providers
		SET name = COALESCE($1, name),
		    base_url = COALESCE($2, base_url),
		    provider_type = COALESCE($12, provider_type),
		    encrypted_key = COALESCE($3, encrypted_key),
		    key_nonce = COALESCE($4, key_nonce),
		    key_salt = COALESCE($5, key_salt),
		    masked_key = COALESCE($6, masked_key),
		    enabled = COALESCE($7, enabled),
		    autodiscovery_enabled = COALESCE($8, autodiscovery_enabled),
		    scheduled_disable_on = CASE
		        WHEN COALESCE($7, enabled) = false THEN NULL
		        WHEN $9 THEN $10::date
		        ELSE scheduled_disable_on
		    END,
		    updated_at = now()
		WHERE id = $11
		RETURNING ` + providerColumns

	p, err := scanProvider(r.pool.QueryRow(ctx, query,
		req.Name, req.BaseURL, encryptedKey, keyNonce, keySalt, maskedKey,
		req.Enabled, req.AutodiscoveryEnabled,
		req.ScheduledDisableOn.Set, req.ScheduledDisableOn.Value, id, req.ProviderType))
	if err != nil {
		debuglog.Error("provider: update failed", "id", id, "error", err)
		return nil, err
	}

	InvalidateProviderCache()
	cacheProvider(p)
	// Cached model rows denormalize provider name and enabled state, so a
	// provider update must drop them or failover entries report stale
	// provider_enabled until the model cache TTL expires.
	model.InvalidateModelCache()
	return p, nil
}

// DisableDueScheduled flips enabled off for every provider whose scheduled
// disable day has arrived on the app server's clock, which is the same clock
// the update validation uses when it rejects a date in the past. The comparison
// date travels as a parameter rather than reading CURRENT_DATE, because the DB
// session's timezone can differ from the server's and a date one path accepts
// as tomorrow would already be due for the other. The schedule is cleared in the
// same statement so the disable fires once. Returns the providers it disabled.
func (r *Repository) DisableDueScheduled(ctx context.Context) ([]*Provider, error) {
	rows, err := r.pool.Query(ctx, `
		UPDATE providers
		SET enabled = false, scheduled_disable_on = NULL, updated_at = now()
		WHERE enabled = true AND scheduled_disable_on IS NOT NULL
		  AND scheduled_disable_on <= $1::date
		RETURNING `+providerColumns, time.Now().Format("2006-01-02"))
	if err != nil {
		debuglog.Error("provider: scheduled disable sweep failed", "error", err)
		return nil, err
	}
	defer rows.Close()

	var out []*Provider
	for rows.Next() {
		p, err := scanProvider(rows)
		if err != nil {
			debuglog.Error("provider: scheduled disable scan failed", "error", err)
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		debuglog.Error("provider: scheduled disable sweep failed", "error", err)
		return nil, err
	}
	if len(out) > 0 {
		// The same invalidations a manual disable through Update performs:
		// cached model rows denormalize the provider's enabled state.
		InvalidateProviderCache()
		model.InvalidateModelCache()
	}
	return out, nil
}

// Delete removes a provider by ID, along with every reference to it in the
// allow-list columns.
//
// The transaction is the point: virtual_keys.allowed_providers and
// users.allowed_providers hold provider UUIDs in a TEXT[], which cannot carry a
// foreign key, so pruning them is this function's job rather than the database's
// (see PruneAllowLists). Doing the delete and the prunes in one transaction is
// what stops a failure between them leaving the provider gone with its id still
// referenced, which is the exact dangling state the pruning exists to prevent.
//
// Concurrency note, because this changed when the transaction was introduced.
// This used to be one autocommit statement holding its locks for the length of
// that statement. It now holds them until commit, and the footprint is wider
// than the three tables named in this file:
//
//   - providers, then virtual_keys, then users, written here directly;
//   - models and provider_quota_snapshots, deleted by FK CASCADE;
//   - request_logs, whose provider_id is set to NULL by FK (migration 010).
//
// That last one is the largest part of the change: request_logs is the
// highest-volume table in the schema, every row belonging to this provider is
// rewritten, and those row locks are now pinned across the two allow-list
// UPDATEs as well instead of being released with the DELETE. It adds no
// deadlock risk of its own, because the writer on the other side is a proxy log
// insert, a single autocommit statement that takes only FOR KEY SHARE on the
// provider row and so can never hold a lock while waiting for one of ours.
//
// A deadlock with a concurrent config-sync import IS possible, though the
// window is narrow. Config-sync funnels through providers first: upsertProviders
// locks the envelope's providers and the declarative delete locks the rest,
// before it touches either allow-list column. But that funnel is SNAPSHOT-
// SCOPED. It covers every provider row that existed when the import's provider
// stage ran, and nothing later in apply() locks providers again, so a provider
// created after that point escapes it entirely. Once it does, the reversed
// second-half order bites: config-sync writes users and then virtual_keys, the
// opposite of the order here.
//
// The sequence, with T2 the import, T1 this function and T3 an ordinary create:
//
//  1. T2 finishes its provider stage, holding every provider row in its snapshot.
//  2. T3 commits INSERT INTO providers for Q; Q is in no lock set of T2.
//  3. T2 reaches applyUsers, whose `UPDATE users SET email = NULL` locks every
//     users row, including U, which caps on Q.
//  4. T1 deletes Q, uncontested, because T2 never locked it.
//  5. T1 prunes virtual_keys, locking VK, which allow-lists Q. Uncontested.
//  6. T1 prunes users, needs U, and blocks on T2.
//  7. T2 reaches upsertVirtualKeys, touches VK, and blocks on T1.
//
// Postgres detects the cycle and aborts one side with SQLSTATE 40P01. Both
// operations are safely retriable, which is what keeps this a nuisance rather
// than a correctness problem: the aborted transaction rolled back whole, so a
// retried delete simply performs the delete (it returns pgx.ErrNoRows only if an
// earlier attempt actually committed), and a member whose import was aborted is
// re-pushed on the next auto-sync tick, because that tick reads the member's own
// config hash and finds it still does not match the primary's. A one-off manual
// sync from the wizard is NOT re-pushed and has to be repeated by the operator.
//
// All of this is a property of config-sync's statement order rather than
// anything enforced, so reordering internal/api/configsync_apply.go's apply()
// means re-deriving it.
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		debuglog.Error("provider: delete failed to begin transaction", "id", id, "error", err)
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	result, err := tx.Exec(ctx, `DELETE FROM providers WHERE id = $1`, id)
	if err != nil {
		debuglog.Error("provider: delete failed", "id", id, "error", err)
		return err
	}

	// Before the prune, so a missing provider is still ErrNoRows and does not
	// pay for two pointless UPDATEs.
	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}

	if err := PruneAllowLists(ctx, tx, []string{id.String()}); err != nil {
		debuglog.Error("provider: pruning allow-lists failed", "id", id, "error", err)
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		debuglog.Error("provider: delete failed to commit", "id", id, "error", err)
		return err
	}

	// Caches are dropped only after the commit: invalidating earlier would let a
	// concurrent read repopulate them from a transaction that then rolled back.
	InvalidateProviderCache()
	// The DB cascade removes this provider's models; drop their cached rows too.
	model.InvalidateModelCache()
	return nil
}

// BackfillTypes gives a stored type to every provider row that has none:
// rows created before provider_type existed, and rows arriving from an older
// dump or fleet export. The type is derived once, from the URL rules that were
// in force when those rows were written, so their behaviour does not change.
// Idempotent, and a no-op once every row has a type.
func (r *Repository) BackfillTypes(ctx context.Context) (int, error) {
	rows, err := r.pool.Query(ctx, `SELECT id, base_url FROM providers WHERE provider_type = ''`)
	if err != nil {
		return 0, err
	}
	type pending struct {
		id  uuid.UUID
		typ string
	}
	var todo []pending
	for rows.Next() {
		var id uuid.UUID
		var baseURL string
		if err := rows.Scan(&id, &baseURL); err != nil {
			rows.Close()
			return 0, err
		}
		todo = append(todo, pending{id: id, typ: LegacyTypeFromURL(baseURL)})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	for _, p := range todo {
		if _, err := r.pool.Exec(ctx, `UPDATE providers SET provider_type = $1 WHERE id = $2 AND provider_type = ''`, p.typ, p.id); err != nil {
			return 0, err
		}
	}
	if len(todo) > 0 {
		InvalidateProviderCache()
		debuglog.Info("provider: backfilled provider types", "count", len(todo))
	}
	return len(todo), nil
}

// ToResponse converts a Provider to a ProviderResponse.
func ToResponse(p *Provider) ProviderResponse {
	maskedKey := "N/A"
	if len(p.EncryptedKey) > 0 {
		maskedKey = "***"
		if p.MaskedKey != nil && *p.MaskedKey != "" {
			maskedKey = *p.MaskedKey
		}
	}

	var sched *string
	if p.ScheduledDisableOn != nil {
		s := p.ScheduledDisableOn.Format("2006-01-02")
		sched = &s
	}

	return ProviderResponse{
		ID:                   p.ID,
		Name:                 p.Name,
		BaseURL:              p.BaseURL,
		ProviderType:         TypeOf(p),
		MaskedKey:            maskedKey,
		Enabled:              p.Enabled,
		AutodiscoveryEnabled: p.AutodiscoveryEnabled,
		ScheduledDisableOn:   sched,
		LastDiscoveredAt:     p.LastDiscoveredAt,
		LastUsedAt:           p.LastUsedAt,
		CreatedAt:            p.CreatedAt,
		UpdatedAt:            p.UpdatedAt,
		ModelCount:           0,
	}
}

// MaskAPIKey returns a masked version of an API key for display.
func MaskAPIKey(apiKey string) string {
	if len(apiKey) <= 4 {
		return "***"
	}
	return apiKey[:2] + "..." + apiKey[len(apiKey)-2:]
}

// TouchLastUsed updates the last_used_at timestamp for a provider.
func (r *Repository) TouchLastUsed(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE providers SET last_used_at = now() WHERE id = $1
	`, id)
	if err != nil {
		debuglog.Error("provider: touch last_used failed", "id", id, "error", err)
		return err
	}
	// A single-row metadata write only invalidates that provider's own cache
	// entries: a full flush here would empty the routing cache on every
	// attempt/probe, and hedged streaming touches every launched candidate.
	EvictProviderCacheByID(id)
	return nil
}
