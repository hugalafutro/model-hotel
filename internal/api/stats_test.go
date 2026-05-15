package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hugalafutro/model-hotel/internal/admin"
)

// ---------------------------------------------------------------------------
// parsePeriod tests
// ---------------------------------------------------------------------------

func TestParsePeriod(t *testing.T) {
	tests := []struct {
		name   string
		period string
		want   time.Duration
	}{
		{
			name:   "1h returns 1 hour",
			period: "1h",
			want:   1 * time.Hour,
		},
		{
			name:   "7d returns 7 days",
			period: "7d",
			want:   7 * 24 * time.Hour,
		},
		{
			name:   "empty returns default 24h",
			period: "",
			want:   24 * time.Hour,
		},
		{
			name:   "unknown value returns default 24h",
			period: "30m",
			want:   24 * time.Hour,
		},
		{
			name:   "garbage value returns default 24h",
			period: "not-a-period",
			want:   24 * time.Hour,
		},
		{
			name:   "wrong format returns default 24h",
			period: "24h",
			want:   24 * time.Hour,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/stats?period="+tt.period, http.NoBody)
			got := parsePeriod(req)
			if got != tt.want {
				t.Errorf("parsePeriod(%q) = %v, want %v", tt.period, got, tt.want)
			}
		})
	}
}

func TestParsePeriod_NoQueryString(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/stats", http.NoBody)
	got := parsePeriod(req)
	want := 24 * time.Hour
	if got != want {
		t.Errorf("parsePeriod() with no query string = %v, want %v", got, want)
	}
}

