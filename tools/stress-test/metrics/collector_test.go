package metrics

import (
	"strings"
	"testing"
	"time"
)

func TestNewCollector(t *testing.T) {
	c := NewCollector(100)
	if c.Len() != 0 {
		t.Errorf("new collector Len() = %d, want 0", c.Len())
	}
}

func TestCollector_RecordAndLen(t *testing.T) {
	c := NewCollector(10)

	c.Record(Result{Duration: 10 * time.Millisecond, StatusCode: 200})
	c.Record(Result{Duration: 20 * time.Millisecond, StatusCode: 200})
	c.Record(Result{Duration: 30 * time.Millisecond, StatusCode: 500})

	if c.Len() != 3 {
		t.Errorf("Len() = %d, want 3", c.Len())
	}
}

func TestCollector_Results(t *testing.T) {
	c := NewCollector(10)
	c.Record(Result{Duration: 10 * time.Millisecond, StatusCode: 200})
	c.Record(Result{Duration: 20 * time.Millisecond, StatusCode: 200})

	results := c.Results()
	if len(results) != 2 {
		t.Fatalf("Results() len = %d, want 2", len(results))
	}

	// Modifying the returned slice should not affect the collector
	results[0] = Result{Duration: 999 * time.Second, StatusCode: 999}
	if c.Results()[0].StatusCode == 999 {
		t.Error("Results() should return a defensive copy")
	}
}

func TestCollector_Summarize_Empty(t *testing.T) {
	c := NewCollector(10)
	s := c.Summarize(time.Now())

	if s.TotalRequests != 0 {
		t.Errorf("TotalRequests = %d, want 0", s.TotalRequests)
	}
	if s.SuccessCount != 0 {
		t.Errorf("SuccessCount = %d, want 0", s.SuccessCount)
	}
	if s.ErrorCount != 0 {
		t.Errorf("ErrorCount = %d, want 0", s.ErrorCount)
	}
}

func TestCollector_Summarize_MixedResults(t *testing.T) {
	c := NewCollector(100)

	// 5 successful requests
	for i := 0; i < 5; i++ {
		c.Record(Result{
			Duration:   time.Duration(10+i*5) * time.Millisecond,
			StatusCode: 200,
			TTFT:       time.Duration(2+i) * time.Millisecond,
			KeyIndex:   i,
		})
	}
	// 2 failed requests
	c.Record(Result{Duration: 5 * time.Millisecond, StatusCode: 500, Error: "internal error"})
	c.Record(Result{Duration: 3 * time.Millisecond, StatusCode: 429, Error: "rate limited"})

	s := c.Summarize(time.Now().Add(-1 * time.Second))

	if s.TotalRequests != 7 {
		t.Errorf("TotalRequests = %d, want 7", s.TotalRequests)
	}
	if s.SuccessCount != 5 {
		t.Errorf("SuccessCount = %d, want 5", s.SuccessCount)
	}
	if s.ErrorCount != 2 {
		t.Errorf("ErrorCount = %d, want 2", s.ErrorCount)
	}

	// Check status codes
	if s.StatusCodes[200] != 5 {
		t.Errorf("StatusCodes[200] = %d, want 5", s.StatusCodes[200])
	}
	if s.StatusCodes[500] != 1 {
		t.Errorf("StatusCodes[500] = %d, want 1", s.StatusCodes[500])
	}
	if s.StatusCodes[429] != 1 {
		t.Errorf("StatusCodes[429] = %d, want 1", s.StatusCodes[429])
	}

	// Should have unique errors
	if len(s.UniqueErrors) != 2 {
		t.Errorf("UniqueErrors count = %d, want 2", len(s.UniqueErrors))
	}
}

func TestCollector_Summarize_LatencyPercentiles(t *testing.T) {
	c := NewCollector(100)

	// Add 100 successful requests with linearly increasing latency
	for i := 0; i < 100; i++ {
		c.Record(Result{
			Duration:   time.Duration(i+1) * time.Millisecond,
			StatusCode: 200,
		})
	}

	s := c.Summarize(time.Now().Add(-1 * time.Second))

	// p50 should be around 50ms, p99 around 99ms
	if s.LatencyP50 < 40*time.Millisecond || s.LatencyP50 > 60*time.Millisecond {
		t.Errorf("LatencyP50 = %v, expected around 50ms", s.LatencyP50)
	}
	if s.LatencyP99 < 90*time.Millisecond {
		t.Errorf("LatencyP99 = %v, expected >= 90ms", s.LatencyP99)
	}
	if s.LatencyMax != 100*time.Millisecond {
		t.Errorf("LatencyMax = %v, want 100ms", s.LatencyMax)
	}
}

