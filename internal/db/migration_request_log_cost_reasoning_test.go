package db

import (
	"context"
	"io/fs"
	"math"
	"testing"
)

// TestRequestLogCostReasoningMigration covers migration 086: a terminal row
// whose cost counted reasoning on top of completion is repriced at completion
// alone, a row without reasoning is left as it was, and a row not yet
// terminal loses the zero the interim write stamped.
func TestRequestLogCostReasoningMigration(t *testing.T) {
	ctx := context.Background()
	b, err := fs.ReadFile(embeddedMigrations, "migrations/086_request_log_cost_reasoning.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	var providerID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO providers (name, base_url, encrypted_key, key_nonce)
		VALUES ('reasoning-migration-provider', 'https://reason.example', '\x00', '\x00') RETURNING id`).Scan(&providerID); err != nil {
		t.Fatalf("seed provider: %v", err)
	}
	t.Cleanup(func() {
		if _, err := testPool.Exec(context.Background(), `DELETE FROM request_logs WHERE model_id LIKE 'reason-%'`); err != nil {
			t.Errorf("cleanup request logs: %v", err)
		}
		if _, err := testPool.Exec(context.Background(), `DELETE FROM providers WHERE id = $1`, providerID); err != nil {
			t.Errorf("cleanup provider: %v", err)
		}
	})
	if _, err := testPool.Exec(ctx, `
		INSERT INTO models (provider_id, model_id, input_price_per_million, output_price_per_million) VALUES ($1, 'reason-m', 1, 4)`, providerID); err != nil {
		t.Fatalf("seed model: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
		INSERT INTO request_logs (model_id, provider_id, state, tokens_prompt, tokens_completion, tokens_completion_reasoning, cost_usd) VALUES
		('reason-m',        $1, 'completed', 1000000, 500000, 250000, 4),
		('reason-plain',    $1, 'completed', 1000000, 500000, 0,      3),
		('reason-streaming', $1, 'streaming', 0,      0,      0,      0)`, providerID); err != nil {
		t.Fatalf("seed request logs: %v", err)
	}
	if _, err := testPool.Exec(ctx, string(b)); err != nil {
		t.Fatalf("run migration: %v", err)
	}
	read := func(model string) *float64 {
		t.Helper()
		var v *float64
		if err := testPool.QueryRow(ctx, `SELECT cost_usd FROM request_logs WHERE model_id = $1`, model).Scan(&v); err != nil {
			t.Fatalf("read %s: %v", model, err)
		}
		return v
	}
	// 1M prompt at $1 + 0.5M completion at $4, reasoning no longer added.
	if got := read("reason-m"); got == nil || math.Abs(*got-3) > 1e-6 {
		t.Errorf("reasoning row cost_usd = %v, want 3", got)
	}
	// No reasoning: not touched (it would have priced to the same 3 anyway,
	// but the row's own model has no row here to reprice against).
	if got := read("reason-plain"); got == nil || *got != 3 {
		t.Errorf("plain row cost_usd = %v, want 3 untouched", got)
	}
	if got := read("reason-streaming"); got != nil {
		t.Errorf("streaming row cost_usd = %v, want NULL", *got)
	}
}
