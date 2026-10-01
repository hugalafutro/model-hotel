import { useTranslation } from "react-i18next";
import { Activity, Calendar, FileText, Tag } from "@/lib/icons";
import type { AppLogEntry, LogEntry } from "../api/types";
import {
	formatLogTimestamp,
	getLevelBadgeVariant,
	withRowLabel,
} from "../utils/logBadgeUtils";
import { displayLogMessage } from "../utils/logText";
import { Badge } from "./Badge";
import { CopyablePill } from "./CopyablePill";
import { DetailItem } from "./LogDetailItem";
import { MaybeJsonBlock } from "./MaybeJsonBlock";
import { Modal } from "./Modal";
import type { ModalNavProps } from "./ModalNav";
import { type LiveClock, RequestLogDetail } from "./RequestLogDetail";

interface LogDetailModalProps {
	log: LogEntry | AppLogEntry | null;
	type: "request" | "app";
	nav?: ModalNavProps;
	/** Ticks an in-progress request's duration, as the row behind the modal does. */
	clock?: LiveClock;
	onClose: () => void;
}

function isRequestLog(log: LogEntry | AppLogEntry): log is LogEntry {
	return "request_hash" in log;
}

function AppLogDetail({
	log,
	nav,
	onClose,
}: {
	log: AppLogEntry;
	nav?: ModalNavProps;
	onClose: () => void;
}) {
	const { t } = useTranslation();
	const message = displayLogMessage(log.message, log.escaped, log.attrs_at);

	return (
		<Modal
			title={t("components.appLogDetail.title")}
			nav={withRowLabel(nav, t, log.timestamp, log.level.toUpperCase())}
			onClose={onClose}
			maxWidth="max-w-lg"
			scrollable
		>
			<div className="space-y-3">
				<DetailItem
					icon={Calendar}
					label={t("components.appLogDetail.timestamp")}
					value={formatLogTimestamp(log.timestamp)}
				/>
				<DetailItem icon={Activity} label={t("components.appLogDetail.level")}>
					<Badge
						variant={getLevelBadgeVariant(log.level)}
						className="text-xs px-2"
					>
						{log.level.toUpperCase()}
					</Badge>
				</DetailItem>
				<DetailItem
					icon={Tag}
					label={t("components.appLogDetail.source")}
					value={log.source}
				/>
				<DetailItem
					icon={FileText}
					label={t("components.appLogDetail.message")}
					labelExtra={
						<CopyablePill
							text={message}
							displayText={t("common.copy")}
							tooltip={t("components.appLogDetail.copyMessage")}
							textClassName="text-[11px] uppercase tracking-wider"
							iconClassName="w-3 h-3"
						/>
					}
				>
					<MaybeJsonBlock
						className="text-sm text-(--text-primary) font-mono whitespace-pre-wrap break-words bg-(--surface-elevated) p-3 rounded-(--radius-box) border border-(--border-subtle) max-h-60 overflow-y-auto"
						text={message}
					/>
				</DetailItem>
			</div>
		</Modal>
	);
}

export function LogDetailModal({
	log,
	type,
	nav,
	clock,
	onClose,
}: LogDetailModalProps) {
	if (!log) return null;

	if (type === "request" && isRequestLog(log)) {
		return (
			<RequestLogDetail
				requestLog={log}
				nav={nav}
				clock={clock}
				onClose={onClose}
			/>
		);
	}

	return <AppLogDetail log={log as AppLogEntry} nav={nav} onClose={onClose} />;
}
