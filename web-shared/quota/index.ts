// Quota payload parsing shared by the Model Hotel dashboard (web/) and Front
// Desk (frontdesk/web/). Pure TypeScript: no React, no i18next, no imports from
// either app. Presentation (pill prefixes, brand colours, modals, reset labels)
// stays in the app that renders it.
//
// Both apps reach this module as `@web-shared/quota`, through the `@web-shared`
// prefix alias wired in their tsconfig `paths`, vite and vitest configs.

export {
	getKimiCodeFiveHourLimit,
	getKimiCodeWeeklyLimit,
	toKimiCodeWindow,
} from "./kimi";
export {
	getMiniMaxFiveHourLimit,
	getMiniMaxGeneralEntry,
	getMiniMaxWeeklyLimit,
} from "./minimax";
export type {
	DeepSeekBalanceLike,
	KimiCodeQuotaLimitEntry,
	KimiCodeQuotaResponse,
	KimiCodeQuotaUsageWindow,
	KimiCodeQuotaWindow,
	KimiCodeQuotaWindowSpec,
	MiniMaxBaseResp,
	MiniMaxModelRemains,
	MiniMaxQuotaResponse,
	MiniMaxQuotaWindow,
	NanoGptUsageLike,
	NeuralWattQuotaLike,
	OllamaCloudAccountLike,
	OpenRouterBalanceLike,
	QuotaProviderType,
	ZaiCodingLimitLike,
	ZaiCodingResponseLike,
} from "./types";
export {
	isDeepSeekQuotaVisible,
	isKimiCodeQuotaVisible,
	isMiniMaxQuotaVisible,
	isNanoGptQuotaVisible,
	isNeuralWattQuotaVisible,
	isOllamaCloudQuotaVisible,
	isOpenRouterQuotaVisible,
	isQuotaPayloadVisible,
	isZaiCodingQuotaVisible,
} from "./visibility";
export {
	getZaiCodingFiveHourLimit,
	getZaiCodingWeeklyLimit,
} from "./zai";
