package db

import (
	"context"
	"io/fs"
	"testing"
)

// TestRedactedMarkerMigrationRewritesStoredAttempts covers migration 083. The
// old content fence left "[content]" in an attempt detail where it dropped a
// run of text, which reads as though the log holds request content. The
// migration rewrites the marker to "[redacted]" and leaves every other detail
// alone. It runs directly rather than through runMigrations, which has already
// applied it to the shared database.
func TestRedactedMarkerMigrationRewritesStoredAttempts(t *testing.T) {
	ctx := context.Background()
	b, err := fs.ReadFile(embeddedMigrations, "migrations/083_redacted_marker_in_attempt_details.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(b)

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM request_logs WHERE model_id IN ('marker-row', 'name-row', 'clean-row')`)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO request_logs (model_id, status_code, attempts) VALUES
		('marker-row', 200, '[{"provider": "Neuralwatt", "detail": "[content]open"},
		                     {"provider": "Z.ai", "detail": "plain [content] run"}]'::jsonb),
		('name-row',   200, '[{"provider": "[content] Inc", "model": "m", "detail": "HTTP 503"}]'::jsonb),
		('clean-row',  200, '[{"provider": "Ollama", "detail": "circuit breaker open"}]'::jsonb)`); err != nil {
		t.Fatalf("seed request logs: %v", err)
	}

	if _, err := testPool.Exec(ctx, sql); err != nil {
		t.Fatalf("run migration: %v", err)
	}

	for _, tc := range []struct {
		model string
		want  string
	}{
		{"marker-row", `[{"detail": "[redacted]open", "provider": "Neuralwatt"}, {"detail": "plain [redacted] run", "provider": "Z.ai"}]`},
		// The marker is rewritten in details only: a provider name is data this
		// migration has no business editing.
		{"name-row", `[{"model": "m", "detail": "HTTP 503", "provider": "[content] Inc"}]`},
		{"clean-row", `[{"detail": "circuit breaker open", "provider": "Ollama"}]`},
	} {
		var same bool
		if err := testPool.QueryRow(ctx,
			`SELECT attempts = $2::jsonb FROM request_logs WHERE model_id = $1`, tc.model, tc.want).Scan(&same); err != nil {
			t.Fatalf("read %s: %v", tc.model, err)
		}
		if !same {
			var got string
			_ = testPool.QueryRow(ctx, `SELECT attempts::text FROM request_logs WHERE model_id = $1`, tc.model).Scan(&got)
			t.Errorf("%s attempts = %s, want %s", tc.model, got, tc.want)
		}
	}
}
