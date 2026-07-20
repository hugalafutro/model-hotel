// Package settings provides database-backed settings with caching and change subscriptions.
package settings

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// AllowedSettings is the allowlist of keys the API will accept.
// The key set MUST be kept in sync with api.allowedSettings — add a
// key to both or neither. TestAllowedSettingsSync enforces this at CI time.
var AllowedSettings = map[string]bool{
	"discovery_interval":           true,
	"discovery_on_startup":         true,
	"discovery_on_provider_create": true,
	"log_retention":                true,
	"stale_request_timeout":        true,
	"request_timeout":              true,
	"failover_on_rate_limit":       true,
	"circuit_breaker_enabled":      true,
	"circuit_breaker_threshold":    true,
	"circuit_breaker_cooldown":     true,
	"rate_limit_enabled":           true,
	"rate_limit_ip_enabled":        true,
	"rate_limit_ip_rps":            true,
	"rate_limit_ip_burst":          true,
	"rate_limit_rps":               true,
	"rate_limit_burst":             true,
	"rate_limit_tpm":               true,
	"rate_limit_max_wait_ms":       true,
	"key_cache_ttl":                true,
	"ttft_timeout":                 true,
	"stream_stall_timeout":         true,
	"hedging_enabled":              true,
	"hedge_delay":                  true,
	"backup_enabled":               true,
	"backup_interval":              true,
	"backup_son_retention":         true,
	"backup_father_retention":      true,
	"backup_grandfather_retention": true,
	"alert_enabled":                true,
	"alert_apprise_api_url":        true,
	"alert_apprise_targets":        true,
	"alert_events":                 true,
	"session_idle_timeout_minutes": true,
	"oidc_enabled":                 true,
	"oidc_issuer_url":              true,
	"oidc_client_id":               true,
	"oidc_client_secret":           true,
	"oidc_allowed_emails":          true,
	"oidc_public_base_url":         true,
	"github_sso_enabled":           true,
	"github_client_id":             true,
	"github_client_secret":         true,
	"github_allowed_emails":        true,
	"github_public_base_url":       true,
	"quota_refresh_interval_min":   true,
}

type cacheEntry struct {
	value     string
	expiresAt time.Time
}

// ChangeEvent represents a settings change delivered to subscribers.
type ChangeEvent struct {
	Key   string
	Value string
}

// subIDCounter is used to assign unique IDs to each subscription.
var subIDCounter atomic.Uint64

// subscription ties a unique ID to a channel so that Unsubscribe can find it
// without relying on channel comparison (which doesn't work across
// directional types in Go).
type subscription struct {
	id uint64
	ch chan ChangeEvent
}

// Repository manages application settings with caching and change notification.
//
// Callers who need to react immediately when a setting changes (instead of
// waiting for the next polling cycle) can use Subscribe to receive change
// events. The returned Subscription provides a channel to read from and an
// Unsubscribe method to clean up.
type Repository struct {
	pool     *pgxpool.Pool
	mu       sync.RWMutex
	cache    map[string]cacheEntry
	cacheTTL time.Duration

	// changeMu protects onChangeCallbacks and subscriptions.
	changeMu          sync.RWMutex
	onChangeCallbacks []func(key, value string)
	subscriptions     []subscription
}

// Subscription represents an active subscription to settings changes.
// Call Unsubscribe when done to prevent goroutine leaks. It is safe to
// call Unsubscribe more than once.
type Subscription struct {
	id    uint64
	ch    <-chan ChangeEvent
	repo  *Repository
	once  sync.Once
	clean func() // teardown callback, set by Subscribe
}

// Events returns the read-only channel on which change events are delivered.
func (s *Subscription) Events() <-chan ChangeEvent {
	return s.ch
}

// Unsubscribe removes the subscription, draining and closing the underlying
// channel. It is safe to call more than once — subsequent calls are no-ops.
func (s *Subscription) Unsubscribe() {
	s.once.Do(s.clean)
}

// NewRepository creates a new settings repository with caching.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{
		pool:     pool,
		cache:    make(map[string]cacheEntry),
		cacheTTL: 30 * time.Second,
	}
}