func TestParsePeriod_MultipleParams(t *testing.T) {
	// When multiple "period" params are given, first one is used.
	req := httptest.NewRequest(http.MethodGet, "/api/stats?period=1h&period=7d", http.NoBody)
	got := parsePeriod(req)
	want := 1 * time.Hour
	if got != want {
		t.Errorf("parsePeriod() with multiple params = %v, want %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// parseMetric tests
// ---------------------------------------------------------------------------

func TestParseMetric(t *testing.T) {
	tests := []struct {
		name   string
		metric string
		want   string
	}{
		{
			name:   "tokens returns tokens",
			metric: "tokens",
			want:   "tokens",
		},
		{
			name:   "empty returns requests (default)",
			metric: "",
			want:   "requests",
		},
		{
			name:   "unknown value returns requests",
			metric: "latency",
			want:   "requests",
		},
		{
			name:   "garbage value returns requests",
			metric: "something-else",
			want:   "requests",
		},
		{
			name:   "case-sensitive: Tokens (capital T) returns requests",
			metric: "Tokens",
			want:   "requests",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/stats?metric="+tt.metric, http.NoBody)
			got := parseMetric(req)
			if got != tt.want {
				t.Errorf("parseMetric(%q) = %q, want %q", tt.metric, got, tt.want)
			}
		})
	}
}

func TestParseMetric_NoQueryString(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/stats", http.NoBody)
	got := parseMetric(req)
	want := "requests"
	if got != want {
		t.Errorf("parseMetric() with no query string = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// parseExcludeDeleted tests
// ---------------------------------------------------------------------------

func TestParseExcludeDeleted(t *testing.T) {
	tests := []struct {
		name           string
		excludeDeleted string
		want           bool
	}{
		{
			name:           "true returns true",
			excludeDeleted: "true",
			want:           true,
		},
		{
			name:           "empty returns false",
			excludeDeleted: "",
			want:           false,
		},
		{
			name:           "false returns false",
			excludeDeleted: "false",
			want:           false,
		},
		{
			name:           "1 returns false",
			excludeDeleted: "1",
			want:           false,
		},
		{
			name:           "yes returns false",
			excludeDeleted: "yes",
			want:           false,
		},
		{
			name:           "TRUE (uppercase) returns false — exact match",
			excludeDeleted: "TRUE",
			want:           false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/stats?exclude_deleted="+tt.excludeDeleted, http.NoBody)
			got := parseExcludeDeleted(req)
			if got != tt.want {
				t.Errorf("parseExcludeDeleted(%q) = %v, want %v", tt.excludeDeleted, got, tt.want)
			}
		})
	}
}

func TestParseExcludeDeleted_NoQueryString(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/stats", http.NoBody)
	got := parseExcludeDeleted(req)
	if got {
		t.Error("parseExcludeDeleted() with no query string = true, want false")
	}
}

// ---------------------------------------------------------------------------
// fillEmptyBuckets tests
// ---------------------------------------------------------------------------

func timeMustParse(layout, value string) time.Time {
	t, err := time.Parse(layout, value)
	if err != nil {
		panic(err)
	}
	return t
}

// bucketFormat formats a time as "2006-01-02T15:04:05Z".
func bucketFormat(t time.Time) string {
	return t.Format("2006-01-02T15:04:05") + "Z"
}

func makePoint(bucket string, count, tokens, errors int) TimeSeriesPoint {
	return TimeSeriesPoint{
		Bucket:            bucket,
		Count:             count,
		Tokens:            tokens,
		Errors:            errors,
		Latency:           0,
		OverheadMs:        0,
		ProviderLatencyMs: 0,
		RateLimitHits:     0,
		AvgTTFTMs:         0,
	}
}

func TestFillEmptyBuckets_Hourly_EmptyPoints(t *testing.T) {
	start := timeMustParse(time.RFC3339, "2026-01-15T10:00:00Z")
	end := timeMustParse(time.RFC3339, "2026-01-16T09:00:00Z") // 24 hours later

	got := fillEmptyBuckets(nil, start, end, "hour")

	if len(got) != 24 {
		t.Fatalf("expected 24 buckets, got %d", len(got))
	}
	for i, p := range got {
		expectedBucket := bucketFormat(start.Add(time.Duration(i) * time.Hour))
		if p.Bucket != expectedBucket {
			t.Errorf("bucket[%d] = %q, want %q", i, p.Bucket, expectedBucket)
		}
		if p.Count != 0 {
			t.Errorf("bucket[%d].Count = %d, want 0", i, p.Count)
		}
	}
}

func TestFillEmptyBuckets_Hourly_PartialGaps(t *testing.T) {
	start := timeMustParse(time.RFC3339, "2026-01-15T10:00:00Z")
	end := timeMustParse(time.RFC3339, "2026-01-16T09:00:00Z")

	// Provide points for hours 0, 12, and 23 only.
	points := []TimeSeriesPoint{
		makePoint(bucketFormat(start), 10, 100, 1),                 // hour 0
		makePoint(bucketFormat(start.Add(12*time.Hour)), 5, 50, 2), // hour 12
		makePoint(bucketFormat(start.Add(23*time.Hour)), 8, 80, 0), // hour 23
	}

	got := fillEmptyBuckets(points, start, end, "hour")

	if len(got) != 24 {
		t.Fatalf("expected 24 buckets, got %d", len(got))
	}

	// Check filled buckets
	for i, p := range got {
		expectedBucket := bucketFormat(start.Add(time.Duration(i) * time.Hour))
		if p.Bucket != expectedBucket {
			t.Errorf("bucket[%d].Bucket = %q, want %q", i, p.Bucket, expectedBucket)
		}
		switch i {
		case 0:
			if p.Count != 10 || p.Tokens != 100 || p.Errors != 1 {
				t.Errorf("bucket[%d] = %+v, want Count=10 Tokens=100 Errors=1", i, p)
			}
		case 12:
			if p.Count != 5 || p.Tokens != 50 || p.Errors != 2 {
				t.Errorf("bucket[%d] = %+v, want Count=5 Tokens=50 Errors=2", i, p)
			}
		case 23:
			if p.Count != 8 || p.Tokens != 80 || p.Errors != 0 {
				t.Errorf("bucket[%d] = %+v, want Count=8 Tokens=80 Errors=0", i, p)
			}
		default:
			// Should be zero-filled
			if p.Count != 0 || p.Tokens != 0 || p.Errors != 0 || p.Latency != 0 || p.RateLimitHits != 0 || p.AvgTTFTMs != 0 {
				t.Errorf("bucket[%d] should be zero-filled, got %+v", i, p)
			}
		}
	}
}

func TestFillEmptyBuckets_Hourly_FullCoverage(t *testing.T) {
	start := timeMustParse(time.RFC3339, "2026-01-15T10:00:00Z")
	end := timeMustParse(time.RFC3339, "2026-01-16T09:00:00Z")

	points := make([]TimeSeriesPoint, 24)
	for i := 0; i < 24; i++ {
		points[i] = makePoint(bucketFormat(start.Add(time.Duration(i)*time.Hour)), i+1, i*10, i%3)
	}

	got := fillEmptyBuckets(points, start, end, "hour")

	if len(got) != 24 {
		t.Fatalf("expected 24 buckets, got %d", len(got))
	}
	for i, p := range got {
		if p.Count != i+1 {
			t.Errorf("bucket[%d].Count = %d, want %d", i, p.Count, i+1)
		}
	}
}

func TestFillEmptyBuckets_Hourly_ConsecutiveGaps(t *testing.T) {
	start := timeMustParse(time.RFC3339, "2026-01-15T10:00:00Z")
	end := timeMustParse(time.RFC3339, "2026-01-16T09:00:00Z")

	// Only first and last bucket filled
	points := []TimeSeriesPoint{
		makePoint(bucketFormat(start), 99, 999, 9),
		makePoint(bucketFormat(end), 1, 1, 0),
	}

	got := fillEmptyBuckets(points, start, end, "hour")

	if len(got) != 24 {
		t.Fatalf("expected 24 buckets, got %d", len(got))
	}
	if got[0].Count != 99 {
		t.Errorf("first bucket Count = %d, want 99", got[0].Count)
	}
	if got[23].Count != 1 {
		t.Errorf("last bucket Count = %d, want 1", got[23].Count)
	}
	for i := 1; i < 23; i++ {
		if got[i].Count != 0 {
			t.Errorf("bucket[%d] should be zero-filled, got Count=%d", i, got[i].Count)
		}
	}
}

func TestFillEmptyBuckets_Daily_EmptyPoints(t *testing.T) {
	start := timeMustParse(time.RFC3339, "2026-01-10T00:00:00Z")
	end := timeMustParse(time.RFC3339, "2026-01-16T00:00:00Z") // 7 days

	got := fillEmptyBuckets(nil, start, end, "day")

	if len(got) != 7 {
		t.Fatalf("expected 7 buckets, got %d", len(got))
	}
	for i, p := range got {
		expectedBucket := bucketFormat(start.Add(time.Duration(i) * 24 * time.Hour))
		if p.Bucket != expectedBucket {
			t.Errorf("bucket[%d] = %q, want %q", i, p.Bucket, expectedBucket)
		}
		if p.Count != 0 {
			t.Errorf("bucket[%d].Count = %d, want 0", i, p.Count)
		}
	}
}

func TestFillEmptyBuckets_Daily_PartialGaps(t *testing.T) {
	start := timeMustParse(time.RFC3339, "2026-01-10T00:00:00Z")
	end := timeMustParse(time.RFC3339, "2026-01-16T00:00:00Z") // 7 days

	// Provide points for days 0, 3, and 6.
	points := []TimeSeriesPoint{
		makePoint(bucketFormat(start), 100, 1000, 1),                     // day 0
		makePoint(bucketFormat(start.Add(3*24*time.Hour)), 50, 500, 2),   // day 3
		makePoint(bucketFormat(start.Add(6*24*time.Hour)), 200, 2000, 0), // day 6
	}

	got := fillEmptyBuckets(points, start, end, "day")

	if len(got) != 7 {
		t.Fatalf("expected 7 buckets, got %d", len(got))
	}

	for i, p := range got {
		expectedBucket := bucketFormat(start.Add(time.Duration(i) * 24 * time.Hour))
		if p.Bucket != expectedBucket {
			t.Errorf("bucket[%d].Bucket = %q, want %q", i, p.Bucket, expectedBucket)
		}
		switch i {
		case 0:
			if p.Count != 100 || p.Tokens != 1000 || p.Errors != 1 {
				t.Errorf("bucket[%d] = %+v, want Count=100 Tokens=1000 Errors=1", i, p)
			}
		case 3:
			if p.Count != 50 || p.Tokens != 500 || p.Errors != 2 {
				t.Errorf("bucket[%d] = %+v, want Count=50 Tokens=500 Errors=2", i, p)
			}
		case 6:
			if p.Count != 200 || p.Tokens != 2000 || p.Errors != 0 {
				t.Errorf("bucket[%d] = %+v, want Count=200 Tokens=2000 Errors=0", i, p)
			}
		default:
			if p.Count != 0 || p.Tokens != 0 || p.Errors != 0 {
				t.Errorf("bucket[%d] should be zero-filled, got %+v", i, p)
			}
		}
	}
}

func TestFillEmptyBuckets_Daily_FullCoverage(t *testing.T) {
	start := timeMustParse(time.RFC3339, "2026-01-10T00:00:00Z")
	end := timeMustParse(time.RFC3339, "2026-01-16T00:00:00Z") // 7 days

	points := make([]TimeSeriesPoint, 7)
	for i := 0; i < 7; i++ {
		points[i] = makePoint(bucketFormat(start.Add(time.Duration(i)*24*time.Hour)), i*5, i*50, i)
	}

	got := fillEmptyBuckets(points, start, end, "day")

	if len(got) != 7 {
		t.Fatalf("expected 7 buckets, got %d", len(got))
	}
	for i, p := range got {
		if p.Count != i*5 {
			t.Errorf("bucket[%d].Count = %d, want %d", i, p.Count, i*5)
		}
		if p.Tokens != i*50 {
			t.Errorf("bucket[%d].Tokens = %d, want %d", i, p.Tokens, i*50)
		}
	}
}

func TestFillEmptyBuckets_PreservesAllFields(t *testing.T) {
	start := timeMustParse(time.RFC3339, "2026-01-15T10:00:00Z")
	end := timeMustParse(time.RFC3339, "2026-01-16T09:00:00Z")

	points := []TimeSeriesPoint{
		{
			Bucket:            bucketFormat(start),
			Count:             5,
			Tokens:            100,
			Errors:            2,
			Latency:           150.5,
			OverheadMs:        10.2,
			ProviderLatencyMs: 140.3,
			RateLimitHits:     1,
			AvgTTFTMs:         45.6,
		},
	}

	got := fillEmptyBuckets(points, start, end, "hour")

	if len(got) != 24 {
		t.Fatalf("expected 24 buckets, got %d", len(got))
	}

	// First bucket — all fields preserved
	p := got[0]
	if p.Bucket != bucketFormat(start) {
		t.Errorf("Bucket = %q, want %q", p.Bucket, bucketFormat(start))
	}
	if p.Count != 5 {
		t.Errorf("Count = %d, want 5", p.Count)
	}
	if p.Tokens != 100 {
		t.Errorf("Tokens = %d, want 100", p.Tokens)
	}
	if p.Errors != 2 {
		t.Errorf("Errors = %d, want 2", p.Errors)
	}
	if p.Latency != 150.5 {
		t.Errorf("Latency = %f, want 150.5", p.Latency)
	}
	if p.OverheadMs != 10.2 {
		t.Errorf("OverheadMs = %f, want 10.2", p.OverheadMs)
	}
	if p.ProviderLatencyMs != 140.3 {
		t.Errorf("ProviderLatencyMs = %f, want 140.3", p.ProviderLatencyMs)
	}
	if p.RateLimitHits != 1 {
		t.Errorf("RateLimitHits = %d, want 1", p.RateLimitHits)
	}
	if p.AvgTTFTMs != 45.6 {
		t.Errorf("AvgTTFTMs = %f, want 45.6", p.AvgTTFTMs)
	}

	// Second bucket (zero-filled) — check defaults
	p = got[1]
	if p.Bucket != bucketFormat(start.Add(1*time.Hour)) {
		t.Errorf("Bucket = %q, want %q", p.Bucket, bucketFormat(start.Add(1*time.Hour)))
	}
	if p.Count != 0 {
		t.Errorf("zero-filled Count = %d, want 0", p.Count)
	}
	if p.Tokens != 0 {
		t.Errorf("zero-filled Tokens = %d, want 0", p.Tokens)
	}
	if p.Errors != 0 {
		t.Errorf("zero-filled Errors = %d, want 0", p.Errors)
	}
	if p.Latency != 0 {
		t.Errorf("zero-filled Latency = %f, want 0", p.Latency)
	}
	if p.OverheadMs != 0 {
		t.Errorf("zero-filled OverheadMs = %f, want 0", p.OverheadMs)
	}
	if p.ProviderLatencyMs != 0 {
		t.Errorf("zero-filled ProviderLatencyMs = %f, want 0", p.ProviderLatencyMs)
	}
	if p.RateLimitHits != 0 {
		t.Errorf("zero-filled RateLimitHits = %d, want 0", p.RateLimitHits)
	}
	if p.AvgTTFTMs != 0 {
		t.Errorf("zero-filled AvgTTFTMs = %f, want 0", p.AvgTTFTMs)
	}
}

func TestFillEmptyBuckets_DailyEndTruncation(t *testing.T) {
	// When bucketSize is "day", the end time gets truncated to 24h boundary.
	// If end is mid-day, it should still produce the right number of buckets.
	start := timeMustParse(time.RFC3339, "2026-01-10T00:00:00Z")
	end := timeMustParse(time.RFC3339, "2026-01-16T15:30:00Z") // mid-day, should truncate

	got := fillEmptyBuckets(nil, start, end, "day")

	if len(got) != 7 {
		t.Fatalf("expected 7 buckets, got %d", len(got))
	}
	// The last bucket should be the truncated end (start + 6 days)
	lastExpected := bucketFormat(start.Add(6 * 24 * time.Hour))
	if got[6].Bucket != lastExpected {
		t.Errorf("last bucket = %q, want %q", got[6].Bucket, lastExpected)
	}
}

// ---------------------------------------------------------------------------
// Integration tests for DB-querying handler functions
// ---------------------------------------------------------------------------

// TestMain is defined in failover_api_test.go for the api package.
// Integration tests use the shared apiTestDBURL variable.

func newStatsHandler(t *testing.T) (*StatsHandler, *pgxpool.Pool, func()) {
	t.Helper()

	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}

	// Clean test data
	pool.Exec(context.Background(), `
		TRUNCATE request_logs, providers, models, virtual_keys CASCADE
	`)

	// Create admin manager
	tmpDir := t.TempDir()
	adminMgr, _, err := admin.New(tmpDir, "test-admin-token")
	if err != nil {
		t.Fatalf("failed to create admin manager: %v", err)
	}

	handler := NewStatsHandler(pool, adminMgr)
	if handler == nil {
		pool.Close()
		t.Fatal("handler is nil")
	}

	cleanup := func() {
		pool.Close()
	}

	return handler, pool, cleanup
}

func insertTestProvider(t *testing.T, pool *pgxpool.Pool, providerID uuid.UUID, name, baseURL string) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO providers (id, name, base_url, enabled, created_at, updated_at)
		VALUES ($1, $2, $3, true, NOW(), NOW())`,
		providerID, name, baseURL)
	if err != nil {
		t.Fatalf("Failed to insert test provider: %v", err)
	}
}

func insertTestRequestLog(t *testing.T, pool *pgxpool.Pool, logID, providerID uuid.UUID, modelID string, statusCode, durationMs, tokensPrompt, tokensCompletion int) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW())`,
		logID, providerID, modelID, statusCode, durationMs, tokensPrompt, tokensCompletion)
	if err != nil {
		t.Fatalf("Failed to insert test request log: %v", err)
	}
}

func TestGetStats_24h(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Set up test data
	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-24h", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	// Create router and call handler
	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.TotalRequestsLast24h != 1 {
		t.Errorf("Expected TotalRequestsLast24h=1, got %d", response.TotalRequestsLast24h)
	}
}

func TestGetStats_7d(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Set up test data
	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-7d", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	// Create router and call handler
	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?period=7d", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	if response.TotalRequestsLast7d != 1 {
		t.Errorf("Expected TotalRequestsLast7d=1, got %d", response.TotalRequestsLast7d)
	}
}

