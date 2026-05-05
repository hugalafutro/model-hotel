package settings

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/debuglog"
)

// AllowedSettings is the allowlist of keys the API will accept.
// Any key not in this set is rejected by UpdateSettings.
var AllowedSettings = map[string]bool{
	"discovery_interval":           true,
	"discovery_on_startup":         true,
	"discovery_on_provider_create": true,
	"log_retention":                true,
	"stale_request_timeout":        true,
	"failover_on_rate_limit":       true,
	"circuit_breaker_enabled":      true,
	"circuit_breaker_threshold":    true,
	"circuit_breaker_cooldown":     true,
	"rate_limit_enabled":           true,
	"rate_limit_rps":               true,
	"rate_limit_burst":             true,
	"theme":                        true,
	"ui_style":                     true,
	"accent_color":                 true,
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
var subIDCounter uint64

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
	id := atomic.AddUint64(&subIDCounter, 1)
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
	r.cache[key] = cacheEntry{value: value, expiresAt: time.Now().Add(r.cacheTTL)}
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
	if err != nil {
		return err
	}
	r.notifyChange(key, value)
	return nil
}

func (r *Repository) SetTx(ctx context.Context, tx pgx.Tx, key string, value string) error {
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

func (r *Repository) GetBool(ctx context.Context, key string, defaultValue bool) bool {
	val := r.GetWithDefault(ctx, key, strconv.FormatBool(defaultValue))
	b, err := strconv.ParseBool(val)
	if err != nil {
		debuglog.Warn("settings: failed to parse as bool, using default", "key", key, "default", defaultValue, "error", err)
		return defaultValue
	}
	return b
}

func (r *Repository) GetDuration(ctx context.Context, key string, defaultValue time.Duration) time.Duration {
	val := r.GetWithDefault(ctx, key, defaultValue.String())
	d, err := time.ParseDuration(val)
	if err != nil {
		debuglog.Warn("settings: failed to parse as duration, using default", "key", key, "default", defaultValue, "error", err)
		return defaultValue
	}
	return d
}

func (r *Repository) GetFloat(ctx context.Context, key string, defaultValue float64) float64 {
	val := r.GetWithDefault(ctx, key, strconv.FormatFloat(defaultValue, 'f', -1, 64))
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		debuglog.Warn("settings: failed to parse as float, using default", "key", key, "default", defaultValue, "error", err)
		return defaultValue
	}
	return f
}

func (r *Repository) GetInt(ctx context.Context, key string, defaultValue int) int {
	val := r.GetWithDefault(ctx, key, strconv.Itoa(defaultValue))
	i, err := strconv.Atoi(val)
	if err != nil {
		debuglog.Warn("settings: failed to parse as int, using default", "key", key, "default", defaultValue, "error", err)
		return defaultValue
	}
	return i
}
