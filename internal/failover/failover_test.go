package failover

import (
	"context"
	"log"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/hugalafutro/model-hotel/internal/db"
)

// ---------------------------------------------------------------------------
// TestMain — integration test database setup
// ---------------------------------------------------------------------------

var testDB *db.DB

// testDBURL is kept for tests that need a second pool with different
// connection settings (see readfailure_test.go).
var testDBURL string

func TestMain(m *testing.M) {
	ctx := context.Background()
	var setupErr error
	testDBURL, setupErr = db.SetupTestDB("failover")
	if setupErr != nil {
		log.Printf("failed to setup test DB: %v", setupErr)
		os.Exit(1)
	}
	defer db.CleanupTestDB("failover")

	var err error
	testDB, err = db.New(ctx, testDBURL, 25, 5)
	if err != nil {
		log.Printf("failed to initialize test DB: %v", err)
		os.Exit(1)
	}
	defer testDB.Close()

	os.Exit(m.Run())
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTestRepo(t *testing.T) *Repository {
	t.Helper()

	return NewRepository(testDB.Pool())
}

// upsertGroup creates a failover group with default config. Tests that only
// need a group to exist call this instead of spelling out UpsertWithConfig's
// five nil options.
func upsertGroup(ctx context.Context, t *testing.T, repo *Repository, displayModel string, priorityOrder []uuid.UUID) (*FailoverGroup, error) {
	t.Helper()
	return repo.UpsertWithConfig(ctx, displayModel, priorityOrder, nil, nil, nil, nil, nil)
}

// containsSubstring is a thin wrapper kept for test readability.
func containsSubstring(s, substr string) bool {
	return strings.Contains(s, substr)
}
