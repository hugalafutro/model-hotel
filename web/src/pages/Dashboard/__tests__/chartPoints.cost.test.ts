import { describe, expect, it } from "vitest";
import { toChartPoints } from "../chartPoints";

const point = {
	bucket: "2025-01-15T10:00:00Z",
	count: 1,
	tokens: 10,
	tokens_cache_hit: 0,
	tokens_cache_miss: 0,
	errors: 0,
	latency_ms: 1,
	overhead_ms: 0,
	provider_latency_ms: 0,
	rate_limit_hits: 0,
	avg_ttft_ms: 0,
};

describe("toChartPoints cost", () => {
	it("carries the bucket's spend and zero-fills a response without it", () => {
		expect(
			toChartPoints({ points: [{ ...point, cost_usd: 0.25 }] }, "24h")[0]
				.cost_usd,
		).toBe(0.25);
		expect(toChartPoints({ points: [point] }, "24h")[0].cost_usd).toBe(0);
	});
});