// Subscribe returns a Subscription whose channel receives a ChangeEvent for
// every settings update written through Set or SetTx + InvalidateCache.
//
// Usage:
//
//	sub := repo.Subscribe()
//	defer sub.Unsubscribe()
//	for change := range sub.Events() {
//	    if change.Key == "discovery_interval" { … }
//	}
func (r *Repository) Subscribe() *Subscription {
	id := subIDCounter.Add(1)
	ch := make(chan ChangeEvent, 16)
	r.changeMu.Lock()
	r.subscriptions = append(r.subscriptions, subscription{id: id, ch: ch})
	r.changeMu.Unlock()
	sub := &Subscription{id: id, ch: ch, repo: r}
	sub.clean = func() { r.unsubscribe(id) }
	return sub
}

// unsubscribe removes and closes a subscription by its unique ID.
func (r *Repository) unsubscribe(id uint64) {
	r.changeMu.Lock()
	defer r.changeMu.Unlock()
	for i, sub := range r.subscriptions {
		if sub.id == id {
			r.subscriptions = append(r.subscriptions[:i], r.subscriptions[i+1:]...)
			// Drain and close in a goroutine in case a publisher is
			// currently blocked on this channel.
			go func(c chan ChangeEvent) {
				// Drain any remaining events from the channel before closing.
				//nolint:revive,gosec // intentional: empty block for channel drain
				for range c {
				}
				close(c)
			}(sub.ch)
			return
		}
	}
}

// RegisterOnChange registers a callback that is invoked (in a goroutine)
// whenever a setting is written through Set or the SetTx+InvalidateCache
// path. The callback receives the key and new value.
func (r *Repository) RegisterOnChange(fn func(key, value string)) {
	r.changeMu.Lock()
	r.onChangeCallbacks = append(r.onChangeCallbacks, fn)
	r.changeMu.Unlock()
}

// notifyChange delivers a settings change to all subscribers and callbacks.
// It is non-blocking: subscriber channels that are full are skipped, and
// callbacks are invoked in goroutines.
func (r *Repository) notifyChange(key, value string) {
	r.changeMu.RLock()
	subs := make([]subscription, len(r.subscriptions))
	copy(subs, r.subscriptions)
	callbacks := make([]func(key, value string), len(r.onChangeCallbacks))
	copy(callbacks, r.onChangeCallbacks)
	r.changeMu.RUnlock()

	change := ChangeEvent{Key: key, Value: value}
	for _, sub := range subs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					debuglog.Warn("settings: failed to send change event, channel closed", "recover", r)
				}
			}()
			select {
			case sub.ch <- change:
			default:
				// Subscriber is too slow; skip to avoid blocking the writer.
			}
		}()
	}
	for _, fn := range callbacks {
		go fn(key, value)
	}
}

// Get retrieves a setting value directly from the database.
func (r *Repository) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := r.pool.QueryRow(ctx, "SELECT value FROM settings WHERE key = $1", key).Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
}

// IsCached reports whether a setting for the given key is present in the
// cache and not expired. It does not modify the cache or access the database.
func (r *Repository) IsCached(key string) bool {
	r.mu.RLock()
	entry, ok := r.cache[key]
	r.mu.RUnlock()
	return ok && time.Now().Before(entry.expiresAt)
}

// GetWithDefault retrieves a setting from cache or database, returning defaultValue if not found.
func (r *Repository) GetWithDefault(ctx context.Context, key, defaultValue string) string {
	r.mu.RLock()
	if entry, ok := r.cache[key]; ok && time.Now().Before(entry.expiresAt) {
		r.mu.RUnlock()
		return entry.value
	}
	r.mu.RUnlock()

	var value string
	err := r.pool.QueryRow(ctx, "SELECT value FROM settings WHERE key = $1", key).Scan(&value)
	if err != nil {
		// An unset key (no row) is the normal "use the default" path and must
		// stay silent. A real DB error, though, silently reverts behaviour to
		// defaults (e.g. rate limits) — worth a Warn so it's not invisible.
		if !errors.Is(err, pgx.ErrNoRows) {
			debuglog.Warn("settings: DB read failed, falling back to default", "key", key, "error", err)
		}
		return defaultValue
	}

	r.mu.Lock()
	r.cache[key] = cacheEntry{value: value, expiresAt: time.Now().Add(r.cacheTTL)}
	r.mu.Unlock()

	return value
}

