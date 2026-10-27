// Payload and derived-window types for the provider quota endpoints.
//
// These declarations are the ones both frontends agree on. Where an app models
// more of a payload than the helpers here read (the dashboard's fuller Z.ai
// limit, for instance), the app keeps its own declaration and the helper takes a
// structural view of it, so neither app has to widen or narrow the other.

/** Provider families that expose a quota or balance endpoint. */
export type QuotaProviderType =
	| "nanogpt"
	| "zai-coding"
	| "kimi-code"
	| "minimax"
	| "deepseek"
	| "openrouter"
	| "ollama-cloud"
	| "neuralwatt"
	| "opencode-go";

// ── Kimi Code ────────────────────────────────────────────────────────────
// Kimi's /usages body is proto3 JSON: numbers are string-encoded and a
// zero-valued field is omitted entirely. The stored snapshot round-trips
// through a Go struct, which materializes an omitted string as "", so absent
// and "" are the same case everywhere below: the field is zero.

/** One quota window as Kimi reports it. Every field is string-encoded. */
export interface KimiCodeQuotaUsageWindow {
	limit?: string;
	remaining?: string;
	used?: string;
	resetTime?: string;
}

export interface KimiCodeQuotaWindowSpec {
	duration?: number;
	timeUnit?: string;
}

export interface KimiCodeQuotaLimitEntry {
	window?: KimiCodeQuotaWindowSpec;
	detail?: KimiCodeQuotaUsageWindow;
}

export interface KimiCodeQuotaResponse {
	user?: {
		userId?: string;
		region?: string;
		membership?: { level?: string };
	};
	/** Weekly window. */
	usage?: KimiCodeQuotaUsageWindow;
	/** Rolling windows; the 300-minute entry is the 5-hour window. */
	limits?: KimiCodeQuotaLimitEntry[];
	parallel?: { limit?: string };
	totalQuota?: { limit?: string; remaining?: string };
	authentication?: { method?: string; scope?: string };
	subType?: string;
}

/**
 * Normalized Kimi window. `percentage` is percent USED, clamped to [0, 100], so
 * badge and modal code stays uniform across providers.
 */
export interface KimiCodeQuotaWindow {
	limit: number;
	remaining: number;
	resetTime: string;
	percentage: number;
}

// ── Z.ai Coding ──────────────────────────────────────────────────────────
// Both apps model the limit entry with different depth, so the extractors are
// generic over it: they select by `type`/`unit` and hand the caller's own entry
// back untouched.

/** The fields the Z.ai window selectors and the spent rule read. */
export interface ZaiCodingLimitLike {
	type?: string;
	unit?: number;
	/** Percent of the window consumed. */
	percentage?: number | null;
	remaining?: number | null;
}

export interface ZaiCodingResponseLike<
	L extends ZaiCodingLimitLike = ZaiCodingLimitLike,
> {
	success?: boolean;
	data?: { limits?: L[] } | null;
}

// ── MiniMax ──────────────────────────────────────────────────────────────
// MiniMax reports REMAINING percentages (0-100) per model class.

export interface MiniMaxModelRemains {
	model_name: string;
	start_time?: number;
	end_time?: number;
	remains_time: number;
	weekly_start_time?: number;
	weekly_end_time?: number;
	weekly_remains_time: number;
	current_interval_status: number;
	current_interval_remaining_percent: number;
	current_weekly_status: number;
	current_weekly_remaining_percent: number;
	/** Request counts; some Token Plan tiers report them as all zero. */
	current_interval_total_count?: number;
	current_interval_usage_count?: number;
	current_weekly_total_count?: number;
	current_weekly_usage_count?: number;
}

export interface MiniMaxBaseResp {
	status_code: number;
	status_msg: string;
}

export interface MiniMaxQuotaResponse {
	model_remains: MiniMaxModelRemains[] | null;
	base_resp: MiniMaxBaseResp;
}

