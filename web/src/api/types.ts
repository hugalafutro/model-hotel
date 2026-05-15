export interface Provider {
	id: string;
	name: string;
	base_url: string;
	masked_key: string;
	enabled: boolean;
	last_discovered_at: string | null;
	last_used_at: string | null;
	created_at: string;
	updated_at: string;
	model_count: number;
	total_tokens: number;
}

export interface CreateProviderRequest {
	name: string;
	base_url: string;
	api_key: string;
}

export interface UpdateProviderRequest {
	name?: string;
	base_url?: string;
	api_key?: string;
	enabled?: boolean;
}

export interface ModelCapabilities {
	streaming?: boolean;
	vision?: boolean;
	video_input?: boolean;
	audio_input?: boolean;
	reasoning?: boolean;
	tool_calling?: boolean;
	parallel_tool_calls?: boolean;
	structured_output?: boolean;
	pdf_upload?: boolean;
}

export interface Model {
	id: string;
	model_id: string;
	name: string;
	description: string;
	display_name: string;
	provider_id: string;
	provider_name: string;
	capabilities: string;
	params: string;
	modality: string;
	input_modalities: string;
	output_modalities: string;
	context_length: number | null;
	max_output_tokens: number | null;
	input_price_per_million: number | null;
	input_price_per_million_cache_hit: number | null;
	output_price_per_million: number | null;
	owned_by: string;
	enabled: boolean;
	disabled_manually: boolean;
	created_at: string;
	last_seen_at: string;
}

export interface LogEntry {
	id: string;
	provider_id: string;
	provider_name: string;
	model_id: string;
	request_hash: string;
	status_code: number;
	latency_ms: number;
	duration_ms: number;
	ttft_ms: number;
	proxy_overhead_ms: number;
	parse_ms: number;
	model_lookup_ms: number;
	provider_lookup_ms: number;
	key_decrypt_ms: number;
	safe_dial_ms: number;
	settings_read_ms: number;
	tokens_per_second: number | null;
	tokens_prompt: number;
	tokens_completion: number;
	tokens_prompt_cache_hit: number;
	tokens_prompt_cache_miss: number;
	streaming: boolean;
	state: string;
	virtual_key_name: string;
	virtual_key_deleted?: boolean;
	virtual_key_id?: string;
	error_message: string;
	failover_attempt: number;
	created_at: string;
}

export interface AppLogEntry {
	timestamp: string;
	level: "info" | "warning" | "error";
	source: string;
	message: string;
}

export interface LogsResponse {
	entries: LogEntry[];
	total: number;
	page: number;
	per_page: number;
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
	avg_tokens_per_request: number;
}

export type MetricType = "requests" | "tokens";
export type Range = "24h" | "7d";