// GetChecked retrieves a setting from cache or database. Unlike GetWithDefault
// it distinguishes "key not set" (found=false, err=nil) from a real read
// failure (err != nil), so callers that change behaviour on a value (fleet
// role, write locks) can refuse to guess when the read failed. On a real DB
// error it does NOT emit the debuglog fallback warning that GetWithDefault
// does: the caller now sees the error and decides how to log it.
func (r *Repository) GetChecked(ctx context.Context, key string) (value string, found bool, err error) {
	r.mu.RLock()
	if entry, ok := r.cache[key]; ok && time.Now().Before(entry.expiresAt) {
		r.mu.RUnlock()
		return entry.value, true, nil
	}
	r.mu.RUnlock()

	var v string
	err = r.pool.QueryRow(ctx, "SELECT value FROM settings WHERE key = $1", key).Scan(&v)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", false, nil // key not set: the normal "unset" path
		}
		return "", false, err // real read failure: let the caller decide
	}

	r.mu.Lock()
	r.cache[key] = cacheEntry{value: v, expiresAt: time.Now().Add(r.cacheTTL)}
	r.mu.Unlock()

	return v, true, nil
}

// Set updates a setting and invalidates the cache.
func (r *Repository) Set(ctx context.Context, key, value string) error {
	r.mu.Lock()
	delete(r.cache, key)
	r.mu.Unlock()

	_, err := r.pool.Exec(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = now()
	`, key, value)
	if err != nil {
		return err
	}
	r.notifyChange(key, value)
	return nil
}

// SetMany upserts several settings in a single multi-row statement and
// invalidates their cache entries. It exists so callers that must persist a
// small fixed group of keys (e.g. the member-side fleet heartbeat) pay one DB
// round-trip instead of one per key, which keeps them comfortably inside a
// caller's request timeout even when the database is briefly slow (a
// simultaneous multi-container restart, say). Semantics otherwise match Set:
// no allowlist gate, cache evicted before the write, subscribers notified
// after it commits. An empty slice is a no-op.
func (r *Repository) SetMany(ctx context.Context, kvs [][2]string) error {
	if len(kvs) == 0 {
		return nil
	}

	// Evict before the write, exactly as Set does, so a read racing the write
	// falls through to the DB rather than serving a stale cached value.
	r.mu.Lock()
	for _, kv := range kvs {
		delete(r.cache, kv[0])
	}
	r.mu.Unlock()

	var sb strings.Builder
	sb.WriteString("INSERT INTO settings (key, value, updated_at) VALUES ")
	args := make([]any, 0, len(kvs)*2)
	for i, kv := range kvs {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "($%d, $%d, now())", i*2+1, i*2+2)
		args = append(args, kv[0], kv[1])
	}
	sb.WriteString(" ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()")

	if _, err := r.pool.Exec(ctx, sb.String(), args...); err != nil {
		return err
	}
	// The row write is atomic (one statement); the notifications are not. We fan
	// out one change event per key after the commit succeeds, exactly as a loop
	// of Set calls would, so a subscriber may observe the keys arrive one at a
	// time. This is fine for the fleet-heartbeat keys, whose consumers read each
	// independently; a caller needing an all-or-nothing notification should not
	// use SetMany.
	for _, kv := range kvs {
		r.notifyChange(kv[0], kv[1])
	}
	return nil
}

// SetTx updates a setting within an existing transaction.
func (r *Repository) SetTx(ctx context.Context, tx pgx.Tx, key, value string) error {
	if !AllowedSettings[key] {
		debuglog.Warn("settings: rejected setting not in allowlist", "key", key)
		return fmt.Errorf("setting %q is not in allowlist", key)
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = $2, updated_at = now()
	`, key, value)
	return err
}

// DeleteKeysTx removes the given settings keys from the database within an
// existing transaction. After deletion, callers that read the setting will
// fall through to their hardcoded Go default.
func (r *Repository) DeleteKeysTx(ctx context.Context, tx pgx.Tx, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	for _, key := range keys {
		if !AllowedSettings[key] {
			return fmt.Errorf("setting %q is not in allowlist", key)
		}
	}
	_, err := tx.Exec(ctx, `DELETE FROM settings WHERE key = ANY($1)`, keys)
	if err != nil {
		return err
	}
	// Cache eviction is intentionally NOT done here. The caller must
	// evict after the surrounding transaction commits; evicting before
	// commit would let concurrent reads repopulate the cache with the
	// pre-delete DB value on tx rollback.
	return nil
}

