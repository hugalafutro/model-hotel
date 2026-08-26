export interface ProviderLatencyEntry {
	provider_name: string;
	total_ms: number;
	overhead_ms: number;
	provider_ms: number;
	request_count: number;
}
export interface Stats {
	total_requests_last_24h: number;
	total_requests_last_7d: number;
	by_model: Record<string, number>;
	by_provider: Record<string, number>;
	by_virtual_key: Record<string, number>;
	avg_latency_ms: number;
	error_rate: number;
	avg_overhead_ms: number;
	rate_limit_hits?: number;
	avg_ttft_ms?: number;
	requests_last_1h?: number;
	total_tokens_prompt: number;
	total_tokens_completion: number;
	total_tokens_cache_hit: number;
	avg_tokens_per_request: number;
	by_provider_latency?: ProviderLatencyEntry[];
}
export type MetricType = "requests" | "tokens";
export type Range = "24h" | "7d";
export interface TimeSeriesPoint {
	bucket: string;
	count: number;
	tokens: number;
	tokens_cache_hit: number;
	tokens_cache_miss: number;
	errors: number;
	latency_ms: number;
	overhead_ms: number;
	provider_latency_ms: number;
	rate_limit_hits: number;
	avg_ttft_ms: number;
}
export interface TimeSeriesStats {
	points: TimeSeriesPoint[];
}
export interface ProviderDistributionItem {
	name: string;
	count: number;
	tokens: number;
	share: number;
}
export interface ProviderDistributionStats {
	items: ProviderDistributionItem[];
}
export interface SystemStats {
	app: {
		heap_alloc_mb: number;
		sys_memory_mb: number;
		goroutines: number;
		gc_cycles: number;
		memory_current_bytes: number;
		memory_limit_bytes: number;
		in_container: boolean;
		uptime_seconds: number;
		cpu_percent: number;
		requests_today: number;
		net_rx_bytes_sec: number;
		net_tx_bytes_sec: number;
		disk_read_bytes_sec: number;
		disk_write_bytes_sec: number;
		procs: number;
	};
	db: {
		size_mb: number;
		connections: number;
		cache_hit_ratio: number;
		// Block accesses behind cache_hit_ratio's sample window. Absent/zero means
		// the ratio is not backed by fresh activity (first sample after a restart,
		// Postgres counter reset, or an idle window).
		cache_window_blocks?: number;
		tx_per_sec: number;
		dead_tuples: number;
		lock_waits: number;
	};
	docker: {
		available: boolean;
		cpu_percent: number;
		memory_usage_bytes: number;
		memory_limit_bytes: number;
		net_rx_bytes_sec: number;
		net_tx_bytes_sec: number;
		disk_read_bytes_sec: number;
		disk_write_bytes_sec: number;
		procs: number;
		container_count: number;
	};
	// HA fleet membership. Absent for a standalone instance Front Desk has never
	// contacted, in which case the dashboard shows no HA line.
	fleet?: FleetStatus;
}
// FleetStatus mirrors the backend api.FleetStatus: this member's own view of its
// HA fleet membership, surfaced on the system payload so the dashboard can show
// an HA line that self-clears when Front Desk stops announcing.
export interface FleetStatus {
	state: "primary" | "member" | "warning" | "member_sync_blocked";
	is_primary: boolean;
	primary_name?: string;
	frontdesk_id?: string;
	managed_seen_at?: string;
	config_synced_at?: string;
}
