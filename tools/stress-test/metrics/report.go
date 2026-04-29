package metrics

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Format controls the output format of the report.
type Format string

const (
	FormatText     Format = "text"
	FormatMarkdown Format = "markdown"
	FormatJSON     Format = "json"
)

// ScenarioReport is the complete report for one test scenario.
type ScenarioReport struct {
	Label       string
	Concurrency int
	RateLimitOn bool
	NumKeys     int
	Streaming   bool
	Summary     Summary
}

// Report is the top-level report containing all scenarios.
type Report struct {
	ProxyURL  string
	MockURL   string
	Scenarios []ScenarioReport
}

// Write renders the full report to w in the requested format.
func (r *Report) Write(w io.Writer, format Format) error {
	switch format {
	case FormatJSON:
		return r.writeJSON(w)
	case FormatText:
		return r.writeText(w)
	case FormatMarkdown:
		return r.writeMarkdown(w)
	default:
		return r.writeMarkdown(w)
	}
}

func (r *Report) writeJSON(w io.Writer) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

func (r *Report) writeText(w io.Writer) error {
	fmt.Fprintf(w, "\n")
	fmt.Fprintf(w, "╔══════════════════════════════════════════════════════════════╗\n")
	fmt.Fprintf(w, "║  Model Hotel Synthetic Stress Test Report                   ║\n")
	fmt.Fprintf(w, "╚══════════════════════════════════════════════════════════════╝\n")
	fmt.Fprintf(w, "Proxy: %s    Mock: %s\n\n", r.ProxyURL, r.MockURL)

	for i, sc := range r.Scenarios {
		s := sc.Summary
		fmt.Fprintf(w, "── Scenario %d: %s ──\n", i+1, sc.Label)
		fmt.Fprintf(w, "  Requests:    %d total → %d success, %d errors\n", s.TotalRequests, s.SuccessCount, s.ErrorCount)
		fmt.Fprintf(w, "  Throughput:  %.1f req/s\n", s.ThroughputRPS)
		fmt.Fprintf(w, "  Latency:     p50=%s  p95=%s  p99=%s  max=%s\n",
			s.LatencyP50.Round(time.Microsecond), s.LatencyP95.Round(time.Microsecond),
			s.LatencyP99.Round(time.Microsecond), s.LatencyMax.Round(time.Microsecond))
		if sc.Streaming {
			fmt.Fprintf(w, "  TTFT:        p50=%s  p95=%s  p99=%s\n",
				s.TTFTP50.Round(time.Microsecond), s.TTFTP95.Round(time.Microsecond),
				s.TTFTP99.Round(time.Microsecond))
		}
		fmt.Fprintf(w, "  Wall time:   %s\n", s.TotalDuration.Round(time.Millisecond))
		fmt.Fprintf(w, "  Status codes: %s\n", formatStatusCodes(s.StatusCodes))
		if len(s.UniqueErrors) > 0 {
			fmt.Fprintf(w, "  Errors:      %s\n", strings.Join(s.UniqueErrors, "; "))
		}
		fmt.Fprintf(w, "\n")
	}

	return nil
}

func (r *Report) writeMarkdown(w io.Writer) error {
	fmt.Fprintf(w, "\n# Model Hotel Synthetic Stress Test Report\n\n")
	fmt.Fprintf(w, "- **Proxy:** `%s`\n", r.ProxyURL)
	fmt.Fprintf(w, "- **Mock upstream:** `%s`\n\n", r.MockURL)

	fmt.Fprintf(w, "| # | Scenario | Requests | Success | Errors | Throughput | p50 | p95 | p99 | TTFT p50 | TTFT p95 | Status codes |\n")
	fmt.Fprintf(w, "|---|----------|----------|---------|--------|------------|-----|-----|-----|----------|----------|-------------|\n")

	for i, sc := range r.Scenarios {
		s := sc.Summary
		ttftP50 := "—"
		ttftP95 := "—"
		if sc.Streaming && s.TTFTP50 > 0 {
			ttftP50 = durStr(s.TTFTP50)
			ttftP95 = durStr(s.TTFTP95)
		}
		fmt.Fprintf(w, "| %d | %s | %d | %d | %d | %.1f/s | %s | %s | %s | %s | %s | %s |\n",
			i+1, sc.Label, s.TotalRequests, s.SuccessCount, s.ErrorCount,
			s.ThroughputRPS, durStr(s.LatencyP50), durStr(s.LatencyP95), durStr(s.LatencyP99),
			ttftP50, ttftP95, formatStatusCodes(s.StatusCodes))
	}

	fmt.Fprintf(w, "\n")

	// Detailed per-scenario blocks
	for i, sc := range r.Scenarios {
		s := sc.Summary
		fmt.Fprintf(w, "## Scenario %d: %s\n\n", i+1, sc.Label)
		fmt.Fprintf(w, "- Concurrency: **%d**\n", sc.Concurrency)
		fmt.Fprintf(w, "- Rate limiting: **%v**\n", sc.RateLimitOn)
		fmt.Fprintf(w, "- Virtual keys: **%d**\n", sc.NumKeys)
		fmt.Fprintf(w, "- Streaming: **%v**\n\n", sc.Streaming)
		fmt.Fprintf(w, "| Metric | Value |\n|--------|-------|\n")
		fmt.Fprintf(w, "| Total requests | %d |\n", s.TotalRequests)
		fmt.Fprintf(w, "| Success | %d |\n", s.SuccessCount)
		fmt.Fprintf(w, "| Errors | %d |\n", s.ErrorCount)
		fmt.Fprintf(w, "| Throughput | %.1f req/s |\n", s.ThroughputRPS)
		fmt.Fprintf(w, "| Wall time | %s |\n", durStr(s.TotalDuration))
		fmt.Fprintf(w, "| Latency p50 | %s |\n", durStr(s.LatencyP50))
		fmt.Fprintf(w, "| Latency p95 | %s |\n", durStr(s.LatencyP95))
		fmt.Fprintf(w, "| Latency p99 | %s |\n", durStr(s.LatencyP99))
		fmt.Fprintf(w, "| Latency max | %s |\n", durStr(s.LatencyMax))
		if sc.Streaming {
			fmt.Fprintf(w, "| TTFT p50 | %s |\n", durStr(s.TTFTP50))
			fmt.Fprintf(w, "| TTFT p95 | %s |\n", durStr(s.TTFTP95))
			fmt.Fprintf(w, "| TTFT p99 | %s |\n", durStr(s.TTFTP99))
		}
		fmt.Fprintf(w, "| Status codes | %s |\n", formatStatusCodes(s.StatusCodes))
		if len(s.UniqueErrors) > 0 {
			fmt.Fprintf(w, "\n**Errors:**\n")
			for _, e := range s.UniqueErrors {
				fmt.Fprintf(w, "- `%s`\n", e)
			}
		}
		fmt.Fprintf(w, "\n")
	}

	return nil
}

// durStr formats a duration in a human-friendly way.
func durStr(d time.Duration) string {
	if d == 0 {
		return "—"
	}
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000.0)
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

func formatStatusCodes(codes map[int]int) string {
	// Sort for deterministic output
	type kv struct {
		code  int
		count int
	}
	var entries []kv
	for c, n := range codes {
		entries = append(entries, kv{c, n})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].code < entries[j].code })

	parts := make([]string, len(entries))
	for i, e := range entries {
		parts[i] = fmt.Sprintf("%d: %d", e.code, e.count)
	}
	return strings.Join(parts, ", ")
}
