import { isQuotaPayloadVisible } from "@quota-shared";
import type { QuotaProviderType, QuotaSnapshot } from "../api/types";

// Front Desk's quota glue. The payload parsing itself lives in
// web-shared/quota, which the Model Hotel dashboard reads the same payloads
// with, so both frontends derive the same numbers for the same fleet. What
// stays here is what only Front Desk needs: snapshot handling, badge models and
// the pill's own presentation. The shared module deliberately carries no
// equivalent of detectQuotaProviderType: the member export stamps `type` on
// every snapshot, so Front Desk never sniffs a base URL.

// The parsing helpers are re-exported because Front Desk components have always
// reached them through this module.
export {
	getKimiCodeFiveHourLimit,
	getKimiCodeWeeklyLimit,
	getMiniMaxFiveHourLimit,
	getMiniMaxGeneralEntry,
	getMiniMaxWeeklyLimit,
	getNeuralWattCreditsSpent,
	getZaiCodingFiveHourLimit,
	getZaiCodingWeeklyLimit,
} from "@quota-shared";

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
] as const satisfies readonly QuotaProviderType[];

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

// ── Badge models ─────────────────────────────────────────────────────────

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
		if (!degraded && !isQuotaPayloadVisible(s.type, payload)) continue;
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
