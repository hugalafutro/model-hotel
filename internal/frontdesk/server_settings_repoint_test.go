package frontdesk

import (
	"context"
	"strconv"
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

	cur, err := store.GetAutoSync(ctx)
	if err != nil {
		t.Fatalf("GetAutoSync: %v", err)
	}
	same, err := srv.repointTargetsCurrentPrimary(ctx, cur, tokenless)
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
	same, err = srv.repointTargetsCurrentPrimary(ctx, cur, tokenless)
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
	if same, err := srv.repointTargetsCurrentPrimary(ctx, AutoSyncConfig{}, m); err != nil || same {
		t.Fatalf("first designation = (%v, %v), want (false, nil)", same, err)
	}

	// Re-selecting the same member row.
	if err := store.SetAutoSync(ctx, true, m.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}
	cur, err := store.GetAutoSync(ctx)
	if err != nil {
		t.Fatalf("GetAutoSync: %v", err)
	}
	if same, err := srv.repointTargetsCurrentPrimary(ctx, cur, m); err != nil || same {
		t.Fatalf("same-row re-select = (%v, %v), want (false, nil)", same, err)
	}
}

// Once the current primary's announces fail for more than 90s its live state
// decays to "warning" while is_primary lingers. If the flag names this desk and
// the candidate is the designated primary's own host (same instance_id) under
// a second URL, it is still caught. The same lingering flag on a DIFFERENT host
// (a former primary this desk repointed away from) is stale and the repoint is
// allowed, as it is when the designation has no row left. A flag naming another
// desk, or none, never counts (regression pins: neither makes the candidate the
// current primary).
func TestRepointTargetsCurrentPrimary_LingeringOwnFlag(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()
	ownID, err := store.EnsureFrontdeskID(ctx)
	if err != nil {
		t.Fatalf("EnsureFrontdeskID: %v", err)
	}
	designated, err := store.CreateVerifiedMember(ctx, "designated", "http://127.0.0.1:9/designated", "tok", "iid-designated")
	if err != nil {
		t.Fatalf("CreateVerifiedMember: %v", err)
	}
	cur := AutoSyncConfig{Enabled: true, PrimaryID: designated.ID}
	own := `{"state":"warning","is_primary":true,"frontdesk_id":"` + ownID + `"}`
	cases := []struct {
		name, fleet, instanceID string
		cur                     AutoSyncConfig
		want                    bool
	}{
		{"designated host under a second URL", own, "iid-designated", cur, true},
		{"former primary with a stale flag", own, "iid-former", cur, false},
		{"designation with no row left", own, "iid-designated", AutoSyncConfig{Enabled: true, PrimaryID: "gone"}, false},
		{"another desk's lingering flag", `{"state":"warning","is_primary":true,"frontdesk_id":"fd-elsewhere"}`, "iid-designated", cur, false},
		{"not flagged", `{"state":"warning","is_primary":false,"frontdesk_id":"` + ownID + `"}`, "iid-designated", cur, false},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			host := fleetIdentityStub(t, tc.fleet, tc.instanceID)
			m, err := store.CreateMember(ctx, "candidate-"+strconv.Itoa(i), host.URL, "tok")
			if err != nil {
				t.Fatalf("CreateMember: %v", err)
			}
			same, err := srv.repointTargetsCurrentPrimary(ctx, tc.cur, m)
			if err != nil || same != tc.want {
				t.Fatalf("repointTargetsCurrentPrimary = (%v, %v), want (%v, nil)", same, err, tc.want)
			}
		})
	}
}
