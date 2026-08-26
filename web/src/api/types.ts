// The dashboard's API payload types. Declared by domain under ./types/* and
// re-exported here so app code keeps importing every API type from one place
// (the same reason the shared quota types below are re-exported).

export * from "./types/auth";
export * from "./types/chat";
export * from "./types/failover";
export * from "./types/logs";
export * from "./types/models";
export * from "./types/providers";
export * from "./types/quota";
export * from "./types/settings";
export * from "./types/stats";

// ── Kimi Code + MiniMax quota ───────────────────────────────────
// Declared in web-shared/quota, which both this dashboard and Front Desk parse
// these payloads with, and re-exported here so app code keeps importing every
// API type from one place.

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
} from "@web-shared/quota";
