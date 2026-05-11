import type {
	DeepSeekBalance,
	DeepSeekBalanceInfo,
	NanoGPTUsage,
	OllamaCloudAccount,
	OpenRouterBalance,
	ZAICodingQuotaResponse,
} from "../api/types";
import type { QuotaDataResult, QuotaProviderType } from "../hooks/useQuotaData";
import {
	detectQuotaProviderType,
	getZaiCodingFiveHourLimit,
	getZaiCodingWeeklyLimit,
} from "../hooks/useQuotaData";
import { formatTokens } from "../utils/format";
import { PROVIDER_PREFIXES } from "../utils/providerBrands";

// ── Variant styling ────────────────────────────────────────────────────

/** Sidebar uses CSS class pills; provider card uses inline Tailwind. */
export type QuotaBadgeVariant = "sidebar" | "card";

const VARIANT_CLASSES: Record<QuotaBadgeVariant, string> = {
	sidebar: "",
	card: "px-2 py-1.5 rounded-full text-xs font-medium cursor-pointer transition-colors",
};

/** Type-safe prefix lookup - backed by the central brand map. */
const TYPE_PREFIX: Record<QuotaProviderType, string> = {
	nanogpt: PROVIDER_PREFIXES.nanogpt,
	"zai-coding": PROVIDER_PREFIXES["zai-coding"],
	deepseek: PROVIDER_PREFIXES.deepseek,
	openrouter: PROVIDER_PREFIXES.openrouter,
	"ollama-cloud": PROVIDER_PREFIXES["ollama-cloud"],
};

const TYPE_STYLES: Record<
	QuotaProviderType,
	Record<QuotaBadgeVariant, string>
> = {
	nanogpt: {
		sidebar: "sidebar-quota-pill sidebar-quota-pill-nanogpt",
		card: "bg-[#0EA5B0]/20 text-[#0EA5B0] border border-[#0EA5B0]/50 hover:bg-[#0EA5B0]/30",
	},
	"zai-coding": {
		sidebar: "sidebar-quota-pill sidebar-quota-pill-zai-coding",
		card: "bg-white/10 text-gray-200 border border-gray-400/50 hover:bg-white/15",
	},
	deepseek: {
		sidebar: "sidebar-quota-pill sidebar-quota-pill-deepseek",
		card: "bg-[#4D6BFE]/20 text-[#4D6BFE] border border-[#4D6BFE]/50 hover:bg-[#4D6BFE]/30",
	},
	openrouter: {
		sidebar: "sidebar-quota-pill sidebar-quota-pill-openrouter",
		card: "bg-[#6366F1]/20 text-[#6366F1] border border-[#6366F1]/50 hover:bg-[#6366F1]/30",
	},
	"ollama-cloud": {
		sidebar: "sidebar-quota-pill sidebar-quota-pill-ollama-cloud",
		card: "bg-white/10 text-gray-200 border border-gray-400/50 hover:bg-white/15",
	},
};

// ── Per-provider rendering logic ────────────────────────────────────────

type BadgeContent = { label: string; title: string };

function nanoBadgeContent(
	weeklyUsed: number | null | undefined,
	weeklyLimit: number | null | undefined,
): BadgeContent {
	return {
		label: `${formatTokens(weeklyUsed)}/${formatTokens(weeklyLimit)}`,
		title: "NanoGPT weekly token quota - click for details",
	};
}

function zaiCodingBadgeContent(
	usage: ZAICodingQuotaResponse | null | undefined,
): BadgeContent {
	const fiveHour = getZaiCodingFiveHourLimit(usage);
	const weekly = getZaiCodingWeeklyLimit(usage);
	const label = `${fiveHour ? `${(100 - fiveHour.percentage).toFixed(0)}%` : "-"}/${weekly ? `${(100 - weekly.percentage).toFixed(0)}%` : "-"}`;
	return { label, title: "Z.ai Coding Plan token quota - click for details" };
}

function deepseekBadgeContent(
	balance: DeepSeekBalance,
	variant: QuotaBadgeVariant,
	dataUpdatedAt?: number,
): BadgeContent {
	const usd = balance.balance_infos.find(
		(b: DeepSeekBalanceInfo) => b.currency === "USD",
	)?.total_balance;
	const label = variant === "sidebar" ? `$ ${usd ?? "-"}` : `${usd ?? "-"} USD`;
	const refreshed = dataUpdatedAt
		? ` - updated ${new Date(dataUpdatedAt).toLocaleTimeString()}`
		: "";
	return {
		label,
		title: `DeepSeek balance: $${usd ?? "?"} USD${refreshed} - click to refresh`,
	};
}

function openRouterBadgeContent(balance: OpenRouterBalance): BadgeContent {
	return {
		label: `$${balance.credits_remaining?.toFixed(2) ?? "-"}`,
		title: "OpenRouter key balance - click for details",
	};
}

function ollamaCloudBadgeContent(
	account: OllamaCloudAccount,
	dataUpdatedAt?: number,
): BadgeContent {
	const plan = account.plan || "unknown";
	const refreshed = dataUpdatedAt
		? ` - updated ${new Date(dataUpdatedAt).toLocaleTimeString()}`
		: "";
	let title = `Ollama Cloud ${plan} plan${refreshed} - click to update`;
	if (account.subscription_period_end?.valid) {
		title = `Ollama Cloud ${plan} plan (ends ${new Date(account.subscription_period_end.time).toLocaleDateString()})${refreshed} - click to update`;
	}
	return { label: plan, title };
}

// ── QuotaBadge component ────────────────────────────────────────────────

