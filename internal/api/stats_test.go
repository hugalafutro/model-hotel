package api

import (
	"context"
	"encoding/json"
	"errors"
	"math"
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

type requestLogOpts struct {
	VirtualKeyID     *uuid.UUID
	VirtualKeyName   string
	ResponseHeaderMs float64
	ProxyOverheadMs  float64
	Streaming        bool
}

func insertRichTestRequestLog(t *testing.T, pool *pgxpool.Pool, logID, providerID uuid.UUID, modelID string, statusCode, durationMs, tokensPrompt, tokensCompletion int, opts requestLogOpts) {
	t.Helper()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, created_at, virtual_key_id, virtual_key_name, response_header_ms, proxy_overhead_ms, streaming)
		VALUES ($1, $2, $3, $4, $5, $6, $7, NOW(), $8, $9, $10, $11, $12)`,
		logID, providerID, modelID, statusCode, durationMs, tokensPrompt, tokensCompletion,
		opts.VirtualKeyID, opts.VirtualKeyName, opts.ResponseHeaderMs, opts.ProxyOverheadMs, opts.Streaming)
	if err != nil {
		t.Fatalf("Failed to insert rich test request log: %v", err)
	}
}

type brokenResponseWriter struct {
	header http.Header
	code   int
}

func (b *brokenResponseWriter) Header() http.Header {
	if b.header == nil {
		b.header = make(http.Header)
	}
	return b.header
}

func (b *brokenResponseWriter) Write(p []byte) (int, error) {
	return 0, errors.New("write error")
}

func (b *brokenResponseWriter) WriteHeader(code int) {
	b.code = code
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

	// With 7d period, should have daily buckets (up to 31 for 30d of panning data, inclusive range)
	if len(response.Points) == 0 {
		t.Error("Expected time series points for 7d period")
	}
	if len(response.Points) > 31 {
		t.Errorf("Expected at most 31 daily buckets, got %d", len(response.Points))
	}
}

func TestGetTimeSeries_CacheTokens(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Set up test data with cache hit/miss tokens
	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-cache", "https://api.example.com/v1")

	logID := uuid.New()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, tokens_prompt_cache_hit, tokens_prompt_cache_miss, created_at)
		VALUES ($1, $2, 'test-model', 200, 100, 50, 30, 40, 10, NOW())`,
		logID, providerID)
	if err != nil {
		t.Fatalf("Failed to insert test request log with cache tokens: %v", err)
	}

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

	if len(response.Points) == 0 {
		t.Fatal("Expected time series points")
	}

	// Find the point with cache hit data
	var found bool
	for _, p := range response.Points {
		if p.TokensCacheHit == 40 && p.TokensCacheMiss == 10 {
			found = true
			break
		}
	}
	if !found {
		var hit, miss int
		for _, p := range response.Points {
			hit += p.TokensCacheHit
			miss += p.TokensCacheMiss
		}
		t.Errorf("Expected a point with tokens_cache_hit=40, tokens_cache_miss=10; got totals hit=%d miss=%d", hit, miss)
	}
}

