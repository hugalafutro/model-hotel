package api

import (
	"context"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
)

// TestCalculateStats_Golden pins the full StatsResponse for a small,
// hand-computable dataset across both metrics and both excludeDeleted states.
// It is the safety net for the calculateStats decomposition: the refactor
// rebuilds query strings from shared fragments (vkScope / metricValueSelect),
// so any whitespace/semantic drift would change one of these fields. Every
// assertion below is derived by hand from the three seeded rows.
//
// Seeded rows (all created NOW(), so inside the 1h/24h/7d windows):
//
//	R1: provider "pa", model "m1", 200, duration 100, prompt 10, comp 20,
//	    streaming, response_header_ms 40, overhead 10, vk = live "live"
//	R2: provider "pb", model "m2", 429, duration  50, prompt  0, comp  0,
//	    non-streaming,                overhead 10, vk = live "live"
//	R3: provider "pa", model "m1", 200, duration 200, prompt 100, comp 100,
//	    non-streaming,                overhead 10, vk = a DELETED key (no row)
func TestCalculateStats_Golden(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()
	ctx := context.Background()

	provA, provB := uuid.New(), uuid.New()
	insertTestProvider(t, pool, provA, "pa", "https://a.example/v1")
	insertTestProvider(t, pool, provB, "pb", "https://b.example/v1")

	liveVK := uuid.New()
	if _, err := pool.Exec(ctx, `
		INSERT INTO virtual_keys (id, name, key_hash, key_preview, created_at)
		VALUES ($1, $2, $3, $4, NOW())`,
		liveVK, "live", "hash-live", "live-...",
	); err != nil {
		t.Fatalf("insert virtual key: %v", err)
	}
	ghostVK := uuid.New() // referenced by R3 but no virtual_keys row → "Deleted"

	insertRichTestRequestLog(t, pool, uuid.New(), provA, "m1", 200, 100, 10, 20, requestLogOpts{
		VirtualKeyID: &liveVK, VirtualKeyName: "live", ResponseHeaderMs: 40, ProxyOverheadMs: 10, Streaming: true,
	})
	insertRichTestRequestLog(t, pool, uuid.New(), provB, "m2", 429, 50, 0, 0, requestLogOpts{
		VirtualKeyID: &liveVK, VirtualKeyName: "live", ProxyOverheadMs: 10, Streaming: false,
	})
	insertRichTestRequestLog(t, pool, uuid.New(), provA, "m1", 200, 200, 100, 100, requestLogOpts{
		VirtualKeyID: &ghostVK, ProxyOverheadMs: 10, Streaming: false,
	})

	type want struct {
		total24h, total7d                     int
		byModel, byProvider, byVirtualKey     map[string]int64
		avgLatency, errorRate, avgOverhead    float64
		tokPrompt, tokCompletion, tokCacheHit int
		avgTokensPerReq                       float64
		rateLimitHits                         int
		avgTTFT                               float64
		requests1h                            int
		modelLatencyLen, providerLatencyLen   int
	}

	cases := []struct {
		name           string
		metric         string
		excludeDeleted bool
		want           want
	}{
		{
			name: "requests/includeDeleted", metric: "requests", excludeDeleted: false,
			want: want{
				total24h: 3, total7d: 3,
				byModel:      map[string]int64{"pa/m1": 2, "pb/m2": 1},
				byProvider:   map[string]int64{"pa": 2, "pb": 1},
				byVirtualKey: map[string]int64{"live": 2, "Deleted": 1},
				avgLatency:   150, // (100 + 200) / 2  (status<400: R1, R3)
				errorRate:    1.0 / 3.0,
				avgOverhead:  10,
				tokPrompt:    110, tokCompletion: 120, tokCacheHit: 0,
				avgTokensPerReq: 115, // (30 + 200) / 2
				rateLimitHits:   1,
				avgTTFT:         40, // R1 streaming, ttft falls back to response_header_ms
				requests1h:      3,
			},
		},
		{
			name: "tokens/excludeDeleted", metric: "tokens", excludeDeleted: true,
			want: want{
				total24h: 2, total7d: 2, // R3 (deleted vk) filtered out by scope
				byModel:      map[string]int64{"pa/m1": 30, "pb/m2": 0},
				byProvider:   map[string]int64{"pa": 30, "pb": 0},
				byVirtualKey: map[string]int64{"live": 30},
				avgLatency:   100, // status<400 in scope: R1 only
				errorRate:    0.5, // R2 of {R1,R2}
				avgOverhead:  10,
				tokPrompt:    10, tokCompletion: 20, tokCacheHit: 0,
				avgTokensPerReq: 30, // R1 only
				rateLimitHits:   1,
				avgTTFT:         40,
				requests1h:      2,
			},
		},
	}

	approx := func(t *testing.T, name string, got, wantV float64) {
		t.Helper()
		if math.Abs(got-wantV) > 1e-9 {
			t.Errorf("%s = %v, want %v", name, got, wantV)
		}
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, err := handler.calculateStats(ctx, 24*time.Hour, tc.excludeDeleted, tc.metric, true)
			if err != nil {
				t.Fatalf("calculateStats: %v", err)
			}
			w := tc.want
			if s.TotalRequestsLast24h != w.total24h {
				t.Errorf("TotalRequestsLast24h = %d, want %d", s.TotalRequestsLast24h, w.total24h)
			}
			if s.TotalRequestsLast7d != w.total7d {
				t.Errorf("TotalRequestsLast7d = %d, want %d", s.TotalRequestsLast7d, w.total7d)
			}
			if !reflect.DeepEqual(s.ByModel, w.byModel) {
				t.Errorf("ByModel = %v, want %v", s.ByModel, w.byModel)
			}
			if !reflect.DeepEqual(s.ByProvider, w.byProvider) {
				t.Errorf("ByProvider = %v, want %v", s.ByProvider, w.byProvider)
			}
			if !reflect.DeepEqual(s.ByVirtualKey, w.byVirtualKey) {
				t.Errorf("ByVirtualKey = %v, want %v", s.ByVirtualKey, w.byVirtualKey)
			}
			approx(t, "AvgLatencyMs", s.AvgLatencyMs, w.avgLatency)
			approx(t, "ErrorRate", s.ErrorRate, w.errorRate)
			approx(t, "AvgOverheadMs", s.AvgOverheadMs, w.avgOverhead)
			if s.TotalTokensPrompt != w.tokPrompt || s.TotalTokensCompletion != w.tokCompletion || s.TotalTokensCacheHit != w.tokCacheHit {
				t.Errorf("tokens = (%d,%d,%d), want (%d,%d,%d)",
					s.TotalTokensPrompt, s.TotalTokensCompletion, s.TotalTokensCacheHit,
					w.tokPrompt, w.tokCompletion, w.tokCacheHit)
			}
			approx(t, "AvgTokensPerRequest", s.AvgTokensPerRequest, w.avgTokensPerReq)
			if s.RateLimitHits != w.rateLimitHits {
				t.Errorf("RateLimitHits = %d, want %d", s.RateLimitHits, w.rateLimitHits)
			}
			approx(t, "AvgTTFTMs", s.AvgTTFTMs, w.avgTTFT)
			if s.RequestsLast1h != w.requests1h {
				t.Errorf("RequestsLast1h = %d, want %d", s.RequestsLast1h, w.requests1h)
			}
			if len(s.ByModelLatency) != w.modelLatencyLen {
				t.Errorf("ByModelLatency len = %d, want %d", len(s.ByModelLatency), w.modelLatencyLen)
			}
			if len(s.ByProviderLatency) != w.providerLatencyLen {
				t.Errorf("ByProviderLatency len = %d, want %d", len(s.ByProviderLatency), w.providerLatencyLen)
			}
		})
	}
}

