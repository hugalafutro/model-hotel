export interface CacheHits {
	failover?: boolean | null;
	model?: boolean | null;
	provider?: boolean | null;
	key?: boolean | null;
	settings?: boolean | null;
}

export interface Provider {
	id: string;
	name: string;
	base_url: string;
	masked_key: string;
	enabled: boolean;
	autodiscovery_enabled: boolean;
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
	autodiscovery_enabled?: boolean;
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
	response_header_ms: number;
	proxy_overhead_ms: number;
	parse_ms: number;
	failover_lookup_ms: number;
	model_lookup_ms: number;
	provider_lookup_ms: number;
	key_decrypt_ms: number;
	dial_ms: number;
	settings_read_ms: number;
	cache_hits?: CacheHits | null;
	tokens_per_second: number | null;
	tokens_prompt: number;
	tokens_completion: number;
	tokens_prompt_cache_hit: number;
	tokens_prompt_cache_miss: number;
	tokens_completion_reasoning: number;
	streaming: boolean;
	state: string;
	virtual_key_name: string;
	virtual_key_deleted?: boolean;
	virtual_key_id?: string;
	error_message: string;
	/** Machine-readable failure classification; "" or absent for legacy rows. */
	error_kind?: string;
	failover_attempt: number;
	created_at: string;
	resolved_model_id: string;
	endpoint_type: string;
}

export interface AppLogEntry {
	id?: string;
	created_at?: string;
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

export interface LogsCursorResponse {
	entries: LogEntry[];
	total: number;
	has_before: boolean;
	has_after: boolean;
}

export interface AppLogsCursorResponse {
	entries: AppLogEntry[];
	total: number;
	has_before: boolean;
	has_after: boolean;
	level_counts?: Record<string, number>;
	source_counts?: Record<string, number>;
}

export interface ModelsCursorResponse {
	entries: Model[];
	total: number;
	has_before: boolean;
	has_after: boolean;
}

export interface ModelLatencyEntry {
	model_id: string;
	total_ms: number;
	overhead_ms: number;
	provider_ms: number;
	request_count: number;
}

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
	by_model_latency?: ModelLatencyEntry[];
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
	rate_limit_tpm?: number | null;
	allowed_providers?: string[] | null;
	strip_reasoning: boolean;
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
	model_enabled: boolean;
	provider_enabled: boolean;
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
	display_model?: string;
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

export interface CircuitBreakerStatus {
	closed: number;
	half_open: number;
	open: number;
	providers?: CircuitBreakerProviderStatus[];
}

export interface CircuitBreakerProviderStatus {
	provider_id: string;
	provider_name?: string;
	state: "closed" | "open" | "half-open";
	consecutive_fails: number;
	opened_at?: string;
	cooldown_ms?: number;
	next_retry_at?: string;
}

export interface DeletedGroupInfo {
	display_model: string;
	reason: string;
	provider_count: number;
	provider_names: string[];
}

export interface PrunedEntryInfo {
	group_display_model: string;
	pruned_model_ids: string[];
}

export interface UpdatedGroupInfo {
	display_model: string;
	removed_model_ids?: string[];
	added_model_ids?: string[];
}

export interface SyncResult {
	deleted_groups: DeletedGroupInfo[];
	updated_groups?: UpdatedGroupInfo[];
	purged_entries?: PrunedEntryInfo[];
}

export interface ModelChange {
	model_id: string;
	/** Machine-readable code: new_model | reappeared | not_listed */
	reason: string;
}

export interface FieldChange {
	/** Machine-readable code: input_price | output_price | input_price_cache | context_length | max_output_tokens */
	field: string;
	/** Previous value as a number; null/undefined means it was unset. */
	old?: number | null;
	/** New value as a number; null/undefined means it is now unset. */
	new?: number | null;
}

export interface ModelUpdate {
	model_id: string;
	changes: FieldChange[];
}

export interface DiscoveryDiff {
	added?: ModelChange[];
	reenabled?: ModelChange[];
	disabled?: ModelChange[];
	updated?: ModelUpdate[];
	failover_deleted_groups?: DeletedGroupInfo[];
	failover_updated_groups?: UpdatedGroupInfo[];
}

/** One provider's recorded background-discovery diff (GET /api/discovery/changes). */
export interface DiscoveryChangeEntry {
	provider_name: string;
	source: string;
	detected_at: string;
	diff: DiscoveryDiff;
}

export interface DiscoveryChangesResponse {
	entries: DiscoveryChangeEntry[];
	count: number;
}

export interface DiscoverAllResult {
	provider_name: string;
	discovered: number;
	diff?: DiscoveryDiff;
	error?: string;
}

export interface BackupEntry {
	filename: string;
	size_bytes: number;
	created_at: string;
}

export interface BackupClassification {
	son: BackupEntry[];
	father: BackupEntry[];
	grandfather: BackupEntry[];
	prune: BackupEntry[];
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

export interface NeuralWattQuotaBalance {
	credits_remaining_usd: number;
	total_credits_usd: number;
	credits_used_usd: number;
	accounting_method: string;
}

export interface NeuralWattQuotaUsagePeriod {
	cost_usd: number;
	requests: number;
	tokens: number;
	energy_kwh: number;
}

export interface NeuralWattQuotaUsage {
	lifetime: NeuralWattQuotaUsagePeriod;
	current_month: NeuralWattQuotaUsagePeriod;
}

export interface NeuralWattQuotaLimits {
	overage_limit_usd: number | null;
	rate_limit_tier: string;
}

export interface NeuralWattQuotaSubscription {
	plan: string;
	status: string;
	billing_interval: string;
	current_period_start: string;
	current_period_end: string;
	auto_renew: boolean;
	kwh_included: number;
	kwh_used: number;
	kwh_remaining: number;
	in_overage: boolean;
}

export interface NeuralWattQuotaKey {
	name: string;
	allowance: number | null;
}

export interface NeuralWattQuotaResponse {
	snapshot_at: string;
	balance: NeuralWattQuotaBalance;
	usage: NeuralWattQuotaUsage;
	limits: NeuralWattQuotaLimits;
	subscription: NeuralWattQuotaSubscription;
	key: NeuralWattQuotaKey;
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

export interface WebAuthnCredential {
	id: string;
	name: string;
	transports: string[];
	created_at: string;
	aaguid: string;
	sign_count: number;
}

// PublicConfig is the unauthenticated subset of server config the SPA reads to
// render correctly (e.g. hide mutation controls in a read-only demo).
export interface PublicConfig {
	read_only: boolean;
}

// DemoLogin carries the admin token to display on a demo instance's login
// screen (empty unless the server has the demo token feature enabled), so an
// operator can share only the URL. Served by GET /api/demo-login.
export interface DemoLogin {
	token: string;
}

// AlertEventDef describes one operator-subscribable alert event, served by
// GET /api/alert/events. The Alerts settings picker is rendered from this list,
// so a new backend event surfaces in the UI without a frontend change.
export interface AlertEventDef {
	type: string;
	category: string;
	severity: "success" | "info" | "warning" | "error";
	defaultOn: boolean;
}

// AlertStatus reports whether the configured apprise-api container is reachable,
// served by GET /api/alert/status. `configured` is false when no URL is set;
// `reachable` means the host answered; `healthy` means GET /status returned 2xx.
export interface AlertStatus {
	configured: boolean;
	reachable: boolean;
	healthy: boolean;
	detail?: string;
}