func TestGetTimeSeries_CacheTokens_ZeroValues(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-zero-cache", "https://api.example.com/v1")

	// Insert row with zero cache tokens
	logID := uuid.New()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, tokens_prompt_cache_hit, tokens_prompt_cache_miss, created_at)
		VALUES ($1, $2, 'test-model', 200, 100, 50, 30, 0, 0, NOW())`,
		logID, providerID)
	if err != nil {
		t.Fatalf("Failed to insert test request log: %v", err)
	}

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

	if len(response.Points) == 0 {
		t.Fatal("Expected time series points")
	}

	// Find the point with the log entry — cache fields should be 0
	var found bool
	for _, p := range response.Points {
		if p.Count > 0 && p.TokensCacheHit == 0 && p.TokensCacheMiss == 0 {
			found = true
			break
		}
	}
	if !found {
		t.Error("Expected a point with zero cache hit/miss tokens and count > 0")
	}
}

func TestGetTimeSeries_CacheTokens_MultiRowAggregation(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-multi", "https://api.example.com/v1")

	// Insert three rows in the same bucket (same NOW() timestamp)
	ctx := context.Background()
	type cacheRow struct{ hit, miss int }
	for i, row := range []cacheRow{
		{40, 10},
		{20, 5},
		{0, 15},
	} {
		logID := uuid.New()
		_, err := pool.Exec(ctx, `
			INSERT INTO request_logs (id, provider_id, model_id, status_code, duration_ms, tokens_prompt, tokens_completion, tokens_prompt_cache_hit, tokens_prompt_cache_miss, created_at)
			VALUES ($1, $2, 'test-model', 200, 100, 50, 30, $3, $4, NOW())`,
			logID, providerID, row.hit, row.miss)
		if err != nil {
			t.Fatalf("Failed to insert test request log %d: %v", i, err)
		}
	}

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

	// SUM(hit) = 40+20+0 = 60, SUM(miss) = 10+5+15 = 30
	var found bool
	for _, p := range response.Points {
		if p.TokensCacheHit == 60 && p.TokensCacheMiss == 30 {
			found = true
			break
		}
	}
	if !found {
		var hit, miss int
		for _, p := range response.Points {
			hit += p.TokensCacheHit
			miss += p.TokensCacheMiss
		}
		t.Errorf("Expected a point with cache_hit=60, cache_miss=30; got totals hit=%d miss=%d", hit, miss)
	}
}

func TestGetTimeSeries_CacheTokens_JSONRoundTrip(t *testing.T) {
	original := TimeSeriesPoint{
		Bucket:          "2025-06-01T12:00:00Z",
		Count:           3,
		Tokens:          150,
		TokensCacheHit:  60,
		TokensCacheMiss: 30,
		Errors:          0,
		Latency:         100,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Failed to marshal: %v", err)
	}

	var decoded TimeSeriesPoint
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Failed to unmarshal: %v", err)
	}

	if decoded.TokensCacheHit != original.TokensCacheHit {
		t.Errorf("TokensCacheHit: got %d, want %d", decoded.TokensCacheHit, original.TokensCacheHit)
	}
	if decoded.TokensCacheMiss != original.TokensCacheMiss {
		t.Errorf("TokensCacheMiss: got %d, want %d", decoded.TokensCacheMiss, original.TokensCacheMiss)
	}

	// Verify JSON keys match the struct tags
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Failed to unmarshal to raw map: %v", err)
	}
	if _, ok := raw["tokens_cache_hit"]; !ok {
		t.Error("Missing tokens_cache_hit key in JSON output")
	}
	if _, ok := raw["tokens_cache_miss"]; !ok {
		t.Error("Missing tokens_cache_miss key in JSON output")
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

func TestGetStats_DeletedVirtualKey(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-delvk", "https://api.example.com/v1")

	// Insert request log with a virtual_key_id that doesn't exist in virtual_keys table
	deletedVKID := uuid.New()
	logID := uuid.New()
	insertRichTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20, requestLogOpts{
		VirtualKeyID: &deletedVKID,
	})

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

	if response.ByVirtualKey["Deleted"] != 1 {
		t.Errorf("Expected ByVirtualKey['Deleted']=1, got %d", response.ByVirtualKey["Deleted"])
	}
}

func TestGetStats_ChatArenaKeys(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-chat", "https://api.example.com/v1")

	// Insert request logs with chat and arena virtual_key_name
	chatLogID := uuid.New()
	insertRichTestRequestLog(t, pool, chatLogID, providerID, "test-model", 200, 100, 10, 20, requestLogOpts{
		VirtualKeyName: "chat",
	})
	arenaLogID := uuid.New()
	insertRichTestRequestLog(t, pool, arenaLogID, providerID, "test-model", 200, 100, 5, 10, requestLogOpts{
		VirtualKeyName: "arena",
	})

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

	if response.ByVirtualKey["chat"] != 1 {
		t.Errorf("Expected ByVirtualKey['chat']=1, got %d", response.ByVirtualKey["chat"])
	}
	if response.ByVirtualKey["arena"] != 1 {
		t.Errorf("Expected ByVirtualKey['arena']=1, got %d", response.ByVirtualKey["arena"])
	}
}

func TestGetTimeSeries_ExcludeDeleted(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-ts-del", "https://api.example.com/v1")

	// Insert one log with deleted VK and one without
	deletedVKID := uuid.New()
	logID1 := uuid.New()
	insertRichTestRequestLog(t, pool, logID1, providerID, "test-model", 200, 100, 10, 20, requestLogOpts{
		VirtualKeyID: &deletedVKID,
	})
	logID2 := uuid.New()
	insertTestRequestLog(t, pool, logID2, providerID, "test-model", 200, 100, 5, 10)

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/timeseries?period=24h&exclude_deleted=true", http.NoBody)
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

	// With exclude_deleted=true, only the log without a deleted VK should be counted.
	// Total count across all points should be 1 (logID2), not 2.
	var totalCount int
	for _, p := range response.Points {
		totalCount += p.Count
	}
	if totalCount != 1 {
		t.Errorf("Expected total count=1 (deleted VK excluded), got %d", totalCount)
	}
}

func TestGetProviderDistribution_ExcludeDeleted(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-pd-del", "https://api.example.com/v1")

	// Insert one log with deleted VK and one without
	deletedVKID := uuid.New()
	logID1 := uuid.New()
	insertRichTestRequestLog(t, pool, logID1, providerID, "test-model", 200, 100, 10, 20, requestLogOpts{
		VirtualKeyID: &deletedVKID,
	})
	logID2 := uuid.New()
	insertTestRequestLog(t, pool, logID2, providerID, "test-model", 200, 100, 5, 10)

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/provider-distribution?period=24h&exclude_deleted=true", http.NoBody)
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

	// With exclude_deleted=true, only the log without a deleted VK should be counted.
	// The provider should have Count=1, not 2.
	if len(response.Items) == 0 {
		t.Fatal("Expected provider distribution items")
	}
	for _, item := range response.Items {
		if item.Name == "test-provider-pd-del" {
			if item.Count != 1 {
				t.Errorf("Expected Count=1 (deleted VK excluded), got %d", item.Count)
			}
			break
		}
	}
}

func TestGetProviderDistribution_TokensMetric(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-pd-tok", "https://api.example.com/v1")

	logID := uuid.New()
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/provider-distribution?period=24h&metric=tokens", http.NoBody)
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

	if len(response.Items) == 0 {
		t.Fatal("Expected provider distribution items")
	}

	// With metric=tokens, Count should be 0 and Tokens should be > 0
	for _, item := range response.Items {
		if item.Name == "test-provider-pd-tok" {
			if item.Count != 0 {
				t.Errorf("Expected Count=0 for tokens metric, got %d", item.Count)
			}
			if item.Tokens <= 0 {
				t.Errorf("Expected Tokens>0 for tokens metric, got %d", item.Tokens)
			}
			break
		}
	}
}

func TestGetStats_ClosedPool(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Close pool before making request to trigger query error
	pool.Close()

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500, got %d", rec.Code)
	}
}

func TestGetTimeSeries_ClosedPool(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	pool.Close()

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/timeseries?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500, got %d", rec.Code)
	}
}

func TestGetProviderDistribution_ClosedPool(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	pool.Close()

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/provider-distribution?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500, got %d", rec.Code)
	}
}

func TestGetStats_JSONEncodeError(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Insert data so calculateStats succeeds
	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-json", "https://api.example.com/v1")
	logID := uuid.New()
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	w := &brokenResponseWriter{}
	r := httptest.NewRequest(http.MethodGet, "/stats?period=24h", http.NoBody)
	r.Header.Set("Authorization", "Bearer test-admin-token")

	handler.GetStats(w, r)

	if w.code != http.StatusInternalServerError {
		t.Errorf("Expected 500, got %d", w.code)
	}
}

func TestGetTimeSeries_JSONEncodeError(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-ts-json", "https://api.example.com/v1")
	logID := uuid.New()
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	w := &brokenResponseWriter{}
	r := httptest.NewRequest(http.MethodGet, "/stats/timeseries?period=24h", http.NoBody)
	r.Header.Set("Authorization", "Bearer test-admin-token")

	handler.GetTimeSeries(w, r)

	if w.code != http.StatusInternalServerError {
		t.Errorf("Expected 500, got %d", w.code)
	}
}

func TestGetProviderDistribution_JSONEncodeError(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-pd-json", "https://api.example.com/v1")
	logID := uuid.New()
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	w := &brokenResponseWriter{}
	r := httptest.NewRequest(http.MethodGet, "/stats/provider-distribution?period=24h", http.NoBody)
	r.Header.Set("Authorization", "Bearer test-admin-token")

	handler.GetProviderDistribution(w, r)

	if w.code != http.StatusInternalServerError {
		t.Errorf("Expected 500, got %d", w.code)
	}
}

// ---------------------------------------------------------------------------
// Additional test coverage for stats.go
// ---------------------------------------------------------------------------

func TestGetStats_MultipleProviders(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Insert 3 providers
	providerA := uuid.New()
	providerB := uuid.New()
	providerC := uuid.New()
	insertTestProvider(t, pool, providerA, "provider-a", "https://api.a.com/v1")
	insertTestProvider(t, pool, providerB, "provider-b", "https://api.b.com/v1")
	insertTestProvider(t, pool, providerC, "provider-c", "https://api.c.com/v1")

	// Insert request logs: 2 for A, 3 for B, 5 for C (total=10)
	for i := 0; i < 2; i++ {
		insertTestRequestLog(t, pool, uuid.New(), providerA, "model-a", 200, 100, 10, 20)
	}
	for i := 0; i < 3; i++ {
		insertTestRequestLog(t, pool, uuid.New(), providerB, "model-b", 200, 100, 10, 20)
	}
	for i := 0; i < 5; i++ {
		insertTestRequestLog(t, pool, uuid.New(), providerC, "model-c", 200, 100, 10, 20)
	}

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

	if response.TotalRequestsLast24h != 10 {
		t.Errorf("Expected TotalRequestsLast24h=10, got %d", response.TotalRequestsLast24h)
	}

	if len(response.ByProvider) != 3 {
		t.Errorf("Expected ByProvider to have 3 entries, got %d", len(response.ByProvider))
	}

	// Check individual provider counts
	expectedCounts := map[string]int64{
		"provider-a": 2,
		"provider-b": 3,
		"provider-c": 5,
	}
	for name, expected := range expectedCounts {
		if response.ByProvider[name] != int64(expected) {
			t.Errorf("Expected ByProvider[%q]=%d, got %d", name, expected, response.ByProvider[name])
		}
	}
}

func TestGetStats_MultipleModels(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider", "https://api.example.com/v1")

	// Insert request logs for 3 different models
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20)
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-b", 200, 100, 10, 20)
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-c", 200, 100, 10, 20)

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

	if len(response.ByModel) != 3 {
		t.Errorf("Expected ByModel to have 3 entries, got %d", len(response.ByModel))
	}

	// ByModel uses format "provider_name/model_id"
	expectedModels := map[string]bool{
		"test-provider/model-a": true,
		"test-provider/model-b": true,
		"test-provider/model-c": true,
	}
	for model := range expectedModels {
		if _, ok := response.ByModel[model]; !ok {
			t.Errorf("Expected ByModel to contain %q, got keys: %v", model, response.ByModel)
		}
	}
}

func TestGetStats_RateLimitHits(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider", "https://api.example.com/v1")

	// Insert 3 request logs: 2 with status 200, 1 with status 429
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20)
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20)
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 429, 100, 0, 0)

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

	if response.TotalRequestsLast24h != 3 {
		t.Errorf("Expected TotalRequestsLast24h=3, got %d", response.TotalRequestsLast24h)
	}

	if response.RateLimitHits != 1 {
		t.Errorf("Expected RateLimitHits=1, got %d", response.RateLimitHits)
	}
}

func TestGetStats_ErrorRate(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider", "https://api.example.com/v1")

	// Insert request logs: 3 with status 200, 1 with status 400, 1 with status 500
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20)
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20)
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20)
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 400, 100, 0, 0)
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 500, 100, 0, 0)

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

	// ErrorRate should be 0.4 (2 errors out of 5 requests)
	if response.ErrorRate <= 0 {
		t.Errorf("Expected ErrorRate > 0, got %f", response.ErrorRate)
	}
	// Allow some tolerance for floating point
	if response.ErrorRate < 0.35 || response.ErrorRate > 0.45 {
		t.Errorf("Expected ErrorRate around 0.4, got %f", response.ErrorRate)
	}
}

func TestGetStats_TTFTAndOverhead(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider", "https://api.example.com/v1")

	// Insert rich request logs with TTFT and overhead values (streaming=true for TTFT)
	insertRichTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20, requestLogOpts{
		ResponseHeaderMs: 50.0,
		ProxyOverheadMs:  5.0,
		Streaming:        true,
	})
	insertRichTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20, requestLogOpts{
		ResponseHeaderMs: 100.0,
		ProxyOverheadMs:  10.0,
		Streaming:        true,
	})
	insertRichTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20, requestLogOpts{
		ResponseHeaderMs: 75.0,
		ProxyOverheadMs:  7.5,
		Streaming:        true,
	})

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

	if response.AvgTTFTMs <= 0 {
		t.Errorf("Expected AvgTTFTMs > 0, got %f", response.AvgTTFTMs)
	}

	if response.AvgOverheadMs <= 0 {
		t.Errorf("Expected AvgOverheadMs > 0, got %f", response.AvgOverheadMs)
	}
}

func TestGetProviderDistribution_MultipleProviders_ShareRounding(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Insert 3 providers
	providerA := uuid.New()
	providerB := uuid.New()
	providerC := uuid.New()
	insertTestProvider(t, pool, providerA, "provider-a", "https://api.a.com/v1")
	insertTestProvider(t, pool, providerB, "provider-b", "https://api.b.com/v1")
	insertTestProvider(t, pool, providerC, "provider-c", "https://api.c.com/v1")

	// Insert request logs: 7 for A, 2 for B, 1 for C (total=10)
	for i := 0; i < 7; i++ {
		insertTestRequestLog(t, pool, uuid.New(), providerA, "model-a", 200, 100, 10, 20)
	}
	for i := 0; i < 2; i++ {
		insertTestRequestLog(t, pool, uuid.New(), providerB, "model-b", 200, 100, 10, 20)
	}
	insertTestRequestLog(t, pool, uuid.New(), providerC, "model-c", 200, 100, 10, 20)

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/provider-distribution?period=24h", http.NoBody)
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

	if len(response.Items) != 3 {
		t.Errorf("Expected 3 items, got %d", len(response.Items))
	}

	// Check share values sum to approximately 100.0
	var totalShare float64
	for _, item := range response.Items {
		totalShare += item.Share
	}
	if totalShare < 99.9 || totalShare > 100.1 {
		t.Errorf("Expected shares to sum to ~100.0, got %f", totalShare)
	}

	// Find provider-a and verify it has the largest share (~70%)
	var providerAShare float64
	for _, item := range response.Items {
		if item.Name == "provider-a" {
			providerAShare = item.Share
			if item.Count != 7 {
				t.Errorf("Expected provider-a Count=7, got %d", item.Count)
			}
			// Verify Count field for requests metric (Count > 0, Tokens == 0)
			if item.Tokens != 0 {
				t.Errorf("Expected provider-a Tokens=0 for requests metric, got %d", item.Tokens)
			}
			break
		}
	}
	if providerAShare < 65 || providerAShare > 75 {
		t.Errorf("Expected provider-a share around 70%%, got %f%%", providerAShare)
	}
}

func TestGetTimeSeries_TokensMetric(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider", "https://api.example.com/v1")

	// Insert request logs with prompt/completion tokens
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 100, 200)
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 150, 250)
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 50, 100)

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/timeseries?period=24h&metric=tokens", http.NoBody)
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

	if len(response.Points) == 0 {
		t.Fatal("Expected response.Points to have entries")
	}

	// Verify some points have Tokens > 0
	var hasTokens bool
	for _, p := range response.Points {
		if p.Tokens > 0 {
			hasTokens = true
			break
		}
	}
	if !hasTokens {
		t.Error("Expected some points to have Tokens > 0")
	}
}

func TestGetStats_VirtualKeyAggregation(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider", "https://api.example.com/v1")

	// Insert a virtual key
	vkID := uuid.New()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO virtual_keys (id, name, key_hash, key_preview, tokens_used, last_used_at, created_at)
		VALUES ($1, $2, $3, $4, 0, NOW(), NOW())`,
		vkID, "test-vk-name", "fakehash123", "preview...")
	if err != nil {
		t.Fatalf("Failed to insert virtual key: %v", err)
	}

	// Insert request logs: some with VK, some without
	insertRichTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20, requestLogOpts{
		VirtualKeyID: &vkID,
	})
	insertRichTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20, requestLogOpts{
		VirtualKeyID: &vkID,
	})
	insertTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20) // no VK

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

	if len(response.ByVirtualKey) == 0 {
		t.Error("Expected ByVirtualKey to have entries")
	}
}

