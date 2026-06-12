import { Activity, Calendar, FileText, Tag } from "lucide-react";
import { useTranslation } from "react-i18next";
import type { AppLogEntry, LogEntry } from "../api/types";
import { CopyablePill } from "./CopyablePill";
import { DetailItem } from "./LogDetailItem";
import { formatDateTime } from "./logDetailUtils";
import { Modal } from "./Modal";
import { RequestLogDetail } from "./RequestLogDetail";

interface LogDetailModalProps {
	log: LogEntry | AppLogEntry | null;
	type: "request" | "app";
	onClose: () => void;
}

function isRequestLog(log: LogEntry | AppLogEntry): log is LogEntry {
	return "request_hash" in log;
}

function AppLogDetail({
	log,
	onClose,
}: {
	log: AppLogEntry;
	onClose: () => void;
}) {
	const { t } = useTranslation();

	return (
		<Modal
			title={t("components.appLogDetail.title")}
			onClose={onClose}
			maxWidth="max-w-lg"
			scrollable
		>
			<div className="space-y-3">
				<DetailItem
					icon={Calendar}
					label={t("components.appLogDetail.timestamp")}
					value={formatDateTime(log.timestamp)}
					accent
				/>
				<DetailItem
					icon={Activity}
					label={t("components.appLogDetail.level")}
					value={log.level.toUpperCase()}
					accent
				>
					<span
						className={`inline-flex items-center px-2 py-px leading-[1.6] text-xs font-medium ui-badge ${
							log.level === "error"
								? "ui-badge-error"
								: log.level === "warning"
									? "ui-badge-warning"
									: "ui-badge-info"
						}`}
					>
						<span className="badge-text">{log.level.toUpperCase()}</span>
					</span>
				</DetailItem>
				<DetailItem
					icon={Tag}
					label={t("components.appLogDetail.source")}
					value={log.source || "-"}
					accent
				/>
				<DetailItem
					icon={FileText}
					label={t("components.appLogDetail.message")}
					accent
					labelExtra={
						<CopyablePill
							text={log.message}
							displayText={t("common.copy")}
							tooltip={t("components.appLogDetail.copyMessage")}
							textClassName="text-[11px] uppercase tracking-wider"
							iconClassName="w-3 h-3"
						/>
					}
				>
					<pre className="text-sm text-(--text-primary) font-mono whitespace-pre-wrap break-words bg-(--surface-elevated) p-3 rounded-(--radius-box) border border-(--border-subtle) max-h-60 overflow-y-auto">
						{log.message}
					</pre>
				</DetailItem>
			</div>
		</Modal>
	);
}

export function LogDetailModal({ log, type, onClose }: LogDetailModalProps) {
	if (!log) return null;

	if (type === "request" && isRequestLog(log)) {
		return <RequestLogDetail requestLog={log} onClose={onClose} />;
	}

	return <AppLogDetail log={log as AppLogEntry} onClose={onClose} />;
}
