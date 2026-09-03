package audit

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

func TestEntityKindOf(t *testing.T) {
	cases := map[string]string{
		"/api/models/{id}/test":     "models",
		"/api/failover-groups/{id}": "failover-groups",
		"/api/users/{id}/password":  "users",
		"/api/settings":             "settings",
		// A spelled-out parameter names its own family, so a provider acted on
		// from another route still resolves to its name.
		"/api/discovery/{provider_id}/dismiss":                     "providers",
		"/api/failover-groups/circuit-breaker/{provider_id}/reset": "providers",
		// A plain {id} wins over a spelled-out parameter beside it.
		"/api/failover-groups/{id}/circuit-breaker/{provider_id}/reset": "failover-groups",
		// The last spelled-out parameter is the entity, as in the middleware.
		"/api/discovery/{model_id}/x/{provider_id}": "providers",
		// A spelled-out parameter no family claims falls back to the route's own
		// family, so a same-family route still resolves.
		"/api/virtual-keys/{vk_id}/rotate": "virtual-keys",
		"/models/{id}":                     "", // unmounted pattern is not the real route
		"":                                 "",
	}
	for route, want := range cases {
		if got := entityKindOf(route); got != want {
			t.Errorf("entityKindOf(%q) = %q, want %q", route, got, want)
		}
	}
}

func TestResolveEntityNames(t *testing.T) {
	rec := newRecorder(t, nil)
	ctx := context.Background()
	pool := testDB.Pool()

	cleanup := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM model_failover_groups WHERE display_model = 'hotel/resolve-me'`)
		_, _ = pool.Exec(ctx, `DELETE FROM virtual_keys WHERE name = 'resolve-vk'`)
		_, _ = pool.Exec(ctx, `DELETE FROM models WHERE model_id IN ('bare-model', 'named-model')`)
		_, _ = pool.Exec(ctx, `DELETE FROM providers WHERE name = 'resolve-provider'`)
	}
	cleanup()
	t.Cleanup(cleanup)

	var groupID, vkID, bareModelID, namedModelID, providerID string
	if err := pool.QueryRow(ctx,
		`INSERT INTO providers (name, base_url, encrypted_key, key_nonce, key_salt) VALUES ('resolve-provider', 'https://example.invalid', ''::bytea, ''::bytea, ''::bytea) RETURNING id::text`,
	).Scan(&providerID); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO model_failover_groups (display_model) VALUES ('hotel/resolve-me') RETURNING id::text`,
	).Scan(&groupID); err != nil {
		t.Fatalf("seed group: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO virtual_keys (name, key_hash, key_preview) VALUES ('resolve-vk', 'hash-resolve-vk', 'sk-***') RETURNING id::text`,
	).Scan(&vkID); err != nil {
		t.Fatalf("seed virtual key: %v", err)
	}
	// display_name NULL falls back to model_id; a set display_name wins.
	if err := pool.QueryRow(ctx,
		`INSERT INTO models (model_id) VALUES ('bare-model') RETURNING id::text`,
	).Scan(&bareModelID); err != nil {
		t.Fatalf("seed bare model: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`INSERT INTO models (model_id, display_name) VALUES ('named-model', 'Nice Name') RETURNING id::text`,
	).Scan(&namedModelID); err != nil {
		t.Fatalf("seed named model: %v", err)
	}

	entries := []Entry{
		{Route: "/api/failover-groups/{id}", EntityID: groupID},
		{Route: "/api/virtual-keys/{id}", EntityID: vkID},
		{Route: "/api/models/{id}/test", EntityID: bareModelID},
		{Route: "/api/models/{id}", EntityID: namedModelID},
		// A provider acted on through a discovery verdict resolves by name.
		{Route: "/api/discovery/{provider_id}/dismiss", EntityID: providerID},
		// Deleted entity: a UUID with no row stays unresolved.
		{Route: "/api/providers/{id}", EntityID: uuid.NewString()},
		// Non-UUID id must not poison the batch for its family.
		{Route: "/api/users/{id}", EntityID: "not-a-uuid"},
		// Unmapped family and entity-less entry stay untouched.
		{Route: "/api/auth/webauthn/credentials/{id}", EntityID: "AAAA"},
		{Route: "/api/settings"},
	}
	rec.ResolveEntityNames(ctx, entries)

	want := []string{"hotel/resolve-me", "resolve-vk", "bare-model", "Nice Name", "resolve-provider", "", "", "", ""}
	for i, w := range want {
		if entries[i].EntityName != w {
			t.Errorf("entries[%d] (%s) EntityName = %q, want %q", i, entries[i].Route, entries[i].EntityName, w)
		}
	}

	// A batch where nothing resolves (only a deleted entity) leaves everything
	// untouched.
	gone := []Entry{{Route: "/api/models/{id}", EntityID: uuid.NewString()}}
	rec.ResolveEntityNames(ctx, gone)
	if gone[0].EntityName != "" {
		t.Errorf("deleted-only batch EntityName = %q, want empty", gone[0].EntityName)
	}

	// Best-effort on lookup failure: a dead context fails the query, and the
	// listing must survive with names simply left empty.
	dead, cancel := context.WithCancel(ctx)
	cancel()
	failed := []Entry{{Route: "/api/models/{id}", EntityID: namedModelID}}
	rec.ResolveEntityNames(dead, failed)
	if failed[0].EntityName != "" {
		t.Errorf("failed-lookup EntityName = %q, want empty", failed[0].EntityName)
	}
}
