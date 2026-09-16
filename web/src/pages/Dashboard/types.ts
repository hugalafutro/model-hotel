import { METRIC_TYPES, type MetricType } from "../../api/types";

/** The time windows the dashboard offers, in the order its toggle shows them. */
export const DASHBOARD_RANGES = ["1h", "24h", "1w"] as const;

export type Range = (typeof DASHBOARD_RANGES)[number];

export type TimeSeriesDataPoint = {
	hour: string;
	rawDate: string;
	total: number;
	errors: number;
	tokens: number;
	tokens_cache_hit: number;
	tokens_cache_miss: number;
	cost_usd?: number;
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
	| "cost_usd"
	| "errors"
	| "latency"
	| "overhead_ms"
	| "provider_latency_ms"
	| "rate_limit_hits"
	| "avg_ttft_ms";

export type UsageEntry = {
	label: string;
	value: number;
	deleted?: boolean;
	/** When true, entry represents a failover group (hotel/ prefix) and should not be clickable */
	failoverGroup?: boolean;
	/** When true, the label is an internal reserved virtual-key name (chat/arena/internal/...)
	 * that Model Hotel meters under its own routes, not a key the user created. */
	reserved?: boolean;
};

export type { MetricType };
export { METRIC_TYPES };
