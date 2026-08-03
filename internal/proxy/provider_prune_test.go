package proxy

import (
	"context"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/provider"
	"github.com/hugalafutro/model-hotel/internal/virtualkey"
)

// Behaviour preservation for the allow-list pruning. Deleting a provider now
// prunes its UUID out of every allow-list, and a key scoped solely to that
// provider is left holding '{}' rather than a dangling UUID. That rewrite must
// not change what the proxy does, in either direction:
//
//   - before the delete the key names a provider that cannot serve the requested
//     model, so the request is refused;
//   - after the delete the key names nothing at all, and the request must still
//     be refused. If pgx scanned '{}' back as a nil *[]string, or if the pruning
//     had produced NULL, effectiveAllowedProviders would see "unrestricted" and
//     this request would succeed. That is the escalation the branch exists to
//     close, so it is asserted through the real middleware and handler rather
//     than by re-reading the column.
func TestChatCompletions_KeyPrunedToNothingStaysDenied(t *testing.T) {
	env := newTestProxyHandler(t)
	defer env.Upstream.Close()
	defer env.Handler.upstreamTransport.CloseIdleConnections()

	ctx := context.Background()
	pool := testDB.Pool()

	// A second provider that never serves anything: it exists only to be named by
	// the key and then deleted. Inserted directly because nothing about its
	// upstream behaviour matters, only its id.
	doomedName := "prune-doomed-" + uuid.New().String()[:8]
	var doomedID uuid.UUID
	if err := pool.QueryRow(ctx,
		`INSERT INTO providers (name, base_url) VALUES ($1, 'https://doomed.invalid') RETURNING id`,
		doomedName).Scan(&doomedID); err != nil {
		t.Fatalf("seed doomed provider: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM providers WHERE id = $1`, doomedID) })

	repo := virtualkey.NewRepository(pool)
	plaintext := "prune-key-" + uuid.New().String()[:8]
	allowed := []string{doomedID.String()}
	created, err := repo.Create(ctx, plaintext, virtualkey.Hash(plaintext), "sk-...pr", nil, nil, nil, &allowed, nil, nil)
	if err != nil {
		t.Fatalf("seed restricted key: %v", err)
	}
	t.Cleanup(func() { _ = repo.Delete(context.Background(), created.ID) })

	// Before: the key is restricted to a provider that does not serve this model.
	if w := doCappedRequest(t, env, plaintext); w.Code != http.StatusForbidden {
		t.Fatalf("before the delete: status = %d, want 403; body %s", w.Code, w.Body.String())
	}

	// The admin delete path, which is one of the two call sites that prune.
	// A raw `DELETE FROM providers` would not prune at all, so it would leave the
	// dangling id in place and test nothing.
	if err := provider.NewRepository(pool).Delete(ctx, doomedID); err != nil {
		t.Fatalf("delete doomed provider: %v", err)
	}

	// Pin the stored shape in SQL as well, so a 403 caused by something other
	// than an empty-but-present allow-list cannot pass this test. NULL-ness and
	// cardinality are read separately: they now mean opposite things and pgx
	// yields a nil slice for both.
	var isNull bool
	var card int
	if err := pool.QueryRow(ctx,
		`SELECT allowed_providers IS NULL, coalesce(cardinality(allowed_providers), -1)
		   FROM virtual_keys WHERE id = $1`, created.ID).Scan(&isNull, &card); err != nil {
		t.Fatalf("reading the pruned allow-list: %v", err)
	}
	if isNull {
		t.Fatal("ESCALATION: pruning left the allow-list NULL, i.e. unrestricted")
	}
	if card != 0 {
		t.Fatalf("allow-list cardinality after pruning = %d, want 0", card)
	}

	// After: still refused, and for the same reason.
	w := doCappedRequest(t, env, plaintext)
	if w.Code != http.StatusForbidden {
		t.Fatalf("after the delete: status = %d, want 403; body %s", w.Code, w.Body.String())
	}
}
