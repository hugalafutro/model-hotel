package api

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/model"
)

// TestBuildProviderClaims_Classification pins the four outcomes that decide the
// badge: a recently gone model counts, a long-gone quiet model ages out, a
// long-gone model that flapped inside the window keeps counting, and a model
// still enabled but mid-streak is suspect and never counts.
func TestBuildProviderClaims_Classification(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	rows := []claimRow{
		{ProviderID: "p1", ProviderName: "NanoGPT", ModelID: "recent-gone", LastSeenAt: now.Add(-3 * 24 * time.Hour)},
		{ProviderID: "p1", ProviderName: "NanoGPT", ModelID: "old-quiet", LastSeenAt: now.Add(-90 * 24 * time.Hour)},
		{ProviderID: "p1", ProviderName: "NanoGPT", ModelID: "old-flappy", LastSeenAt: now.Add(-90 * 24 * time.Hour)},
		{ProviderID: "p1", ProviderName: "NanoGPT", ModelID: "wobbling", LastSeenAt: now.Add(-1 * time.Hour), Enabled: true, MissingScans: 1},
	}
	window := map[flapKey]int{
		{providerID: "p1", modelID: "old-flappy"}: 3,
		{providerID: "p1", modelID: "wobbling"}:   5,
	}
	sinceReview := map[flapKey]int{
		{providerID: "p1", modelID: "old-flappy"}: 1,
	}

	claims, count := buildProviderClaims(rows, window, sinceReview, now)

	if len(claims) != 1 {
		t.Fatalf("expected 1 provider group, got %d", len(claims))
	}
	p := claims[0]
	if len(p.Gone) != 2 {
		t.Fatalf("Gone = %+v, want recent-gone and old-flappy", p.Gone)
	}
	goneIDs := map[string]bool{}
	for _, c := range p.Gone {
		goneIDs[c.ModelID] = true
	}
	if !goneIDs["old-flappy"] {
		t.Errorf("a long-gone model that flapped inside the window must keep counting, got %+v", p.Gone)
	}
	if len(p.Stale) != 1 || p.Stale[0].ModelID != "old-quiet" {
		t.Errorf("Stale = %+v, want only old-quiet", p.Stale)
	}
	if len(p.Suspect) != 1 || p.Suspect[0].ModelID != "wobbling" {
		t.Errorf("Suspect = %+v, want only wobbling", p.Suspect)
	}
	// Only Gone counts. Stale and Suspect are shown but never inflate the badge.
	if count != 2 {
		t.Errorf("claim count = %d, want 2", count)
	}
	for _, c := range p.Gone {
		if c.ModelID == "old-flappy" && c.FlapSinceReview != 1 {
			t.Errorf("old-flappy FlapSinceReview = %d, want 1", c.FlapSinceReview)
		}
	}
}

// TestBuildProviderClaims_Ordering puts the provider with the most counted
// claims first and sinks a stale-only provider to the bottom, so a section that
// resolves during a session moves down rather than disappearing.
func TestBuildProviderClaims_Ordering(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	old := now.Add(-90 * 24 * time.Hour)
	recent := now.Add(-2 * 24 * time.Hour)
	rows := []claimRow{
		{ProviderID: "p3", ProviderName: "Zed", ModelID: "a", LastSeenAt: old},
		{ProviderID: "p1", ProviderName: "Alpha", ModelID: "b", LastSeenAt: recent},
		{ProviderID: "p2", ProviderName: "Beta", ModelID: "c", LastSeenAt: recent},
		{ProviderID: "p2", ProviderName: "Beta", ModelID: "d", LastSeenAt: recent},
	}

	claims, _ := buildProviderClaims(rows, map[flapKey]int{}, map[flapKey]int{}, now)

	got := []string{claims[0].ProviderName, claims[1].ProviderName, claims[2].ProviderName}
	want := []string{"Beta", "Alpha", "Zed"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v (most counted claims first, stale-only last)", got, want)
		}
	}
}

