// Quota payload parsing shared by the Model Hotel dashboard (web/) and Front
// Desk (frontdesk/web/). Pure TypeScript: no React, no i18next, no imports from
// either app. Presentation stays in the app that renders it, apart from the two
// values both apps spell identically (pill prefixes and the window-percentage
// label, in display.ts): brand colours, modal shells and reset labels differ
// per app and live there.
//
// Both apps reach this module as `@web-shared/quota`, through the `@web-shared`
// prefix alias wired in their tsconfig `paths`, vite and vitest configs.

export { QUOTA_PREFIXES, windowPct } from "./display";
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
export {
	getOpenCodeGoWindows,
	type OpenCodeGoWindow,
	type OpenCodeGoWindowKey,
} from "./opencodeGo";
export {
	isDeepSeekQuotaSpent,
	isKimiCodeQuotaSpent,
	isMiniMaxQuotaSpent,
	isNanoGptQuotaSpent,
	isNeuralWattQuotaSpent,
	isOpenCodeGoQuotaSpent,
	isOpenRouterQuotaSpent,
	isQuotaPayloadSpent,
	isZaiCodingQuotaSpent,
} from "./spent";
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
	OpenCodeGoUsageResponse,
	OpenCodeGoUsageWindow,
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
	isOpenCodeGoQuotaVisible,
	isOpenRouterQuotaVisible,
	isQuotaPayloadVisible,
	isZaiCodingQuotaVisible,
} from "./visibility";
export {
	getZaiCodingFiveHourLimit,
	getZaiCodingMcpLimit,
	getZaiCodingWeeklyLimit,
} from "./zai";
