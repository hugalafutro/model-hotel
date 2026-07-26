import type {
	DeepSeekBalance,
	KimiCodeQuotaResponse,
	KimiCodeQuotaWindow,
	MiniMaxModelRemains,
	MiniMaxQuotaResponse,
	MiniMaxQuotaWindow,
	NanoGPTUsage,
	NeuralWattQuotaResponse,
	OllamaCloudAccount,
	OpenRouterBalance,
	QuotaProviderType,
	QuotaSnapshot,
	ZAICodingQuotaLimit,
	ZAICodingQuotaResponse,
} from "../api/types";

// Quota domain logic, ported from the Model Hotel dashboard
// (web/src/hooks/useQuotaData.ts) so both frontends derive the same numbers from
// the same provider payloads. What is NOT ported: detectQuotaProviderType. The
// member export stamps `type` on every snapshot, so Front Desk never sniffs a
// base URL.

// ── Provider type ────────────────────────────────────────────────────────

const QUOTA_PROVIDER_TYPES = [
	"nanogpt",
	"zai-coding",
	"kimi-code",
	"minimax",
	"deepseek",
	"openrouter",
	"ollama-cloud",
	"neuralwatt",
] as const;

const KNOWN_TYPES = new Set<string>(QUOTA_PROVIDER_TYPES);

/** Narrows a server-stamped type string to one this build knows how to render. */
export function isQuotaProviderType(v: string): v is QuotaProviderType {
	return KNOWN_TYPES.has(v);
}

/** Short pill prefixes, matching the Model Hotel sidebar. */
export const QUOTA_PREFIXES: Record<QuotaProviderType, string> = {
	nanogpt: "NG",
	"zai-coding": "ZAI",
	"kimi-code": "KIMI",
	minimax: "MMX",
	deepseek: "DS",
	openrouter: "OR",
	"ollama-cloud": "OLC",
	neuralwatt: "NW",
};

/**
 * Pill accent colour per provider, consumed as the `--quota-brand` custom
 * property. Three brands (Z.ai, Kimi, Ollama) are near-black in their own
 * palette, which is invisible on the dark theme, so they take the neutral muted
 * text colour instead. The Model Hotel dashboard makes the same substitution,
 * there by hand-writing a grey Tailwind class for those three.
 */
export const QUOTA_BRAND_COLORS: Record<QuotaProviderType, string> = {
	nanogpt: "#0EA5B0",
	"zai-coding": "var(--text-muted)",
	"kimi-code": "var(--text-muted)",
	minimax: "#F23F5B",
	deepseek: "#4D6BFE",
	openrouter: "#6366F1",
	"ollama-cloud": "var(--text-muted)",
	neuralwatt: "#AC4324",
};

// ── Payload access ───────────────────────────────────────────────────────

/**
 * The snapshot's payload, or null when the snapshot is not usable.
 *
 * A snapshot is unusable when the primary's last real fetch did not return 200,
 * or when the payload is not a JSON object. Callers render a "-" badge in that
 * case rather than hiding the provider: Front Desk shows another machine's
 * stored snapshot, so a failed fetch there is information, not noise.
 *
 * Arrays are rejected explicitly: `typeof [] === "object"`, so without the guard
 * a JSON array body would pass as a usable payload and (for the providers whose
 * visibility rule tolerates missing fields) render a NON-degraded "-" pill, with
 * neither the italic nor the degraded colour that the same non-reading earns
 * everywhere else.
 */
export function payloadOf<T>(snapshot: QuotaSnapshot): T | null {
	if (snapshot.http_status !== 200) return null;
	const p = snapshot.payload;
	if (p === null || typeof p !== "object" || Array.isArray(p)) return null;
	return p as T;
}

// ── Bar colouring ────────────────────────────────────────────────────────

export type QuotaBarMode = "used" | "remaining";
export type QuotaBarTone = "ok" | "warn" | "high" | "danger";

/**
 * Grades a bar by how alarming it is. `percentUsed` is always the used share;
 * remaining mode inverts it internally. Thresholds are the Model Hotel ones
 * (web/src/components/modals/shared.tsx:7-18).
 */
