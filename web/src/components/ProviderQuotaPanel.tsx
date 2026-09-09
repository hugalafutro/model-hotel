import { useQuery } from "@tanstack/react-query";
import { useCallback, useRef } from "react";
import { useTranslation } from "react-i18next";
import { RefreshCw } from "@/lib/icons";
import { api } from "../api/client";
import { useQuotaModal } from "../context/QuotaModalContext";
import { useToast } from "../context/ToastContext";
import {
	storedBool,
	useLocalStorage,
	useLocalStorageValue,
} from "../hooks/useLocalStorage";
import { useQuotaData } from "../hooks/useQuotaData";
import { quotaRefreshCooldownMs } from "../hooks/useQuotaRefresh";
import { useSettingsQuery } from "../hooks/useSettingsQuery";
import { CollapseBody, CollapsibleToggle } from "./CollapsibleToggle";
import { QuotaBadges } from "./QuotaBadge";

export function ProviderQuotaPanel() {
	const { t } = useTranslation();
	const { toast } = useToast();
	const lastManualRefresh = useRef(0);

	// Stored as "true"/"false"; anything else reads as expanded.
	const [collapsed, setCollapsed] = useLocalStorage<boolean>(
		"sidebarQuotaCollapsed",
		false,
		{ deserialize: storedBool },
	);
	// The show/hide flag belongs to the Settings page; this panel follows the
	// value and never writes it. The refresh interval comes from the server
	// setting below, so it needs no listener of its own.
	const disabled = useLocalStorageValue("sidebarQuotaDisabled", false, {
		deserialize: storedBool,
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

	const { data: settings } = useSettingsQuery();

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
		isOllamaCloudRefetching,
		isNeuralwattRefetching,
		isOpenCodeGoRefetching,
	} = quotaData;

	const anyRefreshing =
		isNanoRefetching ||
		isZaiCodingRefetching ||
		isKimiCodeRefetching ||
		isMiniMaxRefetching ||
		isDsRefetching ||
		isOrRefetching ||
		isOllamaCloudRefetching ||
		isNeuralwattRefetching ||
		isOpenCodeGoRefetching;

	const isAutoRefreshing = anyRefreshing && !collapsed;

	const handleRefresh = useCallback(() => {
		const now = Date.now();
		if (now - lastManualRefresh.current < quotaRefreshCooldownMs) {
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

	// The modals themselves are mounted once by QuotaModalsHost in Layout.
	const { setOpen } = useQuotaModal();

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

			<CollapseBody collapsed={collapsed}>
				<div className="flex flex-wrap gap-1 justify-center">
					<QuotaBadges
						quotaData={quotaData}
						variant="sidebar"
						onNanoClick={() => setOpen("nanogpt")}
						onZaiCodingClick={() => setOpen("zai-coding")}
						onKimiCodeClick={() => setOpen("kimi-code")}
						onMiniMaxClick={() => setOpen("minimax")}
						onDeepseekClick={handleRefresh}
						onOpenRouterClick={() => setOpen("openrouter")}
						onOllamaCloudClick={handleRefresh}
						onNeuralwattClick={() => setOpen("neuralwatt")}
						onOpenCodeGoClick={() => setOpen("opencode-go")}
					/>
				</div>
			</CollapseBody>
		</div>
	);
}
