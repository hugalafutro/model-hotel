package db

import (
	"context"
	"io/fs"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// TestUnpriceablePricesMigration: a price a parser once stored as NaN,
// infinite or negative becomes NULL, and so does a request cost derived from
// one, while sane figures and genuine free prices stay. Replayed twice to
// prove it idempotent.
func TestUnpriceablePricesMigration(t *testing.T) {
	b, err := fs.ReadFile(embeddedMigrations, "migrations/090_null_unpriceable_prices.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	ctx := context.Background()
	suffix := uuid.NewString()[:8]
	var providerID uuid.UUID
	if err := testPool.QueryRow(ctx, `INSERT INTO providers (name, base_url, encrypted_key, key_nonce, key_salt) VALUES ($1, 'https://example.com', '\x00', '\x00', '\x00') RETURNING id`, "unpriceable-"+suffix).Scan(&providerID); err != nil {
		t.Fatalf("insert provider: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM request_logs WHERE model_id LIKE $1`, "unpriceable-"+suffix+"%")
		_, _ = testPool.Exec(context.Background(), `DELETE FROM providers WHERE id = $1`, providerID)
	})
	type row struct {
		name         string
		in, out, hit string
	}
	rows := []row{
		{"nan", "'NaN'", "1", "NULL"},
		{"negative", "1", "-2", "NULL"},
		{"inf", "1", "1", "'Infinity'"},
		{"sane", "1.5", "2.5", "0"},
	}
	for _, r := range rows {
		if _, err := testPool.Exec(ctx, `INSERT INTO models (id, provider_id, model_id, name, input_price_per_million, output_price_per_million, input_price_per_million_cache_hit, price_sources)
			VALUES (gen_random_uuid(), $1, $2, $2, `+r.in+`::double precision, `+r.out+`::double precision, `+r.hit+`::double precision, '{"input":"models.dev","output":"models.dev","cache_hit":"models.dev"}'::jsonb)`, providerID, "unpriceable-"+suffix+"-"+r.name); err != nil {
			t.Fatalf("insert model %s: %v", r.name, err)
		}
	}
	for i, cost := range []string{"'NaN'", "-0.5", "0.25"} {
		if _, err := testPool.Exec(ctx, `INSERT INTO request_logs (id, model_id, status_code, duration_ms, cost_usd, created_at) VALUES (gen_random_uuid(), $1, 200, 1, `+cost+`::double precision, now())`, "unpriceable-"+suffix+"-log"); err != nil {
			t.Fatalf("insert log %d: %v", i, err)
		}
	}

	for pass := 0; pass < 2; pass++ {
		if _, err := testPool.Exec(ctx, string(b)); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
	}

	var nulls int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM models WHERE model_id LIKE $1 AND (input_price_per_million IS NULL OR output_price_per_million IS NULL)`, "unpriceable-"+suffix+"-%").Scan(&nulls); err != nil {
		t.Fatalf("count: %v", err)
	}
	if nulls != 2 {
		t.Errorf("got %d models with a nulled input or output price, want the nan and negative ones", nulls)
	}
	var hitNull, saneHit int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE input_price_per_million_cache_hit IS NULL AND model_id LIKE '%-inf'), count(*) FILTER (WHERE input_price_per_million_cache_hit = 0 AND model_id LIKE '%-sane') FROM models WHERE model_id LIKE $1`, "unpriceable-"+suffix+"-%").Scan(&hitNull, &saneHit); err != nil {
		t.Fatalf("count hits: %v", err)
	}
	if hitNull != 1 || saneHit != 1 {
		t.Errorf("cache-hit prices: inf nulled=%d sane zero kept=%d, want 1 and 1", hitNull, saneHit)
	}
	// The source label leaves with the price and stays with a kept one.
	var srcNan, srcSane string
	if err := testPool.QueryRow(ctx, `SELECT (SELECT price_sources::text FROM models WHERE model_id = $1), (SELECT price_sources::text FROM models WHERE model_id = $2)`, "unpriceable-"+suffix+"-nan", "unpriceable-"+suffix+"-sane").Scan(&srcNan, &srcSane); err != nil {
		t.Fatalf("read sources: %v", err)
	}
	if strings.Contains(srcNan, `"input"`) || !strings.Contains(srcNan, `"output"`) {
		t.Errorf("nan row sources = %s, want input dropped and output kept", srcNan)
	}
	if !strings.Contains(srcSane, `"input"`) || !strings.Contains(srcSane, `"cache_hit"`) {
		t.Errorf("sane row sources = %s, want all kept", srcSane)
	}
	var costNulls, costKept int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE cost_usd IS NULL), count(*) FILTER (WHERE cost_usd = 0.25) FROM request_logs WHERE model_id = $1`, "unpriceable-"+suffix+"-log").Scan(&costNulls, &costKept); err != nil {
		t.Fatalf("count costs: %v", err)
	}
	if costNulls != 2 || costKept != 1 {
		t.Errorf("costs: nulled=%d kept=%d, want 2 and 1", costNulls, costKept)
	}
}
