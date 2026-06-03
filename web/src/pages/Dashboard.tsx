import {
	Activity,
	AlertTriangle,
	ArrowUpRight,
	Bot,
	Clock,
	Gauge as GaugeIcon,
	Hash,
	LayoutDashboard,
	PlugZap,
	RefreshCw,
	ShieldAlert,
	Target,
	Timer,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import { LoadingSpinner } from "../components/LoadingSpinner";
import { ModelDetailModal } from "../components/ModelDetailPanel";
import { PageHeader } from "../components/PageHeader";
import { formatCompact, formatWithCommas } from "../utils/format";
import { Gauge } from "./Dashboard/Gauge";
import { GaugeModal } from "./Dashboard/GaugeModal";
import { ProviderDoughnut } from "./Dashboard/ProviderDoughnut";
import { StatCard } from "./Dashboard/StatCard";
import { TimeSeriesChart } from "./Dashboard/TimeSeriesChart";
import { MetricToggle, RangeToggle } from "./Dashboard/ToggleGroup";
import { TokenSplitBar } from "./Dashboard/TokenSplitBar";
import { UsageBarPanel } from "./Dashboard/UsageBarPanel";
import { useDashboard } from "./Dashboard/useDashboard";

/* =====================================================
   DASHBOARD
   ===================================================== */
export function Dashboard() {
	const { t } = useTranslation();
	const {
		// Global state
		globalRange,
		setGlobalRange,
		globalMetric,
		setGlobalMetric,
		excludeDeleted,
		setExcludeDeleted,

		// Per-section state
		requestsChartRange,
		setRequestsChartRange,
		tokensChartRange,
		setTokensChartRange,
		doughnutRange,
		setDoughnutRange,
		doughnutMetric,
		setDoughnutMetric,
		tokenRange,
		setTokenRange,
		modelsRange,
		setModelsRange,
		modelsMetric,
		setModelsMetric,
		providersRange,
		setProvidersRange,
		providersMetric,
		setProvidersMetric,
		virtualKeysRange,
		setVirtualKeysRange,
		virtualKeysMetric,
		setVirtualKeysMetric,

		// Modal state
		overheadModalOpen,
		setOverheadModalOpen,
		errorModalOpen,
		setErrorModalOpen,
		latencyModalOpen,
		setLatencyModalOpen,
		ttftModalOpen,
		setTtftModalOpen,
		rateLimitModalOpen,
		setRateLimitModalOpen,
		requestsModalOpen,
		setRequestsModalOpen,
		tokensModalOpen,
		setTokensModalOpen,
		detailModel,
		setDetailModel,

		// Refresh state
		isRefreshing,

		// Loading states
		statsLoading,
		modelsLoading,
		providersLoading,
		tsDataLoading,
		tokenTsDataLoading,
		provDistLoading,
		modelsUsageLoading,
		providersUsageLoading,
		vkeysUsageLoading,
		tokenStatsLoading,

		// Error states
		statsError,

		// Query data
		stats,
		models,
		providers,
		provDist,
		tokenStats,

		// Handlers
		handleRefresh,
		handleModelClick,

		// Computed values
		hideManualRefresh,
		totalTokens,
		rangeLabel,
		gaugeRequestCount,
		acData,
		tokenAcData,
		byModel,
		byProvider,
		byVK,
		accents,
	} = useDashboard();

	if (!stats && statsLoading) {
		return <LoadingSpinner />;
	}

	if (!stats && statsError) {
		return (
			<div className="space-y-6">
				<div>
					<h1 className="text-2xl font-bold text-(--text-primary)">
						{t("dashboard.title")}
					</h1>
					<p className="text-gray-400">{t("dashboard.description")}</p>
				</div>
				<div className="bg-red-900/50 border border-red-700 rounded-lg p-6 text-red-300">
					{t("dashboard.failedToLoadGaugeStats")}: {statsError.message}
				</div>
			</div>
		);
	}

	return (
		<div className="space-y-6">
			{/* Page header */}
			<PageHeader
				icon={LayoutDashboard}
				title={t("dashboard.title")}
				description={t("dashboard.description")}
				badge={
					<>
						<button
							type="button"
							onClick={() => setExcludeDeleted(!excludeDeleted)}
							title={
								excludeDeleted
									? t("dashboard.activeKeysOnly")
									: t("dashboard.allKeys")
							}
							aria-label={t("dashboard.toggleKeyFilter")}
							className={`flex items-center gap-1 px-1.5 py-px leading-[1.6] rounded text-[10px] font-semibold transition-colors ${
								excludeDeleted
									? "bg-amber-500/20 text-amber-400 hover:bg-amber-500/30"
									: "bg-green-500/20 text-green-400 hover:bg-green-500/30"
							}`}
						>
							<span
								className={`w-1.5 h-1.5 rounded-full transition-colors ${
									excludeDeleted ? "bg-amber-400" : "bg-green-400"
								}`}
							/>
							<span className="badge-dot-text">
								{excludeDeleted
									? t("dashboard.activeKeysOnly")
									: t("dashboard.allKeys")}
							</span>
						</button>
						<div className="flex items-center gap-1 ml-1.5">
							<RangeToggle value={globalRange} onChange={setGlobalRange} />
							<MetricToggle value={globalMetric} onChange={setGlobalMetric} />
						</div>
						{!hideManualRefresh && (
							<button
								type="button"
								onClick={handleRefresh}
								disabled={isRefreshing}
								title={
									isRefreshing
										? t("dashboard.refreshing")
										: t("dashboard.refresh")
								}
								aria-label={t("dashboard.refresh")}
								className={`flex items-center justify-center w-7 h-7 rounded-full transition-all ${
									isRefreshing
										? "cursor-not-allowed opacity-60"
										: "cursor-pointer hover:bg-(--accent)/20 hover:drop-shadow-[var(--glow-accent-lg)]"
								}`}
							>
								<RefreshCw
									size={14}
									className={`text-(--text-muted) ${isRefreshing ? "animate-spin" : ""}`}
								/>
							</button>
						)}
					</>
				}
				actions={
					<div className="flex gap-4">
						{statsLoading ? (
							<div className="flex items-center justify-center gap-2 py-4 text-sm text-(--text-muted)">
								{t("dashboard.loadingGauges")}
							</div>
						) : statsError ? (
							<div className="flex items-center justify-center gap-2 py-4 text-sm text-red-400">
								<AlertTriangle size={14} />{" "}
								<span>{t("dashboard.gaugeLoadFailed")}</span>
							</div>
						) : (
							<>
								<Gauge
									label={t("dashboard.chart.requestsOver", {
										range: rangeLabel,
									})}
									value={gaugeRequestCount}
									decimals={0}
									suffix=""
									color={accents.requests}
									onClick={() => setRequestsModalOpen(true)}
									tooltip={t("dashboard.gauge.viewRequestHistory")}
									maxScale={Math.max(100, gaugeRequestCount * 1.2)}
								/>
								<Gauge
									label={t("dashboard.chart.avgTtftOver", {
										range: rangeLabel,
									})}
									value={(stats?.avg_ttft_ms || 0) / 1000}
									decimals={1}
									suffix="s"
									color={accents.latency}
									onClick={() => setTtftModalOpen(true)}
									tooltip={t("dashboard.gauge.viewTtftHistory")}
									maxScale={Math.max(
										1,
										((stats?.avg_ttft_ms || 0) / 1000) * 1.5,
									)}
								/>
								<Gauge
									label={t("dashboard.chart.avgOverheadOver", {
										range: rangeLabel,
									})}
									value={stats?.avg_overhead_ms || 0}
									decimals={1}
									suffix="ms"
									color={accents.overhead}
									onClick={() => setOverheadModalOpen(true)}
									tooltip={t("dashboard.gauge.viewOverheadHistory")}
									maxScale={Math.max(100, (stats?.avg_overhead_ms || 0) * 1.5)}
								/>
								<Gauge
									label={t("dashboard.chart.rateLimitHitsOver", {
										range: rangeLabel,
									})}
									value={stats?.rate_limit_hits || 0}
									decimals={0}
									suffix=""
									color="#a855f7"
									onClick={() => setRateLimitModalOpen(true)}
									tooltip={t("dashboard.gauge.viewRateLimitHistory")}
									maxScale={Math.max(10, (stats?.rate_limit_hits || 0) * 1.5)}
								/>
								<Gauge
									label={t("dashboard.chart.errorRateOver", {
										range: rangeLabel,
									})}
									value={(stats?.error_rate || 0) * 100}
									decimals={1}
									suffix="%"
									color={accents.errors}
									onClick={() => setErrorModalOpen(true)}
									tooltip={t("dashboard.gauge.viewErrorRateHistory")}
								/>
							</>
						)}
					</div>
				}
			/>
			{/* Stat cards */}

			<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-6 gap-4">
				<StatCard
					label={t("dashboard.stats.totalProviders")}
					value={providers?.length || 0}
					icon={PlugZap}
					accent={accents.providers}
					loading={providersLoading}
				/>
				<StatCard
					label={t("dashboard.stats.totalModels")}
					value={models?.length || 0}
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
						globalMetric === "tokens"
							? ""
							: t("dashboard.label.requestsPerQuery")
					}
					icon={globalMetric === "tokens" ? Hash : Target}
					accent={accents.tokens}
					formatter={globalMetric === "tokens" ? formatCompact : undefined}
					onClick={() => setTokensModalOpen(true)}
					tooltip={t("dashboard.gauge.viewTokenHistory")}
				/>
			</div>

			{/* Time-series charts row - selected metric renders first */}
			<div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
				{globalMetric === "requests" ? (
					<>
						<TimeSeriesChart
							data={acData}
							range={requestsChartRange}
							onRangeChange={setRequestsChartRange}
							metric="Requests"
							icon={Activity}
							color={accents.requests}
							label={t("dashboard.label.requests")}
							dataKey="total"
							loading={tsDataLoading}
						/>
						<TimeSeriesChart
							data={tokenAcData}
							range={tokensChartRange}
							onRangeChange={setTokensChartRange}
							metric="Tokens"
							icon={Hash}
							color={accents.tokens}
							label={t("dashboard.label.tokens")}
							dataKey="tokens"
							overlayDataKey="tokens_cache_hit"
							overlayColor="var(--accent)"
							overlayLabel={t("dashboard.chart.cacheHit")}
							loading={tokenTsDataLoading}
						/>
					</>
				) : (
					<>
						<TimeSeriesChart
							data={tokenAcData}
							range={tokensChartRange}
							onRangeChange={setTokensChartRange}
							metric="Tokens"
							icon={Hash}
							color={accents.tokens}
							label={t("dashboard.label.tokens")}
							dataKey="tokens"
							overlayDataKey="tokens_cache_hit"
							overlayColor="var(--accent)"
							overlayLabel={t("dashboard.chart.cacheHit")}
							loading={tokenTsDataLoading}
						/>
						<TimeSeriesChart
							data={acData}
							range={requestsChartRange}
							onRangeChange={setRequestsChartRange}
							metric="Requests"
							icon={Activity}
							color={accents.requests}
							label={t("dashboard.label.requests")}
							dataKey="total"
							loading={tsDataLoading}
						/>
					</>
				)}
			</div>

			{/* Charts row: doughnut + token split */}
			<div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
				<ProviderDoughnut
					items={provDist?.items || []}
					range={doughnutRange}
					onRangeChange={setDoughnutRange}
					metric={doughnutMetric}
					onMetricChange={setDoughnutMetric}
					loading={provDistLoading}
				/>
				<TokenSplitBar
					prompt={tokenStats?.total_tokens_prompt || 0}
					completion={tokenStats?.total_tokens_completion || 0}
					cacheHit={tokenStats?.total_tokens_cache_hit || 0}
					total={
						(tokenStats?.total_tokens_prompt || 0) +
						(tokenStats?.total_tokens_completion || 0)
					}
					range={tokenRange}
					onRangeChange={setTokenRange}
					loading={tokenStatsLoading}
				/>
			</div>

			{/* Bottom row: three usage panels with horizontal bars */}
			<div className="grid grid-cols-1 lg:grid-cols-3 gap-6">
				<UsageBarPanel
					title={t("dashboard.models.top")}
					icon={ArrowUpRight}
					entries={byModel}
					range={modelsRange}
					onRangeChange={setModelsRange}
					metric={modelsMetric}
					onMetricChange={setModelsMetric}
					loading={modelsUsageLoading}
					onEntryClick={handleModelClick}
				/>
				<UsageBarPanel
					title={t("dashboard.providers.top")}
					icon={ArrowUpRight}
					entries={byProvider}
					range={providersRange}
					onRangeChange={setProvidersRange}
					metric={providersMetric}
					onMetricChange={setProvidersMetric}
					loading={providersUsageLoading}
				/>
				<UsageBarPanel
					title={t("dashboard.virtualKeys.top")}
					icon={ArrowUpRight}
					entries={byVK}
					range={virtualKeysRange}
					onRangeChange={setVirtualKeysRange}
					metric={virtualKeysMetric}
					onMetricChange={setVirtualKeysMetric}
					loading={vkeysUsageLoading}
				/>
			</div>

			<GaugeModal
				open={overheadModalOpen}
				onClose={() => setOverheadModalOpen(false)}
				title={t("dashboard.modal.avgOverhead")}
				metric={t("dashboard.gauge.modal.overheadMetric")}
				icon={Clock}
				color={accents.overhead}
				dataKey="overhead_ms"
				label={t("dashboard.gauge.modal.overheadLabel")}
			/>
			<GaugeModal
				open={errorModalOpen}
				onClose={() => setErrorModalOpen(false)}
				title={t("dashboard.modal.errorRate")}
				metric="Errors"
				icon={AlertTriangle}
				color={accents.errors}
				dataKey="errors"
				label={t("dashboard.label.errors")}
			/>
			<GaugeModal
				open={latencyModalOpen}
				onClose={() => setLatencyModalOpen(false)}
				title={t("dashboard.modal.avgDuration")}
				metric={t("dashboard.gauge.modal.durationMetric")}
				icon={Timer}
				color={accents.latency}
				dataKey="latency"
				label={t("dashboard.label.seconds")}
			/>
			<GaugeModal
				open={requestsModalOpen}
				onClose={() => setRequestsModalOpen(false)}
				title={t("dashboard.modal.requests")}
				metric={t("dashboard.gauge.modal.requestsMetric")}
				icon={Activity}
				color={accents.requests}
				dataKey="total"
				label={t("dashboard.gauge.modal.requestsLabel")}
				allowDecimals={false}
			/>
			<GaugeModal
				open={ttftModalOpen}
				onClose={() => setTtftModalOpen(false)}
				title={t("dashboard.modal.avgTtft")}
				metric={t("dashboard.gauge.modal.ttftMetric")}
				icon={GaugeIcon}
				color={accents.latency}
				dataKey="avg_ttft_ms"
				label={t("dashboard.label.seconds")}
				scale={0.001}
			/>
			<GaugeModal
				open={rateLimitModalOpen}
				onClose={() => setRateLimitModalOpen(false)}
				title={t("dashboard.modal.rateLimitHits")}
				metric={t("dashboard.gauge.modal.rateLimitHitsMetric")}
				icon={ShieldAlert}
				color="#a855f7"
				dataKey="rate_limit_hits"
				label={t("dashboard.gauge.modal.rateLimitHitsLabel")}
				allowDecimals={false}
			/>
			<GaugeModal
				open={tokensModalOpen}
				onClose={() => setTokensModalOpen(false)}
				title={t("dashboard.modal.avgTokens")}
				metric={t("dashboard.gauge.modal.tokensMetric")}
				icon={Hash}
				color={accents.tokens}
				dataKey="tokens"
				label={t("dashboard.gauge.modal.tokensLabel")}
				allowDecimals={false}
				overlayDataKey="tokens_cache_hit"
				overlayColor="var(--accent)"
				overlayLabel={t("dashboard.chart.cacheHit")}
			/>

			{detailModel && (
				<ModelDetailModal
					model={detailModel}
					onClose={() => setDetailModel(null)}
				/>
			)}
		</div>
	);
}
