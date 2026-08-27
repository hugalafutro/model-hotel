package quota_test

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/db"
	"github.com/hugalafutro/model-hotel/internal/quota"
)

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx := context.Background()
	dbURL, setupErr := db.SetupTestDB("quota")
	if setupErr != nil {
		log.Printf("failed to setup test DB: %v", setupErr)
		os.Exit(1)
	}
	defer db.CleanupTestDB("quota")

	testDB, err := db.New(ctx, dbURL, 25, 5)
	if err != nil {
		log.Printf("failed to initialize test DB: %v", err)
		os.Exit(1) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
	}
	testPool = testDB.Pool()
	defer testDB.Close()

	os.Exit(m.Run()) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
}

// insertTestProvider inserts a provider row and returns its ID.
func insertTestProvider(ctx context.Context, t *testing.T, name string) uuid.UUID {
	t.Helper()

	// Need the same columns that the app would write.
	// encrypted_key, key_nonce, key_salt are nullable after migration 026.
	id := uuid.New()
	_, err := testPool.Exec(ctx, `
		INSERT INTO providers (id, name, base_url, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, true, now(), now())
	`, id, name, "https://test.example.com")
	if err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	return id
}

// cleanupProvider deletes the test provider row (snapshots cascade via FK).
func cleanupProvider(ctx context.Context, t *testing.T, providerID uuid.UUID) {
	t.Helper()

	_, _ = testPool.Exec(ctx, `DELETE FROM providers WHERE id = $1`, providerID)
}

// TestListPopulatesLastError: a failed refresh leaves a row whose last_error is
// carried through List (the export reader relies on the field being populated).
func TestListPopulatesLastError(t *testing.T) {
	ctx := context.Background()
	repo := quota.NewRepository(testPool)
	id := insertTestProvider(ctx, t, "list-lasterr")
	defer cleanupProvider(ctx, t, id)

	if err := repo.RecordFailure(ctx, id, "usage", "boom"); err != nil {
		t.Fatalf("record failure: %v", err)
	}
	snaps, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found bool
	for _, s := range snaps {
		if s.ProviderID == id && s.Kind == "usage" {
			found = true
			if s.LastError != "boom" {
				t.Fatalf("last_error = %q, want boom", s.LastError)
			}
		}
	}
	if !found {
		t.Fatal("failure placeholder row not returned by List")
	}
}

// TestUpsertIfNewerDefaultsFetchedAt: an incoming snapshot with a zero FetchedAt
// is stamped now and applied against an empty row.
func TestUpsertIfNewerDefaultsFetchedAt(t *testing.T) {
	ctx := context.Background()
	repo := quota.NewRepository(testPool)
	id := insertTestProvider(ctx, t, "upsertnewer-default")
	defer cleanupProvider(ctx, t, id)

	wrote, err := repo.UpsertIfNewer(ctx, quota.Snapshot{
		ProviderID: id,
		Kind:       "usage",
		Payload:    json.RawMessage(`{"used":1}`),
		HTTPStatus: 200,
		Source:     "fleet",
		// FetchedAt left zero on purpose.
	})
	if err != nil {
		t.Fatalf("upsert if newer: %v", err)
	}
	if !wrote {
		t.Fatal("first write should apply")
	}
	got, err := repo.Get(ctx, id, "usage")
	if err != nil || got == nil {
		t.Fatalf("get: %v (snap %v)", err, got)
	}
	if got.FetchedAt.IsZero() {
		t.Fatal("fetched_at should have been defaulted to now")
	}
}

// TestRepositoryContextCancelledErrors: each query surfaces the DB error (rather
// than panicking or masking it) when the context is already cancelled.
func TestRepositoryContextCancelledErrors(t *testing.T) {
	repo := quota.NewRepository(testPool)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := repo.Get(ctx, uuid.New(), "usage"); err == nil {
		t.Error("Get: want error on cancelled context")
	}
	if _, err := repo.List(ctx); err == nil {
		t.Error("List: want error on cancelled context")
	}
	if _, err := repo.UpsertIfNewer(ctx, quota.Snapshot{
		ProviderID: uuid.New(),
		Kind:       "usage",
		Payload:    json.RawMessage(`{}`),
		FetchedAt:  time.Now(),
	}); err == nil {
		t.Error("UpsertIfNewer: want error on cancelled context")
	}
}

