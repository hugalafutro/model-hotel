package settings

import (
	"context"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type cacheEntry struct {
	value     string
	expiresAt time.Time
}

type Repository struct {
	pool  *pgxpool.Pool
	mu    sync.RWMutex
	cache map[string]cacheEntry
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{
		pool:  pool,
		cache: make(map[string]cacheEntry),
	}
}

const cacheTTL = 30 * time.Second

func (r *Repository) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := r.pool.QueryRow(ctx, "SELECT value FROM settings WHERE key = $1", key).Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
}

func (r *Repository) GetWithDefault(ctx context.Context, key string, defaultValue string) string {
	r.mu.RLock()
	if entry, ok := r.cache[key]; ok && time.Now().Before(entry.expiresAt) {
		r.mu.RUnlock()
		return entry.value
	}
	r.mu.RUnlock()

	var value string
	err := r.pool.QueryRow(ctx, "SELECT value FROM settings WHERE key = $1", key).Scan(&value)
	if err != nil {
		return defaultValue
	}

	r.mu.Lock()
	r.cache[key] = cacheEntry{value: value, expiresAt: time.Now().Add(cacheTTL)}
	r.mu.Unlock()

	return value
}

func (r *Repository) Set(ctx context.Context, key string, value string) error {
	r.mu.Lock()
	delete(r.cache, key)
	r.mu.Unlock()

	_, err := r.pool.Exec(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = now()
	`, key, value)
	return err
}

func (r *Repository) GetAll(ctx context.Context) (map[string]string, error) {
	rows, err := r.pool.Query(ctx, "SELECT key, value FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			continue
		}
		result[key] = value
	}
	return result, nil
}

func (r *Repository) GetBool(ctx context.Context, key string, defaultValue bool) bool {
	val := r.GetWithDefault(ctx, key, strconv.FormatBool(defaultValue))
	b, err := strconv.ParseBool(val)
	if err != nil {
		return defaultValue
	}
	return b
}

func (r *Repository) GetDuration(ctx context.Context, key string, defaultValue time.Duration) time.Duration {
	val := r.GetWithDefault(ctx, key, defaultValue.String())
	d, err := time.ParseDuration(val)
	if err != nil {
		return defaultValue
	}
	return d
}

func (r *Repository) GetFloat(ctx context.Context, key string, defaultValue float64) float64 {
	val := r.GetWithDefault(ctx, key, strconv.FormatFloat(defaultValue, 'f', -1, 64))
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return defaultValue
	}
	return f
}

func (r *Repository) GetInt(ctx context.Context, key string, defaultValue int) int {
	val := r.GetWithDefault(ctx, key, strconv.Itoa(defaultValue))
	i, err := strconv.Atoi(val)
	if err != nil {
		return defaultValue
	}
	return i
}