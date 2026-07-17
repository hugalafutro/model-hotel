package user

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// TestResolveSSOIdentity exercises the trust-on-first-use provider binding that
// closes the cross-provider account-takeover hole: an account locks to the
// first (provider, subject) that logs in, and any later identity is denied even
// when the verified email matches.
func TestResolveSSOIdentity(t *testing.T) {
	repo := NewRepository(testDB.Pool())
	ctx := context.Background()

	email := "worker-" + uuid.NewString() + "@example.com"
	u := mustCreate(t, repo, "worker-"+uuid.NewString(), &email, RoleUser, []string{"chat"})

	// Unknown email: not found.
	if _, _, err := repo.ResolveSSOIdentity(ctx, "oidc", "sub-1", "nobody@example.com"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown email: got %v, want ErrNotFound", err)
	}

	// First login binds the identity, reports bound=true, returns the account.
	got, bound, err := repo.ResolveSSOIdentity(ctx, "oidc", "iss#abc", email)
	if err != nil {
		t.Fatalf("first login: %v", err)
	}
	if !bound {
		t.Fatalf("first login should report a new binding (bound=true)")
	}
	if got.ID != u.ID {
		t.Fatalf("bound wrong account: %s want %s", got.ID, u.ID)
	}
	if got.SSOProvider == nil || *got.SSOProvider != "oidc" || got.SSOSubject == nil || *got.SSOSubject != "iss#abc" {
		t.Fatalf("binding not recorded on returned user: %+v", got.SSOProvider)
	}
	// The binding persisted.
	if reload, _ := repo.Get(ctx, u.ID); reload.SSOProvider == nil || *reload.SSOProvider != "oidc" {
		t.Fatalf("binding not persisted: %+v", reload.SSOProvider)
	}

	// Same identity re-login: allowed, and NOT reported as a fresh binding.
	if _, bound, err := repo.ResolveSSOIdentity(ctx, "oidc", "iss#abc", email); err != nil || bound {
		t.Fatalf("same-identity re-login: err=%v bound=%v (want nil, false)", err, bound)
	}

	// Different provider, same email: denied.
	if _, _, err := repo.ResolveSSOIdentity(ctx, "github", "424242", email); !errors.Is(err, ErrSSOMismatch) {
		t.Fatalf("cross-provider: got %v, want ErrSSOMismatch", err)
	}
	// Same provider, different subject: denied.
	if _, _, err := repo.ResolveSSOIdentity(ctx, "oidc", "iss#other", email); !errors.Is(err, ErrSSOMismatch) {
		t.Fatalf("different subject: got %v, want ErrSSOMismatch", err)
	}
}

// A disabled account never binds or authenticates, matching GetByEmail's
// enabled-only contract.
func TestResolveSSOIdentity_DisabledDenied(t *testing.T) {
	repo := NewRepository(testDB.Pool())
	ctx := context.Background()

	email := "off-" + uuid.NewString() + "@example.com"
	u := mustCreate(t, repo, "off-"+uuid.NewString(), &email, RoleUser, []string{"chat"})
	if _, err := repo.Update(ctx, u.ID, u.Username, u.DisplayName, &email, u.Role, u.Grants, false, Limits{}); err != nil {
		t.Fatalf("disable: %v", err)
	}
	if _, _, err := repo.ResolveSSOIdentity(ctx, "oidc", "iss#abc", email); !errors.Is(err, ErrNotFound) {
		t.Fatalf("disabled account: got %v, want ErrNotFound", err)
	}
}

// The same external identity cannot be pointed at a second account: the unique
// index rejects the bind, surfaced as ErrSSOMismatch rather than a raw DB error.
func TestResolveSSOIdentity_IdentityBindsAtMostOneAccount(t *testing.T) {
	repo := NewRepository(testDB.Pool())
	ctx := context.Background()

	emailA := "a-" + uuid.NewString() + "@example.com"
	emailB := "b-" + uuid.NewString() + "@example.com"
	mustCreate(t, repo, "a-"+uuid.NewString(), &emailA, RoleUser, []string{"chat"})
	mustCreate(t, repo, "b-"+uuid.NewString(), &emailB, RoleUser, []string{"chat"})

	// Bind the GitHub identity 42 to account A.
	if _, _, err := repo.ResolveSSOIdentity(ctx, "github", "42", emailA); err != nil {
		t.Fatalf("bind A: %v", err)
	}
	// The same GitHub identity now asserting account B's email must be denied.
	if _, _, err := repo.ResolveSSOIdentity(ctx, "github", "42", emailB); !errors.Is(err, ErrSSOMismatch) {
		t.Fatalf("identity reuse across accounts: got %v, want ErrSSOMismatch", err)
	}
}