export interface QuotaBadgeProps {
	type: QuotaProviderType;
	variant: QuotaBadgeVariant;
	onClick?: () => void;
	title?: string;
	/** NanoGPT props */
	weeklyUsed?: number | null;
	weeklyLimit?: number | null;
	nanogptUsage?: NanoGPTUsage;
	/** Z.ai Coding props */
	zaiCodingUsage?: ZAICodingQuotaResponse | null;
	/** DeepSeek props */
	deepseekBalance?: DeepSeekBalance;
	/** OpenRouter props */
	openrouterBalance?: OpenRouterBalance;
	/** Ollama Cloud props */
	ollamaCloudAccount?: OllamaCloudAccount;
}

export function QuotaBadge({
	type,
	variant,
	onClick,
	title,
	weeklyUsed,
	weeklyLimit,
	zaiCodingUsage,
	deepseekBalance,
	openrouterBalance,
	ollamaCloudAccount,
}: QuotaBadgeProps) {
	const { label, title: defaultTitle } = (() => {
		switch (type) {
			case "nanogpt":
				return nanoBadgeContent(weeklyUsed, weeklyLimit);
			case "zai-coding":
				return zaiCodingBadgeContent(zaiCodingUsage);
			case "deepseek": {
				if (!deepseekBalance)
					return { label: "-", title: "DeepSeek balance unavailable" };
				return deepseekBadgeContent(deepseekBalance, variant);
			}
			case "openrouter": {
				if (!openrouterBalance)
					return { label: "-", title: "OpenRouter balance unavailable" };
				return openRouterBadgeContent(openrouterBalance);
			}
			case "ollama-cloud": {
				if (!ollamaCloudAccount)
					return { label: "-", title: "Ollama Cloud account unavailable" };
				return ollamaCloudBadgeContent(ollamaCloudAccount);
			}
		}
	})();

	const className = `${VARIANT_CLASSES[variant]} ${TYPE_STYLES[type][variant]}`;

	return (
		<button
			type="button"
			onClick={onClick}
			className={className}
			title={title ?? defaultTitle}
		>
			{variant === "sidebar" && (
				<span className="sidebar-quota-pill-prefix">{TYPE_PREFIX[type]}</span>
			)}
			{label}
		</button>
	);
}

// ── Convenience: render all visible badges ─────────────────────────────

interface QuotaBadgesProps {
	quotaData: QuotaDataResult;
	variant: QuotaBadgeVariant;
	/**
	 * When provided, only renders the badge matching this provider's type.
	 * Use on provider cards so each card only shows its own quota badge.
	 * Omit (or undefined) to show all visible badges - used by the sidebar panel.
	 */
	providerBaseUrl?: string;
	onNanoClick?: () => void;
	onZaiCodingClick?: () => void;
	onDeepseekClick?: () => void;
	onOpenRouterClick?: () => void;
	onOllamaCloudClick?: () => void;
}

/**
 * Renders visible quota badges for a given quota data result.
 * When `providerBaseUrl` is given, only the badge matching that provider's
 * type is rendered (for per-card use). Otherwise all visible badges render
 * (for sidebar use).
 */
export function QuotaBadges({
	quotaData,
	variant,
	providerBaseUrl,
	onNanoClick,
	onZaiCodingClick,
	onDeepseekClick,
	onOpenRouterClick,
	onOllamaCloudClick,
}: QuotaBadgesProps) {
	const scope = providerBaseUrl
		? detectQuotaProviderType(providerBaseUrl)
		: undefined;

	return (
		<>
			{quotaData.showNanoBadge &&
				quotaData.nanogptUsage &&
				(scope === undefined || scope === "nanogpt") && (
					<QuotaBadge
						type="nanogpt"
						variant={variant}
						weeklyUsed={quotaData.nanoWeeklyUsed}
						weeklyLimit={quotaData.nanoWeeklyLimit}
						onClick={onNanoClick}
					/>
				)}
			{quotaData.showZaiCodingBadge &&
				quotaData.zaiCodingUsage &&
				(scope === undefined || scope === "zai-coding") && (
					<QuotaBadge
						type="zai-coding"
						variant={variant}
						zaiCodingUsage={quotaData.zaiCodingUsage}
						onClick={onZaiCodingClick}
					/>
				)}
			{quotaData.showDsBadge &&
				quotaData.deepseekBalance &&
				(scope === undefined || scope === "deepseek") && (
					<QuotaBadge
						type="deepseek"
						variant={variant}
						deepseekBalance={quotaData.deepseekBalance}
						onClick={onDeepseekClick}
						title={
							deepseekBadgeContent(
								quotaData.deepseekBalance,
								variant,
								quotaData.deepseekDataUpdatedAt,
							).title
						}
					/>
				)}
			{quotaData.showOrBadge &&
				quotaData.openrouterBalance &&
				(scope === undefined || scope === "openrouter") && (
					<QuotaBadge
						type="openrouter"
						variant={variant}
						openrouterBalance={quotaData.openrouterBalance}
						onClick={onOpenRouterClick}
					/>
				)}
			{quotaData.showOllamaCloudBadge &&
				quotaData.ollamaCloudAccount &&
				(scope === undefined || scope === "ollama-cloud") && (
					<QuotaBadge
						type="ollama-cloud"
						variant={variant}
						ollamaCloudAccount={quotaData.ollamaCloudAccount}
						onClick={onOllamaCloudClick}
						title={
							ollamaCloudBadgeContent(
								quotaData.ollamaCloudAccount,
								quotaData.ollamaCloudDataUpdatedAt,
							).title
						}
					/>
				)}
		</>
	);
}