/**
 * Normalized MiniMax window. `percentage` is the USED percentage (100 minus
 * remaining); `resetMs` is the millisecond duration until the window resets.
 */
export interface MiniMaxQuotaWindow {
	percentage: number;
	remainingPercent: number;
	resetMs: number;
}

// ── Structural views for the balance-style providers ─────────────────────
// These providers have no window math, only a visibility rule, so the shared
// module reads just the fields that rule needs.

export interface NanoGptUsageLike {
	providerStatus?: string;
	limits?: {
		weeklyInputTokens?: number | null;
		dailyInputTokens?: number | null;
	} | null;
	weeklyInputTokens?: { used?: number | null } | null;
	dailyInputTokens?: { used?: number | null } | null;
	/** An overage-enabled plan keeps serving past its allowance. */
	allowOverage?: boolean;
}

export interface DeepSeekBalanceLike {
	is_available?: boolean;
	/** DeepSeek string-encodes its balances. */
	balance_infos?: Array<{
		currency?: string;
		total_balance?: string | null;
	}> | null;
}

export interface OpenRouterBalanceLike {
	credits_remaining?: number | null;
	credits_total?: number | null;
	/** A key that never bought credits; it still serves the free models. */
	is_free_tier?: boolean;
	/** Per-key spending cap left, null when the key has no cap. */
	limit_remaining?: number | null;
}

export interface OllamaCloudAccountLike {
	suspended_at?: { valid?: boolean } | null;
}

export interface NeuralWattQuotaLike {
	balance?: { credits_remaining_usd?: number | null } | null;
	subscription?: {
		plan?: string;
		in_overage?: boolean;
		kwh_remaining?: number | null;
	} | null;
}

// ── OpenCode Go ──────────────────────────────────────────────────────────
// GET /zen/go/v1/usage reports three windows as percent consumed. `status` is
// "ok" on a healthy window; the other values are undocumented, so anything
// else reads as spent. A missing subscription answers 403, which the gateway
// turns into a 204 and a null payload rather than a body to parse.

/** One OpenCode Go window: percent consumed, 0 to 100, and its reset. */
export interface OpenCodeGoUsageWindow {
	status?: string;
	percent?: number;
	resetsAt?: string;
}

export interface OpenCodeGoUsageResponse {
	usage?: {
		/** The rolling 5-hour window. */
		rolling?: OpenCodeGoUsageWindow;
		weekly?: OpenCodeGoUsageWindow;
		monthly?: OpenCodeGoUsageWindow;
	};
}

// ── Payloads the shared quota modals render ──────────────────────────────
// Front Desk's declarations, written against another machine's relayed export,
// so a block the provider may omit is optional. The dashboard keeps its own,
// narrower declarations, which stay assignable to these.

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

export interface NanoGPTUsage {
	active: boolean;
	provider: string;
	providerStatus: string;
	cancelAtPeriodEnd: boolean;
	limits: NanoGPTUsageLimits;
	allowOverage: boolean;
	period: { currentPeriodEnd: string };
	dailyImages: NanoGPTUsageTokenInfo | null;
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

export interface NeuralWattQuotaUsagePeriod {
	cost_usd: number;
	requests: number;
	tokens: number;
	energy_kwh: number;
}

/**
 * NeuralWatt's quota body, as relayed by the fleet primary.
 *
 * Only `balance` is required, because the badge visibility gate
 * (isNeuralWattQuotaVisible in web-shared/quota) renders no NeuralWatt badge
 * unless balance.credits_remaining_usd is present, so nothing downstream is
 * reachable without it. Every other block is optional: this is an upstream
 * provider's JSON forwarded through another machine's export, not a shape this
 * build controls, and a fleet primary on a different version may relay less.
 * Optional is what makes the compiler, rather than an operator's crashed
 * dashboard, catch an unguarded nested read.
 */
export interface NeuralWattQuotaResponse {
	balance: {
		/** Absent when the provider omitted the balance; never coerce to 0. */
		credits_remaining_usd?: number | null;
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
