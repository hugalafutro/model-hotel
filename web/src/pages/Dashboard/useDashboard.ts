import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../../api/client";
import type {
	MetricType,
	Model,
	Provider,
	ProviderDistributionStats,
	Stats,
	TimeSeriesStats,
} from "../../api/types";
import { useToast } from "../../context/ToastContext";
import { useLocalStorage } from "../../hooks/useLocalStorage";
import { proxyModelID } from "../../utils/model";
import type { Range } from "./types";

export interface UseDashboardReturn {
	// State: global
	globalRange: Range;
	setGlobalRange: (range: Range) => void;
	globalMetric: MetricType;
	setGlobalMetric: (metric: MetricType) => void;
	excludeDeleted: boolean;
	setExcludeDeleted: (exclude: boolean) => void;

	// State: per-section range/metric
	requestsChartRange: Range;
	setRequestsChartRange: (range: Range) => void;
	tokensChartRange: Range;
	setTokensChartRange: (range: Range) => void;
	doughnutRange: Range;
	setDoughnutRange: (range: Range) => void;
	doughnutMetric: MetricType;
	setDoughnutMetric: (metric: MetricType) => void;
	tokenRange: Range;
	setTokenRange: (range: Range) => void;
	modelsRange: Range;
	setModelsRange: (range: Range) => void;
	modelsMetric: MetricType;
	setModelsMetric: (metric: MetricType) => void;
	providersRange: Range;
	setProvidersRange: (range: Range) => void;
	providersMetric: MetricType;
	setProvidersMetric: (metric: MetricType) => void;
	virtualKeysRange: Range;
	setVirtualKeysRange: (range: Range) => void;
	virtualKeysMetric: MetricType;
	setVirtualKeysMetric: (metric: MetricType) => void;

	// State: modals
	overheadModalOpen: boolean;
	setOverheadModalOpen: (open: boolean) => void;
	errorModalOpen: boolean;
	setErrorModalOpen: (open: boolean) => void;
	latencyModalOpen: boolean;
	setLatencyModalOpen: (open: boolean) => void;
	ttftModalOpen: boolean;
	setTtftModalOpen: (open: boolean) => void;
	rateLimitModalOpen: boolean;
	setRateLimitModalOpen: (open: boolean) => void;
	requestsModalOpen: boolean;
	setRequestsModalOpen: (open: boolean) => void;
	tokensModalOpen: boolean;
	setTokensModalOpen: (open: boolean) => void;
	detailModel: Model | null;
	setDetailModel: (model: Model | null) => void;

	// State: refresh
	isRefreshing: boolean;
	dashboardRefreshMs: number;

	// Loading states
	statsLoading: boolean;
	modelsLoading: boolean;
	providersLoading: boolean;
	tsDataLoading: boolean;
	tokenTsDataLoading: boolean;
	provDistLoading: boolean;
	modelsUsageLoading: boolean;
	providersUsageLoading: boolean;
	vkeysUsageLoading: boolean;
	tokenStatsLoading: boolean;

	// Error states
	statsError: Error | null;

	// Query data
	stats: Stats | undefined;
	models: Model[] | undefined;
	providers: Provider[] | undefined;
	tsData: TimeSeriesStats | undefined;
	tokenTsData: TimeSeriesStats | undefined;
	provDist: ProviderDistributionStats | undefined;
	modelsUsageStats: Stats | undefined;
	providersUsageStats: Stats | undefined;
	vkeysUsageStats: Stats | undefined;
	tokenStats: Stats | undefined;

	// Handlers
	handleRefresh: () => void;
	handleModelClick: (label: string) => void;

