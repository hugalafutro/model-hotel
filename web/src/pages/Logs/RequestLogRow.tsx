import { useTranslation } from "react-i18next";
import type { LogEntry } from "../../api/types";
import { Badge } from "../../components/Badge";
import { Row } from "../../components/DataTable";
import { EndpointTypeBadge } from "../../components/logs";
import { formatNumber } from "../../utils/format";
import {
	formatDurationCell,
	getRowStatusVariant,
	isCancelled,
	isInProgress,
	isStale,
	liveDurationMs,
} from "../../utils/logHelpers";
import { formatMs, formatTPS } from "./utils";

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
	const { t } = useTranslation();
	const inProgress = isInProgress(log, nowMs, staleThresholdMs);
	const stale = isStale(log, nowMs, staleThresholdMs);
	const hasOverhead =
		log.proxy_overhead_ms != null &&
		log.proxy_overhead_ms > 0 &&
		(log.parse_ms > 0 ||
			log.model_lookup_ms > 0 ||
			log.provider_lookup_ms > 0 ||
			log.key_decrypt_ms > 0);
	return (
		<Row className={inProgress ? "animate-pulse-subtle" : ""} onClick={onClick}>
			<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-400 font-mono">
				{log.created_at ? new Date(log.created_at).toLocaleString() : "-"}
			</td>
			<td
				className="px-2 py-1 whitespace-nowrap text-xs text-gray-200 truncate"
				title={
					log.model_id?.startsWith("hotel/") && log.resolved_model_id
						? `${log.model_id} (${log.resolved_model_id})`
						: log.model_id
				}
			>
				<EndpointTypeBadge endpointType={log.endpoint_type} />
				{log.model_id ? (
					log.model_id.startsWith("hotel/") ? (
						<>
							<span className="text-(--accent)">{log.model_id}</span>
							{log.resolved_model_id && (
								<span className="text-gray-500">
									{" "}
									({log.resolved_model_id})
								</span>
							)}
						</>
					) : log.model_id.includes("/") ? (
						log.model_id.slice(log.model_id.indexOf("/") + 1)
					) : (
						log.model_id
					)
				) : (
					"-"
				)}
			</td>
			<td
				className="px-2 py-1 whitespace-nowrap text-xs text-gray-300 truncate"
				title={log.provider_name || undefined}
			>
				{log.provider_name === "Deleted" ? (
					<span
						className="text-red-400 italic"
						title={t("logs.table.providerDeleted")}
					>
						{t("logs.table.deletedProvider")}
					</span>
				) : inProgress && !log.provider_name ? (
					<span className="text-blue-400/60 italic">
						{t("logs.table.resolving")}
					</span>
				) : (
					log.provider_name || "-"
				)}
			</td>
			<td className="px-2 py-1 whitespace-nowrap">
				<Badge
					variant={getRowStatusVariant(log, nowMs, staleThresholdMs)}
					className="gap-1 whitespace-nowrap"
				>
					{stale ? (
						<span className="text-yellow-500/70">⚠</span>
					) : inProgress ? (
						<span className="text-blue-400">
							{log.state === "streaming" ? t("logs.table.live") : "…"}
						</span>
					) : (
						log.status_code
					)}
				</Badge>
			</td>
			<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-400 font-mono">
				{isCancelled(log) ? (
					t("logs.table.interrupted")
				) : log.tokens_prompt + log.tokens_completion > 0 ? (
					<>
						{formatNumber(log.tokens_prompt)}
						<span className="text-gray-600">+</span>
						{formatNumber(log.tokens_completion)}
					</>
				) : (
					"-"
				)}
			</td>
			<td className="px-2 py-1 whitespace-nowrap text-xs font-mono">
				{isCancelled(log) ? (
					"-"
				) : (
					<span
						className={
							log.tokens_prompt_cache_hit > 0 ? "opacity-50" : undefined
						}
						title={
							log.tokens_prompt_cache_hit > 0
								? t("logs.table.cacheInflated")
								: undefined
						}
					>
						{formatTPS(log.tokens_per_second)}
					</span>
				)}
			</td>
			<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-400 font-mono">
				{log.response_header_ms > 0 ? formatMs(log.response_header_ms, 1) : "-"}
			</td>
			<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-400 font-mono">
				{log.ttft_ms > 0 ? formatMs(log.ttft_ms, 1) : "-"}
			</td>
			<td className="px-2 py-1 whitespace-nowrap text-xs text-gray-400 font-mono">
				{inProgress && log.duration_ms === 0 ? (
					<span className="inline-block text-blue-400">
						{formatDurationCell(liveDurationMs(log.created_at, nowMs))}
					</span>
				) : log.duration_ms > 0 ? (
					formatDurationCell(log.duration_ms)
				) : (
					"-"
				)}
			</td>
			<td className="px-2 py-1 whitespace-nowrap text-xs font-mono">
				{log.proxy_overhead_ms != null && log.proxy_overhead_ms > 0 ? (
					<span className={hasOverhead ? "text-(--accent)" : "text-gray-400"}>
						{formatMs(log.proxy_overhead_ms)}
					</span>
				) : (
					<span className="text-gray-400">-</span>
				)}
			</td>
			<td
				className="px-2 py-1 text-xs text-gray-400 max-w-[7rem] truncate"
				title={
					log.virtual_key_deleted
						? undefined
						: log.virtual_key_name || log.virtual_key_id || undefined
				}
			>
				{log.virtual_key_deleted ? (
					<span className="text-red-400 italic">
						{t("logs.table.keyDeleted")}
					</span>
				) : log.virtual_key_name &&
					log.virtual_key_name.toLowerCase() === "internal" ? (
					<span className="text-gray-400 italic">{t("common.internal")}</span>
				) : (
					log.virtual_key_name || log.virtual_key_id || "-"
				)}
			</td>
			<td
				className="px-2 py-1 whitespace-nowrap text-xs text-gray-400 font-mono truncate"
				title={log.client_ip || undefined}
			>
				{log.client_ip || "-"}
			</td>
		</Row>
	);
}
