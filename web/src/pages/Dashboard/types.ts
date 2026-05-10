import type { MetricType } from "../../api/types";

export type Range = "1h" | "24h" | "7d";

export type TimeSeriesDataPoint = {
	hour: string;
	total: number;
	errors: number;
	tokens: number;
	latency: number;
	overhead_ms: number;
	provider_latency_ms: number;
	rate_limit_hits: number;
	avg_ttft_ms: number;
};

export type GaugeDataKey =
	| "total"
	| "tokens"
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
};

export type ProviderDistItem = {
	name: string;
	count: number;
	tokens: number;
	share: number;
};

export type { MetricType };
