package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/failover"
)

// truncateFailoverGroups clears the shared api test database's group table.
// RevalidateCustomGroups below operates on EVERY group in the database, so a
// leftover undersized group from another test in this package would be
// auto-disabled as a side effect and land in the claim count these tests
// measure. Package tests run sequentially, so clearing here is safe.
func truncateFailoverGroups(t *testing.T) {
	t.Helper()
	if _, err := apiTestDB.Pool().Exec(context.Background(), `TRUNCATE model_failover_groups`); err != nil {
		t.Fatalf("truncate model_failover_groups: %v", err)
	}
}

func jsonBytes(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal %v: %v", v, err)
	}
	return b
}

// seedGroupMember inserts one model and returns its UUID, which is what a
// failover group's priority_order actually stores.
func seedGroupMember(t *testing.T, pool *pgxpool.Pool, providerID uuid.UUID, modelID string) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, enabled, disabled_manually, missing_scans, last_seen_at)
		 VALUES ($1, $2, $3, true, false, 0, now())`, id, providerID, modelID); err != nil {
		t.Fatalf("seed member %s: %v", modelID, err)
	}
	return id
}

// seedCustomGroup inserts an enabled, hand-built (auto_created = false) group.
// Custom is the only kind that matters here: the auto-disable skips auto-created
// groups, and sync re-enables or deletes them every scan.
func seedCustomGroup(t *testing.T, pool *pgxpool.Pool, displayModel string, members []uuid.UUID) uuid.UUID {
	t.Helper()
	priority := make([]string, len(members))
	entryEnabled := map[string]bool{}
	for i, m := range members {
		priority[i] = m.String()
		entryEnabled[m.String()] = true
	}
	id := uuid.New()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO model_failover_groups (id, display_model, priority_order, entry_enabled, group_enabled, auto_created)
		 VALUES ($1, $2, $3, $4, true, false)`,
		id, displayModel, jsonBytes(t, priority), jsonBytes(t, entryEnabled)); err != nil {
		t.Fatalf("seed group %s: %v", displayModel, err)
	}
	return id
}