func TestGetStats_TokensMetric(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Set up test data
	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-tokens", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 100, 200)

	// Create router and call handler
	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?metric=tokens", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// With metric=tokens, ByModel and ByProvider should contain token counts
	if len(response.ByModel) == 0 {
		t.Error("Expected ByModel to have entries with metric=tokens")
	}
	if len(response.ByProvider) == 0 {
		t.Error("Expected ByProvider to have entries with metric=tokens")
	}
}

func TestGetStats_ExcludeDeleted(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Set up test data
	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-exclude", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	// Create router and call handler
	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?exclude_deleted=true", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response StatsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should return stats successfully
	if response.TotalRequestsLast24h != 1 {
		t.Errorf("Expected TotalRequestsLast24h=1, got %d", response.TotalRequestsLast24h)
	}
}

func TestGetTimeSeries_24h(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Set up test data
	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-ts24h", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	// Create router and call handler
	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/timeseries?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response TimeSeriesStats
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should have time series points (may be filled with zeros for missing hours)
	if len(response.Points) == 0 {
		t.Error("Expected time series points")
	}
}

func TestGetTimeSeries_7d(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Set up test data
	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-ts7d", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	// Create router and call handler
	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/timeseries?period=7d", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response TimeSeriesStats
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// With 7d period, should have daily buckets (7-8 expected depending on time boundaries)
	if len(response.Points) == 0 {
		t.Error("Expected time series points for 7d period")
	}
	// Check that we have the expected number of buckets (may be 7 or 8 depending on time boundaries)
	if len(response.Points) < 7 || len(response.Points) > 8 {
		t.Errorf("Expected 7-8 daily buckets, got %d", len(response.Points))
	}
}

func TestGetProviderDistribution_Integration(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Set up test data
	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-dist", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	// Create router and call handler
	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/provider-distribution", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var response ProviderDistributionStats
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Should have provider distribution items
	if len(response.Items) == 0 {
		t.Error("Expected provider distribution items")
	}

	// Check that our test provider is in the results
	found := false
	for _, item := range response.Items {
		if item.Name == "test-provider-dist" {
			found = true
			if item.Count != 1 {
				t.Errorf("Expected Count=1 for test-provider-dist, got %d", item.Count)
			}
			break
		}
	}
	if !found {
		t.Error("Expected test-provider-dist in provider distribution")
	}
}
