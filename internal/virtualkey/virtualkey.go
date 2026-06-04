package virtualkey

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// VirtualKey represents a virtual API key entity.
type VirtualKey struct {
	ID               uuid.UUID  `json:"id"`
	Name             string     `json:"name"`
	KeyHash          string     `json:"-"`
	KeyPreview       string     `json:"key_preview"`
	TokensUsed       int64      `json:"tokens_used"`
	LastUsedAt       *time.Time `json:"last_used_at"`
	CreatedAt        time.Time  `json:"created_at"`
	RateLimitRPS     *float64   `json:"rate_limit_rps"`
	RateLimitBurst   *int       `json:"rate_limit_burst"`
	AllowedProviders *[]string  `json:"allowed_providers"`
	StripReasoning   bool       `json:"strip_reasoning"`
}

// CreateVirtualKeyRequest is the request body for creating a virtual key.
type CreateVirtualKeyRequest struct {
	Name             string    `json:"name"`
	RateLimitRPS     *float64  `json:"rate_limit_rps,omitempty"`
	RateLimitBurst   *int      `json:"rate_limit_burst,omitempty"`
	AllowedProviders *[]string `json:"allowed_providers,omitempty"`
	StripReasoning   *bool     `json:"strip_reasoning,omitempty"`
}

// VirtualKeyResponse is the API response for a virtual key.
//
//nolint:revive // stutter is acceptable: VirtualKeyResponse is a domain concept
type VirtualKeyResponse struct {
	ID               string    `json:"id"`
	Name             string    `json:"name"`
	Key              string    `json:"key,omitempty"`
	KeyPreview       string    `json:"key_preview"`
	TokensUsed       int64     `json:"tokens_used"`
	LastUsedAt       *string   `json:"last_used_at"`
	CreatedAt        string    `json:"created_at"`
	RateLimitRPS     *float64  `json:"rate_limit_rps"`
	RateLimitBurst   *int      `json:"rate_limit_burst"`
	AllowedProviders *[]string `json:"allowed_providers"`
	StripReasoning   bool      `json:"strip_reasoning"`
}

// scanner is satisfied by pgx.Row and pgx.Rows.
type scanner interface{ Scan(dest ...any) error }

// vkColumns is the column list for SELECT queries on virtual_keys.
const vkColumns = `id, name, key_hash, key_preview, tokens_used, last_used_at, created_at, rate_limit_rps, rate_limit_burst, allowed_providers, strip_reasoning`

// scanVirtualKey scans a single row into a VirtualKey using the vkColumns order.
func scanVirtualKey(row scanner) (*VirtualKey, error) {
	var vk VirtualKey
	err := row.Scan(&vk.ID, &vk.Name, &vk.KeyHash, &vk.KeyPreview, &vk.TokensUsed, &vk.LastUsedAt, &vk.CreatedAt, &vk.RateLimitRPS, &vk.RateLimitBurst, &vk.AllowedProviders, &vk.StripReasoning)
	if err != nil {
		return nil, err
	}
	return &vk, nil
}

// Repository provides database access for virtual keys.
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a new virtual key repository.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

// Create inserts a new virtual key.
func (r *Repository) Create(ctx context.Context, name, keyHash, keyPreview string, rps *float64, burst *int, allowedProviders *[]string, stripReasoning *bool) (*VirtualKey, error) {
	vk, err := scanVirtualKey(r.pool.QueryRow(ctx,
		`INSERT INTO virtual_keys (name, key_hash, key_preview, rate_limit_rps, rate_limit_burst, allowed_providers, strip_reasoning) VALUES ($1, $2, $3, $4, $5, $6, COALESCE($7, false)) RETURNING `+vkColumns,
		name, keyHash, keyPreview, rps, burst, allowedProviders, stripReasoning))
	if err != nil {
		return nil, err
	}
	return vk, nil
}

// List returns all virtual keys.
func (r *Repository) List(ctx context.Context) ([]*VirtualKey, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT `+vkColumns+` FROM virtual_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var keys []*VirtualKey
	for rows.Next() {
		vk, err := scanVirtualKey(rows)
		if err != nil {
			return nil, err
		}
		keys = append(keys, vk)
	}
	return keys, rows.Err()
}

// Get retrieves a virtual key by ID.
func (r *Repository) Get(ctx context.Context, id uuid.UUID) (*VirtualKey, error) {
	vk, err := scanVirtualKey(r.pool.QueryRow(ctx,
		`SELECT `+vkColumns+` FROM virtual_keys WHERE id = $1`, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return vk, nil
}

// Delete removes a virtual key by ID.
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

// AddTokens increments the token usage counters for a virtual key.
func (r *Repository) AddTokens(ctx context.Context, keyHash string, tokens int) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE virtual_keys SET tokens_used = tokens_used + $1, last_used_at = now() WHERE key_hash = $2`,
		tokens, keyHash)
	return err
}

// TouchLastUsed updates the last used timestamp.
func (r *Repository) TouchLastUsed(ctx context.Context, keyHash string) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE virtual_keys SET last_used_at = now() WHERE key_hash = $1`,
		keyHash)
	if err != nil {
		debuglog.Error("vkey: failed to touch last_used_at", "key_hash", keyHash, "error", err)
	}
	return err
}

// Update modifies virtual key fields.
func (r *Repository) Update(ctx context.Context, id uuid.UUID, name string, rps *float64, burst *int, allowedProviders *[]string, stripReasoning *bool) (*VirtualKey, error) {
	// Always include all updatable fields in SET clause so nil/null
	// values are correctly persisted as NULL (cleared) rather than
	// silently ignored. The UI sends null when a user clears a field.
	setClauses := []string{"name = $1"}
	args := []any{name}
	argIdx := 2

	setClauses = append(setClauses, "rate_limit_rps = $"+fmt.Sprintf("%d", argIdx))
	args = append(args, rps)
	argIdx++
	setClauses = append(setClauses, "rate_limit_burst = $"+fmt.Sprintf("%d", argIdx))
	args = append(args, burst)
	argIdx++
	// allowed_providers and strip_reasoning also always in SET clause.
	setClauses = append(setClauses, "allowed_providers = $"+fmt.Sprintf("%d", argIdx))
	args = append(args, allowedProviders)
	argIdx++
	setClauses = append(setClauses, "strip_reasoning = COALESCE($"+fmt.Sprintf("%d", argIdx)+", false)")
	args = append(args, stripReasoning)
	argIdx++

	args = append(args, id)
	query := `UPDATE virtual_keys SET ` + strings.Join(setClauses, ", ") + ` WHERE id = $` + fmt.Sprintf("%d", argIdx) + ` RETURNING ` + vkColumns

	vk, err := scanVirtualKey(r.pool.QueryRow(ctx, query, args...))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return vk, nil
}

// FindByKeyHash looks up a virtual key by its SHA-256 hash.
func (r *Repository) FindByKeyHash(ctx context.Context, keyHash string) (*VirtualKey, error) {
	vk, err := scanVirtualKey(r.pool.QueryRow(ctx,
		`SELECT `+vkColumns+` FROM virtual_keys WHERE key_hash = $1`, keyHash))
	if err != nil {
		return nil, err
	}
	return vk, nil
}

// ErrNotFound is returned when a virtual key is not found.
var ErrNotFound = &notFoundError{}

type notFoundError struct{}

func (e *notFoundError) Error() string { return "virtual key not found" }