func TestNewStatsHandler_Constructor(t *testing.T) {
	if apiTestDBURL == "" {
		t.Skip("skipping: test database not available")
	}

	pool, err := pgxpool.New(context.Background(), apiTestDBURL)
	if err != nil {
		t.Skip("skipping: test database not available")
	}
	defer pool.Close()

	// Create admin manager
	tmpDir := t.TempDir()
	adminMgr, _, err := admin.New(tmpDir, "test-admin-token")
	if err != nil {
		t.Fatalf("failed to create admin manager: %v", err)
	}

	handler := NewStatsHandler(pool, adminMgr)
	if handler == nil {
		t.Fatal("Expected handler to be non-nil")
		return
	}
}

func TestGetStats_MultipleVirtualKeys(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider", "https://api.example.com/v1")

	// Insert 2 virtual keys
	vk1ID := uuid.New()
	vk2ID := uuid.New()
	ctx := context.Background()
	_, err := pool.Exec(ctx, `
		INSERT INTO virtual_keys (id, name, key_hash, key_preview, tokens_used, last_used_at, created_at)
		VALUES ($1, $2, $3, $4, 0, NOW(), NOW())`,
		vk1ID, "vk-one", "hash1", "pre1")
	if err != nil {
		t.Fatalf("Failed to insert virtual key 1: %v", err)
	}
	_, err = pool.Exec(ctx, `
		INSERT INTO virtual_keys (id, name, key_hash, key_preview, tokens_used, last_used_at, created_at)
		VALUES ($1, $2, $3, $4, 0, NOW(), NOW())`,
		vk2ID, "vk-two", "hash2", "pre2")
	if err != nil {
		t.Fatalf("Failed to insert virtual key 2: %v", err)
	}

	// Insert request logs for each VK
	insertRichTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20, requestLogOpts{
		VirtualKeyID:   &vk1ID,
		VirtualKeyName: "vk-one",
	})
	insertRichTestRequestLog(t, pool, uuid.New(), providerID, "model-a", 200, 100, 10, 20, requestLogOpts{
		VirtualKeyID:   &vk2ID,
		VirtualKeyName: "vk-two",
	})

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

	// Verify both VKs are in ByVirtualKey
	if response.ByVirtualKey["vk-one"] != 1 {
		t.Errorf("Expected ByVirtualKey['vk-one']=1, got %d", response.ByVirtualKey["vk-one"])
	}
	if response.ByVirtualKey["vk-two"] != 1 {
		t.Errorf("Expected ByVirtualKey['vk-two']=1, got %d", response.ByVirtualKey["vk-two"])
	}
}

