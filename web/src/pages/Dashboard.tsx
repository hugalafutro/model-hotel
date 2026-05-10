import { useQuery, useQueryClient } from "@tanstack/react-query";
import {
	Activity,
	AlertTriangle,
	ArrowUpRight,
	Bot,
	Clock,
	Gauge as GaugeIcon,
	LayoutDashboard,
	PlugZap,
	RefreshCw,
	ShieldAlert,
	Target,
	Timer,
	Zap,
} from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api/client";
import type { MetricType, Model } from "../api/types";
import { LoadingSpinner } from "../components/LoadingSpinner";
import { ModelDetailModal } from "../components/ModelDetailPanel";
import { PageHeader } from "../components/PageHeader";
import { useToast } from "../context/ToastContext";
import { proxyModelID } from "../utils/model";
import { Gauge } from "./Dashboard/Gauge";
import { GaugeModal } from "./Dashboard/GaugeModal";
import { ProviderDoughnut } from "./Dashboard/ProviderDoughnut";
import { StatCard } from "./Dashboard/StatCard";
import { TimeSeriesChart } from "./Dashboard/TimeSeriesChart";
import { MetricToggle, RangeToggle } from "./Dashboard/ToggleGroup";
import { TokenSplitBar } from "./Dashboard/TokenSplitBar";
import type { Range } from "./Dashboard/types";
import { UsageBarPanel } from "./Dashboard/UsageBarPanel";
import { formatCompact } from "./Dashboard/utils";

/* =====================================================
   DASHBOARD
   ===================================================== */
