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
	/** Vendor/API family chosen when the provider was added. Fixed after that. */
	provider_type: string;
	masked_key: string;
	enabled: boolean;
	autodiscovery_enabled: boolean;
	scheduled_disable_on: string | null;
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
	provider_type: string;
	api_key: string;
}

export interface UpdateProviderRequest {
	name?: string;
	/** Corrects the stored type. A self-hosted type is re-verified by probing. */
	provider_type?: string;
	base_url?: string;
	api_key?: string;
	enabled?: boolean;
	autodiscovery_enabled?: boolean;
	scheduled_disable_on?: string | null;
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
	/** Owning provider's enabled flag; false means the row is parked, not served. */
	provider_enabled: boolean;
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
	price_customized: boolean;
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
	/** Rows the proxy can serve (model enabled AND provider enabled). */
	enabled_total: number;
	has_before: boolean;
	has_after: boolean;
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
	owner_user_id?: string | null;
	owner_username?: string | null;
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
	/** True when a user disabled the model; false when discovery auto-disabled it
	 * (model no longer offered by the provider). Drives the N/A reason tooltip. */
	disabled_manually: boolean;
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
	// True when the cooldown currently governing this circuit was pinned to the
	// provider's quota reset deadline rather than the ordinary retry backoff. It
	// describes the override in force, not a claim that traffic is blocked right
	// now; when set, next_retry_at is that reset deadline.
	quota_pinned?: boolean;
}

