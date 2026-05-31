import type {
	BackupEntry,
	FailoverGroup,
	Model,
	Provider,
	Stats,
	SystemStats,
	VirtualKey,
} from "../../api/types";

export const mockProvider: Provider = {
	id: "provider-001",
	name: "Test Provider",
	base_url: "https://api.test-provider.com/v1",
	masked_key: "sk_test_••••••••••••••••••••••••",
	enabled: true,
	autodiscovery_enabled: true,
	last_discovered_at: "2026-05-10T12:00:00Z",
	last_used_at: "2026-05-11T08:30:00Z",
	created_at: "2026-01-15T10:00:00Z",
	updated_at: "2026-05-10T12:00:00Z",
	model_count: 5,
	total_tokens: 1250000,
};

export const mockProvider2: Provider = {
	id: "provider-002",
	name: "Test Provider 2",
	base_url: "https://api.test-provider-2.com/v1",
	masked_key: "sk_test_••••••••••••••••••••••••",
	enabled: true,
	autodiscovery_enabled: true,
	last_discovered_at: "2026-05-10T12:00:00Z",
	last_used_at: "2026-05-11T08:30:00Z",
	created_at: "2026-01-15T10:00:00Z",
	updated_at: "2026-05-10T12:00:00Z",
	model_count: 3,
	total_tokens: 750000,
};

export const mockModel: Model = {
	id: "model-001",
	model_id: "test-model-v1",
	name: "Test Model",
	description: "A test model for development",
	display_name: "Test Model v1",
	provider_id: "provider-001",
	provider_name: "Test Provider",
	capabilities: '{"streaming":true,"vision":false,"audio_input":false}',
	params: '{"temperature":0.7,"max_tokens":4096}',
	modality: "text",
	input_modalities: "text",
	output_modalities: "text",
	context_length: 8192,
	max_output_tokens: 4096,
	input_price_per_million: 0.5,
	input_price_per_million_cache_hit: 0.1,
	output_price_per_million: 1.5,
	owned_by: "test-provider",
	enabled: true,
	disabled_manually: false,
	created_at: "2026-01-15T10:00:00Z",
	last_seen_at: "2026-05-11T08:30:00Z",
};

export const mockVirtualKey: VirtualKey = {
	id: "vk-001",
	name: "Test API Key",
	key_preview: "sk_test_••••",
	tokens_used: 50000,
	last_used_at: "2026-05-11T08:00:00Z",
	created_at: "2026-03-01T09:00:00Z",
	rate_limit_rps: 30,
	rate_limit_burst: 60,
	allowed_providers: null,
	strip_reasoning: false,
};

export const mockVirtualKeyWithProviders: VirtualKey = {
	...mockVirtualKey,
	id: "vk-002",
	name: "Restricted Key",
	allowed_providers: ["provider-001"],
};

export const mockStats: Stats = {
	total_requests_last_24h: 0,
	total_requests_last_7d: 0,
	by_model: {},
	by_provider: {},
	by_virtual_key: {},
	avg_latency_ms: 0,
	error_rate: 0,
	avg_overhead_ms: 0,
	rate_limit_hits: 0,
	avg_ttft_ms: 0,
	requests_last_1h: 0,
	total_tokens_prompt: 0,
	total_tokens_completion: 0,
	avg_tokens_per_request: 0,
};

export const mockSystemStats: SystemStats = {
	app: {
		heap_alloc_mb: 0,
		sys_memory_mb: 0,
		goroutines: 0,
		gc_cycles: 0,
		memory_current_bytes: 0,
		memory_limit_bytes: 0,
		in_container: false,
		uptime_seconds: 0,
		cpu_percent: 0,
		requests_today: 0,
		net_rx_bytes_sec: 0,
		net_tx_bytes_sec: 0,
		disk_read_bytes_sec: 0,
		disk_write_bytes_sec: 0,
		procs: 1,
	},
	db: {
		size_mb: 0,
		connections: 0,
		cache_hit_ratio: 0,
		tx_per_sec: 0,
		dead_tuples: 0,
		lock_waits: 0,
	},
	docker: {
		available: false,
		cpu_percent: 0,
		memory_usage_bytes: 0,
		memory_limit_bytes: 0,
		net_rx_bytes_sec: 0,
		net_tx_bytes_sec: 0,
		disk_read_bytes_sec: 0,
		disk_write_bytes_sec: 0,
		procs: 0,
		container_count: 0,
	},
};

export const mockBackupEntry: BackupEntry = {
	filename: "backup-2026-05-11T08-00-00Z.sql.gz",
	size_bytes: 1048576,
	created_at: "2026-05-11T08:00:00Z",
};

export const mockFailoverGroup: FailoverGroup = {
	id: "fg-001",
	display_model: "test-model",
	display_name: "Test Failover Group",
	description: "A test failover group",
	group_enabled: true,
	auto_created: false,
	entries: [],
	total_tokens: 0,
	created_at: "2026-04-01T10:00:00Z",
	updated_at: "2026-05-10T12:00:00Z",
};