// InvalidateCache removes a key from the cache and notifies subscribers.
// For reset-to-default flows where the key was deleted from the DB, use
// NotifyDeleted instead to avoid a wasteful DB query.
func (r *Repository) InvalidateCache(key string) {
	r.mu.Lock()
	delete(r.cache, key)
	r.mu.Unlock()
	// InvalidateCache is called after SetTx commits. We don't know the
	// committed value here, so we do a best-effort lookup to notify
	// subscribers. If the lookup fails (e.g. the key was deleted), we
	// still notify with an empty value so that listeners reset.
	val := r.GetWithDefault(context.Background(), key, "")
	r.notifyChange(key, val)
}

// NotifyDeleted removes a key from the cache and notifies subscribers with
// an empty value. Use this instead of InvalidateCache when the key was
// deleted from the database (reset-to-default) to avoid a redundant DB
// lookup — we already know the value is gone.
func (r *Repository) NotifyDeleted(key string) {
	r.mu.Lock()
	delete(r.cache, key)
	r.mu.Unlock()
	r.notifyChange(key, "")
}

// WarmCache preloads all settings from the database into the in-memory cache.
// Without this, settings are populated lazily on first read and expire after
// cacheTTL (30s), causing periodic cache misses on the hot path.
func (r *Repository) WarmCache(ctx context.Context) {
	all, err := r.GetAll(ctx)
	if err != nil {
		debuglog.Warn("settings: failed to warm cache", "error", err)
		return
	}
	r.mu.Lock()
	for key, value := range all {
		r.cache[key] = cacheEntry{value: value, expiresAt: time.Now().Add(r.cacheTTL)}
	}
	r.mu.Unlock()
	debuglog.Info("settings: warmed cache", "count", len(all))
}

// GetAll retrieves all settings as a key-value map.
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
			return nil, fmt.Errorf("settings scan error: %w", err)
		}
		result[key] = value
	}
	return result, nil
}

// GetBool retrieves a setting and parses it as a boolean.
func (r *Repository) GetBool(ctx context.Context, key string, defaultValue bool) bool {
	val := r.GetWithDefault(ctx, key, strconv.FormatBool(defaultValue))
	b, err := strconv.ParseBool(val)
	if err != nil {
		debuglog.Warn("settings: failed to parse as bool, using default", "key", key, "default", defaultValue, "error", err)
		return defaultValue
	}
	return b
}

// GetDuration retrieves a setting and parses it as a time.Duration.
// Handles Go-compatible durations and the "d" suffix (e.g. "1d" = 24h)
// which may be present from older frontend code.
func (r *Repository) GetDuration(ctx context.Context, key string, defaultValue time.Duration) time.Duration {
	val := r.GetWithDefault(ctx, key, defaultValue.String())
	d, err := parseDuration(val)
	if err != nil {
		debuglog.Warn("settings: failed to parse as duration, using default", "key", key, "default", defaultValue, "error", err)
		return defaultValue
	}
	return d
}

// parseDuration parses a Go time.Duration string and also accepts the "d" suffix
// for day units (1d = 24h0m0s), which Go's time.ParseDuration does not support.
func parseDuration(s string) (time.Duration, error) {
	days := 0
	if i := strings.IndexByte(s, 'd'); i >= 0 {
		dayStr := s[:i]
		n, err := strconv.Atoi(dayStr)
		if err != nil {
			return 0, fmt.Errorf("invalid day suffix in duration %q: %w", s, err)
		}
		days = n
		s = s[i+1:]
	}
	if s == "" {
		return time.Duration(days) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, err
	}
	return d + time.Duration(days)*24*time.Hour, nil
}

// GetFloat retrieves a setting and parses it as a float64.
func (r *Repository) GetFloat(ctx context.Context, key string, defaultValue float64) float64 {
	val := r.GetWithDefault(ctx, key, strconv.FormatFloat(defaultValue, 'f', -1, 64))
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		debuglog.Warn("settings: failed to parse as float, using default", "key", key, "default", defaultValue, "error", err)
		return defaultValue
	}
	return f
}

// GetInt retrieves a setting and parses it as an int.
func (r *Repository) GetInt(ctx context.Context, key string, defaultValue int) int {
	val := r.GetWithDefault(ctx, key, strconv.Itoa(defaultValue))
	i, err := strconv.Atoi(val)
	if err != nil {
		debuglog.Warn("settings: failed to parse as int, using default", "key", key, "default", defaultValue, "error", err)
		return defaultValue
	}
	return i
}
