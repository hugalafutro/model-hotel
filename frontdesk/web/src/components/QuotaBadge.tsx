import type { CSSProperties } from "react";
import { useTranslation } from "react-i18next";
import type {
	DeepSeekBalance,
	NanoGPTUsage,
	NeuralWattQuotaResponse,
	OllamaCloudAccount,
	OpenRouterBalance,
	ZAICodingQuotaResponse,
} from "../api/types";
import { formatDollars, formatKwh, formatTokens } from "../utils/format";
import {
	getKimiCodeFiveHourLimit,
	getKimiCodeWeeklyLimit,
	getMiniMaxFiveHourLimit,
	getMiniMaxWeeklyLimit,
	getZaiCodingFiveHourLimit,
	getZaiCodingWeeklyLimit,
	payloadOf,
	QUOTA_BRAND_COLORS,
	QUOTA_PREFIXES,
	type QuotaBadgeModel,
	type QuotaBarMode,
} from "../utils/quota";
import type { Translate } from "./quota/shared";

/** Renders one window percentage, or "-" when that window is not reported. */
function windowPct(pct: number | undefined, mode: QuotaBarMode): string {
	if (pct == null) return "-";
	return `${(mode === "remaining" ? 100 - pct : pct).toFixed(0)}%`;
}

/** The "5h/weekly" label shared by Z.ai, Kimi Code and MiniMax. */
function windowsLabel(
	fiveHour: number | undefined,
	weekly: number | undefined,
	mode: QuotaBarMode,
): string {
	return `${windowPct(fiveHour, mode)}/${windowPct(weekly, mode)}`;
}

interface BadgeContent {
	label: string;
	title: string;
}