// setModelEnabled flips a member's enabled flag, which is what makes it stop
// counting as routable.
func setModelEnabled(t *testing.T, pool *pgxpool.Pool, modelUUID uuid.UUID, enabled bool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE models SET enabled = $2 WHERE id = $1`, modelUUID, enabled); err != nil {
		t.Fatalf("set model enabled=%t: %v", enabled, err)
	}
}

// runRevalidate runs the real discovery auto-disable. Tests never write
// auto_disabled_at themselves: the stamp has to come from the production path,
// or they would only be testing their own INSERT.
func runRevalidate(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := failover.NewRepository(pool).RevalidateCustomGroups(context.Background()); err != nil {
		t.Fatalf("revalidate: %v", err)
	}
}

// operatorSetGroupEnabled drives the real operator surface: PUT
// /failover-groups/{id}. The dashboard's cascade (disabling a member drops the
// group below two routable entries, so it sends group_enabled: false alongside
// the entry toggle) lands on this same endpoint, so covering it covers both.
func operatorSetGroupEnabled(t *testing.T, r http.Handler, groupID uuid.UUID, enabled bool) {
	t.Helper()
	body := fmt.Sprintf(`{"group_enabled":%t}`, enabled)
	req := httptest.NewRequest(http.MethodPut, "/failover-groups/"+groupID.String(), strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("operator PUT group_enabled=%t = %d, want 200; body: %s", enabled, rec.Code, rec.Body.String())
	}
}

func findGroupClaim(resp DiscoveryStatusResponse, displayModel string) *GroupClaim {
	for i := range resp.GroupClaims {
		if resp.GroupClaims[i].DisplayModel == displayModel {
			return &resp.GroupClaims[i]
		}
	}
	return nil
}

// TestGetDiscoveryStatus_GroupClaimsOnlyCountDiscoveryDisabled is the whole
// point of migration 062. Both groups below end up with group_enabled = false
// and are indistinguishable on that column alone; only the provenance stamp
// separates them. Counting the operator's own group would nag them about their
// own configuration on every 60s poll and leave the badge permanently non-zero,
// which destroys the point of counting at all.
func TestGetDiscoveryStatus_GroupClaimsOnlyCountDiscoveryDisabled(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	_, fr := newFailoverHandlerWithAuth(t)
	pool := h.dbPool.Pool()
	truncateDiscoveryChanges(t)
	truncateFailoverGroups(t)

	provID := seedClaimProvider(t, pool, "group-claim-prov", true)
	memberA := seedGroupMember(t, pool, provID, "grp-member-a")
	memberB := seedGroupMember(t, pool, provID, "grp-member-b")

	seedCustomGroup(t, pool, "discovery-victim", []uuid.UUID{memberA, memberB})
	operatorGroupID := seedCustomGroup(t, pool, "operator-choice", []uuid.UUID{memberA, memberB})

	// Anchor: two healthy enabled groups produce no claims at all, so the
	// assertions below cannot pass on a response that was already full of them,
	// and the +2 delta cannot be an artifact of pre-existing state.
	before := getStatus(t, r, "/discovery/status")
	if len(before.GroupClaims) != 0 {
		t.Fatalf("two enabled groups must produce no group claims, got %+v", before.GroupClaims)
	}

	// The operator switches one off deliberately. No stamp is written.
	operatorSetGroupEnabled(t, fr, operatorGroupID, false)

	// Discovery takes the other one down: a member goes missing, leaving one
	// routable member, and the auto-disable fires.
	setModelEnabled(t, pool, memberB, false)
	runRevalidate(t, pool)

	after := getStatus(t, r, "/discovery/status")

	claim := findGroupClaim(after, "discovery-victim")
	if claim == nil {
		t.Fatalf("the discovery-disabled group must be a claim, got %+v", after.GroupClaims)
	}
	if claim.MemberCount != 2 || claim.RoutableCount != 1 {
		t.Errorf("claim counts = %d members / %d routable, want 2/1 (this is what makes the row actionable)",
			claim.MemberCount, claim.RoutableCount)
	}
	if claim.DisabledAt.IsZero() {
		t.Error("DisabledAt must carry the auto_disabled_at stamp, got the zero time")
	}
	if got := findGroupClaim(after, "operator-choice"); got != nil {
		t.Errorf("an operator-disabled group must never be a claim, got %+v", got)
	}

	// +2, not +1: memberB going missing is a gone-model claim in its own right,
	// and the disabled group is a second, independent claim. Asserting the delta
	// rather than an absolute pins that group claims are ADDED to the model
	// count instead of replacing or shadowing it.
	if delta := after.ClaimCount - before.ClaimCount; delta != 2 {
		t.Errorf("ClaimCount delta = %d, want 2 (1 gone model + 1 disabled group); before=%d after=%d",
			delta, before.ClaimCount, after.ClaimCount)
	}
}

// TestGetDiscoveryStatus_GroupClaimStampSurvivesReEnableCycle walks the full
// provenance lifecycle. Steps 3 and 4 are the ones that would silently rot: if
// re-enabling left the stamp behind, the operator's own later disable would
// inherit it and read as a discovery claim forever — the exact stale-stamp trap
// models.Upsert avoids by clearing discovery_dismissed_at on a sighting (with
// one deliberate exception for traffic-retired models, which are sighted on
// every scan and clear theirs on an operator enable instead).
func TestGetDiscoveryStatus_GroupClaimStampSurvivesReEnableCycle(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	_, fr := newFailoverHandlerWithAuth(t)
	pool := h.dbPool.Pool()
	truncateDiscoveryChanges(t)
	truncateFailoverGroups(t)

	provID := seedClaimProvider(t, pool, "cycle-prov", true)
	memberA := seedGroupMember(t, pool, provID, "cycle-member-a")
	memberB := seedGroupMember(t, pool, provID, "cycle-member-b")
	groupID := seedCustomGroup(t, pool, "cycle-group", []uuid.UUID{memberA, memberB})

	// 1. Discovery disables it: this is a claim.
	setModelEnabled(t, pool, memberB, false)
	runRevalidate(t, pool)
	first := findGroupClaim(getStatus(t, r, "/discovery/status"), "cycle-group")
	if first == nil {
		t.Fatal("step 1: the auto-disabled group must be a claim")
	}
	firstAt := first.DisabledAt

	// 2. The member comes back and the operator re-enables the group. The claim
	//    resolves on its own, with nothing to dismiss.
	setModelEnabled(t, pool, memberB, true)
	operatorSetGroupEnabled(t, fr, groupID, true)
	if got := findGroupClaim(getStatus(t, r, "/discovery/status"), "cycle-group"); got != nil {
		t.Fatalf("step 2: an enabled group must not be a claim, got %+v", got)
	}

	// 3. The operator now switches it off by hand, with every member healthy.
	//    This must NOT be a claim. It only stays quiet because step 2 cleared
	//    the stamp; a leftover stamp would resurrect the old auto-disable here.
	operatorSetGroupEnabled(t, fr, groupID, false)
	if got := findGroupClaim(getStatus(t, r, "/discovery/status"), "cycle-group"); got != nil {
		t.Errorf("step 3: an operator disable must not inherit the earlier discovery stamp, got %+v", got)
	}

	// 4. The operator turns it back on, then discovery disables it again. This
	//    is a FRESH claim: it counts again (a re-enable must not permanently
	//    suppress it), and DisabledAt tracks the NEW disable. The timestamp
	//    assertion is what catches the row being aged from the wrong column —
	//    created_at, say, which is non-zero and therefore sails past the
	//    IsZero() check in the other test while making every claim look as old
	//    as the group itself.
	operatorSetGroupEnabled(t, fr, groupID, true)
	setModelEnabled(t, pool, memberB, false)
	runRevalidate(t, pool)
	second := findGroupClaim(getStatus(t, r, "/discovery/status"), "cycle-group")
	if second == nil {
		t.Fatal("step 4: a re-disabled group must count again as a fresh claim")
	}
	if !second.DisabledAt.After(firstAt) {
		t.Errorf("step 4: DisabledAt = %s, want strictly after the first stamp %s (the stamp must be refreshed, not preserved)",
			second.DisabledAt, firstAt)
	}
}

// TestGetDiscoveryStatus_GroupClaimSurvivesMembershipPrune pins the one-way
// door in this design: revalidateCustomGroups skips groups that are already
// disabled, so nothing ever re-stamps a group whose stamp is lost. Any
// maintenance path that clears auto_disabled_at therefore erases the claim
// PERMANENTLY — the group stays disabled, `hotel/<model>` stays dead, and the
// badge goes quiet about it forever.
//
// pruneStaleEntries is exactly such a path: it fires when a member's model row
// is DELETED (provider removal, bulk delete, rename), which is nobody's opinion
// about the group, and it used to route through the operator-facing Update.
func TestGetDiscoveryStatus_GroupClaimSurvivesMembershipPrune(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	pool := h.dbPool.Pool()
	ctx := context.Background()
	truncateDiscoveryChanges(t)
	truncateFailoverGroups(t)

	provID := seedClaimProvider(t, pool, "prune-prov", true)
	memberA := seedGroupMember(t, pool, provID, "prune-member-a")
	memberB := seedGroupMember(t, pool, provID, "prune-member-b")
	memberC := seedGroupMember(t, pool, provID, "prune-member-c")
	seedCustomGroup(t, pool, "prune-group", []uuid.UUID{memberA, memberB, memberC})

	// Discovery disables it: two members go missing, one routable left.
	setModelEnabled(t, pool, memberB, false)
	setModelEnabled(t, pool, memberC, false)
	runRevalidate(t, pool)

	before := findGroupClaim(getStatus(t, r, "/discovery/status"), "prune-group")
	if before == nil {
		t.Fatal("setup: the auto-disabled group must be a claim before the prune")
	}
	if before.MemberCount != 3 {
		t.Fatalf("setup: MemberCount = %d, want 3", before.MemberCount)
	}

	// A member's model row is deleted outright, then the scheduled sync prunes
	// the dangling UUID. Two valid members remain, so the group is rewritten
	// rather than deleted — the branch that used to go through Update.
	if _, err := pool.Exec(ctx, `DELETE FROM models WHERE id = $1`, memberC); err != nil {
		t.Fatalf("delete member model: %v", err)
	}
	if _, err := failover.NewRepository(pool).SyncAllModels(ctx); err != nil {
		t.Fatalf("sync: %v", err)
	}

	after := findGroupClaim(getStatus(t, r, "/discovery/status"), "prune-group")
	if after == nil {
		t.Fatal("the claim must survive a membership prune: nothing would ever re-stamp it, so losing it here loses it forever")
	}
	// Anchor: MemberCount is priority_order's length, so 3 -> 2 proves the prune
	// actually rewrote THIS group. Without it the assertion above would also
	// pass if the prune had silently skipped the group (or if the model delete
	// had failed), making "the claim survived" meaningless.
	if after.MemberCount != 2 {
		t.Errorf("MemberCount = %d, want 2 (anchor: proves the prune rewrote this group's membership)", after.MemberCount)
	}
	if !after.DisabledAt.Equal(before.DisabledAt) {
		t.Errorf("DisabledAt = %s, want the original stamp %s: a prune must not re-date the claim either",
			after.DisabledAt, before.DisabledAt)
	}
}

// TestGetDiscoveryStatus_GroupClaimsSerializeAsEmptyArray pins the null-slice
// trap: web/src/api/types.ts declares group_claims as GroupClaim[] with no null
// guard, so a nil slice reaching the client as `null` would break `.map()` at
// runtime with nothing catching it at compile time. Asserted on the raw body,
// because decoding into the Go struct turns both `null` and `[]` into a slice
// of length zero and would pass either way.
func TestGetDiscoveryStatus_GroupClaimsSerializeAsEmptyArray(t *testing.T) {
	_, r := newTestHandlerWithRouter(t)
	truncateFailoverGroups(t)

	req := httptest.NewRequest(http.MethodGet, "/discovery/status", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /discovery/status = %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"group_claims":[]`) {
		t.Errorf("group_claims must serialize as [], not null; body: %s", body)
	}
}

