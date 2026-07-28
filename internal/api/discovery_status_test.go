package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
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
	if resp.InformationalUnseen != 2 {
		t.Errorf("InformationalUnseen = %d, want 2 (both rows are unseen news)", resp.InformationalUnseen)
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
	if err := h.settingsRepo.Set(ctx, settingKeyDiscoveryLastReviewed, staleStamp.Format(time.RFC3339)); err != nil {
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

func findClaim(t *testing.T, resp DiscoveryStatusResponse, modelID string) ModelClaim {
	t.Helper()
	for _, p := range resp.Claims {
		for _, group := range [][]ModelClaim{p.Gone, p.Stale, p.Suspect} {
			for _, c := range group {
				if c.ModelID == modelID {
					return c
				}
			}
		}
	}
	t.Fatalf("claim %q not found in %+v", modelID, resp.Claims)
	return ModelClaim{}
}