	// Computed values
	hideManualRefresh: boolean;
	totalTokens: number;
	rangeLabel: string;
	gaugeRequestCount: number;
	acData: Array<{
		hour: string;
		total: number;
		errors: number;
		tokens: number;
		latency: number;
		overhead_ms: number;
		provider_latency_ms: number;
		rate_limit_hits: number;
		avg_ttft_ms: number;
	}>;
	tokenAcData: Array<{
		hour: string;
		total: number;
		errors: number;
		tokens: number;
		latency: number;
		overhead_ms: number;
		provider_latency_ms: number;
		rate_limit_hits: number;
		avg_ttft_ms: number;
	}>;
	byModel: Array<{ label: string; value: number; suffix: string }>;
	byProvider: Array<{ label: string; value: number; suffix: string }>;
	byVK: Array<{
		label: string;
		value: number;
		suffix: string;
		deleted?: boolean;
	}>;
	accents: {
		providers: string;
		models: string;
		requests: string;
		latency: string;
		overhead: string;
		errors: string;
		tokens: string;
		rateLimit: string;
	};
}

const VALID_RANGES: ReadonlySet<Range> = new Set(["1h", "24h", "7d"]);
const VALID_METRICS: ReadonlySet<MetricType> = new Set(["tokens", "requests"]);
const deserializeRange = (stored: string, fallback: Range): Range =>
	VALID_RANGES.has(stored as Range) ? (stored as Range) : fallback;
const deserializeMetric = (stored: string, fallback: MetricType): MetricType =>
	VALID_METRICS.has(stored as MetricType) ? (stored as MetricType) : fallback;

