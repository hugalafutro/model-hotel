import type { LogEntry } from "../../api/types";
import { Row } from "../../components/DataTable";
import { RequestLogCells } from "../../components/logs/RequestLogCells";
import { isInProgress } from "../../utils/logHelpers";

/** One request in the paginated table. Cells match LOG_COL_WIDTHS in order. */
export function RequestLogRow({
	log,
	nowMs,
	staleThresholdMs,
	onClick,
}: {
	log: LogEntry;
	nowMs: number;
	staleThresholdMs: number;
	onClick: () => void;
}) {
	return (
		<Row
			className={
				isInProgress(log, nowMs, staleThresholdMs) ? "animate-pulse-subtle" : ""
			}
			onClick={onClick}
		>
			<RequestLogCells
				log={log}
				nowMs={nowMs}
				staleThresholdMs={staleThresholdMs}
			/>
		</Row>
	);
}
