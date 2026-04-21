package provider

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/google/uuid"
)

type Provider struct {
	ID                uuid.UUID  `json:"id"`
	Name              string     `json:"name"`
	BaseURL           string     `json:"base_url"`
	EncryptedKey      []byte     `json:"-"`
	KeyNonce          []byte     `json:"-"`
	KeySalt           []byte     `json:"-"`
	Enabled           bool       `json:"enabled"`
	LastDiscoveredAt  *time.Time `json:"last_discovered_at"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
}

type CreateProviderRequest struct {
	Name    string `json:"name"`
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
}

type UpdateProviderRequest struct {
	Name     *string `json:"name"`
	BaseURL  *string `json:"base_url"`
	APIKey   *string `json:"api_key"`
	Enabled  *bool   `json:"enabled"`
}

type ProviderResponse struct {
	ID               uuid.UUID  `json:"id"`
	Name             string     `json:"name"`
	BaseURL          string     `json:"base_url"`
	MaskedKey        string     `json:"masked_key"`
	Enabled          bool       `json:"enabled"`
	LastDiscoveredAt *time.Time `json:"last_discovered_at"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

func (r *Repository) Create(ctx context.Context, req CreateProviderRequest, encryptedKey []byte, keyNonce []byte, keySalt []byte) (*Provider, error) {
	query := `
		INSERT INTO providers (name, base_url, encrypted_key, key_nonce, key_salt, enabled)
		VALUES ($1, $2, $3, $4, $5, true)
		RETURNING id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, last_discovered_at, created_at, updated_at
	`

	var p Provider
	err := r.pool.QueryRow(ctx, query, req.Name, req.BaseURL, encryptedKey, keyNonce, keySalt).Scan(
		&p.ID, &p.Name, &p.BaseURL, &p.EncryptedKey, &p.KeyNonce, &p.KeySalt, &p.Enabled,
		&p.LastDiscoveredAt, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return &p, nil
}

const providerColumns = `id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, last_discovered_at, created_at, updated_at`

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
			&p.ID, &p.Name, &p.BaseURL, &p.EncryptedKey, &p.KeyNonce, &p.KeySalt, &p.Enabled,
			&p.LastDiscoveredAt, &p.CreatedAt, &p.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}
		providers = append(providers, &p)
	}

	return providers, nil
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*Provider, error) {
	query := `SELECT ` + providerColumns + ` FROM providers WHERE id = $1`

	var p Provider
	err := r.pool.QueryRow(ctx, query, id).Scan(
		&p.ID, &p.Name, &p.BaseURL, &p.EncryptedKey, &p.KeyNonce, &p.KeySalt, &p.Enabled,
		&p.LastDiscoveredAt, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

	return &p, nil
}

func (r *Repository) Update(ctx context.Context, id uuid.UUID, req UpdateProviderRequest, encryptedKey []byte, keyNonce []byte, keySalt []byte) (*Provider, error) {
	query := `
		UPDATE providers
		SET name = COALESCE($1, name),
		    base_url = COALESCE($2, base_url),
		    encrypted_key = COALESCE($3, encrypted_key),
		    key_nonce = COALESCE($4, key_nonce),
		    key_salt = COALESCE($5, key_salt),
		    enabled = COALESCE($6, enabled),
		    updated_at = now()
		WHERE id = $7
		RETURNING ` + providerColumns

	var p Provider
	err := r.pool.QueryRow(ctx, query, req.Name, req.BaseURL, encryptedKey, keyNonce, keySalt, req.Enabled, id).Scan(
		&p.ID, &p.Name, &p.BaseURL, &p.EncryptedKey, &p.KeyNonce, &p.KeySalt, &p.Enabled,
		&p.LastDiscoveredAt, &p.CreatedAt, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}

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

	return nil
}

func ToResponse(p *Provider) ProviderResponse {
	maskedKey := "***"
	if len(p.EncryptedKey) > 0 {
		maskedKey = "***"
	}

	return ProviderResponse{
		ID:               p.ID,
		Name:             p.Name,
		BaseURL:          p.BaseURL,
		MaskedKey:        maskedKey,
		Enabled:          p.Enabled,
		LastDiscoveredAt: p.LastDiscoveredAt,
		CreatedAt:        p.CreatedAt,
		UpdatedAt:        p.UpdatedAt,
	}
}

func MaskAPIKey(apiKey string) string {
	if len(apiKey) <= 4 {
		return "***"
	}
	return "***" + apiKey[len(apiKey)-4:]
}
