package frontdesk

import (
	"context"
	"testing"
)

// repointTargetsCurrentPrimary decides whether pointing the fleet primary at a
// candidate would land on the host that is already the primary, reached under a
// different URL. It answers by asking the candidate's own HA self-report, which
// needs that candidate's admin token.
//
// A member with no stored token cannot be probed, so the answer is "no" rather
// than an error: the admin-token gate on the repoint itself is the protection,
// and refusing a legitimate repoint because one member happens to have no token
// stored would be the worse failure. The behavioural pair below (a candidate
// that DOES self-report as primary answers true) is what stops that arm from
// being rewritten into an unconditional false.
func TestRepointTargetsCurrentPrimary_TokenlessCandidate(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	current, err := store.CreateMember(ctx, "current-primary", "http://127.0.0.1:9101", "tok-current")
	if err != nil {
		t.Fatalf("CreateMember(current): %v", err)
	}
	// The candidate self-reports as the fleet primary, so the only thing that
	// can hold the answer back is the missing token.
	host := systemMemberServer(t, true)
	tokenless, err := store.CreateMember(ctx, "tokenless", host.URL, "")
	if err != nil {
		t.Fatalf("CreateMember(tokenless): %v", err)
	}
	if err := store.SetAutoSync(ctx, true, current.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}

	same, err := srv.repointTargetsCurrentPrimary(ctx, tokenless.ID)
	if err != nil {
		t.Fatalf("repointTargetsCurrentPrimary: %v", err)
	}
	if same {
		t.Error("a member with no stored token cannot be probed, so it must not be reported as the current primary")
	}

	// Store a token for the very same host and the probe now succeeds, which is
	// what makes the previous assertion about the token and not about the host.
	if err := store.SetMemberToken(ctx, tokenless.ID, "tok-candidate"); err != nil {
		t.Fatalf("SetMemberToken: %v", err)
	}
	same, err = srv.repointTargetsCurrentPrimary(ctx, tokenless.ID)
	if err != nil {
		t.Fatalf("repointTargetsCurrentPrimary after token: %v", err)
	}
	if !same {
		t.Error("a probeable host self-reporting as primary must be recognised as the current primary")
	}
}

// The first designation has no primary to collide with, and re-selecting the
// row that is already the primary is a no-op. Neither probes the host, so
// neither may report a collision.
func TestRepointTargetsCurrentPrimary_NoCollisionToFind(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	host := systemMemberServer(t, true)
	m, err := store.CreateMember(ctx, "first", host.URL, "tok")
	if err != nil {
		t.Fatalf("CreateMember: %v", err)
	}

	// No primary configured yet.
	if same, err := srv.repointTargetsCurrentPrimary(ctx, m.ID); err != nil || same {
		t.Fatalf("first designation = (%v, %v), want (false, nil)", same, err)
	}

	// Re-selecting the same member row.
	if err := store.SetAutoSync(ctx, true, m.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
	if same, err := srv.repointTargetsCurrentPrimary(ctx, m.ID); err != nil || same {
		t.Fatalf("same-row re-select = (%v, %v), want (false, nil)", same, err)
	}
}
