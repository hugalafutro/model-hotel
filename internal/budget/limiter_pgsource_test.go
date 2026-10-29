package budget

import (
	"context"
	"log"
	"math"
	"os"
	"testing"
	"time"

	"github.com/hugalafutro/model-hotel/internal/db"
)

// testDB backs the PGSource tests; the limiter tests use fakeSource and never
// touch it.
var testDB *db.DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	testDBURL, setupErr := db.SetupTestDB("budget")
	if setupErr != nil {
		log.Printf("failed to setup test DB: %v", setupErr)
		os.Exit(1)
	}
	defer db.CleanupTestDB("budget")

	var err error
	testDB, err = db.New(ctx, testDBURL, 5, 1)
	if err != nil {
		log.Printf("failed to initialize test DB: %v", err)
		os.Exit(1)
	}
	defer testDB.Close()

	os.Exit(m.Run())
}

// TestPGSource_SpendSumsBySubjectSinceTheCutoff: a key's spend is its own
// rows, a user's is every row stamped with them as owner (keyed or keyless),
// rows before the cutoff and unpriced rows count for nothing, and a subject
// with no rows is a known zero, not an error.
func TestPGSource_SpendSumsBySubjectSinceTheCutoff(t *testing.T) {
	ctx := context.Background()
	pool := testDB.Pool()
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM request_logs`)
		_, _ = pool.Exec(context.Background(), `DELETE FROM users`)
	})
	var owner, other string
	if err := pool.QueryRow(ctx, `INSERT INTO users (username, password_hash) VALUES ('spender', 'x') RETURNING id::text`).Scan(&owner); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO users (username, password_hash) VALUES ('other', 'x') RETURNING id::text`).Scan(&other); err != nil {
		t.Fatal(err)
	}
	const keyA, keyB = "11111111-1111-1111-1111-111111111111", "22222222-2222-2222-2222-222222222222"
	cutoff := time.Now().UTC().Add(-time.Hour)
	seed := func(key, ownerID *string, cost *float64, at time.Time) {
		t.Helper()
		if _, err := pool.Exec(ctx, `INSERT INTO request_logs (virtual_key_id, owner_user_id, cost_usd, created_at) VALUES ($1, $2, $3, $4)`,
			key, ownerID, cost, at); err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	p := func(s string) *string { return &s }
	f := func(v float64) *float64 { return &v }
	seed(p(keyA), &owner, f(1.25), cutoff.Add(time.Minute)) // key A, owned
	seed(p(keyA), &owner, f(0.5), cutoff)                   // exactly at the cutoff: counted
	seed(p(keyA), &owner, f(100), cutoff.Add(-time.Minute)) // before the cutoff: excluded
	seed(p(keyA), &owner, nil, cutoff.Add(time.Minute))     // unpriced: nothing
	seed(nil, &owner, f(2), cutoff.Add(time.Minute))        // keyless dashboard chat by the owner
	seed(p(keyB), &other, f(7), cutoff.Add(time.Minute))    // someone else's key
	seed(p(keyB), &other, f(30), cutoff.Add(-24*time.Hour)) // someone else, last period

	src := PGSource{Pool: pool}
	cases := []struct {
		kind, id string
		want     float64
	}{
		{KindKey, keyA, 1.75},
		{KindKey, keyB, 7},
		{KindUser, owner, 3.75}, // key A's priced rows plus the keyless one
		{KindUser, other, 7},
		{KindKey, "33333333-3333-3333-3333-333333333333", 0},
		{KindUser, "44444444-4444-4444-4444-444444444444", 0},
	}
	for _, c := range cases {
		got, err := src.Spend(ctx, c.kind, c.id, cutoff)
		if err != nil {
			t.Fatalf("Spend(%s, %s): %v", c.kind, c.id, err)
		}
		if math.Abs(got-c.want) > 1e-9 {
			t.Errorf("Spend(%s, %s) = %v, want %v", c.kind, c.id, got, c.want)
		}
	}
	// A cutoff a day back takes in the earlier rows too.
	if got, err := src.Spend(ctx, KindKey, keyB, cutoff.Add(-48*time.Hour)); err != nil || math.Abs(got-37) > 1e-9 {
		t.Errorf("Spend(key B, two days) = %v, %v; want 37", got, err)
	}
	// A failed query is reported, not read as zero spend.
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := src.Spend(canceled, KindKey, keyA, cutoff); err == nil {
		t.Error("Spend on a canceled context: want an error")
	}
}
