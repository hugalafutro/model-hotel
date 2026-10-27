/* eslint-disable react-refresh/only-export-components -- the quota modal hooks are exported beside the header components they drive */
import { useTranslation } from "react-i18next";
import { ArrowLeftRight, RefreshCw } from "@/lib/icons";
import { useTheme } from "../../context/ThemeContext";
import type { ToastType } from "../../context/ToastContext";
import { useLocalStorage } from "../../hooks/useLocalStorage";
import { formatRelativeTime } from "../../utils/format";
import type { QuotaBarMode } from "../QuotaBadge";
import { Spinner } from "../Spinner";

/** The toast callback every quota modal takes. */
export type OnToast = (msg: string, type: ToastType) => void;

/**
 * Whether the bars read as used or as remaining. One stored preference, so
 * every quota modal and badge flips together.
 */
export function useQuotaBarMode(): [QuotaBarMode, () => void] {
	const [barMode, setBarMode] = useLocalStorage<QuotaBarMode>(
		"quota-bar-mode",
		"remaining",
	);
	return [
		barMode,
		() => setBarMode((prev) => (prev === "remaining" ? "used" : "remaining")),
	];
}

/** A refresh that reports its outcome as a toast. */
export function useQuotaRefreshToast(
	onRefresh: () => Promise<unknown>,
	onToast: OnToast,
) {
	const { t } = useTranslation();
	return async () => {
		try {
			await onRefresh();
			onToast(t("components.providerModals.quotaRefreshed"), "success");
		} catch {
			onToast(t("components.providerModals.failedToRefreshQuota"), "error");
		}
	};
}

/** When the quota shown was last fetched. */
export function LastRefreshedRow({ at }: { at?: number }) {
	const { t } = useTranslation();
	if (!at) return null;
	return (
		<div className="flex justify-between items-center text-xs text-(--text-muted) pt-2">
			<span>{t("components.providerModals.lastRefreshed")}</span>
			<span>{formatRelativeTime(new Date(at).toISOString())}</span>
		</div>
	);
}

interface QuotaModalHeaderActionsProps {
	/** Which way the bars currently read, for the toggle's tooltip. */
	barMode: QuotaBarMode;
	/** Toggle between remaining/used display. */
	onToggleBarMode: () => void;
	/** Trigger a quota refresh. */
	onRefresh: () => void;
	/** Whether a refresh is in progress. */
	isRefreshing: boolean;
}

/**
 * QuotaModalHeaderActions renders the toggle (remaining/used) and refresh
 * buttons in the modal header.
 */
function QuotaModalHeaderActions({
	barMode,
	onToggleBarMode,
	onRefresh,
	isRefreshing,
}: QuotaModalHeaderActionsProps) {
	const { uiStyle } = useTheme();
	const { t } = useTranslation();
	const toggleTitle =
		barMode === "remaining"
			? t("components.providerModals.showQuotaUsed")
			: t("components.providerModals.showQuotaRemaining");

	return (
		<div className="absolute top-4 right-16 flex items-center gap-1">
			<button
				type="button"
				onClick={() => onToggleBarMode()}
				className="ui-icon-btn p-1.5"
				aria-label={t("components.providerModals.toggleRemainingUsed")}
				title={toggleTitle}
			>
				<ArrowLeftRight size={18} />
			</button>
			<button
				type="button"
				onClick={onRefresh}
				disabled={isRefreshing}
				className="ui-icon-btn p-1.5"
				aria-label={t("common.refresh")}
				title={t("components.providerModals.refreshQuotaInfo")}
			>
				{isRefreshing && uiStyle === "cyber-terminal" ? (
					<Spinner className="w-[18px] h-[18px] text-[18px] leading-[18px]" />
				) : (
					<RefreshCw size={18} className={isRefreshing ? "animate-spin" : ""} />
				)}
			</button>
		</div>
	);
}

interface QuotaModalHeaderProps extends QuotaModalHeaderActionsProps {
	/** Already-translated modal title. */
	title: string;
	/** Optional line rendered under the title. */
	subtitle?: React.ReactNode;
}

/**
 * QuotaModalHeader renders a provider quota modal's title, optional subtitle
 * and the toggle/refresh actions every quota modal shares.
 */
export function QuotaModalHeader({
	title,
	subtitle,
	...actions
}: QuotaModalHeaderProps) {
	return (
		<div className="flex justify-between items-start mb-6">
			<div>
				<h2 className="ui-modal-title">{title}</h2>
				{subtitle && <p className="ui-subtitle mt-1">{subtitle}</p>}
			</div>
			<QuotaModalHeaderActions {...actions} />
		</div>
	);
}