export function Dashboard() {
	const [globalRange, setGlobalRange] = useState<Range>(() => {
		try {
			const v = localStorage.getItem("dashboardRange");
			if (v === "1h" || v === "24h" || v === "7d") return v;
		} catch {
			/* ignore */
		}
		return "24h";
	});
	const [globalMetric, setGlobalMetric] = useState<MetricType>(() => {
		try {
			const v = localStorage.getItem("dashboardMetric");
			if (v === "tokens" || v === "requests") return v;
		} catch {
			/* ignore */
		}
		return "tokens";
	});
	const [excludeDeleted, setExcludeDeleted] = useState(false);

	useEffect(() => {
		localStorage.setItem("dashboardRange", globalRange);
	}, [globalRange]);
	useEffect(() => {
		localStorage.setItem("dashboardMetric", globalMetric);
	}, [globalMetric]);

	// Per-section local states: synced from global header toggles,
	// but each component's own toggles only affect that section.
	// Per-section local states: synced from global header toggles,
	// but each component's own toggles only affect that section.
	const [requestsChartRange, setRequestsChartRange] =
		useState<Range>(globalRange);
	const [tokensChartRange, setTokensChartRange] = useState<Range>(globalRange);
	const [doughnutRange, setDoughnutRange] = useState<Range>(globalRange);
	const [doughnutMetric, setDoughnutMetric] =
		useState<MetricType>(globalMetric);
	const [tokenRange, setTokenRange] = useState<Range>(globalRange);
	const [modelsRange, setModelsRange] = useState<Range>(globalRange);
	const [modelsMetric, setModelsMetric] = useState<MetricType>(globalMetric);
	const [providersRange, setProvidersRange] = useState<Range>(globalRange);
	const [providersMetric, setProvidersMetric] =
		useState<MetricType>(globalMetric);
	const [virtualKeysRange, setVirtualKeysRange] = useState<Range>(globalRange);
	const [virtualKeysMetric, setVirtualKeysMetric] =
		useState<MetricType>(globalMetric);

	// Sync locals when global header toggles change (render-time pattern per React docs)
	const [prevGlobalRange, setPrevGlobalRange] = useState(globalRange);
	const [prevGlobalMetric, setPrevGlobalMetric] = useState(globalMetric);
	if (prevGlobalRange !== globalRange) {
		setPrevGlobalRange(globalRange);
		setRequestsChartRange(globalRange);
		setTokensChartRange(globalRange);
		setDoughnutRange(globalRange);
		setTokenRange(globalRange);
		setModelsRange(globalRange);
		setProvidersRange(globalRange);
		setVirtualKeysRange(globalRange);
	}
	if (prevGlobalMetric !== globalMetric) {
		setPrevGlobalMetric(globalMetric);
		setDoughnutMetric(globalMetric);
		setModelsMetric(globalMetric);
		setProvidersMetric(globalMetric);
		setVirtualKeysMetric(globalMetric);
	}

	const [overheadModalOpen, setOverheadModalOpen] = useState(false);
	const [errorModalOpen, setErrorModalOpen] = useState(false);
	const [latencyModalOpen, setLatencyModalOpen] = useState(false);
	const [ttftModalOpen, setTtftModalOpen] = useState(false);
	const [rateLimitModalOpen, setRateLimitModalOpen] = useState(false);
	const [requestsModalOpen, setRequestsModalOpen] = useState(false);
	const [tokensModalOpen, setTokensModalOpen] = useState(false);
	const [detailModel, setDetailModel] = useState<Model | null>(null);

	// Dashboard auto-refresh
	const queryClient = useQueryClient();
	const { toast } = useToast();
	const lastManualRefresh = useRef(0);
	const refreshCooldownMs = 5000;
	const [isRefreshing, setIsRefreshing] = useState(false);

	const [dashboardRefreshMs, setDashboardRefreshMs] = useState(() => {
		try {
			const sec = Number(localStorage.getItem("dashboardRefreshSec"));
			if (sec > 0) return sec * 1000;
		} catch {
			/* ignore */
		}
		return 30000; // default 30s
	});

	// React to interval changes from Settings page mid-session
	useEffect(() => {
		const handler = () => {
			try {
				const sec = Number(localStorage.getItem("dashboardRefreshSec"));
				setDashboardRefreshMs(sec > 0 ? sec * 1000 : 30000);
			} catch {
				setDashboardRefreshMs(30000);
			}
		};
		window.addEventListener("dashboardRefreshChange", handler);
		return () => window.removeEventListener("dashboardRefreshChange", handler);
	}, []);

	const handleRefresh = useCallback(() => {
		const now = Date.now();
		if (now - lastManualRefresh.current < refreshCooldownMs) {
			toast("Please wait before refreshing again", "info");
			return;
		}
		lastManualRefresh.current = now;
		setIsRefreshing(true);
		queryClient.invalidateQueries({ queryKey: ["stats"] });
		queryClient.invalidateQueries({ queryKey: ["models"] });
		queryClient.invalidateQueries({ queryKey: ["providers"] });
		queryClient.invalidateQueries({ queryKey: ["stats-timeseries"] });
		queryClient.invalidateQueries({ queryKey: ["stats-timeseries-tokens"] });
		queryClient.invalidateQueries({
			queryKey: ["stats-provider-distribution"],
		});
		queryClient.invalidateQueries({ queryKey: ["stats-usage-models"] });
		queryClient.invalidateQueries({ queryKey: ["stats-usage-providers"] });
		queryClient.invalidateQueries({ queryKey: ["stats-usage-vkeys"] });
		queryClient.invalidateQueries({ queryKey: ["stats-tokens"] });
		toast("Refreshing dashboard…", "info");
		setTimeout(() => setIsRefreshing(false), refreshCooldownMs);
	}, [queryClient, toast]);

	// Hide manual refresh when auto-refresh is 10s or faster
	const hideManualRefresh =
		dashboardRefreshMs > 0 && dashboardRefreshMs <= 10000;

	const {
		data: stats,
		isLoading: statsLoading,
		error: statsError,
	} = useQuery({
		queryKey: ["stats", globalRange, excludeDeleted],
		queryFn: () => api.stats.get({ period: globalRange, excludeDeleted }),
		placeholderData: (prev) => prev,
		refetchInterval: dashboardRefreshMs,
		retry: 1,
	});

	const {
		data: gaugeStats,
		isLoading: gaugeStatsLoading,
		error: gaugeStatsError,
	} = useQuery({
		queryKey: [
			"stats",
			globalRange === "1h" ? "1h" : globalRange,
			excludeDeleted,
		],
		queryFn: () => api.stats.get({ period: globalRange, excludeDeleted }),
		placeholderData: (prev) => prev,
		refetchInterval: dashboardRefreshMs,
		retry: 1,
	});

	const { data: models, isLoading: modelsLoading } = useQuery({
		queryKey: ["models", excludeDeleted],
		queryFn: () =>
			api.models
				.list()
				.then((d) =>
					excludeDeleted ? d.filter((m) => !m.disabled_manually) : d,
				),
		refetchInterval: dashboardRefreshMs,
	});

	const { data: providers, isLoading: providersLoading } = useQuery({
		queryKey: ["providers", excludeDeleted],
		queryFn: () =>
			api.providers
				.list()
				.then((d) => (excludeDeleted ? d.filter((p) => p.enabled) : d)),
		refetchInterval: dashboardRefreshMs,
	});

	const { data: tsData, isLoading: tsDataLoading } = useQuery({
		queryKey: ["stats-timeseries", requestsChartRange, excludeDeleted],
		queryFn: () =>
			api.stats.getTimeSeries({ period: requestsChartRange, excludeDeleted }),
		placeholderData: (prev) => prev,
		refetchInterval: dashboardRefreshMs,
	});

	const { data: tokenTsData, isLoading: tokenTsDataLoading } = useQuery({
		queryKey: ["stats-timeseries-tokens", tokensChartRange, excludeDeleted],
		queryFn: () =>
			api.stats.getTimeSeries({ period: tokensChartRange, excludeDeleted }),
		placeholderData: (prev) => prev,
		refetchInterval: dashboardRefreshMs,
	});

	const { data: provDist, isLoading: provDistLoading } = useQuery({
		queryKey: [
			"stats-provider-distribution",
			doughnutRange,
			doughnutMetric,
			excludeDeleted,
		],
		queryFn: () =>
			api.stats.getProviderDistribution({
				period: doughnutRange,
				metric: doughnutMetric,
				excludeDeleted,
			}),
		placeholderData: (prev) => prev,
		refetchInterval: dashboardRefreshMs,
	});

	// Three separate queries for usage bar panels - each has its own range/metric
	const { data: modelsUsageStats, isLoading: modelsUsageLoading } = useQuery({
		queryKey: ["stats-usage-models", modelsRange, modelsMetric, excludeDeleted],
		queryFn: () =>
			api.stats.get({
				period: modelsRange,
				metric: modelsMetric,
				excludeDeleted,
			}),
		placeholderData: (prev) => prev,
		refetchInterval: dashboardRefreshMs,
	});

	const { data: providersUsageStats, isLoading: providersUsageLoading } =
		useQuery({
			queryKey: [
				"stats-usage-providers",
				providersRange,
				providersMetric,
				excludeDeleted,
			],
			queryFn: () =>
				api.stats.get({
					period: providersRange,
					metric: providersMetric,
					excludeDeleted,
				}),
			placeholderData: (prev) => prev,
			refetchInterval: dashboardRefreshMs,
		});

	const { data: vkeysUsageStats, isLoading: vkeysUsageLoading } = useQuery({
		queryKey: [
			"stats-usage-vkeys",
			virtualKeysRange,
			virtualKeysMetric,
			excludeDeleted,
		],
		queryFn: () =>
			api.stats.get({
				period: virtualKeysRange,
				metric: virtualKeysMetric,
				excludeDeleted,
			}),
		placeholderData: (prev) => prev,
		refetchInterval: dashboardRefreshMs,
	});

	const { data: tokenStats, isLoading: tokenStatsLoading } = useQuery({
		queryKey: ["stats-tokens", tokenRange, excludeDeleted],
		queryFn: () => api.stats.get({ period: tokenRange, excludeDeleted }),
		placeholderData: (prev) => prev,
		refetchInterval: dashboardRefreshMs,
	});

	// Auth check: on stats error, test if token is still valid.
	// Wrapped in useEffect so window.location.reload() never runs during render.
	useEffect(() => {
		if (!statsError) return;
		const errMsg = statsError.message || "";
		if (
			errMsg.includes("401") ||
			errMsg.includes("Unauthorized") ||
			errMsg.includes("Admin token")
		) {
			localStorage.removeItem("adminToken");
			window.location.reload();
		}
	}, [statsError]);

	const handleModelClick = useCallback(
		(label: string) => {
			const found = models?.find(
				(m) => proxyModelID(m.provider_name, m.model_id) === label,
			);
			if (found) setDetailModel(found);
		},
		[models],
	);

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

	// Derived values
	const totalTokens =
		(stats?.total_tokens_prompt || 0) + (stats?.total_tokens_completion || 0);

	const rangeLabel =
		globalRange === "1h" ? "1h" : globalRange === "24h" ? "1d" : "7d";
	const gaugeRequestCount =
		globalRange === "1h"
			? gaugeStats?.requests_last_1h || 0
			: globalRange === "24h"
				? gaugeStats?.total_requests_last_24h || 0
				: gaugeStats?.total_requests_last_7d || 0;

	const acData = (() => {
		if (!tsData?.points) return [];
		return tsData.points.map((p) => {
			const d = new Date(p.bucket);
			const label =
				requestsChartRange === "7d"
					? d.toLocaleDateString("en-US", {
							month: "short",
							day: "numeric",
						})
					: `${d.getHours().toString().padStart(2, "0")}:00`;
			return {
				hour: label,
				total: p.count,
				errors: p.errors,
				tokens: p.tokens,
				latency: Math.round(p.latency_ms),
				overhead_ms: p.overhead_ms,
				provider_latency_ms: p.provider_latency_ms,
				rate_limit_hits: p.rate_limit_hits,
				avg_ttft_ms: p.avg_ttft_ms,
			};
		});
	})();

	const tokenAcData = (() => {
		if (!tokenTsData?.points) return [];
		return tokenTsData.points.map((p) => {
			const d = new Date(p.bucket);
			const label =
				tokensChartRange === "7d"
					? d.toLocaleDateString("en-US", {
							month: "short",
							day: "numeric",
						})
					: `${d.getHours().toString().padStart(2, "0")}:00`;
			return {
				hour: label,
				total: p.count,
				errors: p.errors,
				tokens: p.tokens,
				latency: Math.round(p.latency_ms),
				overhead_ms: p.overhead_ms,
				provider_latency_ms: p.provider_latency_ms,
				rate_limit_hits: p.rate_limit_hits,
				avg_ttft_ms: p.avg_ttft_ms,
			};
		});
	})();

	// Format usage panels from their respective range queries.
	// Filter out zero-value entries so NULL/empty aggregates don't clutter the UI.
	const byModel = modelsUsageStats
		? Object.entries(modelsUsageStats.by_model)
				.filter(([, v]) => Number(v) > 0)
				.sort(([, a], [, b]) => Number(b) - Number(a))
				.slice(0, 5)
				.map(([k, v]) => ({
					label: k,
					value: Number(v),
					suffix: modelsMetric === "tokens" ? " tokens" : " requests",
				}))
		: [];
	const byProvider = providersUsageStats
		? Object.entries(providersUsageStats.by_provider)
				.filter(([, v]) => Number(v) > 0)
				.sort(([, a], [, b]) => Number(b) - Number(a))
				.slice(0, 5)
				.map(([k, v]) => ({
					label: k,
					value: Number(v),
					suffix: providersMetric === "tokens" ? " tokens" : " requests",
				}))
		: [];
	const byVK = vkeysUsageStats
		? Object.entries(vkeysUsageStats.by_virtual_key)
				.filter(([, v]) => Number(v) > 0)
				.sort(([, a], [, b]) => Number(b) - Number(a))
				.slice(0, 5)
				.map(([k, v]) => ({
					label: k,
					value: Number(v),
					suffix: virtualKeysMetric === "tokens" ? " tokens" : " requests",
					deleted: k === "Deleted",
				}))
		: [];

	// Card accent colors (subtle tints in light, slightly brighter in dark)
	const accents = {
		providers: "#14b8a6",
		models: "#818cf8",
		requests: "#0ea5e9",
		latency: "#f59e0b",
		overhead: "#f472b6",
		errors: "#ef4444",
		tokens: "#22c55e",
		rateLimit: "#a855f7",
	};

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
							className={`flex items-center gap-1 px-1.5 py-0.5 rounded text-[10px] font-semibold transition-colors ${
								excludeDeleted
									? "bg-amber-500/20 text-amber-400 hover:bg-amber-500/30"
									: "bg-gray-700/60 text-gray-400 hover:bg-gray-600"
							}`}
						>
							<span
								className={`w-1.5 h-1.5 rounded-full transition-colors ${
									excludeDeleted ? "bg-amber-400" : "bg-gray-500"
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
						{gaugeStatsLoading ? (
							<div className="flex items-center justify-center gap-2 py-4 text-sm text-(--text-muted)">
								Loading gauges…
							</div>
						) : gaugeStatsError ? (
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
									value={(gaugeStats?.avg_ttft_ms || 0) / 1000}
									decimals={1}
									suffix="s"
									color={accents.latency}
									onClick={() => setTtftModalOpen(true)}
									tooltip="Click to view TTFT history"
									maxScale={Math.max(
										1,
										((gaugeStats?.avg_ttft_ms || 0) / 1000) * 1.5,
									)}
								/>
								<Gauge
									label={`Avg Overhead/${rangeLabel}`}
									value={gaugeStats?.avg_overhead_ms || 0}
									decimals={1}
									suffix="ms"
									color={accents.overhead}
									onClick={() => setOverheadModalOpen(true)}
									tooltip="Click to view overhead history"
									maxScale={Math.max(
										100,
										(gaugeStats?.avg_overhead_ms || 0) * 1.5,
									)}
								/>
								<Gauge
									label={`Rate Limit Hits/${rangeLabel}`}
									value={gaugeStats?.rate_limit_hits || 0}
									decimals={0}
									suffix=""
									color="#a855f7"
									onClick={() => setRateLimitModalOpen(true)}
									tooltip="Click to view rate limit hit history"
									maxScale={Math.max(
										10,
										(gaugeStats?.rate_limit_hits || 0) * 1.5,
									)}
								/>
								<Gauge
									label={`Error Rate/${rangeLabel}`}
									value={(gaugeStats?.error_rate || 0) * 100}
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
							? gaugeStats?.requests_last_1h || 0
							: globalRange === "24h"
								? stats?.total_requests_last_24h || 0
								: stats?.total_requests_last_7d || 0
					}
					icon={Activity}
					accent={accents.requests}
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
					icon={globalMetric === "tokens" ? Zap : Target}
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
							icon={Zap}
							color={accents.tokens}
							label="Tokens"
							dataKey="tokens"
							allowDecimals
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
							icon={Zap}
							color={accents.tokens}
							label="Tokens"
							dataKey="tokens"
							allowDecimals
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
				icon={Zap}
				color={accents.tokens}
				dataKey="tokens"
				label="tokens"
				allowDecimals
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
