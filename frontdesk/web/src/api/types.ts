// Typed mirror of the Front Desk Go API (internal/frontdesk). Keep in step with
// store.go / poller.go / server.go.

export type MemberState = "active" | "drained";

export interface Member {
	id: string;
	name: string;
	url: string;
	state: MemberState;
	has_token: boolean;
	created_at: string;
	updated_at: string;
	// Set after add/edit when the admin token was saved but could not be
	// confirmed (member offline, or an older build). A refused token is a 400.
	token_warning?: string;
	// When Front Desk last applied config to this member (wizard or automatic),
	// RFC3339; absent until the first sync. last_config_sync_reason explains why
	// (e.g. it did not hold the primary's config). Both drive the "Last Config
	// Sync" column on the Members tab.
	last_config_sync_at?: string;
	last_config_sync_reason?: string;
}

// FleetState is the server-computed fleet condition, spelled exactly as it
// travels on the wire (fleet_state on GET/PUT /api/fleet/autosync). Mirrors
// internal/frontdesk/fleetstate.go. An older backend omits it entirely.
export type FleetState = "ok" | "degraded" | "faulty";

// AutoSyncConfig is the GET/PUT /api/fleet/autosync body: the stored config (a
// master on/off plus the designated source-of-truth member, empty when none is
// chosen) plus the server-computed status fields folded onto the same payload
// (see autoSyncStatus in server_settings.go). stale, last_sync_at, fleet_state,
// and fleet_state_reasons are all optional so an older backend that omits them
// still deserializes cleanly. fleet_state_reasons carries wire-constant reason
// codes (member_down, all_members_down, member_drained, drained_to_single,
// sync_held, all_sync_held, sync_incomplete, autosync_stale,
// autosync_stale_long, traefik_config_stale) that the client translates.
export interface AutoSyncConfig {
	enabled: boolean;
	primary_id: string;
	stale?: boolean;
	last_sync_at?: string;
	fleet_state?: FleetState;
	fleet_state_reasons?: string[];
}

export interface HealthStatus {
	known: boolean;
	healthy: boolean;
	latency_ms: number;
	checked_at: string;
	error?: string;
}

export interface MemberStatus {
	health: HealthStatus;
	traefik_status?: string; // "UP" | "DOWN" | ""
	version?: string;
	// Live "auto-sync is running" heartbeat: the last time the auto-syncer
	// confirmed this member matches the primary (a real write, a self-converged
	// empty diff, or a quiet verify tick). RFC3339; absent until first verified,
	// and frozen while the member is unreachable. Distinct from
	// last_config_sync_at, which moves only on a real config write.
	auto_sync_verified_at?: string;
}

// Bellhop device pairing. A PairedDevice is one linked phone; its bearer token
// is never exposed after the pairing exchange. PairStart is the one-time code
// the Paired devices panel renders as a QR / pairing string.
export type DeviceRole = "monitor" | "operator";

export interface PairedDevice {
	id: string;
	label: string;
	role: DeviceRole;
	created_at: string;
	last_seen_at?: string;
}

export interface PairStart {
	code: string;
	role: DeviceRole;
	expires_at: string;
}

// AuthSession is one row of Settings → Active sessions, served by
// GET /api/auth/sessions: the admin identity's live browser sessions, with the
// one this browser rides on flagged current. Sessions minted before the device
// metadata existed carry empty user_agent/ip and no last_seen_at.
export interface AuthSession {
	id: string;
	user_agent: string;
	ip: string;
	created_at: string;
	last_seen_at?: string;
	current: boolean;
}

// memberView from listMembers: a Member plus its live poller status.
export interface MemberView extends Member {
	status: MemberStatus;
}

export interface Settings {
	health_poll_secs: number;
	traefik_poll_secs: number;
	traefik_stale_secs: number;
	event_retention_days: number;
	retry_attempts: number;
	// Consecutive failed health polls before a member is reported down (an error
	// event plus, by default, an Apprise alert). Damps routine-rebuild flap; also
	// governs the Traefik UP->DOWN badge flip. Minimum 1.
	health_fail_threshold: number;
	// Admin-UI inactivity auto-logout window in minutes; 0 disables it. Consumed
	// by useIdleLogout to sign the operator out after inactivity.
	session_idle_timeout_minutes: number;
	// Outbound Apprise alerting. GET /api/settings still serves
	// alert_apprise_targets masked ("********" when a secret is stored), and a PUT
	// that echoes the mask back preserves the stored value, for compatibility with
	// clients that never see the plaintext. The admin UI does not use that path: it
	// reads the plaintext list from GET /api/alert/targets, shows it in clear, and
	// always PUTs a real value ("" clears it) so the mask is never sent back.
	// alert_events is a CSV of enabled event Types (the per-event picker).
	alert_enabled: boolean;
	alert_apprise_api_url: string;
	alert_apprise_targets: string;
	alert_events: string;
	// OIDC SSO admin login. oidc_client_secret is masked over the API ("********"
	// when a secret is stored); echo it back unchanged to preserve the stored
	// value, send a new value to replace it, or "" to clear it. oidc_allowed_emails
	// is a CSV verified-email allowlist (fail-closed: empty denies everyone).
	oidc_enabled: boolean;
	oidc_issuer_url: string;
	oidc_client_id: string;
	oidc_client_secret: string;
	oidc_public_base_url: string;
	oidc_allowed_emails: string;
}

