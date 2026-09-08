import type { TimeSeriesStats } from "../../api/types";
import { bucketLabel } from "./bucketLabel";
import type { Range, TimeSeriesDataPoint } from "./types";

/**
 * Projects a timeseries response onto the shape the charts render: one point
 * per bucket, labelled at the resolution the range implies. The optional
 * cache/TTFT fields fall back to 0, so an absent value plots as a zero rather
 * than a gap. Latency is rounded to whole milliseconds for the dashboard
 * tiles; the gauge modal plots it raw, sub-millisecond part included.
 */
export function toChartPoints(
	ts: TimeSeriesStats | undefined,
	range: Range,
	roundLatency = true,
): TimeSeriesDataPoint[] {
	if (!ts?.points) return [];
	return ts.points.map((p) => ({
		hour: bucketLabel(new Date(p.bucket), range),
		rawDate: p.bucket,
		total: p.count,
		errors: p.errors,
		tokens: p.tokens,
		tokens_cache_hit: p.tokens_cache_hit ?? 0,
		tokens_cache_miss: p.tokens_cache_miss ?? 0,
		latency: roundLatency ? Math.round(p.latency_ms) : p.latency_ms,
		overhead_ms: p.overhead_ms,
		provider_latency_ms: p.provider_latency_ms,
		rate_limit_hits: p.rate_limit_hits,
		avg_ttft_ms: p.avg_ttft_ms ?? 0,
	}));
}
