import { useQuery, useQueryClient } from "@tanstack/react-query";
import { useState, useRef, useCallback, useMemo } from "react";
import { RefreshCw, ChevronDown, ChevronUp } from "lucide-react";
import { api } from "../api/client";
import type {
    NanoGPTUsage,
    ZAIQuotaResponse,
    DeepSeekBalance,
    DeepSeekBalanceInfo,
    Provider,
} from "../api/types";
import { formatTokens } from "../utils/format";
import { useQuotaModal } from "../context/QuotaModalContext";
import { useToast } from "../context/ToastContext";
import { NanoGPTQuotaModal, ZAIQuotaModal } from "./ProviderModals";

const CACHE_PREFIX = "llm-proxy";

function getCachedData<T>(key: string): T | undefined {
    try {
        const raw = localStorage.getItem(`${CACHE_PREFIX}:${key}`);
        if (raw) return JSON.parse(raw) as T;
    } catch { /* ignore */ }
    return undefined;
}

function getRefreshInterval(): number {
    try {
        const raw = localStorage.getItem("sidebarQuotaRefreshMin");
        if (raw) {
            const v = parseInt(raw, 10);
            if (v >= 1) return v * 60_000;
        }
    } catch { /* ignore */ }
    return 5 * 60_000;
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
    const toggleCollapsed = useCallback(() => {
        setCollapsed((prev) => {
            const next = !prev;
            try {
                localStorage.setItem("sidebarQuotaCollapsed", String(next));
            } catch { /* ignore */ }
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

    const zaiProviderId = useMemo(
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

    const refreshMs = getRefreshInterval();

    const {
        data: nanogptUsage,
        isRefetching: isNanoRefetching,
    } = useQuery({
        queryKey: ["nanogpt-usage", nanogptProviderId],
        queryFn: () =>
            api.providers.getUsage(nanogptProviderId!) as Promise<NanoGPTUsage>,
        enabled: Boolean(nanogptProviderId),
        refetchInterval: collapsed ? false : refreshMs,
        initialData: () => getCachedData<NanoGPTUsage>("nanogpt-usage"),
    });

    const {
        data: zaiUsage,
        isRefetching: isZaiRefetching,
    } = useQuery({
        queryKey: ["zai-usage", zaiProviderId],
        queryFn: () =>
            api.providers.getUsage(zaiProviderId!) as Promise<ZAIQuotaResponse>,
        enabled: Boolean(zaiProviderId),
        refetchInterval: collapsed ? false : refreshMs,
        initialData: () => getCachedData<ZAIQuotaResponse>("zai-usage"),
    });

    const {
        data: deepseekBalance,
        isRefetching: isDsRefetching,
    } = useQuery({
        queryKey: ["deepseek-balance", deepseekProviderId],
        queryFn: () => api.providers.getBalance(deepseekProviderId!),
        enabled: Boolean(deepseekProviderId),
        refetchInterval: collapsed ? false : refreshMs,
        initialData: () => getCachedData<DeepSeekBalance>("deepseek-balance"),
    });

    const anyRefreshing =
        isNanoRefetching || isZaiRefetching || isDsRefetching;

    const isAutoRefreshing = anyRefreshing && !collapsed;

    const handleRefresh = useCallback(() => {
        const now = Date.now();
        if (now - lastManualRefresh.current < refreshCooldownMs) {
            toast("Please wait before refreshing again", "info");
            return;
        }
        lastManualRefresh.current = now;
        queryClient.invalidateQueries({ queryKey: ["nanogpt-usage"] });
        queryClient.invalidateQueries({ queryKey: ["zai-usage"] });
        queryClient.invalidateQueries({ queryKey: ["deepseek-balance"] });
        toast("Refreshing quotas...", "info");
    }, [queryClient, toast]);

    const weeklyUsed = nanogptUsage?.weeklyInputTokens?.used;
    const weeklyLimit = nanogptUsage?.limits?.weeklyInputTokens;
    const showNanoBadge =
        nanogptUsage && weeklyUsed != null && weeklyLimit;

    const zaiFiveHour = zaiUsage?.data?.limits?.find(
        (l) => l.type === "TOKENS_LIMIT" && l.unit === 3,
    );
    const zaiWeekly = zaiUsage?.data?.limits?.find(
        (l) => l.type === "TOKENS_LIMIT" && l.unit === 6,
    );
    const showZaiBadge = zaiUsage?.success && (zaiFiveHour || zaiWeekly);

    const showDsBadge = deepseekBalance && deepseekProviderId;

    const hasAnyProvider =
        nanogptProviderId || zaiProviderId || deepseekProviderId;

    const { nanogptUsage: modalNano, setNanogptUsage: setModalNano } =
        useQuotaModal();
    const { zaiUsage: modalZai, setZaiUsage: setModalZai } = useQuotaModal();

    const refreshNano = useCallback(async () => {
        await queryClient.invalidateQueries({
            queryKey: ["nanogpt-usage", nanogptProviderId],
        });
    }, [queryClient, nanogptProviderId]);

    const refreshZai = useCallback(async () => {
        await queryClient.invalidateQueries({
            queryKey: ["zai-usage", zaiProviderId],
        });
    }, [queryClient, zaiProviderId]);

    if (!hasAnyProvider) return null;

    return (
        <div className="sidebar-quota-panel">
            <div className="flex items-center justify-between mb-1.5">
                <span className={`sidebar-quota-label${collapsed ? " invisible" : ""}`}>Quotas</span>
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
                                className={
                                    isAutoRefreshing ? "animate-spin" : ""
                                }
                            />
                        </button>
                    )}
                    <button
                        type="button"
                        onClick={toggleCollapsed}
                        className="sidebar-quota-btn"
                        title={collapsed ? "Expand quotas" : "Collapse"}
                    >
                        {collapsed ? (
                            <ChevronDown size={10} />
                        ) : (
                            <ChevronUp size={10} />
                        )}
                    </button>
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
                    {showZaiBadge && (
                        <button
                            type="button"
                            onClick={() => setModalZai(zaiUsage)}
                            className="sidebar-quota-pill sidebar-quota-pill-zai"
                            title="Z.ai token quota — click for details"
                        >
                            {zaiFiveHour
                                ? `${(100 - zaiFiveHour.percentage).toFixed(0)}%`
                                : "-"}
                            /
                            {zaiWeekly
                                ? `${(100 - zaiWeekly.percentage).toFixed(0)}%`
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
                            {deepseekBalance.balance_infos.find(
                                (b: DeepSeekBalanceInfo) =>
                                    b.currency === "USD",
                            )?.total_balance ?? "-"}{" "}
                            USD
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
                />
            )}
            {modalZai && (
                <ZAIQuotaModal
                    usage={modalZai}
                    onClose={() => setModalZai(null)}
                    onRefresh={refreshZai}
                    isRefreshing={isZaiRefetching}
                    onToast={toast}
                />
            )}
        </div>
    );
}