// Public OIDC SSO status (GET /api/auth/oidc/status), gating the login button.
export interface OidcStatus {
	enabled: boolean;
	display_name?: string;
}

// Running-build identity from GET /api/version, used by the footer to show which
// Front Desk build is deployed. app_version is "dev" for un-stamped builds;
// app_commit is a short SHA, or "unknown" when the build wasn't stamped.
export interface VersionInfo {
	app_version: string;
	app_commit: string;
}

// Read-only log-export integration status from GET /api/observability, derived
// server-side from the process environment. Each integration is enabled by its
// own environment variable and is not runtime-changeable; the Observability
// panel only reflects this state. log_export_metrics reports whether a
// dedicated Prometheus scrape token is configured (the /metrics endpoint
// itself always exists, gated by admin auth otherwise).
export interface ObservabilityStatus {
	log_export_json: boolean;
	log_export_otel: boolean;
	log_export_metrics: boolean;
}

// One alertable event in the Front Desk catalog (GET /api/alert/events), mirroring
// alert.EventDef. The picker is rendered from this list, grouped by category.
export interface AlertEventDef {
	type: string;
	category: string;
	severity: string; // display severity: success|info|warning|error
	defaultOn: boolean;
}

// Reachability of the operator's apprise-api (GET /api/alert/status), mirroring
// alert.Status.
export interface AlertStatus {
	configured: boolean;
	reachable: boolean;
	healthy: boolean;
	/** One of the server's reason codes when not healthy; detail is raw text. */
	reason?: string;
	detail?: string;
}

/**
 * The saved Apprise destination URLs in plaintext, in the order they are
 * stored. Empty when no destination is configured.
 */
export interface AlertTargets {
	targets: string[];
}

export type Severity = "info" | "success" | "warning" | "error";

export interface FdEvent {
	id: string;
	type: string;
	severity: string;
	source: string;
	message: string;
	metadata?: Record<string, unknown>;
	member_id?: string;
	created_at: string;
}

export interface EventsPage {
	events: FdEvent[];
	total: number;
}

// Per-member traffic metrics, as proxied by Front Desk from each member's
// /api/stats/timeseries (last hour, 5-minute buckets).
export interface MemberTrafficPoint {
	bucket: string;
	requests: number;
	errors: number;
}

export interface MemberTraffic {
	member_id: string;
	reachable: boolean;
	window_minutes: number;
	total_requests: number;
	total_errors: number;
	points: MemberTrafficPoint[];
}

// Per-member result of the config-sync POST the wizard runs.
export interface SyncResultItem {
	member_id: string;
	name: string;
	ok: boolean;
	error?: string;
}

export interface SyncResult {
	results: SyncResultItem[];
}

// --- Fleet sync wizard status (GET /api/fleet/status) ---
// One member's convergence state against the chosen primary, mirroring
// internal/frontdesk/fleetstatus.go. The wizard gates each step on these fields.
export interface FleetMemberStatus {
	member_id: string;
	name: string;
	reachable: boolean;
	has_token: boolean;
	// null = MASTER_KEY not evaluated: a keyless fleet (nothing to verify) or a
	// member that could not be probed. A non-null false is a real mismatch.
	master_key_matches: boolean | null;
	schema_ok: boolean;
	added: number;
	updated: number;
	removed: number;
	note?: string;
}

export interface FleetStatus {
	primary_id: string;
	primary_reachable: boolean;
	primary_note?: string;
	members: FleetMemberStatus[];
	// Host port the load balancer is published on (LB_PORT in the HA .env). The
	// wizard's Done step pairs it with the browser host to show where to send
	// /v1 traffic. Absent only if Front Desk was not told the port.
	lb_port?: string;
}

// Version-alignment check (POST /api/fleet/version-check). Re-polls member
// versions on demand and reports the ones that differ from the chosen
// primary's. Drives the Fleet Sync wizard's pre-sync gate and Refresh button.
export interface FleetVersionSkewMember {
	member_id: string;
	name: string;
	version: string; // "" when unknown / unreadable
}

