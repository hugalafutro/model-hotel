package virtualkey

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// tokens_used only ever grows. A non-positive charge is refused at the
// repository as well as at the proxy's metering boundary, so no caller can
// draw a key's counter down.
func TestRepository_AddTokens_RefusesNonPositive(t *testing.T) {
	ctx := context.Background()
	repo := NewRepository(testDB.Pool())
	suffix := uuid.New().String()[:8]
	created, err := repo.Create(ctx, "integration-addtokens-guard-"+suffix, "hash-addtokens-guard-"+suffix, "sk-...ag", nil, nil, nil, nil, nil, nil)
	if err != nil {
		t.Fatalf("Create() setup failed: %v", err)
	}
	defer func() { _ = repo.Delete(ctx, created.ID) }()
	if err := repo.AddTokens(ctx, created.KeyHash, 100); err != nil {
		t.Fatalf("AddTokens(100) failed: %v", err)
	}
	for _, n := range []int{0, -1, -600} {
		if err := repo.AddTokens(ctx, created.KeyHash, n); err != nil {
			t.Fatalf("AddTokens(%d) returned an error, want a silent refusal: %v", n, err)
		}
	}
	updated, err := repo.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("Get() failed: %v", err)
	}
	if updated.TokensUsed != 100 {
		t.Errorf("TokensUsed = %d after non-positive charges, want 100", updated.TokensUsed)
	}
}