// TestBuildProviderClaims_EmptyBucketsSerializeAsEmptyArray pins the wire
// contract the frontend depends on: a provider with only one populated bucket
// must still serialize the other two as `[]`, never `null`. The frontend
// types (web/src/api/types.ts) promise ModelClaim[] with no null guard, so a
// nil slice here would throw at the first real payload that has an
// unpopulated bucket, which is the common case, not the edge case.
func TestBuildProviderClaims_EmptyBucketsSerializeAsEmptyArray(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	rows := []claimRow{
		{ProviderID: "p1", ProviderName: "NanoGPT", ModelID: "only-gone", LastSeenAt: now.Add(-3 * 24 * time.Hour)},
	}

	claims, _ := buildProviderClaims(rows, map[flapKey]int{}, map[flapKey]int{}, now)
	if len(claims) != 1 {
		t.Fatalf("expected 1 provider group, got %d", len(claims))
	}

	out, err := json.Marshal(claims[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(out)
	if !strings.Contains(got, `"stale":[]`) {
		t.Errorf("stale bucket must serialize as [], got %s", got)
	}
	if !strings.Contains(got, `"suspect":[]`) {
		t.Errorf("suspect bucket must serialize as [], got %s", got)
	}
	if !strings.Contains(got, `"retired":[]`) {
		t.Errorf("retired bucket must serialize as [], got %s", got)
	}
	if strings.Contains(got, "null") {
		t.Errorf("no bucket may serialize as null, got %s", got)
	}
}

// TestBuildProviderClaims_RetiredIsItsOwnState covers the state that does not
// come from discovery at all: the proxy disabled the model from live traffic
// because the provider kept listing it and refused every request for it.
//
// It has to be told apart from gone, and the reason is what the operator reads.
// A gone model is missing from the provider's listing, so "last seen" dates it
// and a retest is the obvious next move. A retired model is still listed and was
// seen moments ago, so the same row would say "last seen a minute ago" beside a
// claim that it is unavailable, and a retest would find it present and prove
// nothing.
func TestBuildProviderClaims_RetiredIsItsOwnState(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	retiredAt := now.Add(-2 * time.Hour)
	rows := []claimRow{
		// Still listed (seen a minute ago) but refused by the provider.
		{ProviderID: "p1", ProviderName: "Google", ModelID: "traffic-retired", LastSeenAt: now.Add(-time.Minute), RetiredAt: &retiredAt},
		// Genuinely missing from the listing.
		{ProviderID: "p1", ProviderName: "Google", ModelID: "vanished", LastSeenAt: now.Add(-3 * 24 * time.Hour)},
	}

	claims, count := buildProviderClaims(rows, map[flapKey]int{}, map[flapKey]int{}, now)
	if len(claims) != 1 {
		t.Fatalf("expected 1 provider group, got %d", len(claims))
	}
	g := claims[0]

	if len(g.Retired) != 1 || g.Retired[0].ModelID != "traffic-retired" {
		t.Fatalf("retired bucket = %+v, want just traffic-retired", g.Retired)
	}
	if g.Retired[0].State != ClaimStateRetired {
		t.Errorf("state = %q, want %q", g.Retired[0].State, ClaimStateRetired)
	}
	if g.Retired[0].RetiredAt == nil || !g.Retired[0].RetiredAt.Equal(retiredAt) {
		t.Errorf("retired claim must carry when it was retired, got %v", g.Retired[0].RetiredAt)
	}
	if len(g.Gone) != 1 || g.Gone[0].ModelID != "vanished" {
		t.Errorf("gone bucket = %+v, want just vanished", g.Gone)
	}
	if g.Gone[0].RetiredAt != nil {
		t.Error("a discovery-gone claim must not carry a retirement timestamp")
	}
	// Both are actionable, so both count towards the badge.
	if count != 2 {
		t.Errorf("claim count = %d, want 2", count)
	}
}

// TestBuildProviderClaims_RetiredNeverAgesIntoStale pins the ordering of the two
// checks. Staleness is measured from last_seen_at, and a retired model is still
// being listed, so that clock keeps resetting; but a model retired before a
// provider outage could still present an old last_seen_at, and filing it as
// stale would drop it out of the counted set and stop it being shown as
// something the gateway did.
func TestBuildProviderClaims_RetiredNeverAgesIntoStale(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	retiredAt := now.Add(-2 * time.Hour)
	rows := []claimRow{
		{ProviderID: "p1", ProviderName: "Google", ModelID: "old-and-retired", LastSeenAt: now.Add(-90 * 24 * time.Hour), RetiredAt: &retiredAt},
	}

	claims, count := buildProviderClaims(rows, map[flapKey]int{}, map[flapKey]int{}, now)
	if len(claims[0].Stale) != 0 {
		t.Errorf("a retired model must not be filed as stale, got %+v", claims[0].Stale)
	}
	if len(claims[0].Retired) != 1 {
		t.Fatalf("retired bucket = %+v", claims[0].Retired)
	}
	if count != 1 {
		t.Errorf("claim count = %d, want 1: a retired model stays counted", count)
	}
}

// TestListClaimRows_Exclusions proves the three things that must never appear
// as a claim (a manually disabled model, a model on a disabled provider, and a
// dismissed model), and both halves of the inclusion predicate: a
// discovery-disabled model surfaces, an enabled-but-mid-streak model surfaces
// (the `enabled = true AND missing_scans > 0` branch), and a healthy enabled
// model with no missing scans does not. Without the suspect/healthy pair, a
// typo widening the enabled-branch predicate (e.g. dropping the
// `missing_scans > 0` guard) would surface every enabled model and this test
// would stay green.
func TestListClaimRows_Exclusions(t *testing.T) {
	h, _ := newTestHandlerWithRouter(t)
	pool := h.dbPool.Pool()
	ctx := context.Background()

	enabledProv := seedClaimProvider(t, pool, "claims-enabled", true)
	disabledProv := seedClaimProvider(t, pool, "claims-disabled", false)

	seedClaimModel(t, pool, enabledProv, "genuinely-gone", false, false, 0, nil)
	seedClaimModel(t, pool, enabledProv, "hand-disabled", false, true, 0, nil)
	seedClaimModel(t, pool, enabledProv, "dismissed", false, false, 0, ptrTime(time.Now()))
	seedClaimModel(t, pool, disabledProv, "on-dead-provider", false, false, 0, nil)
	seedClaimModel(t, pool, enabledProv, "suspect-flapping", true, false, 1, nil)
	seedClaimModel(t, pool, enabledProv, "healthy", true, false, 0, nil)

	rows, err := listClaimRows(ctx, pool)
	if err != nil {
		t.Fatalf("listClaimRows: %v", err)
	}
	got := map[string]bool{}
	for _, r := range rows {
		got[r.ModelID] = true
	}
	if !got["genuinely-gone"] {
		t.Error("a discovery-disabled model must surface as a claim")
	}
	if !got["suspect-flapping"] {
		t.Error("an enabled model mid-miss-streak must surface as a claim")
	}
	for _, excluded := range []string{"hand-disabled", "dismissed", "on-dead-provider", "healthy"} {
		if got[excluded] {
			t.Errorf("%s must not surface as a claim", excluded)
		}
	}
}

// TestFlapCounts counts membership transitions only, so a metadata-only entry
// never makes a stable model look flappy. Exercises all three membership
// buckets the SQL hand-writes as JSON keys (added, reenabled, disabled): a
// misspelling of any one is invisible unless every bucket has a seeded row.
func TestFlapCounts(t *testing.T) {
	h, _ := newTestHandlerWithRouter(t)
	pool := h.dbPool.Pool()
	ctx := context.Background()
	truncateDiscoveryChanges(t)

	providerID := uuid.New()
	for _, d := range []*DiscoveryDiff{
		{Disabled: []ModelChange{{ModelID: "flappy", Reason: changeReasonNotListed}}},
		{Reenabled: []ModelChange{{ModelID: "flappy", Reason: changeReasonReappeared}}},
		{Added: []ModelChange{{ModelID: "flappy", Reason: changeReasonReappeared}}},
		{Updated: []ModelUpdate{{ModelID: "steady", Changes: []FieldChange{{Field: changeFieldInputPrice}}}}},
	} {
		if _, err := AppendDiscoveryChange(ctx, pool, "scheduled", &providerID, "NanoGPT", d); err != nil {
			t.Fatalf("seed journal: %v", err)
		}
	}

	counts, err := flapCounts(ctx, pool, time.Now().Add(-ClaimWindow))
	if err != nil {
		t.Fatalf("flapCounts: %v", err)
	}
	// Anchor: proves the query returned rows at all. The paired steady == 0
	// check below is a zero-value map lookup that would also pass if the query
	// silently returned nothing (e.g. a broken JOIN or WHERE clause), so it is
	// only meaningful alongside this non-zero anchor. Do not delete this one
	// while trimming the steady assertion.
	if got := counts[flapKey{providerID: providerID.String(), modelID: "flappy"}]; got != 3 {
		t.Errorf("flappy count = %d, want 3 (disabled + reenabled + added)", got)
	}
	if got := counts[flapKey{providerID: providerID.String(), modelID: "steady"}]; got != 0 {
		t.Errorf("steady count = %d, want 0: a price move is not a flap", got)
	}
}

// TestFlappedModels pins the bridge the prune uses: a model with any
// membership transition in the journal since `since` is flapped; a metadata
// update is not; a row older than `since` is not.
func TestFlappedModels(t *testing.T) {
	h, _ := newTestHandlerWithRouter(t)
	pool := h.dbPool.Pool()
	ctx := context.Background()
	truncateDiscoveryChanges(t)

	providerID := uuid.New()
	seed := func(diff *DiscoveryDiff, age time.Duration) {
		t.Helper()
		if _, err := AppendDiscoveryChange(ctx, pool, "test", &providerID, "flap-prov", diff); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if _, err := pool.Exec(ctx, `UPDATE discovery_changes SET detected_at = now() - $1::interval WHERE detected_at = (SELECT MAX(detected_at) FROM discovery_changes)`, age.String()); err != nil {
			t.Fatalf("age: %v", err)
		}
	}
	seed(&DiscoveryDiff{Disabled: []ModelChange{{ModelID: "flappy", Reason: changeReasonNotListed}}}, time.Hour)
	seed(&DiscoveryDiff{Updated: []ModelUpdate{{ModelID: "priced", Changes: []FieldChange{{Field: changeFieldInputPrice}}}}}, time.Hour)
	seed(&DiscoveryDiff{Disabled: []ModelChange{{ModelID: "ancient", Reason: changeReasonNotListed}}}, ClaimWindow+24*time.Hour)

	got, err := FlappedModels(ctx, pool, time.Now().Add(-ClaimWindow))
	if err != nil {
		t.Fatalf("FlappedModels: %v", err)
	}
	key := func(id string) model.ProviderModelKey {
		return model.ProviderModelKey{ProviderID: providerID, ModelID: id}
	}
	if !got[key("flappy")] {
		t.Error("flappy: not flapped, want flapped")
	}
	if got[key("priced")] {
		t.Error("priced: a metadata update counted as a flap")
	}
	if got[key("ancient")] {
		t.Error("ancient: a journal row older than the window counted as a flap")
	}
}

// TestPruneDiscoveryChanges keeps unseen rows regardless of age, because an
// unseen row is still news the operator has not been shown.
func TestPruneDiscoveryChanges(t *testing.T) {
	h, _ := newTestHandlerWithRouter(t)
	pool := h.dbPool.Pool()
	ctx := context.Background()
	truncateDiscoveryChanges(t)

	providerID := uuid.New()
	diff := &DiscoveryDiff{Added: []ModelChange{{ModelID: "m", Reason: changeReasonNewModel}}}
	for _, seed := range []struct {
		age  time.Duration
		seen bool
	}{{90 * 24 * time.Hour, true}, {90 * 24 * time.Hour, false}, {time.Hour, true}} {
		if _, err := AppendDiscoveryChange(ctx, pool, "scheduled", &providerID, "NanoGPT", diff); err != nil {
			t.Fatalf("seed: %v", err)
		}
		if _, err := pool.Exec(ctx,
			`UPDATE discovery_changes SET detected_at = now() - $1::interval, seen = $2
			  WHERE detected_at = (SELECT MAX(detected_at) FROM discovery_changes)`,
			seed.age.String(), seed.seen); err != nil {
			t.Fatalf("age row: %v", err)
		}
	}

	deleted, err := PruneDiscoveryChanges(ctx, pool, time.Now().Add(-ClaimWindow))
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1 (only the old seen row)", deleted)
	}
}

// TestSetModelsDismissed exercises the one write claim derivation depends on:
// stamping discovery_dismissed_at, and reporting 0 rows for a model ID that does
// not exist so the dismiss endpoint can tell an operator they targeted something
// real.
//
// There is no clearing direction any more. A dismissal is undone by discovery
// sighting the model again, which nulls the column in models.Upsert; nothing
// clears it by hand.
func TestSetModelsDismissed(t *testing.T) {
	h, _ := newTestHandlerWithRouter(t)
	pool := h.dbPool.Pool()
	ctx := context.Background()

	prov := seedClaimProvider(t, pool, "claims-dismiss", true)
	seedClaimModel(t, pool, prov, "to-dismiss", false, false, 0, nil)

	got, err := setModelsDismissed(ctx, pool, prov, []string{"to-dismiss"})
	if err != nil {
		t.Fatalf("dismiss: %v", err)
	}
	// Names what it stamped, not just how many: the caller cannot derive WHICH ids
	// a short result covered, and guessing is what mislabels them downstream.
	if len(got) != 1 || got[0] != "to-dismiss" {
		t.Fatalf("dismissed = %v, want [to-dismiss]", got)
	}
	var dismissedAt *time.Time
	if err := pool.QueryRow(ctx, `SELECT discovery_dismissed_at FROM models WHERE provider_id = $1 AND model_id = $2`,
		prov, "to-dismiss").Scan(&dismissedAt); err != nil {
		t.Fatalf("query: %v", err)
	}
	if dismissedAt == nil {
		t.Fatal("discovery_dismissed_at not set after dismiss")
	}

	got, err = setModelsDismissed(ctx, pool, prov, []string{"does-not-exist"})
	if err != nil {
		t.Fatalf("dismiss unknown: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("dismissed unknown = %v, want empty", got)
	}
}

// TestSetModelsUnpinned exercises the one write the unpin endpoint makes: it
// clears manually_enabled_at for the named rows that actually carry a pin, names
// which, and hands the model back to automatic management with a clean streak.
//
// The missing_scans reset is the part worth guarding. A pin survives any number
// of missed scans, so by the time an operator unpins, the streak is usually well
// past MissingScanThreshold; clearing only the stamp would disable the model on
// the very next scan, which is neither what the operator asked for nor what the
// UI told them would happen.
func TestSetModelsUnpinned(t *testing.T) {
	h, _ := newTestHandlerWithRouter(t)
	pool := h.dbPool.Pool()
	ctx := context.Background()

	prov := seedClaimProvider(t, pool, "claims-unpin", true)
	// A mature streak: what a pin held past the disable threshold really looks
	// like by the time anyone unpins it.
	seedClaimModel(t, pool, prov, "to-unpin", true, false, model.MissingScanThreshold+3, nil)
	pinClaimModel(t, pool, prov, "to-unpin")
	seedClaimModel(t, pool, prov, "never-pinned", true, false, 1, nil)

	got, err := setModelsUnpinned(ctx, pool, prov, []string{"to-unpin", "never-pinned", "does-not-exist"})
	if err != nil {
		t.Fatalf("unpin: %v", err)
	}
	// Names what it cleared: an unpinned model and an unknown ID are absent, so
	// the caller can tell a partial result apart from a total one.
	if len(got) != 1 || got[0] != "to-unpin" {
		t.Fatalf("unpinned = %v, want [to-unpin]", got)
	}

	var pinnedAt *time.Time
	var missing int
	if err := pool.QueryRow(ctx,
		`SELECT manually_enabled_at, missing_scans FROM models WHERE provider_id = $1 AND model_id = $2`,
		prov, "to-unpin").Scan(&pinnedAt, &missing); err != nil {
		t.Fatalf("query: %v", err)
	}
	if pinnedAt != nil {
		t.Errorf("manually_enabled_at = %v after unpin, want nil", *pinnedAt)
	}
	if missing != 0 {
		t.Errorf("missing_scans = %d after unpin, want 0: the model must not be one scan from being disabled", missing)
	}

	// The unpinned row is untouched, proving the reset rides on the same WHERE
	// clause as the stamp clear rather than being a blanket update.
	if err := pool.QueryRow(ctx,
		`SELECT missing_scans FROM models WHERE provider_id = $1 AND model_id = $2`,
		prov, "never-pinned").Scan(&missing); err != nil {
		t.Fatalf("query never-pinned: %v", err)
	}
	if missing != 1 {
		t.Errorf("never-pinned missing_scans = %d, want 1 untouched", missing)
	}
}

// TestSetModelsUnpinned_ClosedPool drives the query error path: a pool that
// cannot run the UPDATE at all must surface the error rather than reporting an
// empty (and misleadingly successful-looking) unpinned list. Uses the same
// closedAPIPool helper as the dismiss/unpin handler failure tests, since
// setModelsUnpinned takes the pool directly rather than going through a
// handler.
func TestSetModelsUnpinned_ClosedPool(t *testing.T) {
	dead := closedAPIPool(t)

	_, err := setModelsUnpinned(context.Background(), dead.Pool(), uuid.New(), []string{"some-model"})
	if err == nil {
		t.Error("expected error with a closed pool, got nil")
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

// seedClaimProvider inserts a minimal provider row. The brief's original
// version guessed a column list (id, name, type, base_url, api_key_encrypted,
// enabled) that does not match this repo's schema: `providers` has no `type`
// column, and the encrypted-key column is `encrypted_key`/`key_nonce` (both
// nullable since migration 026 for keyless providers), not `api_key_encrypted`.
// This follows the existing convention in handler_providers_test.go /
// stats_test.go / models_helpers_test.go / failover_api_test.go, which all
// seed with just (id, name, base_url, enabled, created_at, updated_at).
func seedClaimProvider(t *testing.T, pool *pgxpool.Pool, name string, enabled bool) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO providers (id, name, base_url, enabled, created_at, updated_at)
		 VALUES ($1, $2, 'http://localhost', $3, now(), now())`, id, name, enabled); err != nil {
		t.Fatalf("seed provider %s: %v", name, err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM providers WHERE id = $1`, id)
	})
	return id
}

