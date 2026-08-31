package frontdesk

import (
	"context"
	"testing"
)

// A rolling rebuild replays this exact sequence, and it used to report the
// fleet FAULTY partway through. Observed on the live fleet 2026-08-31:
//
//	19:48:09  MH2 held        (MH2 rebuilt new, primary still old, so it differs)
//	19:48:22  primary drained (the primary starts its own rebuild)
//	19:49:10  MH3, MH4 held   (primary now new, they are still old)
//	19:49:11  primary active  (MH2 now agrees with it again)
//	19:49:14  FAULTY          <- all three candidates still flagged held
//	19:49:25  MH2 recovered   (the next auto-sync pass finally clears it)
//
// Nothing was wrong with the fleet at 19:49:14. MH2's flag was 76 seconds old
// and had been judged against a build no member was running any more; clearing
// it is a polled operation, so for one window every candidate carried a hold
// and the escalation read that as "the whole fleet disagrees with the primary".
//
// The escalation now needs holds judged against the CURRENT primary build, so
// the window degrades rather than condemns.
func TestFleetState_RollingRebuildDoesNotEscalateOnAStaleHold(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := t.Context()

	// Derived, never spelled: the key's format is memberBuild's business, and a
	// test that hardcodes the separator silently stops matching when it changes.
	var (
		oldBuild = memberBuild{Version: "dev", Commit: "b59259c5ceb1"}.key()
		newBuild = memberBuild{Version: "dev", Commit: "02c834188570"}.key()
	)
	mk := func(name, url string) *Member {
		t.Helper()
		m, err := store.CreateMember(ctx, name, url, "tok-"+name)
		if err != nil {
			t.Fatalf("CreateMember %s: %v", name, err)
		}
		return m
	}
	primary := mk("primary", "https://p.example")
	first := mk("rebuilt-first", "https://f.example")
	rest := []*Member{mk("rest-1", "https://r1.example"), mk("rest-2", "https://r2.example")}
	if err := store.SetAutoSync(ctx, true, primary.ID); err != nil {
		t.Fatalf("SetAutoSync: %v", err)
	}

	// Everything starts on the old build, healthy, in sync.
	for _, m := range append([]*Member{primary, first}, rest...) {
		// Health first: setHealth assigns a whole MemberStatus and would wipe
		// the build if it ran second, leaving the fleet on an unknown build and
		// the first hold stale from birth rather than fresh.
		setHealth(srv, m.ID, true)
		setMemberBuild(srv, m.ID, "dev", "b59259c5ceb1")
	}
	if state, reasons := fleetStateFor(ctx, t, srv); state != FleetOK {
		t.Fatalf("a fleet on one build should be ok, got %s %v", state, reasons)
	}

	// The first member is rebuilt: it now differs from the still-old primary.
	setMemberBuild(srv, first.ID, "dev", "02c834188570")
	holdFor(srv, first.ID, oldBuild)
	if state, _ := fleetStateFor(ctx, t, srv); state != FleetDegraded {
		t.Fatalf("one held member should degrade, got %s", state)
	}

	// The primary is rebuilt. The two not yet rebuilt are judged against the
	// new primary and held; the first member now agrees with it again, but its
	// flag has not been re-checked yet.
	setMemberBuild(srv, primary.ID, "dev", "02c834188570")
	for _, m := range rest {
		holdFor(srv, m.ID, newBuild)
	}

	state, reasons := fleetStateFor(ctx, t, srv)
	if state == FleetFaulty {
		t.Errorf("the rebuild window reported the fleet faulty on a stale hold: %s %v", state, reasons)
	}
	if state != FleetDegraded {
		t.Errorf("state = %s %v, want degraded", state, reasons)
	}
	for _, r := range reasons {
		if r == reasonAllSyncHeld {
			t.Errorf("reasons %v carry %q while one candidate's hold predates the current primary", reasons, reasonAllSyncHeld)
		}
	}

	// The genuine condition still escalates: once the stale flag is re-judged
	// against the current primary and stands, every candidate really does
	// disagree.
	holdFor(srv, first.ID, newBuild)
	if state, reasons := fleetStateFor(ctx, t, srv); state != FleetFaulty {
		t.Errorf("state = %s %v, want faulty once every hold is judged against the current primary", state, reasons)
	}

	// And the ordinary recovery clears it.
	releaseHold(srv, first.ID)
	for _, m := range rest {
		releaseHold(srv, m.ID)
	}
	if state, reasons := fleetStateFor(ctx, t, srv); state != FleetOK {
		t.Errorf("state = %s %v, want ok once the fleet is on one build", state, reasons)
	}
}

// fleetStateFor computes the current state the way the background loop does.
func fleetStateFor(ctx context.Context, t *testing.T, s *Server) (FleetState, []string) {
	t.Helper()
	state, reasons, _, err := s.fleetStateNow(ctx)
	if err != nil {
		t.Fatalf("fleetStateNow: %v", err)
	}
	return state, reasons
}

// holdFor records a skew hold judged against a given primary build, which is
// what the auto-sync pass does when it finds a member that differs.
func holdFor(s *Server, memberID, primaryBuild string) {
	s.syncHeldMu.Lock()
	s.syncHeld[memberID] = primaryBuild
	s.syncHeldMu.Unlock()
}

func releaseHold(s *Server, memberID string) {
	s.syncHeldMu.Lock()
	delete(s.syncHeld, memberID)
	s.syncHeldMu.Unlock()
}
