package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/failover"
)

func getStatus(t *testing.T, r http.Handler, path string) DiscoveryStatusResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200; body: %s", path, rec.Code, rec.Body.String())
	}
	var resp DiscoveryStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp
}

// TestGetDiscoveryStatus_StripsDisabledBucket: a gone model is a claim, so
// leaving it in the informational feed too would show the same fact twice with
// two different lifecycles.
func TestGetDiscoveryStatus_StripsDisabledBucket(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	pool := h.dbPool.Pool()
	ctx := context.Background()
	truncateDiscoveryChanges(t)

	providerID := uuid.New()
	if _, err := AppendDiscoveryChange(ctx, pool, "scheduled", &providerID, "NanoGPT", &DiscoveryDiff{
		Added:    []ModelChange{{ModelID: "arrived", Reason: changeReasonNewModel}},
		Disabled: []ModelChange{{ModelID: "departed", Reason: changeReasonNotListed}},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// An entry that is nothing but a disabled bucket must disappear entirely.
	if _, err := AppendDiscoveryChange(ctx, pool, "scheduled", &providerID, "NanoGPT", &DiscoveryDiff{
		Disabled: []ModelChange{{ModelID: "also-departed", Reason: changeReasonNotListed}},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	resp := getStatus(t, r, "/discovery/status")

	if len(resp.Informational) != 1 {
		t.Fatalf("informational entries = %d, want 1 (the disabled-only entry drops out)", len(resp.Informational))
	}
	if len(resp.Informational[0].Diff.Disabled) != 0 {
		t.Errorf("disabled bucket must be stripped, got %+v", resp.Informational[0].Diff.Disabled)
	}
	if len(resp.Informational[0].Diff.Added) != 1 {
		t.Errorf("added bucket must survive, got %+v", resp.Informational[0].Diff.Added)
	}
	// InformationalUnseen counts what the feed actually shows (post-strip), not
	// the raw journal-row total: its only job is to drive a badge dot meaning
	// "there is something to see if you expand Recent changes", and a
	// disabled-only row strips to nothing, so it must not light that dot.
	if resp.InformationalUnseen != 1 {
		t.Errorf("InformationalUnseen = %d, want 1 (matches the stripped, displayed entry only)", resp.InformationalUnseen)
	}
}

// TestGetDiscoveryStatus_PriceOnlyEntryDoesNotCount pins Ruling A: prices move
// on nearly every scan, so an entry whose only content is the metadata `updated`
// bucket must not raise InformationalUnseen — otherwise the dot is permanently
// lit and becomes exactly the ignorable indicator the claim count exists to
// avoid. The price entry must still be VISIBLE in the feed; only the counter
// ignores it.
//
// Four entries, each under its own provider so collapseRoundTrips (which chains
// per provider+model+field) cannot fold any of them into another:
//   - updated-only    -> visible, not counted
//   - added           -> visible, counted
//   - failover        -> visible, counted (proves the rule is "anything but
//     `updated`", not a hardcoded added/reenabled pair)
//   - updated + added -> visible, COUNTED
//
// That last one is the case the rule actually turns on, and it is the realistic
// shape: a single provider scan routinely reports a price move and a new model
// together. The rule is "the entry carries ONLY `updated`", not "the entry
// contains `updated`" — the one-line misreading `if len(e.Diff.Updated) > 0 {
// continue }` would silently stop counting every mixed entry, i.e. most real
// ones, while still passing a test built solely from single-bucket entries.
func TestGetDiscoveryStatus_PriceOnlyEntryDoesNotCount(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	pool := h.dbPool.Pool()
	ctx := context.Background()
	truncateDiscoveryChanges(t)

	oldPrice, newPrice := 1.0, 2.0
	priceProv, addProv, failProv, mixedProv := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	seed := func(providerID uuid.UUID, name string, diff *DiscoveryDiff) {
		t.Helper()
		if _, err := AppendDiscoveryChange(ctx, pool, "scheduled", &providerID, name, diff); err != nil {
			t.Fatalf("seed %s: %v", name, err)
		}
	}
	seed(priceProv, "price-prov", &DiscoveryDiff{
		Updated: []ModelUpdate{{
			ModelID: "priced",
			Changes: []FieldChange{{Field: changeFieldInputPrice, Old: &oldPrice, New: &newPrice}},
		}},
	})
	seed(addProv, "add-prov", &DiscoveryDiff{
		Added: []ModelChange{{ModelID: "arrived", Reason: changeReasonNewModel}},
	})
	seed(failProv, "fail-prov", &DiscoveryDiff{
		FailoverDisabledGroups: []failover.DisabledGroupInfo{{
			DisplayModel:   "some-model",
			EffectiveCount: 1,
			Reason:         "fewer than 2 routable members (need 2+ for failover)",
		}},
	})
	seed(mixedProv, "mixed-prov", &DiscoveryDiff{
		Added: []ModelChange{{ModelID: "also-arrived", Reason: changeReasonNewModel}},
		Updated: []ModelUpdate{{
			ModelID: "also-priced",
			Changes: []FieldChange{{Field: changeFieldOutputPrice, Old: &oldPrice, New: &newPrice}},
		}},
	})

	resp := getStatus(t, r, "/discovery/status")

	// Anchor: all four entries must reach the client. Without this the
	// InformationalUnseen assertion below could pass on a feed that silently
	// came back empty (bad seed, wrong provider filter, a broken strip pass),
	// and it is also the positive half of the ruling: prices stay visible.
	if len(resp.Informational) != 4 {
		t.Fatalf("informational entries = %d, want 4 (all four must stay visible in the feed): %+v", len(resp.Informational), resp.Informational)
	}
	var priced *DiscoveryChangeEntry
	for i := range resp.Informational {
		if resp.Informational[i].ProviderName == "price-prov" {
			priced = &resp.Informational[i]
		}
	}
	if priced == nil {
		t.Fatal("the price-only entry must still be present in the feed")
	}
	if len(priced.Diff.Updated) != 1 || len(priced.Diff.Updated[0].Changes) != 1 {
		t.Fatalf("the price-only entry must keep its updated bucket, got %+v", priced.Diff.Updated)
	}

	// The count skips the price-ONLY entry and keeps the other three, including
	// the mixed one: only an entry with nothing but `updated` drops out.
	if resp.InformationalUnseen != 3 {
		t.Errorf("InformationalUnseen = %d, want 3 (added + failover + mixed count; only the price-only entry must not)", resp.InformationalUnseen)
	}
}

// TestGetDiscoveryStatus_ReviewStampsAfterReading is the ordering bug this
// endpoint has to avoid: stamping before computing would make "since your last
// visit" always report zero.
func TestGetDiscoveryStatus_ReviewStampsAfterReading(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	pool := h.dbPool.Pool()
	ctx := context.Background()
	truncateDiscoveryChanges(t)
	if err := h.settingsRepo.DeleteKey(ctx, settingKeyDiscoveryLastReviewed); err != nil {
		t.Fatalf("clear review key: %v", err)
	}

	providerID := seedClaimProvider(t, pool, "review-prov", true)
	seedClaimModel(t, pool, providerID, "gone-model", false, false, 0, nil)
	if _, err := AppendDiscoveryChange(ctx, pool, "scheduled", &providerID, "review-prov", &DiscoveryDiff{
		Disabled: []ModelChange{{ModelID: "gone-model", Reason: changeReasonNotListed}},
	}); err != nil {
		t.Fatalf("seed journal: %v", err)
	}

	first := getStatus(t, r, "/discovery/status?review=1")
	claim := findClaim(t, first, "gone-model")
	if claim.FlapSinceReview != 1 {
		t.Errorf("first review FlapSinceReview = %d, want 1", claim.FlapSinceReview)
	}

	second := getStatus(t, r, "/discovery/status?review=1")
	claim = findClaim(t, second, "gone-model")
	if claim.FlapSinceReview != 0 {
		t.Errorf("second review FlapSinceReview = %d, want 0: the first call must have stamped the key", claim.FlapSinceReview)
	}
}

// TestGetDiscoveryStatus_PlainGetDoesNotStamp: the 60s badge poll must never
// consume the operator's "since last visit" marker.
func TestGetDiscoveryStatus_PlainGetDoesNotStamp(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	ctx := context.Background()
	if err := h.settingsRepo.DeleteKey(ctx, settingKeyDiscoveryLastReviewed); err != nil {
		t.Fatalf("clear review key: %v", err)
	}

	getStatus(t, r, "/discovery/status")

	if _, found, err := h.settingsRepo.GetChecked(ctx, settingKeyDiscoveryLastReviewed); err != nil {
		t.Fatalf("read key: %v", err)
	} else if found {
		t.Error("a plain GET must not stamp the last-reviewed key")
	}
}

// TestGetDiscoveryStatus_ReviewSkipsStampInReadOnly pins the DemoReadOnly guard
// on the write: a GET must never 403 even in read-only mode (so the badge stays
// browsable on a demo instance), but the review stamp is a write, and read-only
// means no writes. review=1 is the variant that stamps in every other test
// (TestGetDiscoveryStatus_ReviewStampsAfterReading), so seeing it NOT stamp here
// is a real signal that the DemoReadOnly guard fired, not that review=1 is a
// no-op in general.
func TestGetDiscoveryStatus_ReviewSkipsStampInReadOnly(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	ctx := context.Background()
	if err := h.settingsRepo.DeleteKey(ctx, settingKeyDiscoveryLastReviewed); err != nil {
		t.Fatalf("clear review key: %v", err)
	}
	h.cfg.DemoReadOnly = true

	req := httptest.NewRequest(http.MethodGet, "/discovery/status?review=1", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /discovery/status?review=1 in read-only mode = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}

	if _, found, err := h.settingsRepo.GetChecked(ctx, settingKeyDiscoveryLastReviewed); err != nil {
		t.Fatalf("read key: %v", err)
	} else if found {
		t.Error("read-only mode must skip the last-reviewed stamp even with review=1")
	}
}

// TestGetDiscoveryStatus_ReviewStampClampedToWindow pins the gap carried over
// from Task 2: journal rows are pruned at ClaimWindow, so a stored
// last-reviewed stamp older than the window must not be used verbatim — that
// would compute "since review" flap counts against journal rows that no
// longer exist. The clamp must produce exactly the full-window count, not a
// truncated one.
func TestGetDiscoveryStatus_ReviewStampClampedToWindow(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	pool := h.dbPool.Pool()
	ctx := context.Background()
	truncateDiscoveryChanges(t)

	providerID := seedClaimProvider(t, pool, "stale-review-prov", true)
	seedClaimModel(t, pool, providerID, "gone-model", false, false, 0, nil)

	// One journal row inside the window (kept) and one that would be pruned
	// were pruning actually run (outside the window). Both are still present
	// in the table here (this test does not prune), so if the stale stamp were
	// used verbatim (no clamp), sinceReview would see both rows and report 2,
	// silently matching the un-clamped window count and hiding the bug. The
	// clamp must floor the lookback at now-ClaimWindow, which excludes the
	// outside-window row and yields 1 for both the window and since-review
	// counts (identical, per the task's premise: past the window "since your
	// last visit" degrades to "everything we still know about").
	if _, err := AppendDiscoveryChange(ctx, pool, "scheduled", &providerID, "stale-review-prov", &DiscoveryDiff{
		Disabled: []ModelChange{{ModelID: "gone-model", Reason: changeReasonNotListed}},
	}); err != nil {
		t.Fatalf("seed recent journal row: %v", err)
	}
	// Same provider+model as the recent row so both land under the identical
	// flapKey: only detected_at distinguishes them.
	outsideWindowAt := time.Now().Add(-(ClaimWindow + 24*time.Hour))
	if _, err := pool.Exec(ctx,
		`INSERT INTO discovery_changes (source, provider_id, provider_name, diff, detected_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		"scheduled", providerID, "stale-review-prov",
		[]byte(`{"disabled":[{"model_id":"gone-model","reason":"not_listed"}]}`), outsideWindowAt,
	); err != nil {
		t.Fatalf("seed outside-window journal row: %v", err)
	}

	// Stamp last-reviewed well outside the window: past the point where the
	// journal is pruned, so a naive lookup would derive sinceReview from rows
	// that (in production) no longer exist.
	staleStamp := time.Now().Add(-(ClaimWindow + 48*time.Hour))
	if err := h.settingsRepo.Set(ctx, settingKeyDiscoveryLastReviewed, staleStamp.Format(time.RFC3339Nano)); err != nil {
		t.Fatalf("seed stale review stamp: %v", err)
	}

	resp := getStatus(t, r, "/discovery/status?review=1")
	claim := findClaim(t, resp, "gone-model")
	if claim.FlapWindow != 1 {
		t.Fatalf("FlapWindow = %d, want 1 (sanity: the recent row must count)", claim.FlapWindow)
	}
	if claim.FlapSinceReview != claim.FlapWindow {
		t.Errorf("FlapSinceReview = %d, want %d (clamped to the window, matching FlapWindow exactly)", claim.FlapSinceReview, claim.FlapWindow)
	}
}

// TestDismissDiscoveryClaims_RemovesClaim: dismissing a model takes it out of the
// claim set. Asserted via /discovery/status (the observable API contract), not by
// re-reading discovery_dismissed_at, which TestSetModelsDismissed already covers
// at the SQL layer.
//
// There is no un-dismiss direction to test: the endpoint only stamps. The reversal
// is a sighting, and that models.Upsert nulls the column on one is proved in
// internal/model's TestUpsertClearsDiscoveryDismissedAt.
func TestDismissDiscoveryClaims_RemovesClaim(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	pool := h.dbPool.Pool()

	providerID := seedClaimProvider(t, pool, "dismiss-prov", true)
	seedClaimModel(t, pool, providerID, "doomed", false, false, 0, nil)

	// Anchor: the model must show up as a claim before it is dismissed, so the
	// "0 claims after dismiss" assertion below cannot pass by the model never
	// having been a claim in the first place.
	if ids := claimIDs(t, r); len(ids) != 1 || ids[0] != "doomed" {
		t.Fatalf("before dismiss, claims = %v, want [doomed]", ids)
	}

	body := `{"model_ids":["doomed"]}`
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/discovery/%s/dismiss", providerID), strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("dismiss = %d, want 200", rec.Code)
	}
	if len(claimIDs(t, r)) != 0 {
		t.Error("a dismissed model must not appear as a claim")
	}
}

// TestDismissDiscoveryClaims_UnknownModel fails loudly instead of reporting a
// silent success for a model that does not exist.
func TestDismissDiscoveryClaims_UnknownModel(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	providerID := seedClaimProvider(t, h.dbPool.Pool(), "dismiss-unknown", true)

	post := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/discovery/%s/dismiss", providerID), strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	// Anchor: an empty model_ids list is a 400 from the handler's own
	// validation, which only fires once the request actually reaches
	// DismissDiscoveryClaims. A route that were not mounted would 404 here
	// too (chi never gets far enough to read the body), so seeing 400 proves
	// the route is live before the 404 below is trusted to mean "no matching
	// model" rather than "no matching route". This does not touch the store
	// layer at all, so it does not duplicate TestSetModelsDismissed.
	if code := post(`{"model_ids":[]}`); code != http.StatusBadRequest {
		t.Fatalf("empty model_ids = %d, want 400 (anchor: proves the route is mounted)", code)
	}

	if code := post(`{"model_ids":["not-a-model"]}`); code != http.StatusNotFound {
		t.Errorf("unknown model = %d, want 404", code)
	}
}

// TestDismissDiscoveryClaims_SuspectModelNotDismissible pins Amendment 2: a
// model that is still enabled and mid-miss-streak is not gone yet, so
// pre-dismissing it would let it later go gone silently (Upsert only clears
// discovery_dismissed_at on a sighting, and a suspect model is not being
// sighted). Unlike TestDismissDiscoveryClaims_UnknownModel, the model here
// genuinely exists — a regression that dropped the `enabled = false AND
// disabled_manually = false` guard from setModelsDismissed's WHERE clause
// would turn this specific case from 404 into 200, which the unknown-model
// test cannot detect.
func TestDismissDiscoveryClaims_SuspectModelNotDismissible(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	pool := h.dbPool.Pool()

	providerID := seedClaimProvider(t, pool, "dismiss-suspect", true)
	seedClaimModel(t, pool, providerID, "wobbling", true, false, 1, nil)

	// Anchor 1: proves the seeded row is genuinely in suspect state, via
	// GET /discovery/status. This does NOT prove POST
	// /discovery/{provider_id}/dismiss is mounted — that route and the status
	// route are independently registered, so a status-route-only check cannot
	// catch a missing dismiss route.
	if claim := findClaim(t, getStatus(t, r, "/discovery/status"), "wobbling"); claim.State != ClaimStateSuspect {
		t.Fatalf("wobbling state = %q, want %q before the dismiss attempt", claim.State, ClaimStateSuspect)
	}

	post := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/discovery/%s/dismiss", providerID), strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	// Anchor 2: mirrors TestDismissDiscoveryClaims_UnknownModel's pattern. An
	// empty model_ids list only yields 400 once the request reaches the
	// handler's own validation; an unmounted route 404s before that code
	// runs. This proves POST /discovery/{provider_id}/dismiss is live before the 404 below
	// is trusted to mean "suspect model correctly rejected" rather than
	// "route not mounted".
	if code := post(`{"model_ids":[]}`); code != http.StatusBadRequest {
		t.Fatalf("empty model_ids = %d, want 400 (anchor: proves the route is mounted)", code)
	}

	if code := post(`{"model_ids":["wobbling"]}`); code != http.StatusNotFound {
		t.Errorf("dismiss suspect model = %d, want 404", code)
	}

	var dismissedAt *time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT discovery_dismissed_at FROM models WHERE provider_id = $1 AND model_id = $2`,
		providerID, "wobbling").Scan(&dismissedAt); err != nil {
		t.Fatalf("query: %v", err)
	}
	if dismissedAt != nil {
		t.Error("a rejected dismiss on a suspect model must not stamp discovery_dismissed_at")
	}
}

// TestUnpinDiscoveryClaims_ClearsPin walks the whole loop the modal drives: a
// pinned model sits in the informational pinned bucket, the endpoint names the
// row it cleared, and the model returns to automatic management as a suspect.
//
// Asserted through /discovery/status rather than by re-reading the column, which
// TestSetModelsUnpinned already covers at the SQL layer. What is only observable
// here is that the claim is DERIVED: the bucket a model sits in changes with no
// write to any claim state.
func TestUnpinDiscoveryClaims_ClearsPin(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	pool := h.dbPool.Pool()

	providerID := seedClaimProvider(t, pool, "unpin-prov", true)
	seedClaimModel(t, pool, providerID, "held", true, false, 3, nil)
	pinClaimModel(t, pool, providerID, "held")

	// Anchor: the model must read as pinned first, so the "gone from the modal
	// afterwards" assertion cannot pass on a model that was never a claim.
	if claim := findClaim(t, getStatus(t, r, "/discovery/status"), "held"); claim.State != ClaimStatePinned {
		t.Fatalf("held state = %q, want %q before the unpin", claim.State, ClaimStatePinned)
	}

	body := `{"model_ids":["held"]}`
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/discovery/%s/unpin", providerID), strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("unpin = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	// The response names the rows actually cleared; the dashboard marks exactly
	// those and leaves the rest alone.
	var resp struct {
		Unpinned []string `json:"unpinned"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Unpinned) != 1 || resp.Unpinned[0] != "held" {
		t.Fatalf("unpinned = %v, want [held]", resp.Unpinned)
	}

	// The unpinned model leaves the modal entirely rather than reappearing as a
	// suspect. Unpin resets the miss-streak with the stamp, so the row is
	// indistinguishable from a healthy enabled model until a scan misses it
	// again — which is the point: there is no discrepancy left to show, and the
	// next two missed scans raise it as suspect and then disable it, exactly as
	// they would for any unpinned model.
	if claim, ok := findClaimOK(getStatus(t, r, "/discovery/status"), "held"); ok {
		t.Errorf("held still claims %q after unpin, want no claim at all", claim.State)
	}

	// Absence in the modal must mean "nothing to report", not "discovery
	// disabled it on the way out": both read as no claim once the row is also
	// dismissed, and only the enabled flag tells them apart.
	var enabled bool
	if err := pool.QueryRow(context.Background(),
		`SELECT enabled FROM models WHERE provider_id = $1 AND model_id = $2`,
		providerID, "held").Scan(&enabled); err != nil {
		t.Fatalf("query: %v", err)
	}
	if !enabled {
		t.Error("unpin must not disable the model; it only hands it back to automatic management")
	}
}

// TestUnpinDiscoveryClaims_UnknownModel fails loudly instead of reporting a
// silent success, mirroring the dismiss endpoint's contract. A model that exists
// but carries no pin is the same non-event as one that does not exist at all:
// nothing to clear, so nothing to report as cleared.
func TestUnpinDiscoveryClaims_UnknownModel(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	pool := h.dbPool.Pool()
	providerID := seedClaimProvider(t, pool, "unpin-unknown", true)
	seedClaimModel(t, pool, providerID, "never-pinned", true, false, 1, nil)

	post := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/discovery/%s/unpin", providerID), strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	// Anchor: an empty model_ids list is a 400 from the handler's own
	// validation, which only runs once the request reaches UnpinDiscoveryClaims.
	// An unmounted route would 404 before that, so this proves the 404s below
	// mean "no matching model" rather than "no matching route".
	if code := post(`{"model_ids":[]}`); code != http.StatusBadRequest {
		t.Fatalf("empty model_ids = %d, want 400 (anchor: proves the route is mounted)", code)
	}
	if code := post(`{"model_ids":["not-a-model"]}`); code != http.StatusNotFound {
		t.Errorf("unknown model = %d, want 404", code)
	}
	if code := post(`{"model_ids":["never-pinned"]}`); code != http.StatusNotFound {
		t.Errorf("unpinned model = %d, want 404", code)
	}
}

func claimIDs(t *testing.T, r http.Handler) []string {
	t.Helper()
	var ids []string
	for _, p := range getStatus(t, r, "/discovery/status").Claims {
		for _, c := range p.Gone {
			ids = append(ids, c.ModelID)
		}
	}
	return ids
}

func findClaim(t *testing.T, resp DiscoveryStatusResponse, modelID string) ModelClaim {
	t.Helper()
	c, ok := findClaimOK(resp, modelID)
	if !ok {
		t.Fatalf("claim %q not found in %+v", modelID, resp.Claims)
	}
	return c
}

// findClaimOK is the searching half of findClaim, split out for the tests that
// assert a model has NO claim: findClaim's t.Fatalf on a miss is exactly the
// outcome those want to see succeed.
func findClaimOK(resp DiscoveryStatusResponse, modelID string) (ModelClaim, bool) {
	for _, p := range resp.Claims {
		for _, group := range [][]ModelClaim{p.Gone, p.Stale, p.Suspect, p.Retired, p.Pinned} {
			for _, c := range group {
				if c.ModelID == modelID {
					return c, true
				}
			}
		}
	}
	return ModelClaim{}, false
}
