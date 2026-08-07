import { getKimiCodeFiveHourLimit, getKimiCodeWeeklyLimit } from "./kimi";
import { getMiniMaxGeneralEntry } from "./minimax";
import type {
	DeepSeekBalanceLike,
	KimiCodeQuotaResponse,
	MiniMaxQuotaResponse,
	NanoGptUsageLike,
	NeuralWattQuotaLike,
	OllamaCloudAccountLike,
	OpenRouterBalanceLike,
	QuotaProviderType,
	ZaiCodingResponseLike,
} from "./types";
import { getZaiCodingFiveHourLimit, getZaiCodingWeeklyLimit } from "./zai";

// Whether a readable payload holds anything worth putting on a badge. Both
// frontends gate on these, so the same fleet shows the same set of badges in
// each. Whether the provider exists at all, and whether its fetch succeeded,
// are the app's own questions and stay there.

/** Subscription plans too low-tier to be worth a badge. */
const NEURALWATT_EXCLUDED_PLANS = new Set(["free", "starter"]);

export function isNanoGptQuotaVisible(u: NanoGptUsageLike): boolean {
	const cancelled =
		u.providerStatus === "canceled" || u.providerStatus === "cancelled";
	return (
		!cancelled &&
		u.weeklyInputTokens?.used != null &&
		Boolean(u.limits?.weeklyInputTokens)
	);
}

export function isZaiCodingQuotaVisible(u: ZaiCodingResponseLike): boolean {
	return (
		u.success === true &&
		Boolean(getZaiCodingFiveHourLimit(u) || getZaiCodingWeeklyLimit(u))
	);
}

export function isKimiCodeQuotaVisible(u: KimiCodeQuotaResponse): boolean {
	return Boolean(getKimiCodeFiveHourLimit(u) || getKimiCodeWeeklyLimit(u));
}

export function isMiniMaxQuotaVisible(u: MiniMaxQuotaResponse): boolean {
	return Boolean(getMiniMaxGeneralEntry(u));
}

export function isDeepSeekQuotaVisible(b: DeepSeekBalanceLike): boolean {
	return b.is_available === true;
}

export function isOpenRouterQuotaVisible(b: OpenRouterBalanceLike): boolean {
	return b.credits_remaining != null;
}

export function isOllamaCloudQuotaVisible(a: OllamaCloudAccountLike): boolean {
	return a.suspended_at?.valid !== true;
}

export function isNeuralWattQuotaVisible(q: NeuralWattQuotaLike): boolean {
	return (
		q.balance?.credits_remaining_usd != null &&
		!NEURALWATT_EXCLUDED_PLANS.has(q.subscription?.plan?.toLowerCase() ?? "")
	);
}

/**
 * Visibility for a payload whose provider type is known only as a value, which
 * is how Front Desk receives it: the fleet primary stamps the type on every
 * snapshot rather than the SPA sniffing a base URL.
 */
export function isQuotaPayloadVisible(
	type: QuotaProviderType,
	payload: object,
): boolean {
	switch (type) {
		case "nanogpt":
			return isNanoGptQuotaVisible(payload as NanoGptUsageLike);
		case "zai-coding":
			return isZaiCodingQuotaVisible(payload as ZaiCodingResponseLike);
		case "kimi-code":
			return isKimiCodeQuotaVisible(payload as KimiCodeQuotaResponse);
		case "minimax":
			return isMiniMaxQuotaVisible(payload as MiniMaxQuotaResponse);
		case "deepseek":
			return isDeepSeekQuotaVisible(payload as DeepSeekBalanceLike);
		case "openrouter":
			return isOpenRouterQuotaVisible(payload as OpenRouterBalanceLike);
		case "ollama-cloud":
			return isOllamaCloudQuotaVisible(payload as OllamaCloudAccountLike);
		case "neuralwatt":
			return isNeuralWattQuotaVisible(payload as NeuralWattQuotaLike);
	}
}
