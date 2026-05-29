import { Activity, Calendar, FileText, Tag } from "lucide-react";
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

export function LogDetailModal({ log, type, onClose }: LogDetailModalProps) {
	if (!log) return null;

	if (type === "request" && isRequestLog(log)) {
		return <RequestLogDetail requestLog={log} onClose={onClose} />;
	}

	// App Log Detail
	const appLog = log as AppLogEntry;
	return (
		<Modal
			title="Log Entry Details"
			onClose={onClose}
			maxWidth="max-w-lg"
			scrollable
		>
			<div className="space-y-3">
				<DetailItem
					icon={Calendar}
					label="Timestamp"
					value={formatDateTime(appLog.timestamp)}
					accent
				/>
				<DetailItem
					icon={Activity}
					label="Level"
					value={appLog.level.toUpperCase()}
					accent
				>
					<span
						className={`inline-flex items-center px-2 py-0.5 rounded-full text-xs font-medium ${
							appLog.level === "error"
								? "bg-red-500/15 text-red-400 border border-red-500/30"
								: appLog.level === "warning"
									? "bg-yellow-500/15 text-yellow-400 border border-yellow-500/30"
									: "bg-blue-500/15 text-blue-400 border border-blue-500/30"
						}`}
					>
						{appLog.level.toUpperCase()}
					</span>
				</DetailItem>
				<DetailItem
					icon={Tag}
					label="Source"
					value={appLog.source || "-"}
					accent
				/>
				<DetailItem
					icon={FileText}
					label="Message"
					accent
					labelExtra={
						<CopyablePill
							text={appLog.message}
							displayText="Copy"
							tooltip="Copy message"
							textClassName="text-[11px] uppercase tracking-wider"
							iconClassName="w-3 h-3"
						/>
					}
				>
					<pre className="text-sm text-(--text-primary) font-mono whitespace-pre-wrap break-words bg-(--surface-elevated) p-3 rounded-lg border border-(--border-subtle) max-h-60 overflow-y-auto">
						{appLog.message}
					</pre>
				</DetailItem>
			</div>
		</Modal>
	);
}
