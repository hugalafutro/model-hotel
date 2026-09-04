import { getKimiCodeFiveHourLimit, getKimiCodeWeeklyLimit } from "./kimi";
import type {
	DeepSeekBalanceLike,
	KimiCodeQuotaResponse,
	MiniMaxQuotaResponse,
	NanoGptUsageLike,
	NeuralWattQuotaLike,
	OpenRouterBalanceLike,
	QuotaProviderType,
	ZaiCodingLimitLike,
	ZaiCodingResponseLike,
} from "./types";
import { getZaiCodingFiveHourLimit, getZaiCodingWeeklyLimit } from "./zai";

// Whether a readable payload says the account can serve nothing until it
// resets or is topped up. The rules mirror the gateway's quota normalizer
// (internal/quota/normalize.go) where one exists, so a badge reads as spent
// exactly when the breaker would pin the provider. A field that is absent is
// unknown, never spent: guessing here would flag a healthy provider.

/**
 * Below this balance NeuralWatt credits count as spent. NeuralWatt blocks the
 * account with a sub-cent residue that never drains, so an exact zero test
 * would never become true.
 */
const NEURALWATT_CREDITS_SPENT_FLOOR_USD = 0.01;

export function isNanoGptQuotaSpent(u: NanoGptUsageLike): boolean {
	if (u.allowOverage === true) return false;
	const limit = u.limits?.weeklyInputTokens;
	const used = u.weeklyInputTokens?.used;
	return limit != null && limit > 0 && used != null && used >= limit;
}

// A sane percentage decides; Z.ai sends an explicit remaining: 0 on windows
// that are only partially used, so remaining is the fallback, not the rule.
function isZaiCodingWindowSpent(l: ZaiCodingLimitLike): boolean {
	const pct = l.percentage;
	if (pct != null && pct >= 0 && pct <= 100) return pct >= 100;
	return l.remaining != null && l.remaining <= 0;
}

export function isZaiCodingQuotaSpent(u: ZaiCodingResponseLike): boolean {
	return [getZaiCodingFiveHourLimit(u), getZaiCodingWeeklyLimit(u)].some(
		(l) => l != null && isZaiCodingWindowSpent(l),
	);
}

export function isKimiCodeQuotaSpent(u: KimiCodeQuotaResponse): boolean {
	return [getKimiCodeFiveHourLimit(u), getKimiCodeWeeklyLimit(u)].some(
		(w) => w != null && w.remaining <= 0,
	);
}

// One MiniMax window, judged as addMiniMaxWindow does: only a window the plan
// covers (status 1) counts; request counts decide when the payload carries
// them, else a remaining percent inside [0, 100]; anything else is skipped.
function isMiniMaxWindowSpent(
	status: number | undefined,
	total: number | undefined,
	used: number | undefined,
	remainingPercent: number | undefined,
): boolean {
	if (status !== 1) return false;
	if (total != null && total > 0) return used != null && used >= total;
	if (
		remainingPercent != null &&
		remainingPercent >= 0 &&
		remainingPercent <= 100
	)
		return remainingPercent <= 0;
	return false;
}

// Every model class, as the gateway judges the provider: the breaker is per
// provider, so any spent window on any class pins it.
export function isMiniMaxQuotaSpent(u: MiniMaxQuotaResponse): boolean {
	if (u.base_resp?.status_code !== 0) return false;
	return (u.model_remains ?? []).some(
		(m) =>
			isMiniMaxWindowSpent(
				m.current_interval_status,
				m.current_interval_total_count,
				m.current_interval_usage_count,
				m.current_interval_remaining_percent,
			) ||
			isMiniMaxWindowSpent(
				m.current_weekly_status,
				m.current_weekly_total_count,
				m.current_weekly_usage_count,
				m.current_weekly_remaining_percent,
			),
	);
}

export function isDeepSeekQuotaSpent(b: DeepSeekBalanceLike): boolean {
	if (b.is_available === false) return true;
	const infos = b.balance_infos ?? [];
	if (infos.length === 0) return false;
	return infos.every((i) => {
		const raw = i.total_balance?.trim();
		if (!raw) return false;
		const n = Number(raw);
		return Number.isFinite(n) && n <= 0;
	});
}

// A key that never bought credits reports credits_remaining 0 (the gateway
// clamps total minus usage) yet still serves the free models, so only a
// funded key can spend its credits. A spent per-key cap blocks regardless.
export function isOpenRouterQuotaSpent(b: OpenRouterBalanceLike): boolean {
	const funded =
		b.is_free_tier !== true && b.credits_total != null && b.credits_total > 0;
	return (
		(funded && b.credits_remaining != null && b.credits_remaining <= 0) ||
		(b.limit_remaining != null && b.limit_remaining <= 0)
	);
}

// Spent means both meters are gone: the energy allowance (kwh_remaining at
// zero, or the in_overage flag NeuralWatt sets on entering overage) and the
// credits that overage draws on.
export function isNeuralWattQuotaSpent(q: NeuralWattQuotaLike): boolean {
	const sub = q.subscription;
	const energySpent =
		(sub?.kwh_remaining != null && sub.kwh_remaining <= 0) ||
		sub?.in_overage === true;
	const credits = q.balance?.credits_remaining_usd;
	return (
		energySpent &&
		credits != null &&
		credits < NEURALWATT_CREDITS_SPENT_FLOOR_USD
	);
}

/**
 * Spent-ness for a payload whose provider type is known only as a value. Ollama
 * Cloud's account payload names the plan and never the usage, so it can never
 * read as spent from here; its only cap reading is the gateway's cap note.
 */
export function isQuotaPayloadSpent(
	type: QuotaProviderType,
	payload: object,
): boolean {
	switch (type) {
		case "nanogpt":
			return isNanoGptQuotaSpent(payload as NanoGptUsageLike);
		case "zai-coding":
			return isZaiCodingQuotaSpent(payload as ZaiCodingResponseLike);
		case "kimi-code":
			return isKimiCodeQuotaSpent(payload as KimiCodeQuotaResponse);
		case "minimax":
			return isMiniMaxQuotaSpent(payload as MiniMaxQuotaResponse);
		case "deepseek":
			return isDeepSeekQuotaSpent(payload as DeepSeekBalanceLike);
		case "openrouter":
			return isOpenRouterQuotaSpent(payload as OpenRouterBalanceLike);
		case "ollama-cloud":
			return false;
		case "neuralwatt":
			return isNeuralWattQuotaSpent(payload as NeuralWattQuotaLike);
	}
}
