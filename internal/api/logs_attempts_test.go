package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// The per-attempt trail on the logs API: the column rides through the row
// verbatim, is omitted when NULL, and the two attempt filters select on the
// trail rather than on the terminal columns.

const trailJSON = `[{"attempt":0,"provider_id":"%s","provider":"Neuralwatt","model":"glm-5.3","status":429,"error_kind":"provider_saturated","detail":"concurrent_budget_exceeded","duration_ms":412,"breaker":"noop"},` +
	`{"attempt":1,"provider_id":"%s","provider":"Ollama","model":"glm-5.3","status":200,"duration_ms":8299,"ttft_ms":1561,"breaker":"success"}]`

func TestLogs_AttemptTrailRoundTrip(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()
	ctx := context.Background()

	served := createLogTestProvider(t, r, "trail-served-provider")
	defer pool.Exec(ctx, `DELETE FROM providers WHERE id = $1`, served)
	busy := uuid.New()

	withTrail := uuid.New()
	trail := strings.ReplaceAll(strings.ReplaceAll(trailJSON, "%s", "%%"), "%%", "%s")
	trail = strings.Replace(trail, "%s", busy.String(), 1)
	trail = strings.Replace(trail, "%s", served, 1)
	if _, err := pool.Exec(ctx, `INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, failover_attempt, created_at, attempts)
		VALUES ($1, $2, 'hotel/trail', 200, 8700, 1, now(), $3::jsonb)`, withTrail, served, trail); err != nil {
		t.Fatalf("insert trail row: %v", err)
	}
	withoutTrail := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, created_at)
		VALUES ($1, $2, 'hotel/trail', 200, 100, now() - interval '1 minute')`, withoutTrail, served); err != nil {
		t.Fatalf("insert plain row: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM request_logs WHERE id = ANY($1)`, []uuid.UUID{withTrail, withoutTrail})

	get := func(path string) map[string]any {
		t.Helper()
		req := httptest.NewRequest("GET", path, http.NoBody)
		req.Header.Set("Authorization", "Bearer test-admin-token")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET %s: %d %s", path, w.Code, w.Body.String())
		}
		var out map[string]any
		if err := json.NewDecoder(w.Body).Decode(&out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return out
	}

	t.Run("single row carries the trail verbatim and omits it when NULL", func(t *testing.T) {
		row := get("/logs/" + withTrail.String())
		attempts, ok := row["attempts"].([]any)
		if !ok || len(attempts) != 2 {
			t.Fatalf("attempts = %v, want the two-element trail", row["attempts"])
		}
		first, _ := attempts[0].(map[string]any)
		if first["provider"] != "Neuralwatt" || first["status"] != float64(429) || first["breaker"] != "noop" || first["detail"] != "concurrent_budget_exceeded" {
			t.Errorf("attempts[0] = %v", first)
		}
		plain := get("/logs/" + withoutTrail.String())
		if _, present := plain["attempts"]; present {
			t.Errorf("a row without a trail must omit attempts, got %v", plain["attempts"])
		}
	})

	t.Run("attempt filters select on the trail, not the terminal columns", func(t *testing.T) {
		ids := func(out map[string]any) []string {
			entries, _ := out["entries"].([]any)
			var got []string
			for _, e := range entries {
				m, _ := e.(map[string]any)
				got = append(got, m["id"].(string))
			}
			return got
		}
		// The busy provider never served anything: the terminal provider_id
		// filter finds nothing, the attempt filter finds the request it lost.
		if got := ids(get("/logs/?provider_id=" + busy.String())); len(got) != 0 {
			t.Errorf("terminal filter on the busy provider = %v, want none", got)
		}
		got := ids(get("/logs/?attempt_provider_id=" + busy.String() + "&attempt_status=429"))
		if len(got) != 1 || got[0] != withTrail.String() {
			t.Errorf("attempt filter = %v, want the trail row only", got)
		}
		// Both keys must hold on the SAME element: the served provider
		// answered 200, never 429.
		if got := ids(get("/logs/?attempt_provider_id=" + served + "&attempt_status=429")); len(got) != 0 {
			t.Errorf("mismatched pair = %v, want none", got)
		}
		if got := ids(get("/logs/?attempt_status=429&model_id=hotel/trail")); len(got) != 1 {
			t.Errorf("status-only attempt filter = %v, want the trail row", got)
		}
		// The cursor list applies the same filter.
		cursor := get("/logs/cursor?attempt_provider_id=" + busy.String())
		if got := ids(cursor); len(got) != 1 || got[0] != withTrail.String() {
			t.Errorf("cursor attempt filter = %v, want the trail row only", got)
		}
		if total, _ := cursor["total"].(float64); total != 1 {
			t.Errorf("cursor total = %v, want 1", cursor["total"])
		}
	})
}

// The predicate itself: one containment element carrying both keys, indexed
// by jsonb_path_ops; a malformed id or status is dropped, and neither key
// leaves the query untouched.
func TestAppendAttemptFilter(t *testing.T) {
	id := uuid.New()
	query, args, idx := appendAttemptFilter("WHERE 1=1", nil, 1, id.String(), "429")
	if !strings.Contains(query, "rl.attempts @> $1::jsonb") || idx != 2 || len(args) != 1 {
		t.Fatalf("query=%q args=%v idx=%d", query, args, idx)
	}
	if want := `[{"provider_id":"` + id.String() + `","status":429}]`; args[0] != want {
		t.Errorf("needle = %v, want %s", args[0], want)
	}
	query, args, idx = appendAttemptFilter("WHERE 1=1", nil, 1, "not-a-uuid", "abc")
	if query != "WHERE 1=1" || len(args) != 0 || idx != 1 {
		t.Errorf("malformed inputs changed the query: %q %v %d", query, args, idx)
	}
	query, args, _ = appendAttemptFilter("WHERE 1=1", nil, 4, "", "503")
	if !strings.Contains(query, "$4::jsonb") || args[0] != `[{"status":503}]` {
		t.Errorf("status-only = %q %v", query, args)
	}
}