// jsonEqual compares two JSON payloads by value rather than by raw bytes:
// Postgres JSONB re-serializes on write/read (e.g. adds a space after ':'),
// so a raw byte/string comparison against the literal we inserted is not
// reliable even though the payload is unchanged.
func jsonEqual(t *testing.T, a, b json.RawMessage) bool {
	t.Helper()
	var av, bv any
	if err := json.Unmarshal(a, &av); err != nil {
		t.Fatalf("unmarshal a: %v", err)
	}
	if err := json.Unmarshal(b, &bv); err != nil {
		t.Fatalf("unmarshal b: %v", err)
	}
	return reflect.DeepEqual(av, bv)
}

func TestUpsertAndGet(t *testing.T) {
	ctx := context.Background()
	repo := quota.NewRepository(testPool)

	provID := insertTestProvider(ctx, t, "test-quota-upsert-get")
	t.Cleanup(func() { cleanupProvider(ctx, t, provID) })

	err := repo.Upsert(ctx, quota.Snapshot{
		ProviderID: provID,
		Kind:       "usage",
		Payload:    json.RawMessage(`{"used":10}`),
		HTTPStatus: 200,
		Source:     "poll",
	})
	if err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := repo.Get(ctx, provID, "usage")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || !jsonEqual(t, got.Payload, json.RawMessage(`{"used":10}`)) || got.HTTPStatus != 200 {
		t.Fatalf("unexpected snapshot: %+v", got)
	}
}

func TestGetAbsentReturnsNil(t *testing.T) {
	repo := quota.NewRepository(testPool)
	got, err := repo.Get(context.Background(), uuid.New(), "usage")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for absent snapshot, got %+v", got)
	}
}

func TestRecordFailureKeepsPayload(t *testing.T) {
	ctx := context.Background()
	repo := quota.NewRepository(testPool)

	provID := insertTestProvider(ctx, t, "test-quota-record-failure")
	t.Cleanup(func() { cleanupProvider(ctx, t, provID) })

	_ = repo.Upsert(ctx, quota.Snapshot{ProviderID: provID, Kind: "usage", Payload: json.RawMessage(`{"used":5}`), HTTPStatus: 200, Source: "poll"})
	if err := repo.RecordFailure(ctx, provID, "usage", "boom"); err != nil {
		t.Fatalf("record failure: %v", err)
	}
	got, _ := repo.Get(ctx, provID, "usage")
	if got == nil || !jsonEqual(t, got.Payload, json.RawMessage(`{"used":5}`)) || got.LastError != "boom" {
		t.Fatalf("failure should keep payload and set last_error: %+v", got)
	}
}

func TestUpsertIfNewer(t *testing.T) {
	ctx := context.Background()
	repo := quota.NewRepository(testPool)

	provID := insertTestProvider(ctx, t, "test-quota-upsert-if-newer")
	t.Cleanup(func() { cleanupProvider(ctx, t, provID) })

	older := time.Now().Add(-10 * time.Minute)
	newer := time.Now()

	// First write always applies.
	applied, err := repo.UpsertIfNewer(ctx, quota.Snapshot{ProviderID: provID, Kind: "usage", Payload: json.RawMessage(`{"v":1}`), HTTPStatus: 200, Source: "fleet", FetchedAt: newer})
	if err != nil || !applied {
		t.Fatalf("first write should apply: applied=%v err=%v", applied, err)
	}

	// An older incoming write is skipped so it cannot clobber a fresher local one.
	applied, err = repo.UpsertIfNewer(ctx, quota.Snapshot{ProviderID: provID, Kind: "usage", Payload: json.RawMessage(`{"v":2}`), HTTPStatus: 200, Source: "fleet", FetchedAt: older})
	if err != nil || applied {
		t.Fatalf("older write should be skipped: applied=%v err=%v", applied, err)
	}

	got, _ := repo.Get(ctx, provID, "usage")
	if got == nil || !jsonEqual(t, got.Payload, json.RawMessage(`{"v":1}`)) {
		t.Fatalf("older write must not clobber newer payload, got %+v", got)
	}
}

