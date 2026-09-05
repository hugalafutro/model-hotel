import { useTranslation } from "react-i18next";
import {
	Activity,
	AlertTriangle,
	Bot,
	Clock,
	Hash,
	PlugZap,
	Target,
} from "@/lib/icons";
import { formatCompact, formatWithCommas } from "../../utils/format";
import { StatCard } from "./StatCard";
import type { UseDashboardReturn } from "./useDashboard";

type StatCardsRowProps = Pick<
	UseDashboardReturn,
	| "globalRange"
	| "globalMetric"
	| "rangeLabel"
	| "totalTokens"
	| "accents"
	| "stats"
	| "models"
	| "providers"
	| "modelsLoading"
	| "providersLoading"
	| "setRequestsModalOpen"
	| "setErrorModalOpen"
	| "setLatencyModalOpen"
	| "setTokensModalOpen"
>;

/** The six headline tiles under the dashboard header. */
export function StatCardsRow({
	globalRange,
	globalMetric,
	rangeLabel,
	totalTokens,
	accents,
	stats,
	models,
	providers,
	modelsLoading,
	providersLoading,
	setRequestsModalOpen,
	setErrorModalOpen,
	setLatencyModalOpen,
	setTokensModalOpen,
}: StatCardsRowProps) {
	const { t } = useTranslation();
	// The provider/model pills count what the proxy can serve right now:
	// enabled providers, and models that are enabled AND whose provider is
	// enabled (the /v1/models rule the Models page title uses). That subsumes
	// the excludeDeleted pre-filter in useDashboard, which still matters for
	// the other queries riding the same flag.
	return (
		<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-6 gap-4">
			<StatCard
				label={t("dashboard.stats.totalProviders")}
				value={providers?.filter((p) => p.enabled).length ?? 0}
				icon={PlugZap}
				accent={accents.providers}
				loading={providersLoading}
			/>
			<StatCard
				label={t("dashboard.stats.totalModels")}
				value={
					models?.filter((m) => m.enabled && m.provider_enabled).length ?? 0
				}
				icon={Bot}
				accent={accents.models}
				loading={modelsLoading}
			/>
			<StatCard
				label={t("dashboard.chart.requestsOver", { range: rangeLabel })}
				value={
					globalRange === "1h"
						? stats?.requests_last_1h || 0
						: globalRange === "24h"
							? stats?.total_requests_last_24h || 0
							: stats?.total_requests_last_7d || 0
				}
				icon={Activity}
				accent={accents.requests}
				formatter={formatWithCommas}
				onClick={() => setRequestsModalOpen(true)}
				tooltip={t("dashboard.gauge.viewRequestHistory")}
			/>
			<StatCard
				label={t("dashboard.chart.errorRateOver", { range: rangeLabel })}
				value={(stats?.error_rate || 0) * 100}
				decimals={1}
				suffix="%"
				icon={AlertTriangle}
				accent={accents.errors}
				onClick={() => setErrorModalOpen(true)}
				tooltip={t("dashboard.gauge.viewErrorRateHistory")}
			/>
			<StatCard
				label={t("dashboard.chart.avgDurationOver", { range: rangeLabel })}
				value={(stats?.avg_latency_ms || 0) / 1000}
				decimals={1}
				suffix="s"
				icon={Clock}
				accent={accents.latency}
				onClick={() => setLatencyModalOpen(true)}
				tooltip={t("dashboard.gauge.viewDurationHistory")}
			/>
			<StatCard
				label={
					globalMetric === "tokens"
						? t("dashboard.stats.totalTokens", { range: rangeLabel })
						: t("dashboard.stats.avgTokensPerReq")
				}
				value={
					globalMetric === "tokens"
						? totalTokens
						: stats?.avg_tokens_per_request || 0
				}
				decimals={0}
				suffix={
					globalMetric === "tokens" ? "" : t("dashboard.label.requestsPerQuery")
				}
				icon={globalMetric === "tokens" ? Hash : Target}
				accent={accents.tokens}
				formatter={globalMetric === "tokens" ? formatCompact : undefined}
				onClick={() => setTokensModalOpen(true)}
				tooltip={t("dashboard.gauge.viewTokenHistory")}
			/>
		</div>
	);
}