// ---------------------------------------------------------------------------
// Tests moved from coverage_gap3_test.go
// ---------------------------------------------------------------------------

// TestCalculateStats_TokensMetric tests calculateStats with metric=tokens
// to cover the token aggregation branches in by_model, by_provider, by_virtual_key queries.
func TestCalculateStats_TokensMetric(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Set up test data with token counts
	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-tokens-metric", "https://api.example.com/v1")
	// Insert request log with significant token counts
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 100, 200)

	ctx := context.Background()

	// Call calculateStats with metric=tokens
	stats, err := handler.calculateStats(ctx, 24*time.Hour, true, "tokens")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// With metric=tokens, ByModel and ByProvider should contain token counts
	if len(stats.ByModel) == 0 {
		t.Error("Expected ByModel to have entries with metric=tokens")
	}
	if len(stats.ByProvider) == 0 {
		t.Error("Expected ByProvider to have entries with metric=tokens")
	}

	// Check token totals
	if stats.TotalTokensPrompt != 100 {
		t.Errorf("Expected TotalTokensPrompt=100, got %d", stats.TotalTokensPrompt)
	}
	if stats.TotalTokensCompletion != 200 {
		t.Errorf("Expected TotalTokensCompletion=200, got %d", stats.TotalTokensCompletion)
	}
}

