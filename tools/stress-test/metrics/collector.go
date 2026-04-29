package metrics

import (
	"sort"
	"sync"
	"time"
)

// Result captures the outcome of a single proxied request.
type Result struct {
	Duration   time.Duration // total wall time
	TTFT       time.Duration // time to first token (streaming only)
	StatusCode int
	Error      string
	KeyIndex   int // which virtual key was used (0-based)
}

// Collector is a thread-safe accumulator for request results.
type Collector struct {
	mu      sync.Mutex
	results []Result
}

// NewCollector creates a collector pre-allocated for capacity results.
func NewCollector(capacity int) *Collector {
	return &Collector{
		results: make([]Result, 0, capacity),
	}
}

// Record adds a single result. Safe to call from concurrent goroutines.
func (c *Collector) Record(r Result) {
	c.mu.Lock()
	c.results = append(c.results, r)
	c.mu.Unlock()
}

// Len returns the number of recorded results.
func (c *Collector) Len() int {
	c.mu.Lock()
	n := len(c.results)
	c.mu.Unlock()
	return n
}

// Results returns a defensive copy of all results.
func (c *Collector) Results() []Result {
	c.mu.Lock()
	cp := make([]Result, len(c.results))
	copy(cp, c.results)
	c.mu.Unlock()
	return cp
}

// Summary computes aggregate statistics over collected results.
type Summary struct {
	TotalRequests int
	SuccessCount  int
	ErrorCount    int
	StatusCodes   map[int]int
	LatencyP50    time.Duration
	LatencyP95    time.Duration
	LatencyP99    time.Duration
	LatencyMax    time.Duration
	TTFTP50       time.Duration
	TTFTP95       time.Duration
	TTFTP99       time.Duration
	ThroughputRPS float64
	TotalDuration time.Duration // wall time of the entire scenario
	UniqueErrors  []string      // up to 10 distinct error strings
}

// Summarize computes aggregate statistics. The wallStart parameter is
// the absolute time when the scenario started so that throughput can
// be calculated.
func (c *Collector) Summarize(wallStart time.Time) Summary {
	c.mu.Lock()
	results := make([]Result, len(c.results))
	copy(results, c.results)
	c.mu.Unlock()

	s := Summary{
		TotalRequests: len(results),
		StatusCodes:   make(map[int]int),
	}

	if len(results) == 0 {
		return s
	}

	// Separate latencies and errors
	latencies := make([]time.Duration, 0, len(results))
	ttfts := make([]time.Duration, 0, len(results))
	errorSet := make(map[string]bool)

	for _, r := range results {
		s.StatusCodes[r.StatusCode]++
		if r.StatusCode >= 200 && r.StatusCode < 300 && r.Error == "" {
			s.SuccessCount++
			latencies = append(latencies, r.Duration)
			if r.TTFT > 0 {
				ttfts = append(ttfts, r.TTFT)
			}
		} else {
			s.ErrorCount++
			if r.Error != "" && len(errorSet) < 10 {
				errorSet[r.Error] = true
			}
		}
	}

	for err := range errorSet {
		s.UniqueErrors = append(s.UniqueErrors, err)
	}
	sort.Strings(s.UniqueErrors)

	// Percentiles
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	s.LatencyP50 = percentile(latencies, 50)
	s.LatencyP95 = percentile(latencies, 95)
	s.LatencyP99 = percentile(latencies, 99)
	if len(latencies) > 0 {
		s.LatencyMax = latencies[len(latencies)-1]
	}

	sort.Slice(ttfts, func(i, j int) bool { return ttfts[i] < ttfts[j] })
	s.TTFTP50 = percentile(ttfts, 50)
	s.TTFTP95 = percentile(ttfts, 95)
	s.TTFTP99 = percentile(ttfts, 99)

	// Throughput
	s.TotalDuration = time.Since(wallStart)
	if s.TotalDuration.Seconds() > 0 {
		s.ThroughputRPS = float64(s.SuccessCount) / s.TotalDuration.Seconds()
	}

	return s
}

// percentile returns the value at the given percentile (0–100) from a
// sorted slice. Returns 0 for empty slices.
func percentile(sorted []time.Duration, pct int) time.Duration {
	if len(sorted) == 0 {
		return 0
	}
	idx := float64(pct) / 100.0 * float64(len(sorted)-1)
	lo := int(idx)
	hi := lo + 1
	if hi >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	// Linear interpolation
	frac := idx - float64(lo)
	return time.Duration(float64(sorted[lo]) + frac*float64(sorted[hi]-sorted[lo]))
}
