import type { AppLogEntry, LogEntry } from "../api/types";
import { AppLogDetail } from "./AppLogDetail";
import type { ModalNavProps } from "./ModalNav";
import { type LiveClock, RequestLogDetail } from "./RequestLogDetail";

interface LogDetailModalProps {
	log: LogEntry | AppLogEntry | null;
	type: "request" | "app";
	nav?: ModalNavProps;
	/** Ticks an in-progress request's duration, as the row behind the modal does. */
	clock?: LiveClock;
	/** App logs: narrows the list behind the modal to one source. */
	onFilterSource?: (source: string) => void;
	onClose: () => void;
}

function isRequestLog(log: LogEntry | AppLogEntry): log is LogEntry {
	return "request_hash" in log;
}

export function LogDetailModal({
	log,
	type,
	nav,
	clock,
	onFilterSource,
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

	return (
		<AppLogDetail
			log={log as AppLogEntry}
			nav={nav}
			onClose={onClose}
			onFilterSource={onFilterSource}
		/>
	);
}