func TestCollector_Summarize_TTFTPercentiles(t *testing.T) {
	c := NewCollector(50)

	for i := 0; i < 50; i++ {
		c.Record(Result{
			Duration:   time.Duration(50-i) * time.Millisecond,
			StatusCode: 200,
			TTFT:       time.Duration(i+1) * time.Millisecond,
		})
	}

	s := c.Summarize(time.Now().Add(-1 * time.Second))

	if s.TTFTP50 < 20*time.Millisecond || s.TTFTP50 > 35*time.Millisecond {
		t.Errorf("TTFTP50 = %v, expected around 25ms", s.TTFTP50)
	}
}

func TestCollector_Summarize_Throughput(t *testing.T) {
	c := NewCollector(10)

	for i := 0; i < 5; i++ {
		c.Record(Result{Duration: 10 * time.Millisecond, StatusCode: 200})
	}

	// Simulate 1 second wall time
	s := c.Summarize(time.Now().Add(-1 * time.Second))

	if s.ThroughputRPS <= 0 {
		t.Errorf("ThroughputRPS = %f, expected > 0", s.ThroughputRPS)
	}
}

func TestPercentile(t *testing.T) {
	tests := []struct {
		name     string
		sorted   []time.Duration
		pct      int
		wantZero bool
	}{
		{"empty slice", []time.Duration{}, 50, true},
		{"single element", []time.Duration{10 * time.Millisecond}, 50, false},
		{"two elements p50", []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}, 50, false},
		{"two elements p99", []time.Duration{10 * time.Millisecond, 20 * time.Millisecond}, 99, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := percentile(tt.sorted, tt.pct)
			if tt.wantZero && got != 0 {
				t.Errorf("percentile() = %v, want 0", got)
			}
			if !tt.wantZero && got == 0 {
				t.Errorf("percentile() = 0, want non-zero")
			}
		})
	}
}

func TestCollector_Summarize_MaxUniqueErrors(t *testing.T) {
	c := NewCollector(20)

	// Add 15 different errors (more than the 10 limit)
	for i := 0; i < 15; i++ {
		c.Record(Result{
			Duration:   1 * time.Millisecond,
			StatusCode: 500,
			Error:      "error-" + string(rune('a'+i)),
		})
	}

	s := c.Summarize(time.Now().Add(-1 * time.Second))

	// Should cap at 10 unique errors
	if len(s.UniqueErrors) > 10 {
		t.Errorf("UniqueErrors count = %d, want <= 10", len(s.UniqueErrors))
	}
}

func TestCollector_Summarize_SortedUniqueErrors(t *testing.T) {
	c := NewCollector(10)

	c.Record(Result{StatusCode: 500, Error: "z-error"})
	c.Record(Result{StatusCode: 500, Error: "a-error"})
	c.Record(Result{StatusCode: 500, Error: "m-error"})

	s := c.Summarize(time.Now().Add(-1 * time.Second))

	if len(s.UniqueErrors) != 3 {
		t.Fatalf("UniqueErrors count = %d, want 3", len(s.UniqueErrors))
	}
	for i := 1; i < len(s.UniqueErrors); i++ {
		if s.UniqueErrors[i] < s.UniqueErrors[i-1] {
			t.Errorf("UniqueErrors not sorted: %v", s.UniqueErrors)
		}
	}
}

// --- report.go tests ---

