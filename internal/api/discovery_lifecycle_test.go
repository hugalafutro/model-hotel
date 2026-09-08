package api

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/provider"
)

// TestDiscoveryService_BuiltOnce pins the whole point of the lifecycle change:
// the handler builds one DiscoveryService and hands the same one to every
// caller. A fresh service per call throws away the transport's idle-connection
// pool and resets the quota circuit breaker, so the breaker could never reach
// its threshold in a running process.
func TestDiscoveryService_BuiltOnce(t *testing.T) {
	h := newTestHandler(t)

	var built atomic.Int64
	h.newDiscovery = func() *provider.DiscoveryService {
		built.Add(1)
		return provider.NewDiscoveryServiceWithHTTPClient(&http.Client{})
	}

	first := h.discoveryService()
	for range 3 {
		if got := h.discoveryService(); got != first {
			t.Fatal("discoveryService() must return the same instance every time")
		}
	}
	// Request paths go through the same accessor, so a served pass must not
	// build another one either.
	h.PollQuotasOnce(context.Background())
	h.PollQuotasOnce(context.Background())
	if got := h.DiscoveryService(); got != first {
		t.Fatal("the exported accessor must return the same instance")
	}

	if got := built.Load(); got != 1 {
		t.Fatalf("got %d constructions, want exactly 1", got)
	}
}

// failingQuotaHandler returns a handler with one enabled quota-capable provider
// whose upstream always refuses, plus the counter of upstream attempts made.
// The error is deliberately not a network error: a transient one is retried, so
// the count would stop reading as one attempt per pass.
func failingQuotaHandler(t *testing.T, name string) (*Handler, uuid.UUID, *atomic.Int64) {
	t.Helper()
	h := newTestHandler(t)
	id := insertQuotaPollProvider(t, h.dbPool.Pool(), name, "https://api.nano-gpt.com/v1", true)

	attempts := &atomic.Int64{}
	h.newDiscovery = func() *provider.DiscoveryService {
		ds := provider.NewDiscoveryServiceWithHTTPClient(&http.Client{
			Transport: &mockTransport{roundTripFunc: func(_ *http.Request) (*http.Response, error) {
				attempts.Add(1)
				return nil, errors.New("upstream refused the quota fetch")
			}},
		})
		ds.SetRetryBaseDelay(time.Millisecond)
		return ds
	}
	return h, id, attempts
}

// TestPollQuotasOnce_BreakerOpensAcrossPasses is the production symptom the
// single service fixes: consecutive quota failures only add up while the
// breaker state survives the pass. Five failing passes reach the threshold, so
// the sixth is short-circuited without touching the upstream at all.
func TestPollQuotasOnce_BreakerOpensAcrossPasses(t *testing.T) {
	h, _, attempts := failingQuotaHandler(t, "nanogpt-breaker")

	for range 5 {
		h.PollQuotasOnce(context.Background())
	}
	opened := attempts.Load()
	if opened != 5 {
		t.Fatalf("got %d upstream attempts over 5 passes, want 5 (one per pass)", opened)
	}

	h.PollQuotasOnce(context.Background())

	if got := attempts.Load(); got != opened {
		t.Fatalf("the sixth pass must be short-circuited by the open breaker, got %d attempts (was %d)", got, opened)
	}
}

// TestPollQuotasOnce_PrunesBreakersOfRemovedProviders verifies each pass hands
// its provider list to the breaker map. A deleted provider's circuit state
// would otherwise be retained for the life of the process, and would still be
// open if the same id ever came back.
func TestPollQuotasOnce_PrunesBreakersOfRemovedProviders(t *testing.T) {
	h, id, attempts := failingQuotaHandler(t, "nanogpt-prune")

	for range 5 {
		h.PollQuotasOnce(context.Background())
	}
	opened := attempts.Load()

	// The provider goes away, and a pass with it gone prunes its entry.
	deleteProvider(t, h.dbPool.Pool(), id)
	h.PollQuotasOnce(context.Background())

	// Same id back again: with the entry pruned the upstream is tried afresh,
	// with it retained the still-open circuit would refuse to call at all.
	restoreProvider(t, h.dbPool.Pool(), id, "nanogpt-prune-again", "https://api.nano-gpt.com/v1")
	h.PollQuotasOnce(context.Background())

	if got := attempts.Load(); got != opened+1 {
		t.Fatalf("got %d upstream attempts, want %d: a pruned breaker must not short-circuit a provider that came back", got, opened+1)
	}
}

func deleteProvider(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `DELETE FROM providers WHERE id = $1`, id); err != nil {
		t.Fatalf("delete provider: %v", err)
	}
}

func restoreProvider(t *testing.T, pool *pgxpool.Pool, id uuid.UUID, name, baseURL string) {
	t.Helper()
	ek, kn, ks := encryptTestKey(t, "test-api-key", testMasterKey)
	_, err := pool.Exec(context.Background(), `
		INSERT INTO providers (id, name, base_url, encrypted_key, key_nonce, key_salt, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, true, now(), now())`,
		id, name, baseURL, ek, kn, ks)
	if err != nil {
		t.Fatalf("restore provider: %v", err)
	}
}