// TestCalculateStats_TokensMetric_ByVirtualKey tests calculateStats with metric=tokens
// for the by_virtual_key query path.
func TestCalculateStats_TokensMetric_ByVirtualKey(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	ctx := context.Background()

	// Create a virtual key
	vkID := uuid.New()
	_, err := pool.Exec(ctx, `
		INSERT INTO virtual_keys (id, name, key_hash, key_preview, created_at)
		VALUES ($1, 'test-vk-tokens', 'hash', 'sk-...ab', NOW())`,
		vkID)
	if err != nil {
		t.Fatalf("Failed to insert virtual key: %v", err)
	}

	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-vk-tokens", "https://api.example.com/v1")

	// Insert request log with virtual key and token counts
	insertRichTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 50, 75, requestLogOpts{
		VirtualKeyID: &vkID,
	})

	stats, err := handler.calculateStats(ctx, 24*time.Hour, true, "tokens")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// ByVirtualKey should contain the virtual key name with token count
	if stats.ByVirtualKey["test-vk-tokens"] != 125 {
		t.Errorf("Expected ByVirtualKey['test-vk-tokens']=125, got %d", stats.ByVirtualKey["test-vk-tokens"])
	}
}

// TestCalculateStats_ExcludeDeletedFalse tests calculateStats with excludeDeleted=false
// to cover the deleted virtual keys aggregate query path.
func TestCalculateStats_ExcludeDeletedFalse(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	ctx := context.Background()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-del-false", "https://api.example.com/v1")

	// Insert request log with a virtual_key_id that doesn't exist in virtual_keys table
	// This simulates a deleted virtual key
	deletedVKID := uuid.New()
	logID := uuid.New()
	insertRichTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20, requestLogOpts{
		VirtualKeyID: &deletedVKID,
	})

	stats, err := handler.calculateStats(ctx, 24*time.Hour, false, "requests")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// With excludeDeleted=false, deleted VK requests should appear in ByVirtualKey["Deleted"]
	if stats.ByVirtualKey["Deleted"] != 1 {
		t.Errorf("Expected ByVirtualKey['Deleted']=1, got %d", stats.ByVirtualKey["Deleted"])
	}
}