export interface TimeSeriesPoint {
	bucket: string;
	count: number;
	tokens: number;
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

export interface VirtualKey {
	id: string;
	name: string;
	key?: string;
	key_preview: string;
	tokens_used: number;
	last_used_at: string | null;
	created_at: string;
	rate_limit_rps?: number | null;
	rate_limit_burst?: number | null;
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
}

export interface NanoGPTUsageLimits {
	weeklyInputTokens: number | null;
	dailyInputTokens: number | null;
	dailyImages: number | null;
}

export interface NanoGPTUsageTokenInfo {
	used: number;
	remaining: number;
	percentUsed: number;
	resetAt: number;
}

export interface NanoGPTUsageDailyImages {
	used: number;
	remaining: number;
	percentUsed: number;
	resetAt: number;
}

export interface NanoGPTUsagePeriod {
	currentPeriodEnd: string;
}

export interface NanoGPTUsage {
	active: boolean;
	provider: string;
	providerStatus: string;
	providerStatusRaw: string;
	stripeSubscriptionId: string;
	cancellationReason: string | null;
	canceledAt: string | null;
	endedAt: string | null;
	cancelAt: string | null;
	cancelAtPeriodEnd: boolean;
	limits: NanoGPTUsageLimits;
	allowOverage: boolean;
	period: NanoGPTUsagePeriod;
	dailyImages: NanoGPTUsageDailyImages | null;
	dailyInputTokens: NanoGPTUsageTokenInfo | null;
	weeklyInputTokens: NanoGPTUsageTokenInfo | null;
	state: string;
	graceUntil: string | null;
}

export interface DeepSeekBalanceInfo {
	currency: "CNY" | "USD";
	total_balance: string;
	granted_balance: string;
	topped_up_balance: string;
}

export interface DeepSeekBalance {
	is_available: boolean;
	balance_infos: DeepSeekBalanceInfo[];
}

export interface OpenRouterBalance {
	label: string;
	limit: number | null;
	limit_reset: string;
	limit_remaining: number | null;
	usage: number;
	usage_daily: number;
	usage_weekly: number;
	usage_monthly: number;
	credits_total: number;
	credits_used: number;
	credits_remaining: number;
	is_free_tier: boolean;
}

export interface OllamaCloudAccount {
	id: string;
	email: string;
	name: string;
	plan: string;
	customer_id: { string: string; valid: boolean };
	subscription_id: { string: string; valid: boolean };
	subscription_period_start: { time: string; valid: boolean };
	subscription_period_end: { time: string; valid: boolean };
	suspended_at: { time: string; valid: boolean };
}

export interface FailoverEntry {
	model_uuid: string;
	model_id: string;
	provider_id: string;
	provider_name: string;
	display_name: string;
	enabled: boolean;
	context_length: number | null;
	owned_by: string;
}

export interface FailoverGroup {
	id: string;
	display_model: string;
	display_name: string | null;
	description: string;
	group_enabled: boolean;
	auto_created: boolean;
	entries: FailoverEntry[];
	total_tokens: number;
	created_at: string;
	updated_at: string;
}

export interface FailoverListResponse {
	groups: FailoverGroup[];
	last_synced_at: string | null;
}

export interface CreateFailoverGroupRequest {
	display_model: string;
	display_name?: string;
	description?: string;
	entry_ids: string[];
}

export interface UpdateFailoverGroupRequest {
	display_name?: string;
	description?: string;
	group_enabled?: boolean;
	priority_order?: string[];
	entry_enabled?: Record<string, boolean>;
}

export interface CandidateModel {
	model_uuid: string;
	model_id: string;
	provider_id: string;
	provider_name: string;
	display_name: string;
	context_length: number | null;
	owned_by: string;
}

export interface DisabledGroupInfo {
	display_model: string;
	reason: string;
	provider_count: number;
	provider_names: string[];
}

export interface SyncResult {
	disabled_groups: DisabledGroupInfo[];
}

export interface BackupEntry {
	filename: string;
	size_bytes: number;
	created_at: string;
}

export interface ZAICodingQuotaUsageDetail {
	modelCode: string;
	usage: number;
}

export interface ZAICodingQuotaLimit {
	type: string;
	unit: number;
	number: number;
	usage: number;
	currentValue: number;
	remaining: number;
	percentage: number;
	nextResetTime: number;
	usageDetails?: ZAICodingQuotaUsageDetail[];
}

export interface ZAICodingQuotaData {
	limits: ZAICodingQuotaLimit[];
	level: string;
}

export interface ZAICodingQuotaResponse {
	code: number;
	msg: string;
	data: ZAICodingQuotaData;
	success: boolean;
}

export interface GenerationParams {
	temperature?: number;
	max_tokens?: number;
	top_p?: number;
	min_p?: number;
	top_k?: number;
	frequency_penalty?: number;
	presence_penalty?: number;
	reasoning_effort?: string; // "low" | "medium" | "high" — OpenAI o1/o3 reasoning depth
}

/** OpenAI-compatible multimodal content part types */
export type TextContentPart = { type: "text"; text: string };
export type ImageContentPart = {
	type: "image_url";
	image_url: { url: string };
};
export type AudioContentPart = {
	type: "input_audio";
	input_audio: { data: string; format: string };
};
export type ContentPart = TextContentPart | ImageContentPart | AudioContentPart;

export type MessageContent = string | ContentPart[];

export interface ChatMessage {
	role: "user" | "assistant" | "system";
	content: string;
	imageUrl?: string;
	audioAttachment?: { data: string; format: string };
	rawContent?: string;
	thinkingContent?: string;
	error?: string | null;
	aborted?: boolean;
	model?: string;
	timestamp: number;
	metrics?: {
		tokensPerSecond: number | null;
		durationMs: number;
		promptTokens: number;
		completionTokens: number;
	};
	params?: GenerationParams;
}