export function barTone(percentUsed: number, mode: QuotaBarMode): QuotaBarTone {
	if (mode === "remaining") {
		const remaining = 100 - percentUsed;
		if (remaining < 20) return "danger";
		if (remaining < 60) return "warn";
		return "ok";
	}
	if (percentUsed < 50) return "warn";
	if (percentUsed < 80) return "high";
	return "danger";
}

// ── Z.ai Coding ──────────────────────────────────────────────────────────

export function getZaiCodingFiveHourLimit(
	data: ZAICodingQuotaResponse | undefined | null,
): ZAICodingQuotaLimit | undefined {
	return data?.data?.limits?.find(
		(l) => l.type === "TOKENS_LIMIT" && l.unit === 3,
	);
}

export function getZaiCodingWeeklyLimit(
	data: ZAICodingQuotaResponse | undefined | null,
): ZAICodingQuotaLimit | undefined {
	return data?.data?.limits?.find(
		(l) => l.type === "TOKENS_LIMIT" && l.unit === 6,
	);
}

// ── Kimi Code ────────────────────────────────────────────────────────────
// Kimi encodes limit and remaining as JSON strings, so parse with Number()
// before computing. Percent used = (limit - remaining) / limit * 100.

function toKimiCodeWindow(
	limitStr: string | undefined,
	remainingStr: string | undefined,
	resetTime: string | undefined,
): KimiCodeQuotaWindow | undefined {
	if (limitStr == null || remainingStr == null) return undefined;
	const limit = Number(limitStr);
	const remaining = Number(remainingStr);
	if (!Number.isFinite(limit) || !Number.isFinite(remaining)) return undefined;
	const percentage = limit > 0 ? ((limit - remaining) / limit) * 100 : 0;
	return { limit, remaining, resetTime: resetTime ?? "", percentage };
}

/** The rolling 300 minute (5 hour) window. */
export function getKimiCodeFiveHourLimit(
	data: KimiCodeQuotaResponse | undefined | null,
): KimiCodeQuotaWindow | undefined {
	const entry = data?.limits?.find(
		(l) =>
			l.window?.timeUnit === "TIME_UNIT_MINUTE" && l.window?.duration === 300,
	);
	if (!entry) return undefined;
	return toKimiCodeWindow(
		entry.detail?.limit,
		entry.detail?.remaining,
		entry.detail?.resetTime,
	);
}

/** The weekly window, carried in the top-level `usage` block. */
export function getKimiCodeWeeklyLimit(
	data: KimiCodeQuotaResponse | undefined | null,
): KimiCodeQuotaWindow | undefined {
	const usage = data?.usage;
	if (!usage) return undefined;
	return toKimiCodeWindow(usage.limit, usage.remaining, usage.resetTime);
}

// ── MiniMax ──────────────────────────────────────────────────────────────
// MiniMax reports REMAINING percentages per model class. The active class is the
// first "general" entry with current_interval_status === 1. Used = 100 - remaining.

/** First active "general" model-class entry, or undefined. */
export function getMiniMaxGeneralEntry(
	data: MiniMaxQuotaResponse | undefined | null,
): MiniMaxModelRemains | undefined {
	const entries =
		data?.base_resp?.status_code === 0 ? data.model_remains : null;
	if (!Array.isArray(entries)) return undefined;
	return entries.find(
		(m) => m.model_name === "general" && m.current_interval_status === 1,
	);
}

function toMiniMaxWindow(
	remainingPercent: number | undefined,
	resetMs: number | undefined,
): MiniMaxQuotaWindow | undefined {
	if (remainingPercent == null || !Number.isFinite(remainingPercent))
		return undefined;
	return {
		percentage: 100 - remainingPercent,
		remainingPercent,
		resetMs: resetMs ?? 0,
	};
}

export function getMiniMaxFiveHourLimit(
	data: MiniMaxQuotaResponse | undefined | null,
): MiniMaxQuotaWindow | undefined {
	const g = getMiniMaxGeneralEntry(data);
	if (!g) return undefined;
	return toMiniMaxWindow(g.current_interval_remaining_percent, g.remains_time);
}