// TestCalculateStats_ExcludeDeletedFalse_Tokens tests calculateStats with excludeDeleted=false
// and metric=tokens to cover the deleted VK path with token aggregation.
func TestCalculateStats_ExcludeDeletedFalse_Tokens(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	ctx := context.Background()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-del-tok", "https://api.example.com/v1")

	// Insert request log with deleted VK and token counts
	deletedVKID := uuid.New()
	logID := uuid.New()
	insertRichTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 30, 40, requestLogOpts{
		VirtualKeyID: &deletedVKID,
	})

	stats, err := handler.calculateStats(ctx, 24*time.Hour, false, "tokens")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// Deleted VK should have token count (30+40=70)
	if stats.ByVirtualKey["Deleted"] != 70 {
		t.Errorf("Expected ByVirtualKey['Deleted']=70, got %d", stats.ByVirtualKey["Deleted"])
	}
}

// TestCalculateStats_7dPeriod tests calculateStats with period=7d
// to cover the else branch for non-24h secondary queries.
func TestCalculateStats_7dPeriod(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	ctx := context.Background()

	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-7d-period", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	stats, err := handler.calculateStats(ctx, 7*24*time.Hour, true, "requests")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// With 7d period, TotalRequestsLast7d should be set
	if stats.TotalRequestsLast7d != 1 {
		t.Errorf("Expected TotalRequestsLast7d=1, got %d", stats.TotalRequestsLast7d)
	}

	// The else branch should have queried for 24h ago
	// TotalRequestsLast24h should also be set (from the secondary query)
	if stats.TotalRequestsLast24h != 1 {
		t.Errorf("Expected TotalRequestsLast24h=1, got %d", stats.TotalRequestsLast24h)
	}
}

// TestCalculateStats_1hPeriod tests calculateStats with period=1h
// to cover the else branch for non-7d initial period.
func TestCalculateStats_1hPeriod(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	ctx := context.Background()

	providerID := uuid.New()
	logID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-1h-period", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, logID, providerID, "test-model", 200, 100, 10, 20)

	stats, err := handler.calculateStats(ctx, 1*time.Hour, true, "requests")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// With 1h period, TotalRequestsLast24h is populated by the secondary query
	// (since period != 7d, the else branch queries 24h). Logs exist within 24h.
	// The else branch sets TotalRequestsLast24h = 0 initially
	// Then the secondary query for _24hAgo should populate it
	if stats.TotalRequestsLast24h < 1 {
		t.Errorf("Expected TotalRequestsLast24h>=1, got %d", stats.TotalRequestsLast24h)
	}
}

// TestCalculateStats_ChatArenaKeys_Tokens tests calculateStats with chat/arena virtual_key_name
// and metric=tokens to cover the token aggregation path for chat/arena queries.
func TestCalculateStats_ChatArenaKeys_Tokens(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	ctx := context.Background()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-chat-tok", "https://api.example.com/v1")

	// Insert request logs with chat and arena virtual_key_name and token counts
	chatLogID := uuid.New()
	insertRichTestRequestLog(t, pool, chatLogID, providerID, "test-model", 200, 100, 100, 150, requestLogOpts{
		VirtualKeyName: "chat",
	})
	arenaLogID := uuid.New()
	insertRichTestRequestLog(t, pool, arenaLogID, providerID, "test-model", 200, 100, 80, 120, requestLogOpts{
		VirtualKeyName: "arena",
	})

	stats, err := handler.calculateStats(ctx, 24*time.Hour, true, "tokens")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// Chat should have token count (100+150=250)
	if stats.ByVirtualKey["chat"] != 250 {
		t.Errorf("Expected ByVirtualKey['chat']=250, got %d", stats.ByVirtualKey["chat"])
	}
	// Arena should have token count (80+120=200)
	if stats.ByVirtualKey["arena"] != 200 {
		t.Errorf("Expected ByVirtualKey['arena']=200, got %d", stats.ByVirtualKey["arena"])
	}
}

// TestCalculateStats_QueryError tests calculateStats with a closed pool
// to cover the error paths for various queries beyond the first one.
func TestCalculateStats_QueryError(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Close pool before calling calculateStats
	pool.Close()

	ctx := context.Background()
	_, err := handler.calculateStats(ctx, 24*time.Hour, true, "requests")
	if err == nil {
		t.Error("Expected error when pool is closed")
	}
}

// ---------------------------------------------------------------------------
// Additional coverage for remaining uncovered lines
// ---------------------------------------------------------------------------

// TestGetTimeSeries_1hPeriod tests the 5min bucket query path (period < 24h).
func TestGetTimeSeries_1hPeriod(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-1h-ts", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20)

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/timeseries?period=1h", http.NoBody)
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

	// With 1h period, should have 5min buckets (up to 288 buckets for 24h panning)
	if len(response.Points) == 0 {
		t.Error("Expected time series points for 1h period")
	}
}

// TestGetStats_1hPeriod_HTTP tests GetStats with ?period=1h through HTTP handler.
func TestGetStats_1hPeriod_HTTP(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-1h-http", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20)

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats?period=1h", http.NoBody)
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

	// Stats should be populated
	if response.TotalRequestsLast24h < 1 {
		t.Errorf("Expected TotalRequestsLast24h>=1, got %d", response.TotalRequestsLast24h)
	}
}

// TestGetProviderDistribution_RequestsMetric tests provider distribution with requests metric (default).
func TestGetProviderDistribution_RequestsMetric(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-req-metric", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20)

	r := chi.NewRouter()
	handler.Register(r)

	// Default metric=requests (no metric param)
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

	if len(response.Items) == 0 {
		t.Fatal("Expected provider distribution items")
	}

	// With requests metric, Count should be > 0 and Tokens should be 0
	for _, item := range response.Items {
		if item.Name == "test-provider-req-metric" {
			if item.Count != 1 {
				t.Errorf("Expected Count=1 for requests metric, got %d", item.Count)
			}
			if item.Tokens != 0 {
				t.Errorf("Expected Tokens=0 for requests metric, got %d", item.Tokens)
			}
			break
		}
	}
}

