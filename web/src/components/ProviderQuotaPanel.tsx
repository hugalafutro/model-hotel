import { useQuery } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { useCallback, useEffect, useRef, useState } from "react";
import { useTranslation } from "react-i18next";
import { api } from "../api/client";
import { useQuotaModal } from "../context/QuotaModalContext";
import { useToast } from "../context/ToastContext";
import { useQuotaData } from "../hooks/useQuotaData";
import { CollapsibleToggle } from "./CollapsibleToggle";
import {
	NanoGPTQuotaModal,
	NeuralWattQuotaModal,
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
	const { t } = useTranslation();
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
				toast(t("components.providerQuotaPanel.quotaPanelCollapsed"), "info");
			} else {
				toast(t("components.providerQuotaPanel.quotaPanelExpanded"), "info");
			}
			return next;
		});
	}, [toast, t]);

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
		isOrRefetching ||
		quotaData.isNeuralwattRefetching;

	const isAutoRefreshing = anyRefreshing && !collapsed;

	const handleRefresh = useCallback(() => {
		const now = Date.now();
		if (now - lastManualRefresh.current < refreshCooldownMs) {
			toast(
				t("components.providerQuotaPanel.pleaseWaitBeforeRefreshing"),
				"info",
			);
			return;
		}
		lastManualRefresh.current = now;
		invalidateAll();
		toast(t("components.providerQuotaPanel.refreshingQuotas"), "info");
	}, [toast, invalidateAll, t]);

	const {
		isNanoOpen,
		setNanoOpen,
		isZaiCodingOpen,
		setZaiCodingOpen,
		isOpenRouterOpen,
		setOpenRouterOpen,
		isNeuralwattOpen,
		setNeuralwattOpen,
	} = useQuotaModal();

	if (!quotaData.hasAnyProvider || disabled) return null;

	return (
		<div className="sidebar-quota-panel">
			<div className="flex items-center justify-between mb-1.5">
				<span className={`sidebar-quota-label${collapsed ? " invisible" : ""}`}>
					{t("components.providerQuotaPanel.quotas")}
				</span>
				<div className="flex items-center gap-0.5">
					{!collapsed && (
						<button
							type="button"
							onClick={handleRefresh}
							disabled={anyRefreshing}
							className="sidebar-quota-btn"
							title={t("components.providerQuotaPanel.refreshAllQuotas")}
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
						expandTitle={t("providers.quotas.expand")}
						collapseTitle={t("common.collapse")}
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
							onNanoClick={() => setNanoOpen(true)}
							onZaiCodingClick={() => setZaiCodingOpen(true)}
							onDeepseekClick={handleRefresh}
							onOpenRouterClick={() => setOpenRouterOpen(true)}
							onOllamaCloudClick={handleRefresh}
							onNeuralwattClick={() => setNeuralwattOpen(true)}
						/>
					</div>
				</div>
			</div>

			{isNanoOpen && quotaData.nanogptUsage && (
				<NanoGPTQuotaModal
					usage={quotaData.nanogptUsage}
					onClose={() => setNanoOpen(false)}
					onRefresh={quotaData.refetchNano}
					isRefreshing={quotaData.isNanoRefetching}
					onToast={toast}
					lastRefreshed={quotaData.nanogptDataUpdatedAt}
				/>
			)}
			{isZaiCodingOpen && quotaData.zaiCodingUsage && (
				<ZAICodingQuotaModal
					usage={quotaData.zaiCodingUsage}
					onClose={() => setZaiCodingOpen(false)}
					onRefresh={quotaData.refetchZaiCoding}
					isRefreshing={quotaData.isZaiCodingRefetching}
					onToast={toast}
					lastRefreshed={quotaData.zaiCodingDataUpdatedAt}
				/>
			)}
			{isOpenRouterOpen && quotaData.openrouterBalance && (
				<OpenRouterQuotaModal
					balance={quotaData.openrouterBalance}
					onClose={() => setOpenRouterOpen(false)}
					onRefresh={quotaData.refetchOpenRouter}
					isRefreshing={quotaData.isOrRefetching}
					onToast={toast}
					lastRefreshed={quotaData.openrouterDataUpdatedAt}
				/>
			)}
			{isNeuralwattOpen && quotaData.neuralwattQuota && (
				<NeuralWattQuotaModal
					quota={quotaData.neuralwattQuota}
					onClose={() => setNeuralwattOpen(false)}
					onRefresh={quotaData.refetchNeuralwatt}
					isRefreshing={quotaData.isNeuralwattRefetching}
					onToast={toast}
					lastRefreshed={quotaData.neuralwattDataUpdatedAt}
				/>
			)}
		</div>
	);
}