export interface FleetVersionCheck {
	primary_id: string;
	primary_version: string;
	skewed: FleetVersionSkewMember[];
}

// Last successful fleet-sync wizard run (GET /api/fleet/last-sync). Absent
// (null/204) until the wizard converges at least one member.
export interface FleetSyncState {
	last_run_at: string;
	primary_id: string;
	primary_name: string;
}

// --- Admin authentication (passkeys + TOTP), Settings → Security ---

export interface WebAuthnCredential {
	id: string;
	name: string;
	transports: string[];
	created_at: string;
	aaguid: string;
	sign_count: number;
}

/** Admin-gated detail for the Security panel (not the polled public status). */
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
}

// ── Quota ────────────────────────────────────────────────────────────────
// Payload shapes mirror the upstream provider APIs, ported from the Model Hotel
// dashboard (web/src/api/types.ts). Front Desk receives them verbatim inside a
// QuotaSnapshot.payload, so the field names are the providers', not ours.

/** The provider families that expose a quota or balance endpoint. */
export type { QuotaProviderType } from "@quota-shared";

/**
 * One provider's quota snapshot as proxied from the fleet primary.
 *
 * `type` is deliberately `string`, not `QuotaProviderType`: the member stamps it
 * from its own provider-type detection, so a Front Desk newer than its primary
 * (or a primary carrying a type this build predates) can legitimately send a
 * value we do not know. Unknown types are filtered out in utils/quota.ts.
 *
 * `payload` arrives as nested JSON (the Go side carries it as json.RawMessage),
 * so it is an object here, never a string.
 */
export interface QuotaSnapshot {
	provider_name: string;
	type: string;
	kind: string;
	payload: unknown;
	http_status: number;
	fetched_at: string;
}

/** Counters returned by a forced upstream re-poll on the primary. */
export interface QuotaRefreshResult {
	results: unknown[];
	refreshed: number;
	failed: number;
	skipped: number;
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

export interface NanoGPTUsage {
	active: boolean;
	provider: string;
	providerStatus: string;
	cancelAtPeriodEnd: boolean;
	limits: NanoGPTUsageLimits;
	allowOverage: boolean;
	period: { currentPeriodEnd: string };
	dailyImages: NanoGPTUsageDailyImages | null;
	dailyInputTokens: NanoGPTUsageTokenInfo | null;
	weeklyInputTokens: NanoGPTUsageTokenInfo | null;
}

export interface ZAICodingQuotaUsageDetail {
	modelCode: string;
	usage: number;
}

export interface ZAICodingQuotaLimit {
	type: string;
	unit: number;
	percentage: number;
	nextResetTime?: number;
	usageDetails?: ZAICodingQuotaUsageDetail[];
}

export interface ZAICodingQuotaResponse {
	success: boolean;
	data?: {
		level?: string;
		limits?: ZAICodingQuotaLimit[];
	};
}

// ── Kimi Code + MiniMax quota ───────────────────────────────────
// Declared in web-shared/quota, which both Front Desk and the Model Hotel
// dashboard parse these payloads with, and re-exported here so app code keeps
// importing every API type from one place.

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

export interface DeepSeekBalanceInfo {
	currency: "CNY" | "USD";
	total_balance: string;
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
	plan: string;
	subscription_period_end: { time: string; valid: boolean };
	suspended_at: { time: string; valid: boolean };
}

export interface NeuralWattQuotaUsagePeriod {
	cost_usd: number;
	requests: number;
	tokens: number;
	energy_kwh: number;
}

/**
 * NeuralWatt's quota body, as relayed by the fleet primary.
 *
 * Only `balance` is declared required, and only because the badge visibility
 * gate (isNeuralWattQuotaVisible in web-shared/quota) refuses to render a
 * NeuralWatt badge at all unless balance.credits_remaining_usd is present, so
 * nothing downstream can be reached without it. Every other block is optional on purpose: this is an
 * upstream provider's JSON forwarded through another machine's export, not a
 * shape this build controls, and a fleet primary on a different version may
 * relay less than the current one does. Optional here is what makes the
 * compiler, rather than an operator's crashed dashboard, catch an unguarded
 * nested read.
 */
export interface NeuralWattQuotaResponse {
	balance: {
		credits_remaining_usd: number;
		total_credits_usd: number;
		credits_used_usd: number;
		accounting_method: string;
	};
	usage?: {
		lifetime?: NeuralWattQuotaUsagePeriod;
		current_month?: NeuralWattQuotaUsagePeriod;
	};
	limits?: { overage_limit_usd: number | null; rate_limit_tier: string };
	subscription?: {
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
	};
	key?: { name: string; allowance: number | null };
}