// TestGetDiscoveryStatus_StripsOnlyClaimedGroupsFromTheFeed pins the group half
// of the two-zone rule, in both directions from a SINGLE journal row naming both
// groups. That shape is what makes it a real test: a blanket
// `trimmed.FailoverDisabledGroups = nil` and a no-op strip each satisfy exactly
// one of the two assertions.
//
//   - claimed-group is a live claim, so zone 1 owns it and it must leave the
//     feed. Left in both, it would carry two lifecycles: the claim disappears
//     the instant the group is routable again, while the journal row would keep
//     saying it was disabled until pruning at 30 days.
//   - pre-062-group is disabled with NO auto_disabled_at, exactly what an
//     upgrade leaves behind. It is not a claim, so the feed is the only place it
//     shows at all; stripping it would make it invisible in both zones for the
//     whole retention window.
func TestGetDiscoveryStatus_StripsOnlyClaimedGroupsFromTheFeed(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	pool := h.dbPool.Pool()
	ctx := context.Background()
	truncateDiscoveryChanges(t)
	truncateFailoverGroups(t)

	provID := seedClaimProvider(t, pool, "strip-prov", true)
	memberA := seedGroupMember(t, pool, provID, "strip-member-a")
	memberB := seedGroupMember(t, pool, provID, "strip-member-b")

	seedCustomGroup(t, pool, "claimed-group", []uuid.UUID{memberA, memberB})
	preMigrationID := seedCustomGroup(t, pool, "pre-062-group", []uuid.UUID{memberA, memberB})
	if _, err := pool.Exec(ctx,
		`UPDATE model_failover_groups SET group_enabled = false, auto_disabled_at = NULL WHERE id = $1`,
		preMigrationID); err != nil {
		t.Fatalf("age the pre-062 group: %v", err)
	}

	if _, err := AppendDiscoveryChange(ctx, pool, "scheduled", &provID, "Failover", &DiscoveryDiff{
		FailoverDisabledGroups: []failover.DisabledGroupInfo{
			{DisplayModel: "claimed-group", EffectiveCount: 1, Reason: "fewer than 2 routable members"},
			{DisplayModel: "pre-062-group", EffectiveCount: 1, Reason: "fewer than 2 routable members"},
		},
	}); err != nil {
		t.Fatalf("seed journal: %v", err)
	}

	// Discovery takes claimed-group down for real. Never stamped by hand: the
	// provenance has to come from the production path. pre-062-group is already
	// disabled, so revalidation skips it and it stays unstamped.
	setModelEnabled(t, pool, memberB, false)
	runRevalidate(t, pool)

	resp := getStatus(t, r, "/discovery/status")

	// Anchors: the strip is keyed on the claim set, so both halves are meaningless
	// unless exactly one of these groups is actually a claim.
	if findGroupClaim(resp, "claimed-group") == nil {
		t.Fatalf("anchor: claimed-group must be a live claim, got %+v", resp.GroupClaims)
	}
	if got := findGroupClaim(resp, "pre-062-group"); got != nil {
		t.Fatalf("anchor: a group with no auto_disabled_at must not be a claim, got %+v", got)
	}

	fed := map[string]bool{}
	for _, e := range resp.Informational {
		for _, g := range e.Diff.FailoverDisabledGroups {
			fed[g.DisplayModel] = true
		}
	}
	if fed["claimed-group"] {
		t.Errorf("a group already rendered as a claim must not also appear in the feed; feed = %v", fed)
	}
	if !fed["pre-062-group"] {
		t.Errorf("a disabled group with no provenance stamp is not a claim, so the feed is its only home; feed = %v", fed)
	}
}
