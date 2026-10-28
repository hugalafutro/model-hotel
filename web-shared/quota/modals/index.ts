// The provider quota modal bodies both dashboards render. Each app wraps a body
// in its own chrome (modal, header buttons, timestamp) and hands it a `t`
// scoped to its own key prefix, through the QuotaModalKit.

export { KimiCodeQuotaModal } from "./KimiCodeQuotaModal";
export {
	type QuotaBodyProps,
	type QuotaModalKit,
	quotaRightText,
	resetSublabel,
	resetSublabelFromEpoch,
	type TimeFormat,
	type Translate,
} from "./labels";
export { MiniMaxQuotaModal } from "./MiniMaxQuotaModal";
export { NanoGPTQuotaModal } from "./NanoGPTQuotaModal";
export { NeuralWattQuotaModal } from "./NeuralWattQuotaModal";
export { OpenCodeGoQuotaModal } from "./OpenCodeGoQuotaModal";
export { OpenRouterQuotaModal } from "./OpenRouterQuotaModal";
export { QuotaBar, QuotaDetailGrid, QuotaDetailItem } from "./primitives";
export { ZAICodingQuotaModal } from "./ZAICodingQuotaModal";