export function getMiniMaxWeeklyLimit(
	data: MiniMaxQuotaResponse | undefined | null,
): MiniMaxQuotaWindow | undefined {
	const g = getMiniMaxGeneralEntry(data);
	if (!g) return undefined;
	return toMiniMaxWindow(
		g.current_weekly_remaining_percent,
		g.weekly_remains_time,
	);
}

// ── Badge models ─────────────────────────────────────────────────────────

/** Subscription plans too low-tier to be worth a badge. */
const NEURALWATT_EXCLUDED_PLANS = new Set(["free", "starter"]);

/**
 * Whether a healthy payload has anything worth showing. Ported one-for-one from
 * the Model Hotel visibility booleans (web/src/hooks/useQuotaData.ts:644-690) so
 * both dashboards show the same set of badges for the same fleet.
 */
function isVisible(type: QuotaProviderType, payload: object): boolean {
	switch (type) {
		case "nanogpt": {
			const u = payload as NanoGPTUsage;
			const cancelled =
				u.providerStatus === "canceled" || u.providerStatus === "cancelled";
			return (
				!cancelled &&
				u.weeklyInputTokens?.used != null &&
				Boolean(u.limits?.weeklyInputTokens)
			);
		}
		case "zai-coding": {
			const u = payload as ZAICodingQuotaResponse;
			return (
				u.success === true &&
				Boolean(getZaiCodingFiveHourLimit(u) || getZaiCodingWeeklyLimit(u))
			);
		}
		case "kimi-code": {
			const u = payload as KimiCodeQuotaResponse;
			return Boolean(getKimiCodeFiveHourLimit(u) || getKimiCodeWeeklyLimit(u));
		}
		case "minimax":
			return Boolean(getMiniMaxGeneralEntry(payload as MiniMaxQuotaResponse));
		case "deepseek":
			return (payload as DeepSeekBalance).is_available === true;
		case "openrouter":
			return (payload as OpenRouterBalance).credits_remaining != null;
		case "ollama-cloud":
			return (payload as OllamaCloudAccount).suspended_at?.valid !== true;
		case "neuralwatt": {
			const q = payload as NeuralWattQuotaResponse;
			return (
				q.balance?.credits_remaining_usd != null &&
				!NEURALWATT_EXCLUDED_PLANS.has(
					q.subscription?.plan?.toLowerCase() ?? "",
				)
			);
		}
	}
}

export interface QuotaBadgeModel {
	/** Stable identity: `${type}:${provider_name}`. */
	key: string;
	type: QuotaProviderType;
	providerName: string;
	/** True when the fleet has more than one provider of this type. */
	showProviderName: boolean;
	/** True when the primary's last fetch failed or the payload is unreadable. */
	degraded: boolean;
	snapshot: QuotaSnapshot;
}

/**
 * Turns the raw snapshot list into the badges to render.
 *
 * Order of operations matters: unknown types are dropped first (version skew),
 * then unusable snapshots become degraded "-" badges, and only a healthy payload
 * is subject to the per-provider visibility rule.
 *
 * Sorting uses plain comparisons rather than localeCompare so the order is
 * identical in every locale, which keeps both the UI and its tests stable.
 */
export function toBadgeModels(snapshots: QuotaSnapshot[]): QuotaBadgeModel[] {
	const kept: Omit<QuotaBadgeModel, "showProviderName">[] = [];

	for (const s of snapshots) {
		if (!isQuotaProviderType(s.type)) continue;
		const payload = payloadOf<object>(s);
		const degraded = payload === null;
		if (!degraded && !isVisible(s.type, payload)) continue;
		kept.push({
			key: `${s.type}:${s.provider_name}`,
			type: s.type,
			providerName: s.provider_name,
			degraded,
			snapshot: s,
		});
	}

	kept.sort((a, b) => {
		if (a.type !== b.type) return a.type < b.type ? -1 : 1;
		if (a.providerName === b.providerName) return 0;
		return a.providerName < b.providerName ? -1 : 1;
	});

	const perType = new Map<QuotaProviderType, number>();
	for (const m of kept) perType.set(m.type, (perType.get(m.type) ?? 0) + 1);

	return kept.map((m) => ({
		...m,
		showProviderName: (perType.get(m.type) ?? 0) > 1,
	}));
}