func seedClaimModel(t *testing.T, pool *pgxpool.Pool, providerID uuid.UUID, modelID string, enabled, manual bool, missing int, dismissed *time.Time) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO models (id, provider_id, model_id, enabled, disabled_manually, missing_scans, discovery_dismissed_at, last_seen_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, now())`,
		uuid.New(), providerID, modelID, enabled, manual, missing, dismissed); err != nil {
		t.Fatalf("seed model %s: %v", modelID, err)
	}
}

// pinClaimModel arms the operator pin on an already-seeded model.
// seedClaimModel does not take it as a parameter because every claims test but
// the pin ones has no opinion about it and would have to pass nil.
func pinClaimModel(t *testing.T, pool *pgxpool.Pool, providerID uuid.UUID, modelID string) {
	t.Helper()
	tag, err := pool.Exec(context.Background(),
		`UPDATE models SET manually_enabled_at = now() WHERE provider_id = $1 AND model_id = $2`,
		providerID, modelID)
	if err != nil {
		t.Fatalf("pin model %s: %v", modelID, err)
	}
	if tag.RowsAffected() != 1 {
		t.Fatalf("pin model %s affected %d rows, want 1", modelID, tag.RowsAffected())
	}
}

// TestBuildProviderClaims_PinnedRow pins the informational state: the operator
// enabled the model by hand while the provider's listing still omits it, so the
// row keeps missing scans but is not evidence of anything the operator has yet
// to decide. It is shown so a forgotten pin stays visible and never counted.
//
// The identical unpinned row proves the pin is what moves it: without that pair,
// a classifier that filed every mid-streak model as pinned would stay green.
func TestBuildProviderClaims_PinnedRow(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	pinnedAt := now.Add(-6 * time.Hour)
	rows := []claimRow{
		{ProviderID: "p1", ProviderName: "NanoGPT", ModelID: "pinned",
			LastSeenAt: now.Add(-3 * 24 * time.Hour), Enabled: true, MissingScans: 3, PinnedAt: &pinnedAt},
		{ProviderID: "p1", ProviderName: "NanoGPT", ModelID: "wobbling",
			LastSeenAt: now.Add(-3 * 24 * time.Hour), Enabled: true, MissingScans: 3},
		// Anchor: one genuinely counted claim, so the "count is unchanged by the
		// pinned row" assertion cannot pass on a count that is zero throughout.
		{ProviderID: "p1", ProviderName: "NanoGPT", ModelID: "vanished", LastSeenAt: now.Add(-3 * 24 * time.Hour)},
	}

	claims, count := buildProviderClaims(rows, map[flapKey]int{}, map[flapKey]int{}, now)
	if len(claims) != 1 {
		t.Fatalf("expected 1 provider group, got %d", len(claims))
	}
	g := claims[0]

	if len(g.Pinned) != 1 || g.Pinned[0].ModelID != "pinned" {
		t.Fatalf("pinned bucket = %+v, want just pinned", g.Pinned)
	}
	if g.Pinned[0].State != ClaimStatePinned {
		t.Errorf("state = %q, want %q", g.Pinned[0].State, ClaimStatePinned)
	}
	if g.Pinned[0].PinnedAt == nil || !g.Pinned[0].PinnedAt.Equal(pinnedAt) {
		t.Errorf("pinned claim must carry when it was pinned, got %v", g.Pinned[0].PinnedAt)
	}
	if g.Pinned[0].MissingScans != 3 {
		t.Errorf("pinned MissingScans = %d, want 3: the streak keeps accruing under a pin", g.Pinned[0].MissingScans)
	}
	if len(g.Suspect) != 1 || g.Suspect[0].ModelID != "wobbling" {
		t.Errorf("suspect bucket = %+v, want just wobbling: only the pin moves a mid-streak row", g.Suspect)
	}
	if g.Suspect[0].PinnedAt != nil {
		t.Error("an unpinned suspect claim must not carry a pin timestamp")
	}
	if count != 1 {
		t.Errorf("claim count = %d, want 1 (only vanished): a pinned row is never counted", count)
	}

	out, err := json.Marshal(g)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"state":"pinned"`) {
		t.Errorf("pinned state must serialize as \"pinned\", got %s", out)
	}
}

