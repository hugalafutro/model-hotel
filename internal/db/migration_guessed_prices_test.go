package db

import (
	"context"
	"io/fs"
	"testing"

	"github.com/google/uuid"
)

// TestGuessedPricesOnOperatorServedMigration: on a custom or self-hosted
// provider a price supplied by models.dev or the catalog (matched by name)
// becomes NULL with its source label, column by column, while a
// price the provider itself reported, an operator's figure, a pinned row, and
// every price on a hosted provider stay. Replayed twice to prove it idempotent.
func TestGuessedPricesOnOperatorServedMigration(t *testing.T) {
	b, err := fs.ReadFile(embeddedMigrations, "migrations/094_null_guessed_prices_on_operator_served.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	ctx := context.Background()
	suffix := uuid.NewString()[:8]
	provider := func(label, kind, baseURL string) uuid.UUID {
		var id uuid.UUID
		if err := testPool.QueryRow(ctx, `INSERT INTO providers (name, base_url, provider_type, encrypted_key, key_nonce, key_salt) VALUES ($1, $2, $3, '\x00', '\x00', '\x00') RETURNING id`,
			"guessed-"+label+"-"+suffix, baseURL, kind).Scan(&id); err != nil {
			t.Fatalf("insert provider %s: %v", kind, err)
		}
		t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM providers WHERE id = $1`, id) })
		return id
	}
	custom := provider("custom", "custom", "http://10.0.0.5:8082/v1")
	ollama := provider("ollama", "ollama", "http://10.0.0.5:11434")
	relay := provider("relay", "openai", "https://relay.example.com/v1")
	hosted := provider("hosted", "openai", "https://api.openai.com/v1")

	insert := func(prov uuid.UUID, name, sources string, pinned bool) {
		if _, err := testPool.Exec(ctx, `INSERT INTO models (id, provider_id, model_id, name, input_price_per_million, output_price_per_million, price_customized, price_sources)
			VALUES (gen_random_uuid(), $1, $2, $2, 1, 2, $3, $4::jsonb)`, prov, name+"-"+suffix, pinned, sources); err != nil {
			t.Fatalf("insert model %s: %v", name, err)
		}
	}
	insert(custom, "custom-guessed", `{"input":"modelsdev","output":"catalog"}`, false)
	insert(custom, "custom-own", `{"input":"provider","output":"manual"}`, false)
	insert(custom, "custom-pinned", `{"input":"modelsdev","output":"modelsdev"}`, true)
	insert(ollama, "ollama-guessed", `{"input":"modelsdev","output":"modelsdev"}`, false)
	insert(relay, "relay-guessed", `{"input":"modelsdev","output":"modelsdev"}`, false)
	insert(hosted, "hosted-guessed", `{"input":"modelsdev","output":"modelsdev"}`, false)

	// Every price column, with a mixed-source row: each column and its label
	// are judged on their own.
	if _, err := testPool.Exec(ctx, `INSERT INTO models (id, provider_id, model_id, name, input_price_per_million, input_price_per_million_cache_hit, output_price_per_million, search_price_per_thousand, price_sources)
		VALUES (gen_random_uuid(), $1, $2, $2, 1, 0.5, 2, 3, '{"input":"modelsdev","cache_hit":"catalog","output":"manual","search":"modelsdev"}'::jsonb)`, ollama, "ollama-mixed-"+suffix); err != nil {
		t.Fatalf("insert mixed model: %v", err)
	}

	for pass := 0; pass < 2; pass++ {
		if _, err := testPool.Exec(ctx, string(b)); err != nil {
			t.Fatalf("pass %d: %v", pass, err)
		}
	}

	for name, wantPriced := range map[string]bool{
		"custom-guessed": false, "custom-own": true, "custom-pinned": true,
		"ollama-guessed": false, "relay-guessed": true, "hosted-guessed": true,
	} {
		var in, out *float64
		var sources string
		if err := testPool.QueryRow(ctx, `SELECT input_price_per_million, output_price_per_million, price_sources::text FROM models WHERE model_id = $1`, name+"-"+suffix).Scan(&in, &out, &sources); err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if priced := in != nil && out != nil; priced != wantPriced {
			t.Errorf("%s: input %v output %v (sources %s), want priced=%v", name, in, out, sources, wantPriced)
		}
		if !wantPriced && sources != "{}" {
			t.Errorf("%s: sources %s, want the labels gone with the prices", name, sources)
		}
	}

	var in, hit, out, search *float64
	var sources string
	if err := testPool.QueryRow(ctx, `SELECT input_price_per_million, input_price_per_million_cache_hit, output_price_per_million, search_price_per_thousand, price_sources::text FROM models WHERE model_id = $1`, "ollama-mixed-"+suffix).Scan(&in, &hit, &out, &search, &sources); err != nil {
		t.Fatalf("read mixed: %v", err)
	}
	if in != nil || hit != nil || search != nil || out == nil || *out != 2 {
		t.Errorf("mixed: input %v cache_hit %v search %v output %v, want only the operator's output price kept", in, hit, search, out)
	}
	if sources != `{"output": "manual"}` {
		t.Errorf("mixed: sources %s, want only the output label left", sources)
	}
}