// TestCalculateStats_CrossFill exercises statTotals' period switch in BOTH
// directions — the 24h primary path (cross-fills 7d) and the 7d primary path
// (cross-fills 24h) — with a dataset whose 24h and 7d counts differ, so a
// swapped cross-fill assignment is caught.
func TestCalculateStats_CrossFill(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()
	ctx := context.Background()

	provA := uuid.New()
	insertTestProvider(t, pool, provA, "pa", "https://a.example/v1")

	// One recent row (within 24h) and one ~2 days old (within 7d, outside 24h):
	// 24h count = 1, 7d count = 2.
	mustInsert := func(age string) {
		if _, err := pool.Exec(ctx, `
			INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
			VALUES ($1, $2, 'm1', 200, 100, 10, 20, NOW() - $3::interval)`,
			uuid.New(), provA, age); err != nil {
			t.Fatalf("insert request log (%s): %v", age, err)
		}
	}
	mustInsert("1 hour")
	mustInsert("2 days")

	// period = 24h: primary fills 24h (1 recent), cross-fill fills 7d (both).
	s24, err := handler.calculateStats(ctx, 24*time.Hour, false, "requests", false)
	if err != nil {
		t.Fatalf("calculateStats 24h: %v", err)
	}
	if s24.TotalRequestsLast24h != 1 || s24.TotalRequestsLast7d != 2 {
		t.Errorf("24h period: got 24h=%d 7d=%d, want 24h=1 7d=2", s24.TotalRequestsLast24h, s24.TotalRequestsLast7d)
	}

	// period = 7d: primary fills 7d (both), cross-fill fills 24h (1 recent).
	s7, err := handler.calculateStats(ctx, 7*24*time.Hour, false, "requests", false)
	if err != nil {
		t.Fatalf("calculateStats 7d: %v", err)
	}
	if s7.TotalRequestsLast7d != 2 || s7.TotalRequestsLast24h != 1 {
		t.Errorf("7d period: got 7d=%d 24h=%d, want 7d=2 24h=1", s7.TotalRequestsLast7d, s7.TotalRequestsLast24h)
	}
}