// TestBuildProviderClaims_PinnedBucketSerializesAsEmptyArray extends the wire
// contract the other buckets already hold to the new one: the frontend types
// promise ModelClaim[] with no null guard, and a provider with no pinned model
// is the common case, not the edge case.
func TestBuildProviderClaims_PinnedBucketSerializesAsEmptyArray(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	rows := []claimRow{
		{ProviderID: "p1", ProviderName: "NanoGPT", ModelID: "only-gone", LastSeenAt: now.Add(-3 * 24 * time.Hour)},
	}

	claims, _ := buildProviderClaims(rows, map[flapKey]int{}, map[flapKey]int{}, now)
	out, err := json.Marshal(claims[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(out), `"pinned":[]`) {
		t.Errorf("pinned bucket must serialize as [], got %s", out)
	}
	// pinned_at is omitempty, so a claim that was never pinned must not carry
	// the key at all rather than a null the frontend would have to guard.
	if strings.Contains(string(out), "pinned_at") {
		t.Errorf("an unpinned claim must not emit pinned_at, got %s", out)
	}
}

// TestListClaimRows_CarriesPin proves the derivation query reads the pin. The
// WHERE clause already matches a pinned row through its
// `enabled = true AND missing_scans > 0` branch, so the only thing that can be
// missing is the column itself — and without it every pinned model would file as
// suspect and nag the operator about a decision they already made.
func TestListClaimRows_CarriesPin(t *testing.T) {
	h, _ := newTestHandlerWithRouter(t)
	pool := h.dbPool.Pool()
	ctx := context.Background()

	prov := seedClaimProvider(t, pool, "claims-pin", true)
	seedClaimModel(t, pool, prov, "pinned-model", true, false, 3, nil)
	pinClaimModel(t, pool, prov, "pinned-model")
	seedClaimModel(t, pool, prov, "unpinned-model", true, false, 3, nil)

	rows, err := listClaimRows(ctx, pool)
	if err != nil {
		t.Fatalf("listClaimRows: %v", err)
	}
	got := map[string]*claimRow{}
	for i := range rows {
		got[rows[i].ModelID] = &rows[i]
	}
	pinned := got["pinned-model"]
	if pinned == nil {
		t.Fatal("a pinned mid-streak model must still surface as a claim row")
	}
	if pinned.PinnedAt == nil {
		t.Error("pinned-model must carry PinnedAt: without it the row files as suspect")
	}
	unpinned := got["unpinned-model"]
	if unpinned == nil {
		t.Fatal("unpinned-model must surface as a claim row")
	}
	if unpinned.PinnedAt != nil {
		t.Errorf("unpinned-model PinnedAt = %v, want nil", unpinned.PinnedAt)
	}
}

// TestBuildProviderClaims_OrderingIsTotal pins that the provider ordering is a
// total order, decided in the documented sequence and never by chance.
//
// out is built by ranging over a Go map and sorted with sort.Slice, which is
// not stable, so any pair the comparator declares equivalent can swap position
// between two calls on identical input. The dashboard re-fetches this list
// every 60 seconds; a comparator that stops at the counted-claim count would
// make the sections jump around under the operator's cursor while nothing had
// actually changed.
//
// The fixture is built so that removing any single tiebreak changes the
// expected order rather than merely making it non-deterministic:
//   - Zeta wins on counted claims alone.
//   - Alpha beats both Betas and Gamma only on the suspect count.
//   - Beta beats Gamma only on name, and Gamma deliberately holds the
//     lexicographically smallest provider ID, so dropping the name comparison
//     would float it to the front.
//   - The two Betas are separable only by provider ID.
func TestBuildProviderClaims_OrderingIsTotal(t *testing.T) {
	now := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	gone := func(providerID, name, modelID string) claimRow {
		return claimRow{ProviderID: providerID, ProviderName: name, ModelID: modelID, LastSeenAt: now.Add(-24 * time.Hour)}
	}
	suspect := func(providerID, name, modelID string) claimRow {
		return claimRow{ProviderID: providerID, ProviderName: name, ModelID: modelID,
			LastSeenAt: now.Add(-time.Hour), Enabled: true, MissingScans: 1}
	}
	rows := []claimRow{
		gone("p-zeta", "Zeta", "z1"), gone("p-zeta", "Zeta", "z2"),
		gone("p-alpha", "Alpha", "a1"), suspect("p-alpha", "Alpha", "a2"), suspect("p-alpha", "Alpha", "a3"),
		gone("p-beta-2", "Beta", "b1"), suspect("p-beta-2", "Beta", "b2"),
		gone("p-beta-1", "Beta", "b3"), suspect("p-beta-1", "Beta", "b4"),
		gone("p-0000000", "Gamma", "g1"), suspect("p-0000000", "Gamma", "g2"),
	}
	want := []string{"p-zeta", "p-alpha", "p-beta-1", "p-beta-2", "p-0000000"}

	// Repeated because map iteration order is randomised per range: one pass
	// could agree with the expectation by luck, twenty cannot.
	for i := range 20 {
		claims, count := buildProviderClaims(rows, nil, nil, now)
		if count != 6 {
			t.Fatalf("pass %d: counted claims = %d, want 6 (one gone model per provider, two for Zeta)", i, count)
		}
		got := make([]string, 0, len(claims))
		for _, c := range claims {
			got = append(got, c.ProviderID)
		}
		if len(got) != len(want) {
			t.Fatalf("pass %d: got %d provider groups, want %d", i, len(got), len(want))
		}
		for j := range want {
			if got[j] != want[j] {
				t.Fatalf("pass %d: provider order = %v, want %v", i, got, want)
			}
		}
	}
}

// TestFlappedModels_QueryErrorSurfaces pins that a refused journal query comes
// back as an error: the prune must skip rather than run without flap history.
func TestFlappedModels_QueryErrorSurfaces(t *testing.T) {
	h, _ := newTestHandlerWithRouter(t)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := FlappedModels(cancelled, h.dbPool.Pool(), time.Now()); err == nil {
		t.Fatal("FlappedModels: cancelled context returned no error")
	}
}
