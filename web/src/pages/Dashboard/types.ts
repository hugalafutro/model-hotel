import type { MetricType } from "../../api/types";

export type Range = "1h" | "24h" | "1w";

export type TimeSeriesDataPoint = {
	hour: string;
	rawDate: string;
	total: number;
	errors: number;
	tokens: number;
	tokens_cache_hit: number;
	tokens_cache_miss: number;
	latency: number;
	overhead_ms: number;
	provider_latency_ms: number;
	rate_limit_hits: number;
	avg_ttft_ms: number;
};

export type GaugeDataKey =
	| "total"
	| "tokens"
	| "tokens_cache_hit"
	| "errors"
	| "latency"
	| "overhead_ms"
	| "provider_latency_ms"
	| "rate_limit_hits"
	| "avg_ttft_ms";

export type UsageEntry = {
	label: string;
	value: number;
	suffix?: string;
	deleted?: boolean;
	/** When true, entry represents a failover group (hotel/ prefix) and should not be clickable */
	failoverGroup?: boolean;
};

export type { MetricType };