func TestDurStr(t *testing.T) {
	tests := []struct {
		input time.Duration
		want  string
	}{
		{0, "—"},
		{500 * time.Microsecond, "500µs"},
		{5 * time.Millisecond, "5.0ms"},
		{1500 * time.Millisecond, "1.50s"},
	}

	for _, tt := range tests {
		got := durStr(tt.input)
		if got != tt.want {
			t.Errorf("durStr(%v) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestFormatStatusCodes(t *testing.T) {
	codes := map[int]int{200: 50, 429: 10, 500: 5}
	result := formatStatusCodes(codes)

	// Should be sorted by code: 200, 429, 500
	if !strings.Contains(result, "200: 50") {
		t.Errorf("missing 200: 50 in %q", result)
	}
	if !strings.Contains(result, "429: 10") {
		t.Errorf("missing 429: 10 in %q", result)
	}
	if !strings.Contains(result, "500: 5") {
		t.Errorf("missing 500: 5 in %q", result)
	}

	// Verify order: 200 should come before 429, 429 before 500
	idx200 := strings.Index(result, "200:")
	idx429 := strings.Index(result, "429:")
	idx500 := strings.Index(result, "500:")
	if idx200 > idx429 || idx429 > idx500 {
		t.Errorf("status codes not sorted: %q", result)
	}
}

func TestReport_WriteMarkdown(t *testing.T) {
	report := &Report{
		ProxyURL: "http://localhost:8081",
		MockURL:  "http://localhost:9090/v1",
		Scenarios: []ScenarioReport{
			{
				Label:       "10-conc, RL=false, 1-key, stream=true",
				Concurrency: 10,
				RateLimitOn: false,
				NumKeys:     1,
				Streaming:   true,
				Summary: Summary{
					TotalRequests: 100,
					SuccessCount:  98,
					ErrorCount:    2,
					StatusCodes:   map[int]int{200: 98, 500: 2},
					LatencyP50:    50 * time.Millisecond,
					LatencyP95:    100 * time.Millisecond,
					LatencyP99:    200 * time.Millisecond,
					LatencyMax:    300 * time.Millisecond,
					TTFTP50:       10 * time.Millisecond,
					TTFTP95:       20 * time.Millisecond,
					TTFTP99:       30 * time.Millisecond,
					ThroughputRPS: 89.3,
					TotalDuration: 1 * time.Second,
				},
			},
		},
	}

	var buf strings.Builder
	if err := report.Write(&buf, FormatMarkdown); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Model Hotel Synthetic Stress Test Report") {
		t.Error("markdown output missing title")
	}
	if !strings.Contains(output, "10-conc") {
		t.Error("markdown output missing scenario label")
	}
}

func TestReport_WriteText(t *testing.T) {
	report := &Report{
		ProxyURL: "http://localhost:8081",
		MockURL:  "http://localhost:9090/v1",
		Scenarios: []ScenarioReport{
			{
				Label: "test-scenario",
				Summary: Summary{
					TotalRequests: 10,
					SuccessCount:  10,
					StatusCodes:   map[int]int{200: 10},
				},
			},
		},
	}

	var buf strings.Builder
	if err := report.Write(&buf, FormatText); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Model Hotel Synthetic Stress Test Report") {
		t.Error("text output missing title")
	}
}

func TestReport_WriteJSON(t *testing.T) {
	report := &Report{
		ProxyURL: "http://localhost:8081",
		MockURL:  "http://localhost:9090/v1",
		Scenarios: []ScenarioReport{
			{
				Label: "json-scenario",
				Summary: Summary{
					TotalRequests: 5,
					SuccessCount:  5,
					StatusCodes:   map[int]int{200: 5},
				},
			},
		},
	}

	var buf strings.Builder
	if err := report.Write(&buf, FormatJSON); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, `"ProxyURL"`) {
		t.Error("JSON output missing ProxyURL field")
	}
	if !strings.Contains(output, `"json-scenario"`) {
		t.Error("JSON output missing scenario label")
	}
}

func TestReport_Write_UnknownFormat(t *testing.T) {
	report := &Report{
		ProxyURL:  "http://localhost:8081",
		MockURL:   "http://localhost:9090/v1",
		Scenarios: []ScenarioReport{},
	}

	var buf strings.Builder
	// Unknown format should fall back to markdown
	if err := report.Write(&buf, Format("unknown")); err != nil {
		t.Fatalf("Write() error: %v", err)
	}
}

func TestReport_Write_NonStreamingScenario(t *testing.T) {
	report := &Report{
		ProxyURL: "http://localhost:8081",
		MockURL:  "http://localhost:9090/v1",
		Scenarios: []ScenarioReport{
			{
				Label:     "non-stream-test",
				Streaming: false,
				Summary: Summary{
					TotalRequests: 10,
					SuccessCount:  10,
					StatusCodes:   map[int]int{200: 10},
					LatencyP50:    50 * time.Millisecond,
				},
			},
		},
	}

	var buf strings.Builder
	if err := report.Write(&buf, FormatText); err != nil {
		t.Fatalf("Write() error: %v", err)
	}

	output := buf.String()
	// Non-streaming should not include TTFT line
	if strings.Contains(output, "TTFT") {
		t.Error("non-streaming report should not include TTFT metrics")
	}
}
