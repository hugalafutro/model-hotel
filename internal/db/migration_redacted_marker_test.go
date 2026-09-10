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
			`DELETE FROM request_logs WHERE model_id IN ('marker-row', 'name-row', 'clean-row', 'empty-row', 'object-row', 'null-row')`)
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO request_logs (model_id, status_code, attempts) VALUES
		('marker-row', 200, '[{"provider": "Neuralwatt", "detail": "[content]open"},
		                     {"provider": "Z.ai", "detail": "plain [content] run"}]'::jsonb),
		('name-row',   200, '[{"provider": "[content] Inc", "model": "m", "detail": "HTTP 503"}]'::jsonb),
		('clean-row',  200, '[{"provider": "Ollama", "detail": "circuit breaker open"}]'::jsonb),
		('empty-row',  200, '[]'::jsonb),
		('object-row', 200, '{"detail": "[content]open"}'::jsonb),
		('null-row',   200, NULL)`); err != nil {
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
		// A row the column's missing array constraint allows: left alone rather
		// than aborting the migration, and with it the startup that runs it.
		{"empty-row", `[]`},
		{"object-row", `{"detail": "[content]open"}`},
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

	// A NULL attempts column is the shape every row carried before the trail
	// existed. It must survive as NULL rather than becoming an empty array,
	// which would read as "this request tried nothing".
	var isNull bool
	if err := testPool.QueryRow(ctx,
		`SELECT attempts IS NULL FROM request_logs WHERE model_id = 'null-row'`).Scan(&isNull); err != nil {
		t.Fatalf("read null-row: %v", err)
	}
	if !isNull {
		t.Error("a NULL attempts column should stay NULL")
	}
}
