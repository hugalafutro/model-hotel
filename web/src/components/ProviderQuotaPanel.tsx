import { useQuery, useQueryClient } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { api } from "../api/client";
import type {
	DeepSeekBalance,
	DeepSeekBalanceInfo,
	NanoGPTUsage,
	OpenRouterBalance,
	Provider,
	ZAICodingQuotaResponse,
} from "../api/types";
import { useQuotaModal } from "../context/QuotaModalContext";
import { useToast } from "../context/ToastContext";
import { formatTokens } from "../utils/format";
import { CollapsibleToggle } from "./CollapsibleToggle";
import { NanoGPTQuotaModal, ZAICodingQuotaModal } from "./ProviderModals";

const CACHE_PREFIX = "model-hotel";

function getCachedData<T>(key: string): T | undefined {
	try {
		const raw = localStorage.getItem(`${CACHE_PREFIX}:${key}`);
		if (raw) return JSON.parse(raw) as T;
	} catch {
		/* ignore */
	}
	return undefined;
}

function isQuotaDisabled(): boolean {
	try {
		return localStorage.getItem("sidebarQuotaDisabled") === "true";
	} catch {
		return false;
	}
}

export function ProviderQuotaPanel() {
	const queryClient = useQueryClient();
	const { toast } = useToast();
	const lastManualRefresh = useRef(0);
	const refreshCooldownMs = 10_000;

	const [collapsed, setCollapsed] = useState(() => {
		try {
			return localStorage.getItem("sidebarQuotaCollapsed") === "true";
		} catch {
			return false;
		}
	});
	const [disabled, setDisabled] = useState(() => isQuotaDisabled());
	const [refreshIntervalMin, setRefreshIntervalMin] = useState(() => {
		try {
			return localStorage.getItem("sidebarQuotaRefreshMin") || "5";
		} catch {
			return "5";
		}
	});

	// Listen for toggle and refresh-interval changes from Settings page (same tab)
	useEffect(() => {
		const toggleHandler = () => setDisabled(isQuotaDisabled());
		const refreshHandler = () => {
			try {
				setRefreshIntervalMin(
					localStorage.getItem("sidebarQuotaRefreshMin") || "5",
				);
			} catch {
				setRefreshIntervalMin("5");
			}
		};
		window.addEventListener("sidebarQuotaToggle", toggleHandler);
		window.addEventListener("sidebarQuotaRefreshChange", refreshHandler);
		// Also listen for storage events (cross-tab)
		window.addEventListener("storage", toggleHandler);
		return () => {
			window.removeEventListener("sidebarQuotaToggle", toggleHandler);
			window.removeEventListener("sidebarQuotaRefreshChange", refreshHandler);
			window.removeEventListener("storage", toggleHandler);
		};
	}, []);

	const toggleCollapsed = useCallback(() => {
		setCollapsed((prev) => {
			const next = !prev;
			try {
				localStorage.setItem("sidebarQuotaCollapsed", String(next));
			} catch {
				/* ignore */
			}
			if (next) {
				toast("Quota panel collapsed — auto-refresh paused", "info");
			} else {
				toast("Quota panel expanded — auto-refresh resumed", "info");
			}
			return next;
		});
	}, [toast]);

	const { data: providers } = useQuery({
		queryKey: ["providers"],
		queryFn: () => api.providers.list(),
		staleTime: 60_000,
	});

	const nanogptProviderId = useMemo(
		() =>
			providers?.find((p: Provider) => {
				try {
					return new URL(p.base_url).hostname.endsWith("nano-gpt.com");
				} catch {
					return false;
				}
			})?.id,
		[providers],
	);

	const zaiCodingProviderId = useMemo(
		() =>
			providers?.find((p: Provider) => {
				try {
					const h = new URL(p.base_url).hostname;
					return h === "z.ai" || h.endsWith(".z.ai");
				} catch {
					return false;
				}
			})?.id,
		[providers],
	);

	const deepseekProviderId = useMemo(
		() =>
			providers?.find((p: Provider) => {
				try {
					return new URL(p.base_url).hostname.endsWith("deepseek.com");
				} catch {
					return false;
				}
			})?.id,
		[providers],
	);

	const openrouterProviderId = useMemo(
		() =>
			providers?.find((p: Provider) => {
				try {
					return new URL(p.base_url).hostname.endsWith("openrouter.ai");
				} catch {
					return false;
				}
			})?.id,
		[providers],
	);

	// Derive the refresh interval from the reactive state so changes
	// (triggered by the sidebarQuotaRefreshChange event) take effect
	// immediately without a page reload.
	const refreshMs: number | false = (() => {
		const v = parseInt(refreshIntervalMin, 10);
		if (v === 0) return false;
		if (v >= 1) return v * 60_000;
		return 5 * 60_000;
	})();

	const {
		data: nanogptUsage,
		dataUpdatedAt: nanoDataUpdatedAt,
		isRefetching: isNanoRefetching,
	} = useQuery({
		queryKey: ["nanogpt-usage", nanogptProviderId],
		queryFn: () =>
			api.providers.getUsage(
				nanogptProviderId as string,
			) as Promise<NanoGPTUsage>,
		enabled: Boolean(nanogptProviderId),
		refetchInterval: collapsed ? false : refreshMs,
		initialData: () => getCachedData<NanoGPTUsage>("nanogpt-usage"),
	});

	const {
		data: zaiCodingUsage,
		dataUpdatedAt: zaiCodingDataUpdatedAt,
		isRefetching: isZaiCodingRefetching,
	} = useQuery({
		queryKey: ["zai-coding-usage", zaiCodingProviderId],
		queryFn: () =>
			api.providers.getUsage(
				zaiCodingProviderId as string,
			) as Promise<ZAICodingQuotaResponse>,
		enabled: Boolean(zaiCodingProviderId),
		refetchInterval: collapsed ? false : refreshMs,
		initialData: () =>
			getCachedData<ZAICodingQuotaResponse>("zai-coding-usage"),
	});

	const { data: deepseekBalance, isRefetching: isDsRefetching } = useQuery({
		queryKey: ["deepseek-balance", deepseekProviderId],
		queryFn: () => api.providers.getBalance(deepseekProviderId as string),
		enabled: Boolean(deepseekProviderId),
		refetchInterval: collapsed ? false : refreshMs,
		initialData: () => getCachedData<DeepSeekBalance>("deepseek-balance"),
	});

	const { data: openrouterBalance, isRefetching: isOrRefetching } = useQuery({
		queryKey: ["openrouter-key", openrouterProviderId],
		queryFn: () =>
			api.providers.getOpenRouterBalance(openrouterProviderId as string),
		enabled: Boolean(openrouterProviderId),
		refetchInterval: collapsed ? false : refreshMs,
		initialData: () => getCachedData<OpenRouterBalance>("openrouter-key"),
	});

	const anyRefreshing =
		isNanoRefetching ||
		isZaiCodingRefetching ||
		isDsRefetching ||
		isOrRefetching;

	const isAutoRefreshing = anyRefreshing && !collapsed;

	const handleRefresh = useCallback(() => {
		const now = Date.now();
		if (now - lastManualRefresh.current < refreshCooldownMs) {
			toast("Please wait before refreshing again", "info");
			return;
		}
		lastManualRefresh.current = now;
		queryClient.invalidateQueries({ queryKey: ["nanogpt-usage"] });
		queryClient.invalidateQueries({ queryKey: ["zai-coding-usage"] });
		queryClient.invalidateQueries({ queryKey: ["deepseek-balance"] });
		queryClient.invalidateQueries({ queryKey: ["openrouter-key"] });
		toast("Refreshing quotas...", "info");
	}, [queryClient, toast]);

	const weeklyUsed = nanogptUsage?.weeklyInputTokens?.used;
	const weeklyLimit = nanogptUsage?.limits?.weeklyInputTokens;
	const showNanoBadge = nanogptUsage && weeklyUsed != null && weeklyLimit;

	const zaiCodingFiveHour = zaiCodingUsage?.data?.limits?.find(
		(l) => l.type === "TOKENS_LIMIT" && l.unit === 3,
	);
	const zaiCodingWeekly = zaiCodingUsage?.data?.limits?.find(
		(l) => l.type === "TOKENS_LIMIT" && l.unit === 6,
	);
	const showZaiCodingBadge =
		zaiCodingUsage?.success && (zaiCodingFiveHour || zaiCodingWeekly);

	const showDsBadge = deepseekBalance && deepseekProviderId;

	const showOrBadge = openrouterBalance && openrouterProviderId;

	const hasAnyProvider =
		nanogptProviderId ||
		zaiCodingProviderId ||
		deepseekProviderId ||
		openrouterProviderId;

	const { nanogptUsage: modalNano, setNanogptUsage: setModalNano } =
		useQuotaModal();
	const {
		zaiCodingUsage: modalZaiCoding,
		setZaiCodingUsage: setModalZaiCoding,
	} = useQuotaModal();

	const refreshNano = useCallback(async () => {
		await queryClient.invalidateQueries({
			queryKey: ["nanogpt-usage", nanogptProviderId],
		});
	}, [queryClient, nanogptProviderId]);

	const refreshZaiCoding = useCallback(async () => {
		await queryClient.invalidateQueries({
			queryKey: ["zai-coding-usage", zaiCodingProviderId],
		});
	}, [queryClient, zaiCodingProviderId]);

	if (!hasAnyProvider || disabled) return null;

	return (
		<div className="sidebar-quota-panel">
			<div className="flex items-center justify-between mb-1.5">
				<span className={`sidebar-quota-label${collapsed ? " invisible" : ""}`}>
					Quotas
				</span>
				<div className="flex items-center gap-0.5">
					{!collapsed && (
						<button
							type="button"
							onClick={handleRefresh}
							disabled={anyRefreshing}
							className="sidebar-quota-btn"
							title="Refresh all quotas"
						>
							<RefreshCw
								size={10}
								className={isAutoRefreshing ? "animate-spin" : ""}
							/>
						</button>
					)}
					<CollapsibleToggle
						collapsed={collapsed}
						onToggle={toggleCollapsed}
						size={10}
						expandTitle="Expand quotas"
						collapseTitle="Collapse"
						className="sidebar-quota-btn"
					/>
				</div>
			</div>

			{!collapsed && (
				<div className="flex flex-wrap gap-1 justify-center">
					{showNanoBadge && (
						<button
							type="button"
							onClick={() => setModalNano(nanogptUsage)}
							className="sidebar-quota-pill sidebar-quota-pill-nano"
							title="NanoGPT weekly token quota — click for details"
						>
							{formatTokens(weeklyUsed)}/{formatTokens(weeklyLimit)}
						</button>
					)}
					{showZaiCodingBadge && (
						<button
							type="button"
							onClick={() => setModalZaiCoding(zaiCodingUsage)}
							className="sidebar-quota-pill sidebar-quota-pill-zai-coding"
							title="Z.ai Coding Plan token quota — click for details"
						>
							{zaiCodingFiveHour
								? `${(100 - zaiCodingFiveHour.percentage).toFixed(0)}%`
								: "-"}
							/
							{zaiCodingWeekly
								? `${(100 - zaiCodingWeekly.percentage).toFixed(0)}%`
								: "-"}
						</button>
					)}
					{showDsBadge && (
						<button
							type="button"
							onClick={handleRefresh}
							className="sidebar-quota-pill sidebar-quota-pill-ds"
							title="DeepSeek balance — click to refresh"
						>
							${" "}
							{deepseekBalance.balance_infos.find(
								(b: DeepSeekBalanceInfo) => b.currency === "USD",
							)?.total_balance ?? "-"}
						</button>
					)}
					{showOrBadge && (
						<button
							type="button"
							onClick={handleRefresh}
							className="sidebar-quota-pill sidebar-quota-pill-or"
							title="OpenRouter key balance — click to refresh"
						>
							{"$"}
							{openrouterBalance.credits_remaining.toFixed(2)}
						</button>
					)}
				</div>
			)}

			{modalNano && (
				<NanoGPTQuotaModal
					usage={modalNano}
					onClose={() => setModalNano(null)}
					onRefresh={refreshNano}
					isRefreshing={isNanoRefetching}
					onToast={toast}
					lastRefreshed={nanoDataUpdatedAt}
				/>
			)}
			{modalZaiCoding && (
				<ZAICodingQuotaModal
					usage={modalZaiCoding}
					onClose={() => setModalZaiCoding(null)}
					onRefresh={refreshZaiCoding}
					isRefreshing={isZaiCodingRefetching}
					onToast={toast}
					lastRefreshed={zaiCodingDataUpdatedAt}
				/>
			)}
		</div>
	);
}
