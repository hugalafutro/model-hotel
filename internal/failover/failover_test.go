package failover

import (
	"context"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/hugalafutro/model-hotel/internal/db"
)

// ---------------------------------------------------------------------------
// TestMain — integration test database setup
// ---------------------------------------------------------------------------

var testDB *db.DB

func TestMain(m *testing.M) {
	ctx := context.Background()
	testDBURL, setupErr := db.SetupTestDB("failover")
	if setupErr != nil {
		log.Printf("failed to setup test DB: %v", setupErr)
		os.Exit(1)
	}
	defer db.CleanupTestDB("failover")

	var err error
	testDB, err = db.New(ctx, testDBURL, 25, 5)
	if err != nil {
		log.Printf("failed to initialize test DB: %v", err)
		os.Exit(1) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
	}
	defer testDB.Close()

	os.Exit(m.Run()) //nolint:gocritic // test-only: os.Exit in TestMain is intentional
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestRepo(t *testing.T) *Repository {
	t.Helper()

	return NewRepository(testDB.Pool())
}

// containsSubstring is a thin wrapper kept for test readability.
func containsSubstring(s, substr string) bool {
	return strings.Contains(s, substr)
}
