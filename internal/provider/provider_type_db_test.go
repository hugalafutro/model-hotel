package provider

import (
	"context"
	"testing"

	"github.com/google/uuid"
)

// insertUntypedProvider writes a row the way it looked before provider_type
// existed: the column at its empty-string default.
func insertUntypedProvider(t *testing.T, name, baseURL string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	err := testDB.Pool().QueryRow(context.Background(), `
		INSERT INTO providers (name, base_url, enabled, autodiscovery_enabled)
		VALUES ($1, $2, true, true) RETURNING id`, name, baseURL).Scan(&id)
	if err != nil {
		t.Fatalf("insert untyped provider: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testDB.Pool().Exec(context.Background(), `DELETE FROM providers WHERE id = $1`, id)
		InvalidateProviderCache()
	})
	return id
}

func storedType(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var typ string
	if err := testDB.Pool().QueryRow(context.Background(),
		`SELECT provider_type FROM providers WHERE id = $1`, id).Scan(&typ); err != nil {
		t.Fatalf("read provider_type: %v", err)
	}
	return typ
}

func TestCreate_StoresTheChosenType(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	p, err := repo.Create(ctx, CreateProviderRequest{
		Name:         "typed-create-" + uuid.NewString()[:8],
		BaseURL:      "http://192.168.1.163:11234/v1",
		ProviderType: "lmstudio",
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testDB.Pool().Exec(ctx, `DELETE FROM providers WHERE id = $1`, p.ID)
		InvalidateProviderCache()
	})

	// The port says nothing here: only the operator's choice does.
	if p.ProviderType != "lmstudio" {
		t.Errorf("returned type = %q, want lmstudio", p.ProviderType)
	}
	if got := storedType(t, p.ID); got != "lmstudio" {
		t.Errorf("stored type = %q, want lmstudio", got)
	}
	if got := TypeOf(p); got != "lmstudio" {
		t.Errorf("TypeOf = %q, want lmstudio", got)
	}
}

func TestBackfillTypes_FillsPreexistingRows(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	suffix := uuid.NewString()[:8]
	cloud := insertUntypedProvider(t, "backfill-cloud-"+suffix, "https://api.deepseek.com/v1")
	local := insertUntypedProvider(t, "backfill-local-"+suffix, "http://192.168.1.9:11434")
	unknown := insertUntypedProvider(t, "backfill-unknown-"+suffix, "https://gateway.example.com/v1")

	if _, err := repo.BackfillTypes(ctx); err != nil {
		t.Fatalf("BackfillTypes: %v", err)
	}

	// Each row gets the type the URL rules implied when it was written, so
	// upgrading changes nothing about how it behaves.
	for _, tc := range []struct {
		id   uuid.UUID
		want string
	}{
		{cloud, "deepseek"},
		{local, "ollama"},
		{unknown, "openai"},
	} {
		if got := storedType(t, tc.id); got != tc.want {
			t.Errorf("stored type = %q, want %q", got, tc.want)
		}
	}
}

func TestBackfillTypes_LeavesChosenTypesAlone(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	p, err := repo.Create(ctx, CreateProviderRequest{
		Name:         "backfill-keeps-" + uuid.NewString()[:8],
		BaseURL:      "http://192.168.1.163:5001/v1",
		ProviderType: "custom",
	}, nil, nil, nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testDB.Pool().Exec(ctx, `DELETE FROM providers WHERE id = $1`, p.ID)
		InvalidateProviderCache()
	})

	// The legacy rules would call :5001 koboldcpp. A row that already has a
	// type must not be second-guessed.
	if _, err := repo.BackfillTypes(ctx); err != nil {
		t.Fatalf("BackfillTypes: %v", err)
	}
	if got := storedType(t, p.ID); got != "custom" {
		t.Errorf("stored type = %q, want custom", got)
	}
}

func TestBackfillTypes_Idempotent(t *testing.T) {
	repo := newTestRepo(t)
	ctx := context.Background()

	insertUntypedProvider(t, "backfill-twice-"+uuid.NewString()[:8], "https://api.openai.com/v1")
	if _, err := repo.BackfillTypes(ctx); err != nil {
		t.Fatalf("first BackfillTypes: %v", err)
	}
	second, err := repo.BackfillTypes(ctx)
	if err != nil {
		t.Fatalf("second BackfillTypes: %v", err)
	}
	if second != 0 {
		t.Errorf("second run backfilled %d rows, want 0", second)
	}
}
