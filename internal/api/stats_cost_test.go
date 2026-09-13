package api

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

// seedCostRows inserts four rows under two providers, all created now:
//
//	R1: pa / m1, priced $0.50
//	R2: pa / m1, dispatched but unpriced (cost NULL)
//	R3: pa / m2, priced $0.25
//	R4: pb / m3, dispatched but unpriced
//	R5: no provider (never dispatched), unpriced
//
// so pa's spend is $0.75, pb has none to show, and two dispatched rows are
// unpriced.
func seedCostRows(t *testing.T, exec func(ctx context.Context, sql string, args ...any) error, provA, provB uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	rows := []struct {
		prov  *uuid.UUID
		model string
		cost  *float64
	}{
		{&provA, "m1", ptr(0.5)},
		{&provA, "m1", nil},
		{&provA, "m2", ptr(0.25)},
		{&provB, "m3", nil},
		{nil, "m1", nil},
	}
	for _, r := range rows {
		if err := exec(ctx, `INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, cost_usd, created_at)
			VALUES (gen_random_uuid(), $1, $2, 200, 10, 5, 5, $3, NOW())`, r.prov, r.model, r.cost); err != nil {
			t.Fatalf("seed cost row: %v", err)
		}
	}
}

func ptr(v float64) *float64 { return &v }

// TestStats_CostMetric covers the dollar side of the stats API: the cost
// metric on the three breakdowns and the provider distribution, the spend
// scalars, and the time series' per-bucket cost. Unpriced rows add nothing
// and are counted instead, so a total is never mistaken for exact.
func TestStats_CostMetric(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()
	ctx := context.Background()

	provA, provB := uuid.New(), uuid.New()
	insertTestProvider(t, pool, provA, "pa", "https://a.example/v1")
	insertTestProvider(t, pool, provB, "pb", "https://b.example/v1")
	seedCostRows(t, func(ctx context.Context, sql string, args ...any) error {
		_, err := pool.Exec(ctx, sql, args...)
		return err
	}, provA, provB)

	approx := func(name string, got, want float64) {
		t.Helper()
		if math.Abs(got-want) > 1e-9 {
			t.Errorf("%s = %v, want %v", name, got, want)
		}
	}

	s, err := handler.calculateStats(ctx, 24*time.Hour, false, "cost", false, "")
	if err != nil {
		t.Fatalf("calculateStats: %v", err)
	}
	approx("ByModel[pa/m1]", s.ByModel["pa/m1"], 0.5)
	approx("ByModel[pa/m2]", s.ByModel["pa/m2"], 0.25)
	approx("ByProvider[pa]", s.ByProvider["pa"], 0.75)
	approx("ByProvider[pb]", s.ByProvider["pb"], 0)
	approx("TotalCostUSD", s.TotalCostUSD, 0.75)
	if s.RequestsUnpriced != 2 {
		t.Errorf("RequestsUnpriced = %d, want 2 (the never-dispatched row does not count)", s.RequestsUnpriced)
	}

	// The request metric still counts unpriced rows: pricing is orthogonal.
	s, err = handler.calculateStats(ctx, 24*time.Hour, false, "requests", false, "")
	if err != nil {
		t.Fatalf("calculateStats(requests): %v", err)
	}
	approx("requests ByModel[pa/m1]", s.ByModel["pa/m1"], 2)

	w := httptest.NewRecorder()
	handler.GetTimeSeries(w, httptest.NewRequest(http.MethodGet, "/stats/timeseries?period=24h", http.NoBody))
	if w.Code != http.StatusOK {
		t.Fatalf("timeseries: %d %s", w.Code, w.Body.String())
	}
	var ts TimeSeriesStats
	if err := json.Unmarshal(w.Body.Bytes(), &ts); err != nil {
		t.Fatalf("decode timeseries: %v", err)
	}
	var bucketed float64
	for _, p := range ts.Points {
		bucketed += p.CostUSD
	}
	approx("time series cost", bucketed, 0.75)

	w = httptest.NewRecorder()
	handler.GetProviderDistribution(w, httptest.NewRequest(http.MethodGet, "/stats/provider-distribution?period=24h&metric=cost", http.NoBody))
	if w.Code != http.StatusOK {
		t.Fatalf("distribution: %d %s", w.Code, w.Body.String())
	}
	var dist ProviderDistributionStats
	if err := json.Unmarshal(w.Body.Bytes(), &dist); err != nil {
		t.Fatalf("decode distribution: %v", err)
	}
	// pb served only unpriced requests, so it has no spend to chart.
	if len(dist.Items) != 1 || dist.Items[0].Name != "pa" {
		t.Fatalf("distribution items = %+v, want pa alone", dist.Items)
	}
	approx("distribution cost", dist.Items[0].CostUSD, 0.75)
	approx("distribution share", dist.Items[0].Share, 100)
}

// TestListLogs_Cost covers the log row's cost: a priced row carries the number,
// an unpriced one carries null rather than 0, and sort_by=cost orders by it
// with unpriced rows last.
func TestListLogs_Cost(t *testing.T) {
	h, r := newTestHandlerWithRouter(t)
	pool := h.Pool().Pool()
	ctx := context.Background()

	providerID := createLogTestProvider(t, r, "cost-sort-provider")
	defer pool.Exec(ctx, `DELETE FROM providers WHERE id = $1`, providerID)
	for _, c := range []struct {
		model string
		cost  *float64
	}{{"cheap", ptr(0.01)}, {"unpriced", nil}, {"dear", ptr(2)}} {
		if _, err := pool.Exec(ctx, `INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, cost_usd, created_at)
			VALUES (gen_random_uuid(), $1, $2, 200, 50, $3, now())`, providerID, c.model, c.cost); err != nil {
			t.Fatalf("insert %s: %v", c.model, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/logs/?sort_by=cost&sort_dir=desc", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp LogsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var order []string
	byModel := map[string]*float64{}
	for _, e := range resp.Entries {
		if e.ProviderID == providerID {
			order = append(order, e.ModelID)
			byModel[e.ModelID] = e.CostUSD
		}
	}
	if len(order) != 3 || order[0] != "dear" || order[1] != "cheap" || order[2] != "unpriced" {
		t.Errorf("sort_by=cost desc order = %v, want [dear cheap unpriced]", order)
	}
	if byModel["dear"] == nil || *byModel["dear"] != 2 {
		t.Errorf("dear cost_usd = %v, want 2", byModel["dear"])
	}
	if byModel["unpriced"] != nil {
		t.Errorf("unpriced cost_usd = %v, want null", *byModel["unpriced"])
	}
}
