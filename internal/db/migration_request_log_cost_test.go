package db

import (
	"context"
	"io/fs"
	"math"
	"testing"
)

// TestRequestLogCostMigrationBackfills covers migration 085. Rows written
// before cost_usd existed are priced at the serving model's current prices:
// the failover group's resolved member when there was one, else the requested
// model. Unpriced models and rows no provider served stay NULL, and a row that
// already carries a cost is left alone so the statement can run again. It runs
// directly rather than through runMigrations, which has already applied it to
// the shared database.
func TestRequestLogCostMigrationBackfills(t *testing.T) {
	ctx := context.Background()
	b, err := fs.ReadFile(embeddedMigrations, "migrations/085_request_log_cost.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(b)

	var providerID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO providers (name, base_url, encrypted_key, key_nonce)
		VALUES ('cost-migration-provider', 'https://cost.example', '\x00', '\x00') RETURNING id`).Scan(&providerID); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	t.Cleanup(func() {
		if _, err := testPool.Exec(context.Background(), `DELETE FROM request_logs WHERE model_id LIKE 'cost-%' OR model_id = 'hotel/cost-group'`); err != nil {
			t.Errorf("cleanup request logs: %v", err)
		}
		// Cascades to the seeded models.
		if _, err := testPool.Exec(context.Background(), `DELETE FROM providers WHERE id = $1`, providerID); err != nil {
			t.Errorf("cleanup provider: %v", err)
		}
	})

	if _, err := testPool.Exec(ctx, `
		INSERT INTO models (provider_id, model_id, input_price_per_million, input_price_per_million_cache_hit, output_price_per_million) VALUES
		($1, 'cost-cached',   1,    0.1,  4),
		($1, 'cost-plain',    1,    NULL, 4),
		($1, 'cost-free',     0,    0,    0),
		($1, 'cost-unpriced', NULL, NULL, NULL)`, providerID); err != nil {
		t.Fatalf("seed models: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
		INSERT INTO request_logs (model_id, resolved_model_id, provider_id, tokens_prompt, tokens_prompt_cache_hit, tokens_prompt_cache_miss, tokens_completion, tokens_completion_reasoning, cost_usd) VALUES
		('cost-cached',       '',            $1,   1000000, 800000, 200000, 250000, 250000, NULL),
		('cost-plain',        '',            $1,   1000000, 800000, 200000, 500000, 0,      NULL),
		('cost-walked',       'cost-cached', $1,   1500000, 800000, 200000, 250000, 250000, NULL),
		('hotel/cost-group',  'cost-cached', $1,   1000000, 0,      0,      500000, 0,      NULL),
		('cost-free',         '',            $1,   1000000, 0,      0,      500000, 0,      NULL),
		('cost-unpriced',     '',            $1,   1000000, 0,      0,      500000, 0,      NULL),
		('cost-unserved',     '',            NULL, NULL,    0,      0,      NULL,   0,      NULL),
		('cost-already',      '',            $1,   1000000, 0,      0,      500000, 0,      9.5)`, providerID); err != nil {
		t.Fatalf("seed request logs: %v", err)
	}

	if _, err := testPool.Exec(ctx, sql); err != nil {
		t.Fatalf("run migration: %v", err)
	}

	read := func(model string) *float64 {
		t.Helper()
		var cost *float64
		if err := testPool.QueryRow(ctx, `SELECT cost_usd FROM request_logs WHERE model_id = $1`, model).Scan(&cost); err != nil {
			t.Fatalf("read %s: %v", model, err)
		}
		return cost
	}
	for _, tc := range []struct {
		model string
		want  float64
	}{
		// 0.8M hits at $0.1 + 0.2M misses at $1 + 0.5M output (half reasoning) at $4.
		{"cost-cached", 2.28},
		// No cache-hit price: the whole prompt takes the input price.
		{"cost-plain", 3},
		// Prompt beyond the split (a walked group's rejected candidates) takes
		// the input price: 2.28 + 0.5M at $1.
		{"cost-walked", 2.78},
		// The group's resolved member is what gets priced, not the group name.
		{"hotel/cost-group", 3},
		{"cost-free", 0},
		// Left alone: a cost already stamped is history, not an estimate.
		{"cost-already", 9.5},
	} {
		got := read(tc.model)
		if got == nil {
			t.Errorf("%s cost_usd = NULL, want %v", tc.model, tc.want)
			continue
		}
		// The price columns are REAL, so 0.1 arrives as float32(0.1) and the
		// product carries that rounding.
		if math.Abs(*got-tc.want) > 1e-6 {
			t.Errorf("%s cost_usd = %v, want %v", tc.model, *got, tc.want)
		}
	}
	for _, model := range []string{"cost-unpriced", "cost-unserved"} {
		if got := read(model); got != nil {
			t.Errorf("%s cost_usd = %v, want NULL", model, *got)
		}
	}
}