// Outcome of an operator forcing one provider's circuit back closed.
// previous_state is what the breaker reported a moment before the reset, and
// reset is false when there was nothing to clear (an already-closed or
// never-tracked provider), so the UI can report a no-op honestly instead of
// claiming a recovery that did not happen.
export interface CircuitBreakerResetResult {
	provider_id: string;
	previous_state: "closed" | "open" | "half-open";
	reset: boolean;
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

export interface DisabledGroupInfo {
	display_model: string;
	effective_count: number;
	reason: string;
}

export interface SyncResult {
	deleted_groups: DeletedGroupInfo[];
	updated_groups?: UpdatedGroupInfo[];
	disabled_groups?: DisabledGroupInfo[];
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
	failover_disabled_groups?: DisabledGroupInfo[];
}

/**
 * One provider's recorded background-discovery diff. Served as the
 * `informational` feed of GET /api/discovery/status and as the rows returned by
 * POST /api/discovery/changes/ack; the GET that used to serve it is gone.
 */
export interface DiscoveryChangeEntry {
	/** Empty when the provider was deleted after the change was recorded. */
	provider_id?: string;
	provider_name: string;
	source: string;
	detected_at: string;
	diff: DiscoveryDiff;
}

/**
 * Response of POST /api/discovery/changes/ack: exactly the rows that call marked
 * seen. `count` is always 0 (the badge is empty once they are acked).
 */
export interface DiscoveryChangesResponse {
	entries: DiscoveryChangeEntry[];
	count: number;
}

/** What discovery currently believes about one model.
 *
 *  `retired` is the odd one out: it did not come from discovery at all. The
 *  proxy disabled the model from live traffic because the provider kept listing
 *  it and refused every request for it. The distinction matters to the operator,
 *  because a retired model is still listed and was seen moments ago, so the
 *  "last seen" wording and the Retest button that fit the other states would
 *  both mislead.
 *
 *  `pinned` is the other odd one out, in the opposite direction: the operator
 *  enabled the model by hand while the provider's listing stopped naming it, so
 *  discovery keeps counting the misses but never disables it. It is shown so a
 *  forgotten pin cannot rot silently, and never counted: it is a decision, not
 *  a problem. */
export type ClaimState = "gone" | "stale" | "suspect" | "retired" | "pinned";

export interface ModelClaim {
	model_id: string;
	state: ClaimState;
	/** When the provider last listed it; for a gone model, when it went missing. */
	last_seen_at: string;
	missing_scans: number;
	/** Membership transitions over the 30-day claim window. */
	flap_window: number;
	/** Membership transitions since the operator last opened the modal. */
	flap_since_review: number;
	/** When the proxy retired it from traffic. Present only on a `retired` claim;
	 *  `last_seen_at` keeps being refreshed for those, so it cannot date them. */
	retired_at?: string;
	/** When the operator last enabled it by hand. Present only on a `pinned`
	 *  claim; `last_seen_at` dates the listing, not the decision. */
	pinned_at?: string;
}

export interface ProviderClaims {
	provider_id: string;
	provider_name: string;
	gone: ModelClaim[];
	stale: ModelClaim[];
	suspect: ModelClaim[];
	retired: ModelClaim[];
	/** Always sent (empty rather than null) by servers that know about the
	 *  operator pin, and absent entirely from ones that do not, which a rolling
	 *  deploy puts behind this dashboard. Read it through `?? []`. */
	pinned: ModelClaim[];
}

/** One failover group discovery disabled: `hotel/<display_model>` routing for it
 *  is dead until it is fixed. Operator-disabled groups never appear here. */
export interface GroupClaim {
	display_model: string;
	/** Members configured on the group. */
	member_count: number;
	/** Members whose model AND provider are both enabled right now. */
	routable_count: number;
	/** When discovery disabled it. */
	disabled_at: string;
}

/** GET /api/discovery/status. `claim_count` counts `gone` and `retired` models
 *  plus `group_claims`; `stale` and `suspect` are shown but never counted.
 *  `informational_unseen` skips entries whose only content is metadata
 *  (`updated`), so price churn cannot light the badge dot. */
export interface DiscoveryStatusResponse {
	claims: ProviderClaims[];
	group_claims: GroupClaim[];
	informational: DiscoveryChangeEntry[];
	claim_count: number;
	informational_unseen: number;
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
	/** "manual" (operator-created), "scheduled" (GFS rotation), or "frontdesk"
	 *  (snapshot Front Desk took before an HA config sync). Absent on responses
	 *  from servers predating origin tracking; treat as manual, matching the
	 *  backend's default for filenames without an origin marker. */
	origin?: "manual" | "scheduled" | "frontdesk";
	/** Whether a signature sidecar exists for this backup, so its integrity can
	 *  be checked on download. Reports presence, not validity: the signature is
	 *  verified when the file is served, not when the list is built. Absent on
	 *  responses from servers predating backup signing, and false for backups
	 *  taken before it or when no MASTER_KEY is configured. */
	signed?: boolean;
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

// ── Kimi Code + MiniMax quota ───────────────────────────────────
// Declared in web-shared/quota, which both this dashboard and Front Desk parse
// these payloads with, and re-exported here so app code keeps importing every
// API type from one place.

export type {
	KimiCodeQuotaLimitEntry,
	KimiCodeQuotaResponse,
	KimiCodeQuotaUsageWindow,
	KimiCodeQuotaWindow,
	KimiCodeQuotaWindowSpec,
	MiniMaxBaseResp,
	MiniMaxModelRemains,
	MiniMaxQuotaResponse,
	MiniMaxQuotaWindow,
} from "@quota-shared";

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

export interface TotpStatus {
	enabled: boolean;
	/** RFC3339 confirmation time; absent when TOTP is disabled. */
	enabled_at?: string;
}

/**
 * Public OIDC SSO status, read unauthenticated on the login screen and in
 * settings. Reports only whether SSO is enabled and fully configured plus a
 * display name (the IdP host) for the button label; never any secret.
 */
export interface OidcStatus {
	enabled: boolean;
	/** IdP host, shown on the sign-in button; absent when not configured. */
	display_name?: string;
}

/**
 * Public GitHub SSO status, read unauthenticated on the login screen and in
 * settings. Reports only whether GitHub SSO is enabled and fully configured;
 * the button label is fixed ("GitHub"), so there is no display name.
 */
export interface GithubStatus {
	enabled: boolean;
}

/** Admin-gated detail for the settings panel (not the polled public status). */
export interface TotpInfo {
	recovery_remaining: number;
	recovery_total: number;
	/** RFC3339 time a TOTP code was last accepted; absent if never used. */
	last_used_at?: string;
}

export interface TotpEnrollStart {
	uri: string;
	secret: string;
}

export interface TotpEnrollVerify {
	recovery_codes: string[];
	// Session token minted on enable so the admin stays logged in (the raw
	// admin token is no longer a valid bearer once 2FA is on). Absent only if
	// the server could not mint one, in which case the user must re-login.
	token?: string;
}

export interface TotpLoginResponse {
	token: string;
}

// UserTotpStatus is the caller's own second-factor state, served by
// GET /api/auth/totp/status (users-row identities only).
export interface UserTotpStatus {
	enabled: boolean;
	/** RFC3339 confirmation time; absent when disabled. */
	enabled_at?: string;
	recovery_remaining?: number;
	recovery_total?: number;
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
// served by GET /api/alert/status and POST /api/alert/probe. `configured` is
// false when no URL is set; `reachable` means the host answered; `healthy`
// means GET /status returned 2xx. `reason` is a stable machine-readable code
// (not_configured, invalid_url, unreachable, unhealthy) the wizard can branch
// on instead of matching `detail`'s English text.
export interface AlertStatus {
	configured: boolean;
	reachable: boolean;
	healthy: boolean;
	reason?: string;
	detail?: string;
}

// AlertTargets is the saved destination list, served decrypted by
// GET /api/alert/targets for the admin UI's readable list.
export interface AlertTargets {
	targets: string[];
}

// AuditEntry is one recorded admin action, served by GET /api/audit
// (admin-only). Request bodies are never recorded server-side.
export interface AuditEntry {
	id: string;
	created_at: string;
	actor: string;
	actor_role: string;
	method: string;
	route: string;
	path: string;
	entity_id?: string;
	status_code: number;
	remote_addr: string;
	// Current display name of the entity, resolved server-side at read time.
	// Absent when the entity was deleted or the route family is unmapped.
	entity_name?: string;
}

// AuditListResponse is the cursor-paginated audit page.
export interface AuditListResponse {
	entries: AuditEntry[];
	total: number;
	has_more: boolean;
	next_cursor?: string;
}

// AuthStatus reports whether any enabled user accounts exist, read
// unauthenticated on the login screen to decide whether to render the
// username/password form. Served by GET /api/auth/status.
export interface AuthStatus {
	enabled: boolean;
}

// Me is the caller's resolved identity, served by GET /api/auth/me. The
// sidebar and routes gate on role/grants; the server enforces regardless.
export interface Me {
	username: string;
	display_name?: string;
	role: "admin" | "user";
	grants: string[];
	/** True for users-row identities (not the env-token admin); gates the Security page. */
	user_account?: boolean;
	/**
	 * Account provider cap. null/undefined means no cap (every provider); a
	 * non-null array restricts every key this user owns to exactly its members,
	 * so an EMPTY array denies every provider. Empty is reachable on READ even
	 * though the users API refuses to write one: deleting the last provider a
	 * capped account named rewrites the stored list to `[]`
	 * (provider.PruneAllowLists), and a fleet config-sync imports one verbatim.
	 * Test this field's presence, never its length. Advisory only — the server
	 * is the enforcement point (checked at virtual-key write time and again on
	 * every proxied request), not this field.
	 */
	allowed_providers?: string[] | null;
}

// AuthSession is one row of the Settings active-sessions list, served by
// GET /api/auth/sessions: the caller's own live sessions only, with the one
// this browser rides on flagged current. Sessions minted before the device
// metadata existed carry empty user_agent/ip and no last_seen_at.
export interface AuthSession {
	id: string;
	user_agent: string;
	ip: string;
	created_at: string;
	last_seen_at?: string;
	current: boolean;
}

// DashboardUser is a managed user account (admin-only Users page). The
// password hash never leaves the backend.
export interface DashboardUser {
	id: string;
	username: string;
	display_name: string;
	email: string | null;
	role: "admin" | "user";
	grants: string[];
	enabled: boolean;
	created_at: string;
	updated_at: string;
	last_login_at: string | null;
	// Aggregate proxy limits across the user's owned virtual keys (null = no cap).
	rate_limit_rps?: number | null;
	rate_limit_burst?: number | null;
	rate_limit_tpm?: number | null;
	/** Whether the account has a confirmed TOTP second factor. */
	totp_enabled?: boolean;
	/**
	 * Account provider cap. null/undefined means no cap (every provider); a
	 * non-null array restricts every key this user owns to exactly its members,
	 * so an EMPTY array denies every provider. Empty is reachable on READ even
	 * though the users API refuses to write one: deleting the last provider a
	 * capped account named rewrites the stored list to `[]`
	 * (provider.PruneAllowLists), and a fleet config-sync imports one verbatim.
	 * Test this field's presence, never its length. Advisory only — the server
	 * is the enforcement point (checked at virtual-key write time and again on
	 * every proxied request), not this field.
	 */
	allowed_providers?: string[] | null;
}

// UserUpsertRequest is the create/update body for POST/PUT /api/users.
// password is create-only; enabled is update-only.
export interface UserUpsertRequest {
	username: string;
	display_name: string;
	email: string | null;
	password?: string;
	role: "admin" | "user";
	grants: string[];
	enabled?: boolean;
	rate_limit_rps?: number | null;
	rate_limit_burst?: number | null;
	rate_limit_tpm?: number | null;
	/**
	 * Account provider cap. Omit to leave the stored cap unchanged (update
	 * only); send null to clear it (every provider); send a non-empty array
	 * to restrict every key this user owns to exactly those provider IDs. An
	 * empty array is rejected by the API. Advisory only, the server enforces.
	 */
	allowed_providers?: string[] | null;
}
