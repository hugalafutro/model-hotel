import { getKimiCodeFiveHourLimit, getKimiCodeWeeklyLimit } from "./kimi";
import { getMiniMaxFiveHourLimit, getMiniMaxWeeklyLimit } from "./minimax";
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

export function isMiniMaxQuotaSpent(u: MiniMaxQuotaResponse): boolean {
	return [getMiniMaxFiveHourLimit(u), getMiniMaxWeeklyLimit(u)].some(
		(w) => w != null && w.remainingPercent <= 0,
	);
}

export function isDeepSeekQuotaSpent(b: DeepSeekBalanceLike): boolean {
	if (b.is_available === false) return true;
	const infos = b.balance_infos ?? [];
	if (infos.length === 0) return false;
	return infos.every((i) => {
		const n = Number(i.total_balance);
		return i.total_balance != null && Number.isFinite(n) && n <= 0;
	});
}

export function isOpenRouterQuotaSpent(b: OpenRouterBalanceLike): boolean {
	return (
		(b.credits_remaining != null && b.credits_remaining <= 0) ||
		(b.limit_remaining != null && b.limit_remaining <= 0)
	);
}

// Spent means both meters are gone: the energy allowance (in overage) and the
// credits that overage draws on.
export function isNeuralWattQuotaSpent(q: NeuralWattQuotaLike): boolean {
	const remaining = q.balance?.credits_remaining_usd;
	return (
		q.subscription?.in_overage === true &&
		remaining != null &&
		remaining < NEURALWATT_CREDITS_SPENT_FLOOR_USD
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
