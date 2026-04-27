package provider

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Provider struct {
	ID               uuid.UUID  `json:"id"`
	Name             string     `json:"name"`
	BaseURL          string     `json:"base_url"`
	EncryptedKey     []byte     `json:"-"`
	KeyNonce         []byte     `json:"-"`
	KeySalt          []byte     `json:"-"`
	MaskedKey        *string    `json:"masked_key"`
	Enabled          bool       `json:"enabled"`
	LastDiscoveredAt *time.Time `json:"last_discovered_at"`
	LastUsedAt       *time.Time `json:"last_used_at"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type CreateProviderRequest struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
}

type UpdateProviderRequest struct {
	Name    *string `json:"name"`
	BaseURL *string `json:"base_url"`
	APIKey  *string `json:"api_key"`
	Enabled *bool   `json:"enabled"`
}

type ProviderResponse struct {
	ID               uuid.UUID  `json:"id"`
	Name             string     `json:"name"`
	BaseURL          string     `json:"base_url"`
	MaskedKey        string     `json:"masked_key"`
	Enabled          bool       `json:"enabled"`
	LastDiscoveredAt *time.Time `json:"last_discovered_at"`
	LastUsedAt       *time.Time `json:"last_used_at"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	ModelCount       int        `json:"model_count"`
	TotalTokens      int        `json:"total_tokens"`
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, req CreateProviderRequest, encryptedKey []byte, keyNonce []byte, keySalt []byte) (*Provider, error) {
	mk := MaskAPIKey(req.APIKey)
	query := `
		INSERT INTO providers (name, base_url, encrypted_key, key_nonce, key_salt, masked_key, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, true)
		RETURNING id, name, base_url, encrypted_key, key_nonce, key_salt, masked_key, enabled, last_discovered_at, last_used_at, created_at, updated_at
	`

	var p Provider
	err := r.pool.QueryRow(ctx, query, req.Name, req.BaseURL, encryptedKey, keyNonce, keySalt, mk).Scan(
		&p.ID, &p.Name, &p.BaseURL, &p.EncryptedKey, &p.KeyNonce, &p.KeySalt, &p.MaskedKey, &p.Enabled,
		&p.LastDiscoveredAt, &p.LastUsedAt, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	cacheProvider(&p)
	return &p, nil
}

const providerColumns = `id, name, base_url, encrypted_key, key_nonce, key_salt, masked_key, enabled, last_discovered_at, last_used_at, created_at, updated_at`

func (r *Repository) List(ctx context.Context) ([]*Provider, error) {
	query := `SELECT ` + providerColumns + ` FROM providers ORDER BY created_at DESC`

	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var providers []*Provider
	for rows.Next() {
		var p Provider
		err := rows.Scan(
			&p.ID, &p.Name, &p.BaseURL, &p.EncryptedKey, &p.KeyNonce, &p.KeySalt, &p.MaskedKey, &p.Enabled,
			&p.LastDiscoveredAt, &p.LastUsedAt, &p.CreatedAt, &p.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		providers = append(providers, &p)
	}

	return providers, nil
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*Provider, error) {
	if p, ok := GetCachedByID(id); ok {
		return p, nil
	}

	query := `SELECT ` + providerColumns + ` FROM providers WHERE id = $1`

	var p Provider
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&p.ID, &p.Name, &p.BaseURL, &p.EncryptedKey, &p.KeyNonce, &p.KeySalt, &p.MaskedKey, &p.Enabled,
		&p.LastDiscoveredAt, &p.LastUsedAt, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	cacheProvider(&p)
	return &p, nil
}

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
		var p Provider
		if err := rows.Scan(
			&p.ID, &p.Name, &p.BaseURL, &p.EncryptedKey, &p.KeyNonce, &p.KeySalt, &p.MaskedKey, &p.Enabled,
			&p.LastDiscoveredAt, &p.LastUsedAt, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		cacheProvider(&p)
		result[p.ID] = &p
	}

	return result, rows.Err()
}

func (r *Repository) GetByName(ctx context.Context, name string) (*Provider, error) {
	if p, ok := GetCachedByName(name); ok {
		return p, nil
	}

	query := `SELECT ` + providerColumns + ` FROM providers WHERE name = $1`

	var p Provider
	err := r.pool.QueryRow(ctx, query, name).Scan(
		&p.ID, &p.Name, &p.BaseURL, &p.EncryptedKey, &p.KeyNonce, &p.KeySalt, &p.MaskedKey, &p.Enabled,
		&p.LastDiscoveredAt, &p.LastUsedAt, &p.CreatedAt, &p.UpdatedAt,
	)
	if err == nil {
		cacheProvider(&p)
		return &p, nil
	}

	normalized := NormalizeName(name)
	normalizedQuery := `SELECT ` + providerColumns + ` FROM providers WHERE REPLACE(name, ' ', '-') = $1`
	err = r.pool.QueryRow(ctx, normalizedQuery, normalized).Scan(
		&p.ID, &p.Name, &p.BaseURL, &p.EncryptedKey, &p.KeyNonce, &p.KeySalt, &p.MaskedKey, &p.Enabled,
		&p.LastDiscoveredAt, &p.LastUsedAt, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	cacheProvider(&p)
	return &p, nil
}

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
		    encrypted_key = COALESCE($3, encrypted_key),
		    key_nonce = COALESCE($4, key_nonce),
		    key_salt = COALESCE($5, key_salt),
		    masked_key = COALESCE($6, masked_key),
		    enabled = COALESCE($7, enabled),
		    updated_at = now()
		WHERE id = $8
		RETURNING ` + providerColumns

	var p Provider
	err := r.pool.QueryRow(ctx, query, req.Name, req.BaseURL, encryptedKey, keyNonce, keySalt, maskedKey, req.Enabled, id).Scan(
		&p.ID, &p.Name, &p.BaseURL, &p.EncryptedKey, &p.KeyNonce, &p.KeySalt, &p.MaskedKey, &p.Enabled,
		&p.LastDiscoveredAt, &p.LastUsedAt, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	InvalidateProviderCache()
	cacheProvider(&p)
	return &p, nil
}

func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	query := `DELETE FROM providers WHERE id = $1`
	result, err := r.pool.Exec(ctx, query, id)
	if err != nil {
		return err
	}

	if result.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}

	InvalidateProviderCache()
	return nil
}

func ToResponse(p *Provider) ProviderResponse {
	maskedKey := "***"
	if p.MaskedKey != nil && *p.MaskedKey != "" {
		maskedKey = *p.MaskedKey
	}

	return ProviderResponse{
		ID:               p.ID,
		Name:             p.Name,
		BaseURL:          p.BaseURL,
		MaskedKey:        maskedKey,
		Enabled:          p.Enabled,
		LastDiscoveredAt: p.LastDiscoveredAt,
		LastUsedAt:       p.LastUsedAt,
		CreatedAt:        p.CreatedAt,
		UpdatedAt:        p.UpdatedAt,
		ModelCount:       0,
	}
}

func MaskAPIKey(apiKey string) string {
	if len(apiKey) <= 4 {
		return "***"
	}
	return apiKey[:2] + "..." + apiKey[len(apiKey)-2:]
}

func (r *Repository) TouchLastUsed(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE providers SET last_used_at = now() WHERE id = $1
	`, id)
	if err != nil {
		return err
	}
	InvalidateProviderCache()
	return nil
}