// TestGetStats_EmptyDB tests stats with no request_logs (zero-result paths).
func TestGetStats_EmptyDB(t *testing.T) {
	handler, _, cleanup := newStatsHandler(t)
	defer cleanup()

	// No data inserted - all queries return zero/empty results

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

	// All stats should be zero/empty
	if response.TotalRequestsLast24h != 0 {
		t.Errorf("Expected TotalRequestsLast24h=0, got %d", response.TotalRequestsLast24h)
	}
	if response.AvgLatencyMs != 0 {
		t.Errorf("Expected AvgLatencyMs=0, got %f", response.AvgLatencyMs)
	}
	if response.ErrorRate != 0 {
		t.Errorf("Expected ErrorRate=0, got %f", response.ErrorRate)
	}
	if response.AvgOverheadMs != 0 {
		t.Errorf("Expected AvgOverheadMs=0, got %f", response.AvgOverheadMs)
	}
	if response.TotalTokensPrompt != 0 {
		t.Errorf("Expected TotalTokensPrompt=0, got %d", response.TotalTokensPrompt)
	}
	if response.AvgTokensPerRequest != 0 {
		t.Errorf("Expected AvgTokensPerRequest=0, got %f", response.AvgTokensPerRequest)
	}
	if response.RateLimitHits != 0 {
		t.Errorf("Expected RateLimitHits=0, got %d", response.RateLimitHits)
	}
	if response.AvgTTFTMs != 0 {
		t.Errorf("Expected AvgTTFTMs=0, got %f", response.AvgTTFTMs)
	}
	if response.RequestsLast1h != 0 {
		t.Errorf("Expected RequestsLast1h=0, got %d", response.RequestsLast1h)
	}
}

// TestGetStats_RequestsLast1h tests the requests_last_1h field with recent data.
func TestGetStats_RequestsLast1h(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-1h", "https://api.example.com/v1")

	// Insert a request log within the last hour (NOW())
	insertTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20)

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

	if response.RequestsLast1h < 1 {
		t.Errorf("Expected RequestsLast1h>=1, got %d", response.RequestsLast1h)
	}
}

// TestCalculateStats_LateQueryErrors tests all computed fields with diverse data
// to exercise the value-setting paths (lines 356-454). With a mix of success,
// error, and rate-limited requests, all stat fields should be non-zero.
func TestCalculateStats_LateQueryErrors(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-late-err", "https://api.example.com/v1")

	// Insert logs with various status codes, TTFT, and overhead to exercise all paths
	vkID := uuid.New()
	_, err := pool.Exec(context.Background(), `
		INSERT INTO virtual_keys (id, name, key_hash, key_preview, created_at)
		VALUES ($1, 'test-vk-late', 'hash', 'sk-...la', NOW())`, vkID)
	if err != nil {
		t.Fatalf("Failed to insert virtual key: %v", err)
	}

	// Success request (streaming — has TTFT)
	insertRichTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20, requestLogOpts{
		VirtualKeyID:     &vkID,
		ResponseHeaderMs: 50.0,
		ProxyOverheadMs:  5.0,
		Streaming:        true,
	})
	// Error request
	insertRichTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 500, 200, 5, 10, requestLogOpts{
		VirtualKeyID:     &vkID,
		ResponseHeaderMs: 0,
		ProxyOverheadMs:  3.0,
	})
	// Rate limited request
	insertRichTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 429, 50, 2, 3, requestLogOpts{
		VirtualKeyID:     &vkID,
		ResponseHeaderMs: 0,
		ProxyOverheadMs:  1.0,
	})

	ctx := context.Background()
	stats, err := handler.calculateStats(ctx, 24*time.Hour, true, "requests")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// Verify all the computed fields have reasonable values
	if stats.AvgLatencyMs <= 0 {
		t.Errorf("Expected AvgLatencyMs>0, got %f", stats.AvgLatencyMs)
	}
	if stats.ErrorRate <= 0 {
		t.Errorf("Expected ErrorRate>0, got %f", stats.ErrorRate)
	}
	if stats.AvgOverheadMs <= 0 {
		t.Errorf("Expected AvgOverheadMs>0, got %f", stats.AvgOverheadMs)
	}
	if stats.TotalTokensPrompt <= 0 {
		t.Errorf("Expected TotalTokensPrompt>0, got %d", stats.TotalTokensPrompt)
	}
	if stats.TotalTokensCompletion <= 0 {
		t.Errorf("Expected TotalTokensCompletion>0, got %d", stats.TotalTokensCompletion)
	}
	if stats.AvgTokensPerRequest <= 0 {
		t.Errorf("Expected AvgTokensPerRequest>0, got %f", stats.AvgTokensPerRequest)
	}
	if stats.RateLimitHits != 1 {
		t.Errorf("Expected RateLimitHits=1, got %d", stats.RateLimitHits)
	}
	if stats.AvgTTFTMs <= 0 {
		t.Errorf("Expected AvgTTFTMs>0, got %f", stats.AvgTTFTMs)
	}
	if stats.RequestsLast1h < 3 {
		t.Errorf("Expected RequestsLast1h>=3, got %d", stats.RequestsLast1h)
	}
}