export function useDashboard(): UseDashboardReturn {
	// Global state with localStorage persistence
	const [globalRange, setGlobalRange] = useLocalStorage<Range>(
		"dashboardRange",
		"24h",
		{ deserialize: deserializeRange },
	);
	const [globalMetric, setGlobalMetric] = useLocalStorage<MetricType>(
		"dashboardMetric",
		"tokens",
		{ deserialize: deserializeMetric },
	);
	const [excludeDeleted, setExcludeDeleted] = useState(false);

	// Per-section local states: persisted in localStorage, synced from global
	// header toggles when those change.
	const [requestsChartRange, setRequestsChartRange] = useLocalStorage<Range>(
		"dashboard.requestsChartRange",
		globalRange,
		{ deserialize: deserializeRange },
	);
	const [tokensChartRange, setTokensChartRange] = useLocalStorage<Range>(
		"dashboard.tokensChartRange",
		globalRange,
		{ deserialize: deserializeRange },
	);
	const [doughnutRange, setDoughnutRange] = useLocalStorage<Range>(
		"dashboard.doughnutRange",
		globalRange,
		{ deserialize: deserializeRange },
	);
	const [doughnutMetric, setDoughnutMetric] = useLocalStorage<MetricType>(
		"dashboard.doughnutMetric",
		globalMetric,
		{ deserialize: deserializeMetric },
	);
	const [tokenRange, setTokenRange] = useLocalStorage<Range>(
		"dashboard.tokenRange",
		globalRange,
		{ deserialize: deserializeRange },
	);
	const [modelsRange, setModelsRange] = useLocalStorage<Range>(
		"dashboard.modelsRange",
		globalRange,
		{ deserialize: deserializeRange },
	);
	const [modelsMetric, setModelsMetric] = useLocalStorage<MetricType>(
		"dashboard.modelsMetric",
		globalMetric,
		{ deserialize: deserializeMetric },
	);
	const [providersRange, setProvidersRange] = useLocalStorage<Range>(
		"dashboard.providersRange",
		globalRange,
		{ deserialize: deserializeRange },
	);
	const [providersMetric, setProvidersMetric] = useLocalStorage<MetricType>(
		"dashboard.providersMetric",
		globalMetric,
		{ deserialize: deserializeMetric },
	);
	const [virtualKeysRange, setVirtualKeysRange] = useLocalStorage<Range>(
		"dashboard.virtualKeysRange",
		globalRange,
		{ deserialize: deserializeRange },
	);
	const [virtualKeysMetric, setVirtualKeysMetric] = useLocalStorage<MetricType>(
		"dashboard.virtualKeysMetric",
		globalMetric,
		{ deserialize: deserializeMetric },
	);

	// Sync locals when global header toggles change
	const prevGlobalRangeRef = useRef(globalRange);
	const prevGlobalMetricRef = useRef(globalMetric);
	useEffect(() => {
		if (prevGlobalRangeRef.current !== globalRange) {
			prevGlobalRangeRef.current = globalRange;
			setRequestsChartRange(globalRange);
			setTokensChartRange(globalRange);
			setDoughnutRange(globalRange);
			setTokenRange(globalRange);
			setModelsRange(globalRange);
			setProvidersRange(globalRange);
			setVirtualKeysRange(globalRange);
		}
	}, [
		globalRange,
		setRequestsChartRange,
		setTokensChartRange,
		setDoughnutRange,
		setTokenRange,
		setModelsRange,
		setProvidersRange,
		setVirtualKeysRange,
	]);
	useEffect(() => {
		if (prevGlobalMetricRef.current !== globalMetric) {
			prevGlobalMetricRef.current = globalMetric;
			setDoughnutMetric(globalMetric);
			setModelsMetric(globalMetric);
			setProvidersMetric(globalMetric);
			setVirtualKeysMetric(globalMetric);
		}
	}, [
		globalMetric,
		setDoughnutMetric,
		setModelsMetric,
		setProvidersMetric,
		setVirtualKeysMetric,
	]);

	// Modal states
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
		queryClient.invalidateQueries({ queryKey: ["stats-usage"] });
		queryClient.invalidateQueries({ queryKey: ["stats-tokens"] });
		toast("Refreshing dashboard…", "info");
		setTimeout(() => setIsRefreshing(false), refreshCooldownMs);
	}, [queryClient, toast]);

	// Hide manual refresh when auto-refresh is 10s or faster
	const hideManualRefresh =
		dashboardRefreshMs > 0 && dashboardRefreshMs <= 10000;

	// Queries
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

	// Usage bar panels: each panel has independent range/metric controls.
	// React Query de-dupes by query key, so when params match only 1 request is made.
	const { data: modelsUsageStats, isLoading: modelsUsageLoading } = useQuery({
		queryKey: ["stats-usage", modelsRange, modelsMetric, excludeDeleted],
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
				"stats-usage",
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
			"stats-usage",
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
			// Backend returns by_model keys with raw provider names (spaces),
			// but proxyModelID normalizes spaces to hyphens. Normalize the
			// label before comparing so both formats match.
			const normalized = label.replace(/ /g, "-");
			const found = models?.find(
				(m) => proxyModelID(m.provider_name, m.model_id) === normalized,
			);
			if (found) setDetailModel(found);
		},
		[models],
	);

	// Derived values
	const totalTokens =
		(stats?.total_tokens_prompt || 0) + (stats?.total_tokens_completion || 0);

	const rangeLabel =
		globalRange === "1h" ? "1h" : globalRange === "24h" ? "1d" : "7d";
	const gaugeRequestCount =
		globalRange === "1h"
			? stats?.requests_last_1h || 0
			: globalRange === "24h"
				? stats?.total_requests_last_24h || 0
				: stats?.total_requests_last_7d || 0;

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
				avg_ttft_ms: p.avg_ttft_ms ?? 0,
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
				avg_ttft_ms: p.avg_ttft_ms ?? 0,
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

	return {
		// State: global
		globalRange,
		setGlobalRange,
		globalMetric,
		setGlobalMetric,
		excludeDeleted,
		setExcludeDeleted,

		// State: per-section range/metric
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

		// State: modals
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

		// State: refresh
		isRefreshing,
		dashboardRefreshMs,

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
		tsData,
		tokenTsData,
		provDist,
		modelsUsageStats,
		providersUsageStats,
		vkeysUsageStats,
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
	};
}
