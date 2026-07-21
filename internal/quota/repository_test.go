package quota_test

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"reflect"
	"testing"

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