// TestCalculateStats_24hPeriod_7dQuery exercises the period==24h branch
// that queries the 7d count as a secondary query (lines 176-183).
func TestCalculateStats_24hPeriod_7dQuery(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-24h-7d", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20)

	ctx := context.Background()
	stats, err := handler.calculateStats(ctx, 24*time.Hour, true, "requests")
	if err != nil {
		t.Fatalf("calculateStats failed: %v", err)
	}

	// Both 24h and 7d counts should be populated
	if stats.TotalRequestsLast24h != 1 {
		t.Errorf("Expected TotalRequestsLast24h=1, got %d", stats.TotalRequestsLast24h)
	}
	if stats.TotalRequestsLast7d != 1 {
		t.Errorf("Expected TotalRequestsLast7d=1, got %d", stats.TotalRequestsLast7d)
	}
}

// TestGetProviderDistribution_ShareRounding verifies the share rounding
// logic in GetProviderDistribution (lines 673-682).
func TestGetProviderDistribution_ShareRounding(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	// Insert 3 providers with equal request counts so each share is 33.3%,
	// which rounds to sum=99.9 and exercises the compensation logic
	providers := []struct {
		name  string
		count int
	}{
		{"prov-round-a", 1},
		{"prov-round-b", 1},
		{"prov-round-c", 1}, // 3 equal shares (33.3% each) → sum=99.9, exercises rounding compensation
	}

	for _, p := range providers {
		pid := uuid.New()
		insertTestProvider(t, pool, pid, p.name, "https://api.example.com/v1")
		for i := 0; i < p.count; i++ {
			insertTestRequestLog(t, pool, uuid.New(), pid, "model-x", 200, 100, 1, 1)
		}
	}

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/provider-distribution?period=24h", http.NoBody)
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

	if len(response.Items) != 3 {
		t.Fatalf("Expected 3 items, got %d", len(response.Items))
	}

	// Verify share sums to exactly 100.0
	var sum float64
	for _, item := range response.Items {
		sum += item.Share
	}
	if math.Abs(sum-100.0) > 0.01 {
		t.Errorf("Expected share sum=100.0, got %f", sum)
	}

	// Each share should be ~33.3%, and the first item should have the
	// rounding compensation applied so the total sums to 100.0.
	for _, item := range response.Items {
		if item.Share < 33.0 || item.Share > 34.0 {
			t.Errorf("Expected share ~33.3%%, got %f for %q", item.Share, item.Name)
		}
	}
}

// TestCalculateStats_CancelledContext tests the error-return branches in
// calculateStats by using a pre-cancelled context. With a cancelled context,
// the very first query fails, hitting the error path. This is a complementary
// test to TestCalculateStats_QueryError (which closes the pool).
func TestCalculateStats_CancelledContext(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-cancel-ctx", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20)

	// Use a pre-cancelled context - the first query will fail immediately
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := handler.calculateStats(ctx, 24*time.Hour, true, "requests")
	if err == nil {
		t.Error("Expected error with cancelled context")
	}
}

// TestCalculateStats_CancelledContext_1h tests the else branch error path
// (lines 187-190) with a cancelled context and period=1h.
func TestCalculateStats_CancelledContext_1h(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-cancel-1h", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := handler.calculateStats(ctx, 1*time.Hour, true, "requests")
	if err == nil {
		t.Error("Expected error with cancelled context")
	}
}

// TestCalculateStats_CancelledContext_7d tests the 7d branch error path
// (lines 179-182) with a cancelled context and period=7d.
func TestCalculateStats_CancelledContext_7d(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-cancel-7d", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := handler.calculateStats(ctx, 7*24*time.Hour, true, "requests")
	if err == nil {
		t.Error("Expected error with cancelled context")
	}
}

// TestGetTimeSeries_CancelledContext tests the query error path (lines 525-528)
// and scan error path (lines 535-536) in GetTimeSeries.
func TestGetTimeSeries_CancelledContext(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-ts-cancel", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20)

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/timeseries?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	// Use a context that is already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Should get 500 because the query fails with cancelled context
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 for cancelled context, got %d", rec.Code)
	}
}

// TestGetProviderDistribution_CancelledContext tests the query error path
// (lines 632-635) and scan error path (lines 646-647) in GetProviderDistribution.
func TestGetProviderDistribution_CancelledContext(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-pd-cancel", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20)

	r := chi.NewRouter()
	handler.Register(r)

	req := httptest.NewRequest(http.MethodGet, "/stats/provider-distribution?period=24h", http.NoBody)
	req.Header.Set("Authorization", "Bearer test-admin-token")

	// Use a context that is already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req = req.WithContext(ctx)

	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	// Should get 500 because the query fails with cancelled context
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("Expected 500 for cancelled context, got %d", rec.Code)
	}
}

func TestGetTimeSeries_5minBucket(t *testing.T) {
	handler, pool, cleanup := newStatsHandler(t)
	defer cleanup()

	providerID := uuid.New()
	insertTestProvider(t, pool, providerID, "test-provider-5min", "https://api.example.com/v1")
	insertTestRequestLog(t, pool, uuid.New(), providerID, "test-model", 200, 100, 10, 20)

	r := chi.NewRouter()
	handler.Register(r)

	// 1h period triggers 5min bucket format
	req := httptest.NewRequest(http.MethodGet, "/stats/timeseries?period=1h", http.NoBody)
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

	// Verify bucket format matches YYYY-MM-DDTHH:MM:SSZ (5min truncated)
	for _, p := range response.Points {
		if p.Count > 0 {
			// Bucket should end with 'Z' and have format YYYY-MM-DDTHH:MM:SSZ
			if len(p.Bucket) != 20 || p.Bucket[19] != 'Z' {
				t.Errorf("Expected bucket format YYYY-MM-DDTHH:MM:SSZ, got %q", p.Bucket)
			}
		}
	}
}
