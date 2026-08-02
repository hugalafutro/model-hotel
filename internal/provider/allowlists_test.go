package provider

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// readAllowList returns the two facts that now mean opposite things, read in
// SQL: whether the column is SQL NULL (unrestricted) and its members. Never
// scanned straight into a []string, because pgx yields a nil slice for both NULL
// and '{}' and that is the exact distinction under test.
func readAllowList(t *testing.T, query, subject string) (isNull bool, card int, members []string) {
	t.Helper()
	if err := testDB.Pool().QueryRow(context.Background(), query, subject).Scan(&isNull, &card, &members); err != nil {
		t.Fatalf("reading allowed_providers for %q: %v", subject, err)
	}
	return isNull, card, members
}

const pruneTestKeyQuery = `
	SELECT allowed_providers IS NULL,
	       coalesce(cardinality(allowed_providers), -1),
	       coalesce(allowed_providers, '{}'::text[])
	  FROM virtual_keys WHERE key_hash = $1`

const pruneTestUserQuery = `
	SELECT allowed_providers IS NULL,
	       coalesce(cardinality(allowed_providers), -1),
	       coalesce(allowed_providers, '{}'::text[])
	  FROM users WHERE username = $1`

// seedAllowListRows inserts one restricted key, one restricted user and one
// unrestricted example of each, all scoped to the given suffix so parallel
// packages cannot collide, and registers their cleanup.
func seedAllowListRows(t *testing.T, suffix string, allowed []string) {
	t.Helper()
	ctx := context.Background()
	pool := testDB.Pool()

	if _, err := pool.Exec(ctx, `
		INSERT INTO virtual_keys (name, key_hash, key_preview, allowed_providers) VALUES
			($1, $1, 'sk-...pr', $3),
			($2, $2, 'sk-...op', NULL)`,
		"restricted-"+suffix, "open-"+suffix, allowed); err != nil {
		t.Fatalf("seed virtual keys: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO users (username, password_hash, allowed_providers) VALUES
			($1, 'x', $3),
			($2, 'x', NULL)`,
		"capped-"+suffix, "uncapped-"+suffix, allowed); err != nil {
		t.Fatalf("seed users: %v", err)
	}

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM virtual_keys WHERE key_hash IN ($1, $2)`, "restricted-"+suffix, "open-"+suffix)
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM users WHERE username IN ($1, $2)`, "capped-"+suffix, "uncapped-"+suffix)
	})
}

// The core contract: the named ids come out of both columns, everything else
// stays, and a list emptied by the prune is left as '{}' rather than NULL. The
// last part is the one that matters most, since NULL would mean "every provider"
// and hand a deliberately restricted key the run of the gateway.
func TestPruneAllowLists_RemovesOnlyTheNamedIDs(t *testing.T) {
	ctx := context.Background()
	keep := uuid.New().String()
	doomed := uuid.New().String()

	seedAllowListRows(t, "multi", []string{keep, doomed})
	seedAllowListRows(t, "solo", []string{doomed})

	if err := PruneAllowLists(ctx, testDB.Pool(), []string{doomed}); err != nil {
		t.Fatalf("PruneAllowLists: %v", err)
	}

	for _, tc := range []struct {
		what    string
		query   string
		subject string
	}{
		{"virtual key", pruneTestKeyQuery, "restricted-multi"},
		{"user", pruneTestUserQuery, "capped-multi"},
	} {
		isNull, card, members := readAllowList(t, tc.query, tc.subject)
		if isNull {
			t.Fatalf("%s: ESCALATION: the allow-list is now NULL, i.e. unrestricted", tc.what)
		}
		if card != 1 || members[0] != keep {
			t.Fatalf("%s: allow-list = %v (cardinality %d), want just [%s]", tc.what, members, card, keep)
		}
	}

	for _, tc := range []struct {
		what    string
		query   string
		subject string
	}{
		{"virtual key", pruneTestKeyQuery, "restricted-solo"},
		{"user", pruneTestUserQuery, "capped-solo"},
	} {
		isNull, card, _ := readAllowList(t, tc.query, tc.subject)
		if isNull {
			t.Fatalf("%s: ESCALATION: pruning the last entry produced NULL, not '{}'", tc.what)
		}
		if card != 0 {
			t.Fatalf("%s: allow-list cardinality = %d, want 0", tc.what, card)
		}
	}

	// The other direction, and a silent lockout if it ever broke: an
	// unrestricted row must not acquire a restriction. The WHERE clause is an
	// array-overlap test and NULL && anything is NULL rather than true, so these
	// rows are never selected.
	for _, tc := range []struct {
		what    string
		query   string
		subject string
	}{
		{"virtual key", pruneTestKeyQuery, "open-multi"},
		{"user", pruneTestUserQuery, "uncapped-multi"},
	} {
		isNull, card, _ := readAllowList(t, tc.query, tc.subject)
		if !isNull {
			t.Fatalf("%s: LOCKOUT: an unrestricted row was given an allow-list (cardinality %d)", tc.what, card)
		}
	}
}

// Every config-sync import that deletes no providers reaches this with an empty
// set. It must be a true no-op rather than a statement that rewrites every
// allow-list row in both tables on every sync.
func TestPruneAllowLists_NoIDsIsANoOp(t *testing.T) {
	ctx := context.Background()
	keep := uuid.New().String()
	seedAllowListRows(t, "noop", []string{keep})

	if err := PruneAllowLists(ctx, testDB.Pool(), nil); err != nil {
		t.Fatalf("PruneAllowLists with no ids: %v", err)
	}

	isNull, card, members := readAllowList(t, pruneTestKeyQuery, "restricted-noop")
	if isNull || card != 1 || members[0] != keep {
		t.Fatalf("allow-list = %v (cardinality %d, null %v), want it untouched as [%s]", members, card, isNull, keep)
	}
}

// failingExecer fails the nth Exec, so both statements' error paths can be
// reached without breaking the real database.
type failingExecer struct {
	calls  int
	failAt int
}

func (f *failingExecer) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	f.calls++
	if f.calls == f.failAt {
		return pgconn.CommandTag{}, errors.New("simulated database failure")
	}
	return pgconn.CommandTag{}, nil
}

// A swallowed error here would be worse than a loud failure: the caller runs
// this inside the same transaction as the provider DELETE precisely so that a
// prune that did not happen aborts the delete. Reporting success would commit
// the delete and leave the ids dangling, which is the state all of this exists
// to prevent.
func TestPruneAllowLists_PropagatesDatabaseErrors(t *testing.T) {
	for _, tt := range []struct {
		name   string
		failAt int
	}{
		{"virtual_keys statement fails", 1},
		{"users statement fails", 2},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db := &failingExecer{failAt: tt.failAt}
			err := PruneAllowLists(context.Background(), db, []string{uuid.New().String()})
			if err == nil {
				t.Fatal("PruneAllowLists reported success despite a failing statement")
			}
			if db.calls != tt.failAt {
				t.Fatalf("kept going after the failure: %d statements ran, want %d", db.calls, tt.failAt)
			}
		})
	}
}

// Delete is the admin path, and the reason it now opens a transaction. This
// covers the wiring rather than the SQL: that the repository actually calls the
// prune, so a provider removed through the dashboard cannot leave its id behind.
func TestDelete_PrunesAllowLists(t *testing.T) {
	ctx := context.Background()
	repo := newTestRepo(t)

	created, err := repo.Create(ctx, CreateProviderRequest{
		Name:    "prune-delete-" + uuid.New().String()[:8],
		BaseURL: "https://prune.example",
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	seedAllowListRows(t, "viadelete", []string{created.ID.String()})

	if err := repo.Delete(ctx, created.ID); err != nil {
		t.Fatalf("delete provider: %v", err)
	}

	isNull, card, _ := readAllowList(t, pruneTestKeyQuery, "restricted-viadelete")
	if isNull {
		t.Fatal("ESCALATION: the key's allow-list is NULL after the provider delete")
	}
	if card != 0 {
		t.Fatalf("allow-list cardinality = %d, want 0: Delete did not prune", card)
	}
	isNull, card, _ = readAllowList(t, pruneTestUserQuery, "capped-viadelete")
	if isNull {
		t.Fatal("ESCALATION: the account cap is NULL after the provider delete")
	}
	if card != 0 {
		t.Fatalf("cap cardinality = %d, want 0: Delete did not prune the users column", card)
	}
}
