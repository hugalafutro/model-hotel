import { useQuery } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { api } from "../api/client";
import { useQuotaModal } from "../context/QuotaModalContext";
import { useToast } from "../context/ToastContext";
import { useQuotaData } from "../hooks/useQuotaData";
import { CollapsibleToggle } from "./CollapsibleToggle";
import {
	NanoGPTQuotaModal,
	OpenRouterQuotaModal,
	ZAICodingQuotaModal,
} from "./ProviderModals";
import { QuotaBadges } from "./QuotaBadge";

function isQuotaDisabled(): boolean {
	try {
		return localStorage.getItem("sidebarQuotaDisabled") === "true";
	} catch {
		return false;
	}
}

export function ProviderQuotaPanel() {
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
				toast("Quota panel collapsed - auto-refresh paused", "info");
			} else {
				toast("Quota panel expanded - auto-refresh resumed", "info");
			}
			return next;
		});
	}, [toast]);

	const { data: providers } = useQuery({
		queryKey: ["providers"],
		queryFn: () => api.providers.list(),
		staleTime: 60_000,
	});

	// Derive the refresh interval from the reactive state so changes
	// (triggered by the sidebarQuotaRefreshChange event) take effect
	// immediately without a page reload.
	const refreshMs: number | false = (() => {
		const v = parseInt(refreshIntervalMin, 10);
		if (v === 0) return false;
		if (v >= 1) return v * 60_000;
		return 5 * 60_000;
	})();

	const quotaData = useQuotaData(providers, {
		refetchInterval: collapsed ? false : refreshMs,
		collapsed,
	});

	const {
		invalidateAll,
		isNanoRefetching,
		isZaiCodingRefetching,
		isDsRefetching,
		isOrRefetching,
	} = quotaData;

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
		invalidateAll();
		toast("Refreshing quotas...", "info");
	}, [toast, invalidateAll]);

	const {
		nanogptUsage: modalNano,
		setNanogptUsage: setModalNano,
		zaiCodingUsage: modalZaiCoding,
		setZaiCodingUsage: setModalZaiCoding,
		openrouterBalance: modalOpenRouter,
		setOpenrouterBalance: setModalOpenRouter,
	} = useQuotaModal();

	if (!quotaData.hasAnyProvider || disabled) return null;

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
						iconStyle="double"
						expandTitle="Expand quotas"
						collapseTitle="Collapse"
					/>
				</div>
			</div>

			<div
				className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${collapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"}`}
			>
				<div className="overflow-hidden">
					<div className="flex flex-wrap gap-1 justify-center">
						<QuotaBadges
							quotaData={quotaData}
							variant="sidebar"
							onNanoClick={() => setModalNano(quotaData.nanogptUsage ?? null)}
							onZaiCodingClick={() =>
								setModalZaiCoding(quotaData.zaiCodingUsage ?? null)
							}
							onDeepseekClick={handleRefresh}
							onOpenRouterClick={() =>
								setModalOpenRouter(quotaData.openrouterBalance ?? null)
							}
						/>
					</div>
				</div>
			</div>

			{modalNano && (
				<NanoGPTQuotaModal
					usage={modalNano}
					onClose={() => setModalNano(null)}
					onRefresh={quotaData.refetchNano}
					isRefreshing={quotaData.isNanoRefetching}
					onToast={toast}
					lastRefreshed={quotaData.nanogptDataUpdatedAt}
				/>
			)}
			{modalZaiCoding && (
				<ZAICodingQuotaModal
					usage={modalZaiCoding}
					onClose={() => setModalZaiCoding(null)}
					onRefresh={quotaData.refetchZaiCoding}
					isRefreshing={quotaData.isZaiCodingRefetching}
					onToast={toast}
					lastRefreshed={quotaData.zaiCodingDataUpdatedAt}
				/>
			)}
			{modalOpenRouter && (
				<OpenRouterQuotaModal
					balance={modalOpenRouter}
					onClose={() => setModalOpenRouter(null)}
					onRefresh={quotaData.refetchOpenRouter}
					isRefreshing={quotaData.isOrRefetching}
					onToast={toast}
					lastRefreshed={quotaData.openrouterDataUpdatedAt}
				/>
			)}
		</div>
	);
}
