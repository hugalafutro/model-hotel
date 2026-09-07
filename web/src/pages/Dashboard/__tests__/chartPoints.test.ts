import { describe, expect, it } from "vitest";
import type { TimeSeriesStats } from "../../../api/types";
import { toChartPoints } from "../chartPoints";

const point = {
	bucket: "2025-01-15T10:30:00Z",
	count: 5,
	errors: 1,
	tokens: 1000,
	tokens_cache_hit: 40,
	tokens_cache_miss: 960,
	latency_ms: 250.5,
	overhead_ms: 10,
	provider_latency_ms: 240.5,
	rate_limit_hits: 2,
	avg_ttft_ms: 50,
};

describe("toChartPoints", () => {
	it("returns an empty array without data", () => {
		expect(toChartPoints(undefined, "24h")).toEqual([]);
		expect(toChartPoints({ points: [] } as TimeSeriesStats, "24h")).toEqual([]);
	});

	it("maps a bucket onto the chart point shape", () => {
		const [mapped] = toChartPoints(
			{ points: [point] } as TimeSeriesStats,
			"1w",
		);
		expect(mapped).toMatchObject({
			rawDate: "2025-01-15T10:30:00Z",
			total: 5,
			errors: 1,
			tokens: 1000,
			tokens_cache_hit: 40,
			tokens_cache_miss: 960,
			overhead_ms: 10,
			provider_latency_ms: 240.5,
			rate_limit_hits: 2,
			avg_ttft_ms: 50,
		});
		// A week of buckets is labelled by date.
		expect(mapped.hour).toMatch(/15/);
	});

	it("rounds latency and zero-fills the optional fields", () => {
		const sparse = {
			...point,
			latency_ms: 250.5,
			tokens_cache_hit: undefined,
			tokens_cache_miss: undefined,
			avg_ttft_ms: undefined,
		};
		const [mapped] = toChartPoints(
			{ points: [sparse] } as unknown as TimeSeriesStats,
			"24h",
		);
		expect(mapped.latency).toBe(251);
		expect(mapped.tokens_cache_hit).toBe(0);
		expect(mapped.tokens_cache_miss).toBe(0);
		expect(mapped.avg_ttft_ms).toBe(0);
	});

	it("keeps latency raw when rounding is off", () => {
		const [mapped] = toChartPoints(
			{ points: [point] } as TimeSeriesStats,
			"24h",
			false,
		);
		expect(mapped.latency).toBe(250.5);
	});
});
