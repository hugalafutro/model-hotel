import { useQuery } from "@tanstack/react-query";
import { useCallback, useRef } from "react";
import { useTranslation } from "react-i18next";
import { RefreshCw } from "@/lib/icons";
import { api } from "../api/client";
import { useQuotaModal } from "../context/QuotaModalContext";
import { useToast } from "../context/ToastContext";
import {
	useLocalStorage,
	useLocalStorageValue,
} from "../hooks/useLocalStorage";
import { useQuotaData } from "../hooks/useQuotaData";
import { CollapsibleToggle } from "./CollapsibleToggle";
import {
	KimiCodeQuotaModal,
	MiniMaxQuotaModal,
	NanoGPTQuotaModal,
	NeuralWattQuotaModal,
	OpenRouterQuotaModal,
	ZAICodingQuotaModal,
} from "./ProviderModals";
import { QuotaBadges } from "./QuotaBadge";

export function ProviderQuotaPanel() {
	const { t } = useTranslation();
	const { toast } = useToast();
	const lastManualRefresh = useRef(0);
	const refreshCooldownMs = 10_000;

	// Stored as "true"/"false"; anything else reads as expanded.
	const [collapsed, setCollapsed] = useLocalStorage<boolean>(
		"sidebarQuotaCollapsed",
		false,
		{ deserialize: (stored) => stored === "true" },
	);
	// The show/hide flag belongs to the Settings page, which announces its writes
	// as "sidebarQuotaToggle"; this panel follows the value and never writes it.
	// The refresh interval comes from the server setting below, so it needs no
	// listener of its own.
	const disabled = useLocalStorageValue("sidebarQuotaDisabled", false, {
		deserialize: (stored) => stored === "true",
		events: ["sidebarQuotaToggle"],
	});

	// The toast is announced here, outside the state updater, which React may
	// call more than once for a single toggle.
	const toggleCollapsed = useCallback(() => {
		const next = !collapsed;
		setCollapsed(next);
		toast(
			next
				? t("components.providerQuotaPanel.quotaPanelCollapsed")
				: t("components.providerQuotaPanel.quotaPanelExpanded"),
			"info",
		);
	}, [collapsed, setCollapsed, toast, t]);

	const { data: providers } = useQuery({
		queryKey: ["providers"],
		queryFn: () => api.providers.list(),
		staleTime: 60_000,
	});

	const { data: settings } = useQuery({
		queryKey: ["settings"],
		queryFn: () => api.settings.get(),
	});

	// Derive the refresh interval from the server setting. Query invalidation
	// (triggered when the Settings page saves the value) re-runs this without a
	// page reload. 0 disables auto-refresh; anything invalid falls back to 5min.
	const refreshMs: number | false = (() => {
		const v = parseInt(settings?.quota_refresh_interval_min ?? "5", 10);
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
		isKimiCodeRefetching,
		isMiniMaxRefetching,
		isDsRefetching,
		isOrRefetching,
	} = quotaData;

	const anyRefreshing =
		isNanoRefetching ||
		isZaiCodingRefetching ||
		isKimiCodeRefetching ||
		isMiniMaxRefetching ||
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
		toast(t("components.providerQuotaPanel.refreshingQuotas"), "info");
		// Force the server to refetch upstream and persist fresh snapshots, then
		// re-read them into the UI. invalidateAll on its own only re-reads the
		// stored (possibly stale) snapshot through the read-through GET. If the
		// server refresh fails we still invalidate, so the UI falls back to the
		// last-good stored snapshot the server keeps.
		void api.providers
			.refreshQuotas()
			.catch(() => undefined)
			.finally(() => {
				invalidateAll();
			});
	}, [toast, invalidateAll, t]);

	const {
		isNanoOpen,
		setNanoOpen,
		isZaiCodingOpen,
		setZaiCodingOpen,
		isKimiCodeOpen,
		setKimiCodeOpen,
		isMiniMaxOpen,
		setMiniMaxOpen,
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
							className="sidebar-quota-btn ui-icon-btn"
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
							onKimiCodeClick={() => setKimiCodeOpen(true)}
							onMiniMaxClick={() => setMiniMaxOpen(true)}
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
			{isKimiCodeOpen && quotaData.kimiCodeUsage && (
				<KimiCodeQuotaModal
					usage={quotaData.kimiCodeUsage}
					onClose={() => setKimiCodeOpen(false)}
					onRefresh={quotaData.refetchKimiCode}
					isRefreshing={quotaData.isKimiCodeRefetching}
					onToast={toast}
					lastRefreshed={quotaData.kimiCodeDataUpdatedAt}
				/>
			)}
			{isMiniMaxOpen && quotaData.minimaxUsage && (
				<MiniMaxQuotaModal
					usage={quotaData.minimaxUsage}
					onClose={() => setMiniMaxOpen(false)}
					onRefresh={quotaData.refetchMiniMax}
					isRefreshing={quotaData.isMiniMaxRefetching}
					onToast={toast}
					lastRefreshed={quotaData.minimaxDataUpdatedAt}
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
