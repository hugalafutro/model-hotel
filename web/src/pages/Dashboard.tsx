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

	if (statsError) {
		return (
			<div className="space-y-6">
				<div>
					<h1 className="text-2xl font-bold text-(--text-primary)">
						Dashboard
					</h1>
					<p className="text-gray-400">Overview of your Model Hotel usage</p>
				</div>
				<div className="bg-red-900/50 border border-red-700 rounded-lg p-6 text-red-300">
					Failed to load stats: {statsError.message}
				</div>
			</div>
		);
	}

	return (
		<div className="space-y-6">
			{/* Page header */}
			<PageHeader
				icon={LayoutDashboard}
				title="Dashboard"
				description="Overview of your Model Hotel usage"
				badge={
					<>
						<button
							type="button"
							onClick={() => setExcludeDeleted(!excludeDeleted)}
							title={
								excludeDeleted
									? "Showing only active (non-deleted) virtual keys. Click to include deleted keys in stats."
									: "Showing all virtual keys including deleted ones. Click to filter to active keys only."
							}
							aria-label="Toggle key filter"
							className={`flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-semibold transition-colors ${
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
							{excludeDeleted ? "Active Keys Only" : "All Keys"}
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
								title={isRefreshing ? "Refreshing…" : "Refresh dashboard"}
								aria-label="Refresh dashboard"
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
								Loading gauges…
							</div>
						) : statsError ? (
							<div className="flex items-center justify-center gap-2 py-4 text-sm text-red-400">
								<AlertTriangle size={14} />{" "}
								<span>Failed to load gauge stats</span>
							</div>
						) : (
							<>
								<Gauge
									label={`Requests/${rangeLabel}`}
									value={gaugeRequestCount}
									decimals={0}
									suffix=""
									color={accents.requests}
									onClick={() => setRequestsModalOpen(true)}
									tooltip="Click to view request history"
									maxScale={Math.max(100, gaugeRequestCount * 1.2)}
								/>
								<Gauge
									label={`Avg TTFT/${rangeLabel}`}
									value={(stats?.avg_ttft_ms || 0) / 1000}
									decimals={1}
									suffix="s"
									color={accents.latency}
									onClick={() => setTtftModalOpen(true)}
									tooltip="Click to view TTFT history"
									maxScale={Math.max(
										1,
										((stats?.avg_ttft_ms || 0) / 1000) * 1.5,
									)}
								/>
								<Gauge
									label={`Avg Overhead/${rangeLabel}`}
									value={stats?.avg_overhead_ms || 0}
									decimals={1}
									suffix="ms"
									color={accents.overhead}
									onClick={() => setOverheadModalOpen(true)}
									tooltip="Click to view overhead history"
									maxScale={Math.max(100, (stats?.avg_overhead_ms || 0) * 1.5)}
								/>
								<Gauge
									label={`Rate Limit Hits/${rangeLabel}`}
									value={stats?.rate_limit_hits || 0}
									decimals={0}
									suffix=""
									color="#a855f7"
									onClick={() => setRateLimitModalOpen(true)}
									tooltip="Click to view rate limit hit history"
									maxScale={Math.max(10, (stats?.rate_limit_hits || 0) * 1.5)}
								/>
								<Gauge
									label={`Error Rate/${rangeLabel}`}
									value={(stats?.error_rate || 0) * 100}
									decimals={1}
									suffix="%"
									color={accents.errors}
									onClick={() => setErrorModalOpen(true)}
									tooltip="Click to view error rate history"
								/>
							</>
						)}
					</div>
				}
			/>
			{/* Stat cards */}

			<div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-6 gap-4">
				<StatCard
					label="Total Providers"
					value={providers?.length || 0}
					icon={PlugZap}
					accent={accents.providers}
					loading={providersLoading}
				/>
				<StatCard
					label="Total Models"
					value={models?.length || 0}
					icon={Bot}
					accent={accents.models}
					loading={modelsLoading}
				/>
				<StatCard
					label={`Requests/${rangeLabel}`}
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
					tooltip="Click to view request history"
				/>
				<StatCard
					label={`Error Rate/${rangeLabel}`}
					value={(stats?.error_rate || 0) * 100}
					decimals={1}
					suffix="%"
					icon={AlertTriangle}
					accent={accents.errors}
					onClick={() => setErrorModalOpen(true)}
					tooltip="Click to view error rate history"
				/>
				<StatCard
					label={`Avg Duration/${rangeLabel}`}
					value={(stats?.avg_latency_ms || 0) / 1000}
					decimals={1}
					suffix="s"
					icon={Clock}
					accent={accents.latency}
					onClick={() => setLatencyModalOpen(true)}
					tooltip="Click to view duration history"
				/>
				<StatCard
					label={
						globalMetric === "tokens"
							? `Total Tokens/${rangeLabel}`
							: "Avg Tokens/Req"
					}
					value={
						globalMetric === "tokens"
							? totalTokens
							: stats?.avg_tokens_per_request || 0
					}
					decimals={0}
					suffix={globalMetric === "tokens" ? "" : "T/Rq"}
					icon={globalMetric === "tokens" ? Hash : Target}
					accent={accents.tokens}
					formatter={globalMetric === "tokens" ? formatCompact : undefined}
					onClick={() => setTokensModalOpen(true)}
					tooltip="Click to view token history"
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
							label="Requests"
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
							label="Tokens"
							dataKey="tokens"
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
							label="Tokens"
							dataKey="tokens"
							loading={tokenTsDataLoading}
						/>
						<TimeSeriesChart
							data={acData}
							range={requestsChartRange}
							onRangeChange={setRequestsChartRange}
							metric="Requests"
							icon={Activity}
							color={accents.requests}
							label="Requests"
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
					title="Top Models"
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
					title="Top Providers"
					icon={ArrowUpRight}
					entries={byProvider}
					range={providersRange}
					onRangeChange={setProvidersRange}
					metric={providersMetric}
					onMetricChange={setProvidersMetric}
					loading={providersUsageLoading}
				/>
				<UsageBarPanel
					title="Top Virtual Keys"
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
				title="Avg Overhead"
				metric="Overhead"
				icon={Clock}
				color={accents.overhead}
				dataKey="overhead_ms"
				label="ms"
			/>
			<GaugeModal
				open={errorModalOpen}
				onClose={() => setErrorModalOpen(false)}
				title="Error Rate"
				metric="Errors"
				icon={AlertTriangle}
				color={accents.errors}
				dataKey="errors"
				label="errors"
			/>
			<GaugeModal
				open={latencyModalOpen}
				onClose={() => setLatencyModalOpen(false)}
				title="Avg Duration"
				metric="Duration"
				icon={Timer}
				color={accents.latency}
				dataKey="latency"
				label="s"
			/>
			<GaugeModal
				open={requestsModalOpen}
				onClose={() => setRequestsModalOpen(false)}
				title="Requests"
				metric="Requests"
				icon={Activity}
				color={accents.requests}
				dataKey="total"
				label="requests"
				allowDecimals={false}
			/>
			<GaugeModal
				open={ttftModalOpen}
				onClose={() => setTtftModalOpen(false)}
				title="Avg TTFT"
				metric="TTFT"
				icon={GaugeIcon}
				color={accents.latency}
				dataKey="avg_ttft_ms"
				label="s"
				scale={0.001}
			/>
			<GaugeModal
				open={rateLimitModalOpen}
				onClose={() => setRateLimitModalOpen(false)}
				title="Rate Limit Hits"
				metric="Rate Limit Hits"
				icon={ShieldAlert}
				color="#a855f7"
				dataKey="rate_limit_hits"
				label="hits"
				allowDecimals={false}
			/>
			<GaugeModal
				open={tokensModalOpen}
				onClose={() => setTokensModalOpen(false)}
				title="Avg Tokens"
				metric="Tokens"
				icon={Hash}
				color={accents.tokens}
				dataKey="tokens"
				label="tokens"
				allowDecimals={false}
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