// TestUpsertIfNewerPersistsFailureMarker: the fleet import path must store the
// failure marker it was handed rather than forcing it to NULL. A member that
// drops the marker classifies a row whose latest refresh failed as affirmative
// recovery evidence and releases a still-exhausted provider's quota pin.
func TestUpsertIfNewerPersistsFailureMarker(t *testing.T) {
	ctx := context.Background()
	repo := quota.NewRepository(testPool)

	provID := insertTestProvider(ctx, t, "test-quota-upsertnewer-marker")
	t.Cleanup(func() { cleanupProvider(ctx, t, provID) })

	if _, err := repo.UpsertIfNewer(ctx, quota.Snapshot{
		ProviderID: provID, Kind: "usage", Payload: json.RawMessage(`{"v":1}`),
		HTTPStatus: 200, Source: "fleet", FetchedAt: time.Now(), LastError: "upstream 500",
	}); err != nil {
		t.Fatalf("upsert if newer: %v", err)
	}

	got, err := repo.Get(ctx, provID, "usage")
	if err != nil || got == nil {
		t.Fatalf("get: %v (snap %v)", err, got)
	}
	if got.LastError != "upstream 500" {
		t.Fatalf("last_error = %q, want the incoming marker to be persisted", got.LastError)
	}
}

// TestUpsertIfNewerAddsFailureMarkerAtEqualFetchedAt covers the gap the
// strictly-newer WHERE clause leaves open. RecordFailure preserves fetched_at,
// so a failed refresh does not make the primary's row newer: a member that
// already holds that exact snapshot would otherwise never learn the refresh
// behind it has started failing, and would keep treating it as recovery
// evidence for as long as it stays inside the staleness bound. Taking the
// marker at an equal timestamp can only ever hold a pin longer, never release
// one, which is the cheap direction to be wrong in.
func TestUpsertIfNewerAddsFailureMarkerAtEqualFetchedAt(t *testing.T) {
	ctx := context.Background()
	repo := quota.NewRepository(testPool)

	provID := insertTestProvider(ctx, t, "test-quota-upsertnewer-equal-marker")
	t.Cleanup(func() { cleanupProvider(ctx, t, provID) })

	at := time.Now()
	if _, err := repo.UpsertIfNewer(ctx, quota.Snapshot{
		ProviderID: provID, Kind: "usage", Payload: json.RawMessage(`{"v":1}`),
		HTTPStatus: 200, Source: "fleet", FetchedAt: at,
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	wrote, err := repo.UpsertIfNewer(ctx, quota.Snapshot{
		ProviderID: provID, Kind: "usage", Payload: json.RawMessage(`{"v":1}`),
		HTTPStatus: 200, Source: "fleet", FetchedAt: at, LastError: "upstream 500",
	})
	if err != nil {
		t.Fatalf("marker top-up: %v", err)
	}
	if !wrote {
		t.Fatal("a marker arriving on an equally fresh snapshot must be taken: RecordFailure preserves fetched_at, so the failure never arrives as a newer row")
	}
	got, err := repo.Get(ctx, provID, "usage")
	if err != nil || got == nil {
		t.Fatalf("get: %v (snap %v)", err, got)
	}
	if got.LastError != "upstream 500" {
		t.Fatalf("last_error = %q, want the marker to have been applied", got.LastError)
	}
}

// TestUpsertIfNewerKeepsMarkerWhenNoNewerEvidence: the equal-timestamp path is
// one-way. A marker-free snapshot at the same fetched_at is not proof the
// provider recovered (it is the same reading, from a node that has not seen the
// failure), so it must not clear a marker already held. Only a strictly newer
// snapshot, i.e. an actual successful refresh, does that.
func TestUpsertIfNewerKeepsMarkerWhenNoNewerEvidence(t *testing.T) {
	ctx := context.Background()
	repo := quota.NewRepository(testPool)

	provID := insertTestProvider(ctx, t, "test-quota-upsertnewer-keeps-marker")
	t.Cleanup(func() { cleanupProvider(ctx, t, provID) })

	at := time.Now()
	if _, err := repo.UpsertIfNewer(ctx, quota.Snapshot{
		ProviderID: provID, Kind: "usage", Payload: json.RawMessage(`{"v":1}`),
		HTTPStatus: 200, Source: "fleet", FetchedAt: at, LastError: "upstream 500",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	wrote, err := repo.UpsertIfNewer(ctx, quota.Snapshot{
		ProviderID: provID, Kind: "usage", Payload: json.RawMessage(`{"v":1}`),
		HTTPStatus: 200, Source: "fleet", FetchedAt: at,
	})
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if wrote {
		t.Fatal("a marker-free copy of the same snapshot must not be written: it is not newer evidence")
	}
	got, err := repo.Get(ctx, provID, "usage")
	if err != nil || got == nil {
		t.Fatalf("get: %v (snap %v)", err, got)
	}
	if got.LastError != "upstream 500" {
		t.Fatalf("last_error = %q, want the marker kept — only a newer successful refresh clears it", got.LastError)
	}
}

// TestUpsertIfNewerNewerSuccessClearsMarker is what stops the marker wedging a
// pin permanently: once the primary polls successfully again its row is
// strictly newer, and that write clears the marker on the member too.
func TestUpsertIfNewerNewerSuccessClearsMarker(t *testing.T) {
	ctx := context.Background()
	repo := quota.NewRepository(testPool)

	provID := insertTestProvider(ctx, t, "test-quota-upsertnewer-clears-marker")
	t.Cleanup(func() { cleanupProvider(ctx, t, provID) })

	at := time.Now()
	if _, err := repo.UpsertIfNewer(ctx, quota.Snapshot{
		ProviderID: provID, Kind: "usage", Payload: json.RawMessage(`{"v":1}`),
		HTTPStatus: 200, Source: "fleet", FetchedAt: at, LastError: "upstream 500",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if _, err := repo.UpsertIfNewer(ctx, quota.Snapshot{
		ProviderID: provID, Kind: "usage", Payload: json.RawMessage(`{"v":2}`),
		HTTPStatus: 200, Source: "fleet", FetchedAt: at.Add(time.Minute),
	}); err != nil {
		t.Fatalf("newer success: %v", err)
	}

	got, err := repo.Get(ctx, provID, "usage")
	if err != nil || got == nil {
		t.Fatalf("get: %v (snap %v)", err, got)
	}
	if got.LastError != "" {
		t.Fatalf("last_error = %q, want a newer successful refresh to clear it", got.LastError)
	}
}

// TestUpsertClearsFailureMarker pins the other half of the asymmetry: Upsert is
// the local successful-fetch path, so it clears any marker. Without this the
// guard could wedge a pin forever on a node that recovers on its own.
func TestUpsertClearsFailureMarker(t *testing.T) {
	ctx := context.Background()
	repo := quota.NewRepository(testPool)

	provID := insertTestProvider(ctx, t, "test-quota-upsert-clears-marker")
	t.Cleanup(func() { cleanupProvider(ctx, t, provID) })

	if err := repo.Upsert(ctx, quota.Snapshot{
		ProviderID: provID, Kind: "usage", Payload: json.RawMessage(`{"v":1}`),
		HTTPStatus: 200, Source: "poll",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := repo.RecordFailure(ctx, provID, "usage", "boom"); err != nil {
		t.Fatalf("record failure: %v", err)
	}
	if err := repo.Upsert(ctx, quota.Snapshot{
		ProviderID: provID, Kind: "usage", Payload: json.RawMessage(`{"v":2}`),
		HTTPStatus: 200, Source: "poll",
	}); err != nil {
		t.Fatalf("successful re-poll: %v", err)
	}

	got, err := repo.Get(ctx, provID, "usage")
	if err != nil || got == nil {
		t.Fatalf("get: %v (snap %v)", err, got)
	}
	if got.LastError != "" {
		t.Fatalf("last_error = %q, want a successful local fetch to clear it", got.LastError)
	}
}

func TestList(t *testing.T) {
	ctx := context.Background()
	repo := quota.NewRepository(testPool)

	provID := insertTestProvider(ctx, t, "test-quota-list")
	t.Cleanup(func() { cleanupProvider(ctx, t, provID) })

	if err := repo.Upsert(ctx, quota.Snapshot{ProviderID: provID, Kind: "usage", Payload: json.RawMessage(`{}`), HTTPStatus: 200, Source: "poll"}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	all, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// The test DB is shared across quota tests, so assert our row is present
	// rather than an exact count.
	found := false
	for _, s := range all {
		if s.ProviderID == provID && s.Kind == "usage" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("list did not contain the inserted snapshot (got %d rows)", len(all))
	}
}

// TestUpsertIfNewer_ClampsFutureFetchedAt covers a wedge, not a cosmetic oddity.
// FetchedAt arrives from the wire on the fleet distribution path (internal/api
// ReceiveSnapshots) and was stored unvalidated, which broke two things at once:
// every "is this still fresh" check uses time.Since, which goes negative for a
// future stamp and so reads as permanently fresh; and the upsert keeps the newer
// row, so a future-dated snapshot outranks every real poll that follows it. One
// poisoned row therefore froze a provider's quota data indefinitely.
//
// A fetch cannot have happened in the future, so the honest repair is to clamp
// rather than reject: the row is still recorded, just not with a timestamp that
// wins forever.
func TestUpsertIfNewer_ClampsFutureFetchedAt(t *testing.T) {
	ctx := context.Background()
	repo := quota.NewRepository(testPool)

	provID := insertTestProvider(ctx, t, "test-quota-future-fetched-at")
	t.Cleanup(func() { cleanupProvider(ctx, t, provID) })

	future := time.Now().Add(48 * time.Hour)
	if _, err := repo.UpsertIfNewer(ctx, quota.Snapshot{
		ProviderID: provID, Kind: "usage", Payload: json.RawMessage(`{"poisoned":true}`),
		HTTPStatus: 200, Source: "fleet", FetchedAt: future,
	}); err != nil {
		t.Fatalf("UpsertIfNewer(future): %v", err)
	}

	stored, err := repo.Get(ctx, provID, "usage")
	if err != nil || stored == nil {
		t.Fatalf("Get after future upsert: %v", err)
	}
	if stored.FetchedAt.After(time.Now().Add(time.Minute)) {
		t.Errorf("stored fetched_at = %v, want clamped to about now, not %v", stored.FetchedAt, future)
	}

	// The point of the clamp: an ordinary poll landing afterwards must still win.
	applied, err := repo.UpsertIfNewer(ctx, quota.Snapshot{
		ProviderID: provID, Kind: "usage", Payload: json.RawMessage(`{"real":true}`),
		HTTPStatus: 200, Source: "poll", FetchedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("UpsertIfNewer(real): %v", err)
	}
	if !applied {
		t.Fatal("a real poll after a future-dated row was rejected; the poisoned row still outranks it")
	}
	after, _ := repo.Get(ctx, provID, "usage")
	if after == nil || !jsonEqual(t, after.Payload, json.RawMessage(`{"real":true}`)) {
		t.Errorf("payload = %v, want the later real poll to have replaced the poisoned row", after)
	}
}
