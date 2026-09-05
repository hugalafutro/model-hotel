import type { UseQueryResult } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { RefreshCw } from "@/lib/icons";
import type { api } from "../../../api/client";
import { REASON_CODES } from "./apiText";

type AlertStatus = Awaited<ReturnType<typeof api.alert.status>>;

/**
 * Whether apprise-api can be reached: a dot, a word, an optional reason, and a
 * recheck link. Sits beside the destination list it qualifies.
 */
export function AppriseStatus({
	statusQuery,
}: {
	statusQuery: UseQueryResult<AlertStatus>;
}) {
	const { t } = useTranslation();
	const status = statusQuery.data;
	const statusDotColor =
		status?.reachable && status.healthy
			? "var(--success-text)"
			: status?.reachable
				? "var(--warning-text)"
				: "var(--error-text)";
	const statusText =
		status?.reachable && status.healthy
			? t("settings.alerts.status.reachable")
			: status?.reachable
				? t("settings.alerts.status.issues")
				: t("settings.alerts.status.unreachable");
	// The reason code is the translated, actionable half of the probe result; the
	// detail is raw server text (English, sometimes an HTTP status). The note
	// therefore carries the reason and keeps the detail as the tooltip, where an
	// operator who wants the literal answer can still find it.
	const statusReason =
		status &&
		(!status.reachable || !status.healthy) &&
		status.reason &&
		REASON_CODES.has(status.reason)
			? t(`settings.alerts.reason.${status.reason}`)
			: "";

	return (
		<div className="flex items-center gap-2 text-xs" data-testid="alert-status">
			{statusQuery.isFetching ? (
				<span className="inline-flex items-center gap-1.5 text-(--text-muted)">
					<RefreshCw size={12} className="animate-spin" />
					{t("settings.alerts.status.checking")}
				</span>
			) : statusQuery.isError ? (
				<span className="inline-flex items-center gap-1.5 text-(--text-secondary)">
					<span
						className="inline-block w-2 h-2 rounded-full"
						style={{ background: "var(--error-text)" }}
						aria-hidden="true"
					/>
					{t("settings.alerts.status.checkFailed")}
				</span>
			) : status ? (
				<>
					<span
						className="inline-flex items-center gap-1.5 text-(--text-secondary)"
						title={status.detail}
					>
						<span
							className="inline-block w-2 h-2 rounded-full"
							style={{ background: statusDotColor }}
							aria-hidden="true"
						/>
						{statusText}
					</span>
					{statusReason !== "" && (
						<span
							className="text-(--text-secondary)"
							data-testid="alert-status-note"
						>
							{statusReason}
						</span>
					)}
				</>
			) : null}
			<button
				type="button"
				className="ui-link-accent inline-flex items-center gap-1"
				onClick={() => statusQuery.refetch()}
				data-testid="alert-status-recheck"
			>
				<RefreshCw size={11} />
				{t("settings.alerts.status.recheck")}
			</button>
		</div>
	);
}
