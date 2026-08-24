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
	| "neuralwatt";

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

/** The two fields the Z.ai window selectors read. */
export interface ZaiCodingLimitLike {
	type?: string;
	unit?: number;
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
	limits?: { weeklyInputTokens?: number | null } | null;
	weeklyInputTokens?: { used?: number | null } | null;
}

export interface DeepSeekBalanceLike {
	is_available?: boolean;
}

export interface OpenRouterBalanceLike {
	credits_remaining?: number | null;
}

export interface OllamaCloudAccountLike {
	suspended_at?: { valid?: boolean } | null;
}

export interface NeuralWattQuotaLike {
	balance?: { credits_remaining_usd?: number | null } | null;
	subscription?: { plan?: string } | null;
}

export interface NeuralWattBalanceLike {
	credits_used_usd?: number | null;
	credits_remaining_usd?: number | null;
	total_credits_usd?: number | null;
}