function contentFor(
	model: QuotaBadgeModel,
	mode: QuotaBarMode,
	t: Translate,
): BadgeContent {
	const provider = model.providerName;
	const payload = payloadOf<object>(model.snapshot);

	if (payload === null) {
		// payloadOf() returns null for three different reasons and they need
		// different words, because two of the three are not failures at all:
		//
		//   204 - the fetch SUCCEEDED and the provider has no quota to report.
		//         internal/api/quota_snapshot.go:90 emits this for a NeuralWatt
		//         free-tier account (a nil result becomes 204 with a null body).
		//   200 - the fetch SUCCEEDED and returned a body we cannot use.
		//   else - the fetch genuinely failed (424 dead credential, 5xx, ...).
		//
		// Collapsing these onto the failure message produced "last fetch failed
		// (HTTP 204)" for a free-tier account that is working exactly as designed.
		const status = model.snapshot.http_status;
		if (status === 204) {
			return { label: "-", title: t("quota.badge.noQuota", { provider }) };
		}
		return {
			label: "-",
			title:
				status === 200
					? t("quota.badge.unreadable", { provider })
					: t("quota.badge.degraded", { provider, status }),
		};
	}

	switch (model.type) {
		case "nanogpt": {
			const u = payload as NanoGPTUsage;
			const limit = u.limits?.weeklyInputTokens ?? null;
			const used = u.weeklyInputTokens?.used ?? 0;
			const shown =
				mode === "remaining" ? Math.max(0, (limit ?? 0) - used) : used;
			return {
				label: `${formatTokens(shown)}/${formatTokens(limit)}`,
				title: t(
					mode === "remaining"
						? "quota.badge.nanogptRemaining"
						: "quota.badge.nanogptUsed",
					{ provider },
				),
			};
		}
		case "zai-coding": {
			const u = payload as ZAICodingQuotaResponse;
			return {
				label: windowsLabel(
					getZaiCodingFiveHourLimit(u)?.percentage,
					getZaiCodingWeeklyLimit(u)?.percentage,
					mode,
				),
				title: t(
					mode === "remaining"
						? "quota.badge.windowsRemaining"
						: "quota.badge.windowsUsed",
					{ provider },
				),
			};
		}
		case "kimi-code": {
			const u = payload as Parameters<typeof getKimiCodeFiveHourLimit>[0];
			return {
				label: windowsLabel(
					getKimiCodeFiveHourLimit(u)?.percentage,
					getKimiCodeWeeklyLimit(u)?.percentage,
					mode,
				),
				title: t(
					mode === "remaining"
						? "quota.badge.windowsRemaining"
						: "quota.badge.windowsUsed",
					{ provider },
				),
			};
		}
		case "minimax": {
			const u = payload as Parameters<typeof getMiniMaxFiveHourLimit>[0];
			return {
				label: windowsLabel(
					getMiniMaxFiveHourLimit(u)?.percentage,
					getMiniMaxWeeklyLimit(u)?.percentage,
					mode,
				),
				title: t(
					mode === "remaining"
						? "quota.badge.windowsRemaining"
						: "quota.badge.windowsUsed",
					{ provider },
				),
			};
		}
		case "deepseek": {
			const b = payload as DeepSeekBalance;
			const usd = b.balance_infos?.find(
				(i) => i.currency === "USD",
			)?.total_balance;
			return {
				label: `$${usd ?? "-"}`,
				title: t("quota.badge.deepseekBalance", { provider }),
			};
		}
		case "openrouter": {
			const b = payload as OpenRouterBalance;
			return {
				label: formatDollars(b.credits_remaining ?? 0),
				title: t("quota.badge.openrouterBalance", { provider }),
			};
		}
		case "ollama-cloud": {
			const a = payload as OllamaCloudAccount;
			return {
				label: a.plan || "-",
				title: t("quota.badge.ollamaCloudPlan", { provider }),
			};
		}
		case "neuralwatt": {
			const q = payload as NeuralWattQuotaResponse;
			const used = q.subscription?.kwh_used ?? 0;
			const included = q.subscription?.kwh_included ?? 0;
			// In overage the kwh_used counter freezes at the included amount and
			// the spend moves to the credit balance, so the frozen kWh label
			// alone would read as "nothing is happening"; the tooltip says so.
			// No dollar figure: NeuralWatt exposes no cumulative draw (see
			// getNeuralWattCreditsSpent).
			const title = q.subscription?.in_overage
				? t("quota.badge.neuralwattEnergyOverage", { provider })
				: t("quota.badge.neuralwattEnergy", { provider });
			return {
				label:
					included > 0
						? `${formatKwh(used)}/${formatKwh(included)} kWh`
						: `${formatKwh(used)} kWh`,
				title,
			};
		}
	}
}

export interface QuotaBadgeProps {
	model: QuotaBadgeModel;
	barMode: QuotaBarMode;
	onClick: () => void;
}

/**
 * One quota pill. The accent colour rides in as a CSS custom property so a
 * single `.fd-quota-pill` rule covers every provider and both themes, rather
 * than one hardcoded rule per brand.
 */
export function QuotaBadge({ model, barMode, onClick }: QuotaBadgeProps) {
	const { t } = useTranslation();
	const { label, title } = contentFor(model, barMode, t);
	// A degraded badge deliberately gets NO inline custom property: an inline
	// declaration outranks any author rule, custom properties included, so
	// emitting one here would make `.fd-quota-pill-degraded { --quota-brand: ... }`
	// dead and a failed provider would render in full brand colour, visually
	// identical to a healthy one. Leaving it off lets the CSS rule be the single
	// source of the degraded colour.
	const style = model.degraded
		? undefined
		: ({ "--quota-brand": QUOTA_BRAND_COLORS[model.type] } as CSSProperties);

	return (
		<button
			type="button"
			data-testid={`quota-badge-${model.key}`}
			className={`fd-quota-pill${model.degraded ? " fd-quota-pill-degraded" : ""}`}
			style={style}
			onClick={onClick}
			title={title}
		>
			<span className="fd-quota-pill-prefix">{QUOTA_PREFIXES[model.type]}</span>
			{model.showProviderName && (
				<span className="fd-quota-pill-name">{model.providerName}</span>
			)}
			<span className="fd-quota-pill-value">{label}</span>
		</button>
	);
}
