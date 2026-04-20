package virtualkey

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type VirtualKey struct {
	ID         uuid.UUID  `json:"id"`
	Name       string     `json:"name"`
	KeyHash    string     `json:"-"`
	TokensUsed int64      `json:"tokens_used"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
}

type CreateVirtualKeyRequest struct {
	Name string `json:"name"`
}

type VirtualKeyResponse struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Key        string  `json:"key,omitempty"`
	KeyPreview string  `json:"key_preview"`
	TokensUsed int64   `json:"tokens_used"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

var columns = []string{"id", "name", "key_hash", "tokens_used", "last_used_at", "created_at"}

func (r *Repository) Create(ctx context.Context, name, keyHash string) (*VirtualKey, error) {
	var vk VirtualKey
	err := r.pool.QueryRow(ctx,
		`INSERT INTO virtual_keys (name, key_hash) VALUES ($1, $2) RETURNING id, name, key_hash, tokens_used, last_used_at, created_at`,
		name, keyHash,
	).Scan(&vk.ID, &vk.Name, &vk.KeyHash, &vk.TokensUsed, &vk.LastUsedAt, &vk.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &vk, nil
}

func (r *Repository) List(ctx context.Context) ([]*VirtualKey, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT id, name, key_hash, tokens_used, last_used_at, created_at FROM virtual_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []*VirtualKey
	for rows.Next() {
		var vk VirtualKey
		if err := rows.Scan(&vk.ID, &vk.Name, &vk.KeyHash, &vk.TokensUsed, &vk.LastUsedAt, &vk.CreatedAt); err != nil {
			return nil, err
		}
		keys = append(keys, &vk)
	}
	return keys, nil
}

func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*VirtualKey, error) {
	var vk VirtualKey
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, key_hash, tokens_used, last_used_at, created_at FROM virtual_keys WHERE id = $1`, id,
	).Scan(&vk.ID, &vk.Name, &vk.KeyHash, &vk.TokensUsed, &vk.LastUsedAt, &vk.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &vk, nil
}

func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM virtual_keys WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *Repository) AddTokens(ctx context.Context, keyHash string, tokens int) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE virtual_keys SET tokens_used = tokens_used + $1, last_used_at = now() WHERE key_hash = $2`,
		tokens, keyHash)
	return err
}

func (r *Repository) FindByKeyHash(ctx context.Context, keyHash string) (*VirtualKey, error) {
	var vk VirtualKey
	err := r.pool.QueryRow(ctx,
		`SELECT id, name, key_hash, tokens_used, last_used_at, created_at FROM virtual_keys WHERE key_hash = $1`, keyHash,
	).Scan(&vk.ID, &vk.Name, &vk.KeyHash, &vk.TokensUsed, &vk.LastUsedAt, &vk.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &vk, nil
}

var ErrNotFound = &notFoundError{}

type notFoundError struct{}

func (e *notFoundError) Error() string { return "virtual key not found" }
