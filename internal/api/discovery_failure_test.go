package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/db"
)

// closedAPIPool returns a second handle on the test database that is already
// closed, so every query issued through it fails the way a real outage does.
// A closed pool is used rather than a cancelled context because the handlers
// derive their context from the request: a cancelled request context would also
// change what the HTTP layer does with the response.
func closedAPIPool(t *testing.T) *db.DB {
	t.Helper()
	if apiTestDBURL == "" {
		t.Fatal("test database not available")
	}
	database, err := db.New(context.Background(), apiTestDBURL, 2, 1)
	if err != nil {
		t.Fatalf("open second test DB handle: %v", err)
	}
	database.Close()
	return database
}

// TestGetDiscoveryStatus_DatabaseFailureIs500AndDoesNotStamp pins what the
// endpoint owes the operator when the claim derivation cannot run at all.
//
// Two things matter and the second is the one that is easy to get wrong. The
// response must be a 500 rather than an empty-but-successful claim set, because
// an empty claim set is exactly how the dashboard says "nothing is wrong" and
// an outage must never be rendered as nothing being wrong. And the review stamp
// must NOT be written: review=1 consumes the operator's "since your last visit"
// marker, so stamping on a request that showed them nothing would silently
// discard flap counts they never got to see.
func TestGetDiscoveryStatus_DatabaseFailureIs500AndDoesNotStamp(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	ctx := context.Background()
	if err := h.settingsRepo.DeleteKey(ctx, settingKeyDiscoveryLastReviewed); err != nil {
		t.Fatalf("clear review key: %v", err)
	}

	// Anchor: the identical request succeeds while the pool is healthy, so the
	// 500 below cannot come from an unmounted route or a rejected token. A plain
	// GET (no review=1) never stamps, so this leaves the marker untouched.
	getStatus(t, r, "/discovery/status")

	h.dbPool = closedAPIPool(t)

	req := httptest.NewRequest(http.MethodGet, "/discovery/status?review=1", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("GET /discovery/status?review=1 with a dead pool = %d, want 500; body: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"claims"`) {
		t.Errorf("a failed derivation must not also emit a claim payload, got %s", rec.Body.String())
	}
	if _, found, err := h.settingsRepo.GetChecked(ctx, settingKeyDiscoveryLastReviewed); err != nil {
		t.Fatalf("read review key: %v", err)
	} else if found {
		t.Error("a status request that failed must not consume the last-reviewed marker")
	}
}

// TestGetDiscoveryStatus_StampFailureStillServesTheModal pins that the review
// stamp is best-effort.
//
// The stamp decides one thing only: whether the next visit's flap counts are
// labelled "since your last visit". Failing the whole request over it would
// blank the discrepancy modal because of a settings write that has nothing to
// do with the discrepancies themselves. The write failure is logged and the
// operator still gets their claims.
func TestGetDiscoveryStatus_StampFailureStillServesTheModal(t *testing.T) {
	h := newTestHandler(t)
	pool := h.dbPool.Pool()
	providerID := seedClaimProvider(t, pool, "stamp-fail-prov", true)
	seedClaimModel(t, pool, providerID, "gone-model", false, false, 0, nil)

	stampErr := errors.New("settings write refused")
	attempted := 0
	h.settingsRepo = &mockSettingsStore{
		// No stored stamp: the "never reviewed" branch, which is the one that
		// reaches the write below.
		getWithDefaultFn: func(context.Context, string, string) string { return "" },
		setFn: func(_ context.Context, key, _ string) error {
			if key == settingKeyDiscoveryLastReviewed {
				attempted++
			}
			return stampErr
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/discovery/status?review=1", http.NoBody)
	rec := httptest.NewRecorder()
	h.GetDiscoveryStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status with a failing stamp write = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	// Without this the test would pass just as happily against a handler that
	// never attempted the write at all.
	if attempted != 1 {
		t.Fatalf("the last-reviewed write was attempted %d times, want exactly 1", attempted)
	}
	var resp DiscoveryStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode status body: %v", err)
	}
	if resp.ClaimCount != 1 {
		t.Fatalf("claim_count = %d, want 1: the claims must survive a failed stamp", resp.ClaimCount)
	}
	if claim := findClaim(t, resp, "gone-model"); claim.State != ClaimStateGone {
		t.Errorf("gone-model state = %q, want %q", claim.State, ClaimStateGone)
	}
}

// TestGetDiscoveryStatus_CorruptReviewStampOverReports pins parseLastReviewed's
// documented degradation direction.
//
// A last-reviewed value that does not parse (written by an older build, hand
// edited, truncated) must be read as "never reviewed", which shows every flap
// in the window again, rather than as "reviewed just now", which would hide
// them. The endpoint must also rewrite the value in the current format, so the
// corruption costs one over-report and then self-heals.
func TestGetDiscoveryStatus_CorruptReviewStampOverReports(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	pool := h.dbPool.Pool()
	ctx := context.Background()
	truncateDiscoveryChanges(t)

	providerID := seedClaimProvider(t, pool, "corrupt-stamp-prov", true)
	seedClaimModel(t, pool, providerID, "gone-model", false, false, 0, nil)
	if _, err := AppendDiscoveryChange(ctx, pool, "scheduled", &providerID, "corrupt-stamp-prov", &DiscoveryDiff{
		Disabled: []ModelChange{{ModelID: "gone-model", Reason: changeReasonNotListed}},
	}); err != nil {
		t.Fatalf("seed journal: %v", err)
	}
	if err := h.settingsRepo.Set(ctx, settingKeyDiscoveryLastReviewed, "not-a-timestamp"); err != nil {
		t.Fatalf("seed corrupt stamp: %v", err)
	}

	claim := findClaim(t, getStatus(t, r, "/discovery/status?review=1"), "gone-model")
	if claim.FlapWindow != 1 {
		t.Fatalf("FlapWindow = %d, want 1 (sanity: the journal row must be counted at all)", claim.FlapWindow)
	}
	if claim.FlapSinceReview != claim.FlapWindow {
		t.Errorf("FlapSinceReview = %d, want %d: an unparseable stamp must degrade to \"never reviewed\", not to \"just reviewed\"",
			claim.FlapSinceReview, claim.FlapWindow)
	}

	// Self-heal: the corrupt value is replaced by a parseable one, so the very
	// next visit is measured normally instead of over-reporting forever.
	stored := h.settingsRepo.GetWithDefault(ctx, settingKeyDiscoveryLastReviewed, "")
	if _, err := time.Parse(time.RFC3339Nano, stored); err != nil {
		t.Fatalf("stored stamp %q is still not parseable: %v", stored, err)
	}
	if second := findClaim(t, getStatus(t, r, "/discovery/status?review=1"), "gone-model"); second.FlapSinceReview != 0 {
		t.Errorf("second review FlapSinceReview = %d, want 0: the corrupt stamp must have been overwritten", second.FlapSinceReview)
	}
}

// TestDismissDiscoveryClaims_RejectsMalformedRequests pins that the endpoint
// refuses a request it cannot understand instead of guessing.
//
// Both rejections are 400 and neither is allowed to reach the database. The
// distinction that matters is 400 versus 404: 404 means "understood, matched
// nothing", which the client shows as "that model is already gone from the
// list". A body that does not decode names no provider at all, and a
// provider_id that is not a UUID cannot be compared against models.provider_id,
// so neither has any business claiming the request was understood.
func TestDismissDiscoveryClaims_RejectsMalformedRequests(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	pool := h.dbPool.Pool()
	providerID := seedClaimProvider(t, pool, "dismiss-malformed", true)
	seedClaimModel(t, pool, providerID, "gone-model", false, false, 0, nil)

	post := func(provider, body string) int {
		req := httptest.NewRequest(http.MethodPost, "/discovery/"+provider+"/dismiss", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := post(providerID.String(), `{"model_ids": not json at all`); code != http.StatusBadRequest {
		t.Errorf("undecodable body = %d, want 400", code)
	}
	if code := post("nanogpt", `{"model_ids":["gone-model"]}`); code != http.StatusBadRequest {
		t.Errorf("non-UUID provider_id = %d, want 400 (not 404: the request was never understood)", code)
	}

	// Anchor: an otherwise identical, well-formed request succeeds against the
	// same fixture, so the 400s above are the validation firing rather than the
	// endpoint rejecting everything.
	if code := post(providerID.String(), `{"model_ids":["gone-model"]}`); code != http.StatusOK {
		t.Fatalf("well-formed dismiss = %d, want 200", code)
	}
}

// TestUnpinDiscoveryClaims_RejectsMalformedRequests mirrors
// TestDismissDiscoveryClaims_RejectsMalformedRequests for the unpin endpoint's
// twin validation: an undecodable body and a non-UUID provider path are both 400,
// and neither reaches the database, for the same 400-vs-404 reason as dismiss.
func TestUnpinDiscoveryClaims_RejectsMalformedRequests(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	pool := h.dbPool.Pool()
	providerID := seedClaimProvider(t, pool, "unpin-malformed", true)
	seedClaimModel(t, pool, providerID, "held-model", true, false, 1, nil)
	pinClaimModel(t, pool, providerID, "held-model")

	post := func(provider, body string) int {
		req := httptest.NewRequest(http.MethodPost, "/discovery/"+provider+"/unpin", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer test-admin-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := post(providerID.String(), `{"model_ids": not json at all`); code != http.StatusBadRequest {
		t.Errorf("undecodable body = %d, want 400", code)
	}
	if code := post("nanogpt", `{"model_ids":["held-model"]}`); code != http.StatusBadRequest {
		t.Errorf("non-UUID provider_id = %d, want 400 (not 404: the request was never understood)", code)
	}

	// Anchor: an otherwise identical, well-formed request succeeds against the
	// same fixture, so the 400s above are the validation firing rather than the
	// endpoint rejecting everything.
	if code := post(providerID.String(), `{"model_ids":["held-model"]}`); code != http.StatusOK {
		t.Fatalf("well-formed unpin = %d, want 200", code)
	}
}

// TestDismissDiscoveryClaims_DatabaseFailureIs500 pins that a dismissal which
// did not land is reported as a failure.
//
// The dashboard hides the claim as soon as it sees a 2xx and offers Undo from a
// toast. Reporting success for an UPDATE that never ran would leave the
// operator believing they had silenced a live discrepancy; it would then
// reappear on the next 60s poll with no explanation, and the Undo they were
// offered would refer to a dismissal that never existed.
func TestDismissDiscoveryClaims_DatabaseFailureIs500(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	livePool := h.dbPool.Pool()
	providerID := seedClaimProvider(t, livePool, "dismiss-db-down", true)
	seedClaimModel(t, livePool, providerID, "gone-model", false, false, 0, nil)

	h.dbPool = closedAPIPool(t)

	body := `{"model_ids":["gone-model"]}`
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/discovery/%s/dismiss", providerID), strings.NewReader(body))
	req.Header.Set("Authorization", "Bearer test-admin-token")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("dismiss with a dead pool = %d, want 500; body: %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), `"updated"`) {
		t.Errorf("a failed dismissal must not report an updated count, got %s", rec.Body.String())
	}

	// The claim really is still live: checked through the healthy pool, so this
	// asserts the state of the database rather than the state of the handler.
	var dismissedAt *time.Time
	if err := livePool.QueryRow(context.Background(),
		`SELECT discovery_dismissed_at FROM models WHERE provider_id = $1 AND model_id = $2`,
		providerID, "gone-model").Scan(&dismissedAt); err != nil {
		t.Fatalf("query dismissal stamp: %v", err)
	}
	if dismissedAt != nil {
		t.Error("a 500 must mean nothing was dismissed, but discovery_dismissed_at is set")
	}
}
