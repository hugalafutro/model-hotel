import {
	AlertTriangle,
	Box,
	Calendar,
	Clock,
	Gauge,
	Hash,
	Info,
	Key,
	Layers,
	Server,
	Timer,
	Zap,
} from "lucide-react";
import { useTranslation } from "react-i18next";
import type { LogEntry } from "../api/types";
import { formatMs } from "../pages/Logs/utils";
import { CopyablePill } from "./CopyablePill";
import { DetailItem } from "./LogDetailItem";
import { StatusBadge } from "./LogDetailStatusBadge";
import { formatDateTime, splitDuration } from "./logDetailUtils";
import { EndpointTypeBadge } from "./logs";
import { Modal } from "./Modal";

export function RequestLogDetail({
	requestLog,
	onClose,
}: {
	requestLog: LogEntry;
	onClose: () => void;
}) {
	const { t } = useTranslation();
	const totalTokens =
		requestLog.tokens_prompt +
		requestLog.tokens_completion +
		requestLog.tokens_completion_reasoning;
	const hasCache =
		requestLog.tokens_prompt_cache_hit > 0 ||
		requestLog.tokens_prompt_cache_miss > 0;
	const hasReasoning = requestLog.tokens_completion_reasoning > 0;

	return (
		<Modal
			header={
				<div className="flex items-center gap-3 flex-wrap mb-4">
					<h2 className="text-xl font-bold text-(--text-primary)">
						{t("components.requestLogDetail.title")}
					</h2>
					<StatusBadge
						code={requestLog.status_code}
						state={requestLog.state}
						errorMessage={requestLog.error_message}
					/>
					<EndpointTypeBadge
						endpointType={requestLog.endpoint_type}
						showLabel
					/>
					{requestLog.failover_attempt > 0 && (
						<span className="ui-badge inline-flex items-center gap-1 px-2 py-1 leading-[1.6] text-xs font-medium bg-purple-500/15 text-purple-400 border border-purple-500/30">
							<Layers size={12} />
							<span className="badge-text">
								{t("components.requestLogDetail.attempt", {
									number: requestLog.failover_attempt + 1,
								})}
							</span>
						</span>
					)}
				</div>
			}
			onClose={onClose}
			maxWidth="max-w-2xl"
			scrollable
		>
			{/* Timing Overview */}
			<div className="grid grid-cols-2 sm:grid-cols-5 gap-3 mb-6">
				<div className="p-3 rounded-lg bg-(--surface-bg) border border-(--border-subtle) text-center">
					<Clock size={16} className="mx-auto mb-1 text-(--accent)" />
					<div className="text-lg font-bold text-(--text-primary)">
						{(() => {
							const d = splitDuration(requestLog.duration_ms);
							return (
								<>
									{d.value}
									<span className="text-(--text-tertiary)">{d.unit}</span>
								</>
							);
						})()}
					</div>
					<div className="flex items-center justify-center gap-1 text-[10px] uppercase tracking-wider text-(--text-tertiary)">
						{t("components.requestLogDetail.duration")}
						<span
							title={t("components.requestLogDetail.totalWallClockTime")}
							className="text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)] transition-all cursor-help"
						>
							<Info size={12} />
						</span>
					</div>
				</div>
				<div className="p-3 rounded-lg bg-(--surface-bg) border border-(--border-subtle) text-center">
					<Timer size={16} className="mx-auto mb-1 text-(--accent)" />
					<div className="text-lg font-bold text-(--text-primary)">
						{requestLog.response_header_ms > 0
							? (() => {
									const d = splitDuration(requestLog.response_header_ms);
									return (
										<>
											{d.value}
											<span className="text-(--text-tertiary)">{d.unit}</span>
										</>
									);
								})()
							: "-"}
					</div>
					<div className="flex items-center justify-center gap-1 text-[10px] uppercase tracking-wider text-(--text-tertiary)">
						{t("components.requestLogDetail.headers")}
						<span
							title={t("components.requestLogDetail.timeToReceiveHeaders")}
							className="text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)] transition-all cursor-help"
						>
							<Info size={12} />
						</span>
					</div>
				</div>
				<div className="p-3 rounded-lg bg-(--surface-bg) border border-(--border-subtle) text-center">
					<Timer size={16} className="mx-auto mb-1 text-(--accent)" />
					<div className="text-lg font-bold text-(--text-primary)">
						{requestLog.ttft_ms > 0
							? (() => {
									const d = splitDuration(requestLog.ttft_ms);
									return (
										<>
											{d.value}
											<span className="text-(--text-tertiary)">{d.unit}</span>
										</>
									);
								})()
							: "-"}
					</div>
					<div className="flex items-center justify-center gap-1 text-[10px] uppercase tracking-wider text-(--text-tertiary)">
						{t("components.requestLogDetail.ttft")}
						<span
							title={t("components.requestLogDetail.timeToFirstToken")}
							className="text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)] transition-all cursor-help"
						>
							<Info size={12} />
						</span>
					</div>
				</div>
				<div className="p-3 rounded-lg bg-(--surface-bg) border border-(--border-subtle) text-center">
					<Zap size={16} className="mx-auto mb-1 text-(--accent)" />
					<div
						className={`text-lg font-bold ${requestLog.tokens_prompt_cache_hit > 0 ? "text-(--text-tertiary)" : "text-(--text-primary)"}`}
						title={
							requestLog.tokens_prompt_cache_hit > 0
								? t("components.virtualLogTable.inflatedByCacheHits")
								: undefined
						}
					>
						{(requestLog.tokens_per_second ?? 0) > 0
							? (requestLog.tokens_per_second as number).toFixed(1)
							: "-"}
					</div>
					<div className="flex items-center justify-center gap-1 text-[10px] uppercase tracking-wider text-(--text-tertiary)">
						{t("components.requestLogDetail.tokensPerSecond")}
						<span
							title={t("components.requestLogDetail.outputTokensPerSecond")}
							className="text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)] transition-all cursor-help"
						>
							<Info size={12} />
						</span>
					</div>
				</div>
				<div className="p-3 rounded-lg bg-(--surface-bg) border border-(--border-subtle) text-center">
					<Gauge size={16} className="mx-auto mb-1 text-(--accent)" />
					<div className="text-lg font-bold text-(--text-primary)">
						{totalTokens > 0 ? totalTokens.toLocaleString() : "-"}
					</div>
					<div className="flex items-center justify-center gap-1 text-[10px] uppercase tracking-wider text-(--text-tertiary)">
						{t("components.requestLogDetail.totalTokens")}
						<span
							title={t("components.requestLogDetail.sumOfTokens")}
							className="text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)] transition-all cursor-help"
						>
							<Info size={12} />
						</span>
					</div>
				</div>
			</div>

			{/* Details Grid */}
			<div className="grid grid-cols-1 sm:grid-cols-2 gap-3 mb-6">
				<DetailItem
					icon={Calendar}
					label={t("components.requestLogDetail.timestamp")}
					value={formatDateTime(requestLog.created_at)}
				/>
				<DetailItem
					icon={Hash}
					label={t("components.requestLogDetail.requestHash")}
					value={requestLog.request_hash}
					mono
				/>
				<DetailItem icon={Box} label={t("components.requestLogDetail.model")}>
					<CopyablePill
						text={requestLog.model_id}
						tooltip={t("components.requestLogDetail.copyModelId")}
						textClassName="font-mono text-sm"
						lines={2}
					/>
					{requestLog.model_id.startsWith("hotel/") &&
						requestLog.resolved_model_id && (
							<span className="text-xs text-gray-500 ml-1">
								({t("components.requestLogDetail.resolved")}{" "}
								<span className="text-(--accent) font-mono">
									{requestLog.resolved_model_id}
								</span>
								)
							</span>
						)}
				</DetailItem>
				<DetailItem
					icon={Server}
					label={t("components.requestLogDetail.provider")}
					value={requestLog.provider_name}
				/>
				<DetailItem
					icon={Hash}
					label={t("components.requestLogDetail.dbRowId")}
				>
					<CopyablePill
						text={requestLog.id}
						tooltip={t("components.requestLogDetail.copyDbRowId")}
						textClassName="font-mono text-sm"
					/>
				</DetailItem>
				<DetailItem
					icon={Key}
					label={t("components.requestLogDetail.virtualKey")}
					value={
						requestLog.virtual_key_name || requestLog.virtual_key_id || "-"
					}
				/>
			</div>

			{/* Token Breakdown */}
			{totalTokens > 0 && (
				<div className="mb-6 p-4 rounded-lg bg-(--surface-bg) border border-(--border-subtle)">
					<h4 className="text-sm font-semibold text-(--text-primary) mb-3 flex items-center gap-2">
						<Layers size={14} className="text-(--accent)" />
						{t("components.requestLogDetail.tokenUsage")}
					</h4>
					<div className="grid grid-cols-3 gap-3">
						<div>
							<div className="text-[11px] uppercase text-(--text-tertiary)">
								{t("components.requestLogDetail.prompt")}
							</div>
							<div className="text-sm font-mono text-(--text-primary)">
								{requestLog.tokens_prompt.toLocaleString()}
							</div>
						</div>
						<div>
							<div className="text-[11px] uppercase text-(--text-tertiary)">
								{t("components.requestLogDetail.completion")}
							</div>
							<div className="text-sm font-mono text-(--text-primary)">
								{requestLog.tokens_completion.toLocaleString()}
							</div>
						</div>
						{hasReasoning && (
							<div>
								<div className="text-[11px] uppercase text-(--text-tertiary)">
									{t("components.requestLogDetail.reasoning")}
								</div>
								<div className="text-sm font-mono text-purple-400">
									{requestLog.tokens_completion_reasoning.toLocaleString()}
								</div>
							</div>
						)}
						{hasCache && (
							<div className="col-span-3">
								<div className="grid grid-cols-3 gap-3">
									<div>
										<div className="text-[11px] uppercase text-(--text-tertiary)">
											{t("components.requestLogDetail.cacheHit")}
										</div>
										<div className="text-sm font-mono text-green-400">
											{requestLog.tokens_prompt_cache_hit.toLocaleString()}
										</div>
									</div>
									<div>
										<div className="text-[11px] uppercase text-(--text-tertiary)">
											{t("components.requestLogDetail.cacheMiss")}
										</div>
										<div className="text-sm font-mono text-orange-400">
											{requestLog.tokens_prompt_cache_miss.toLocaleString()}
										</div>
									</div>
								</div>
							</div>
						)}
					</div>
				</div>
			)}

			{/* Overhead Breakdown */}
			{requestLog.proxy_overhead_ms > 0 && (
				<div className="mb-6 p-4 rounded-lg bg-(--surface-bg) border border-(--border-subtle)">
					<h4 className="text-sm font-semibold text-(--text-primary) mb-3 flex items-center gap-2">
						<Gauge size={14} className="text-(--accent)" />
						{t("components.requestLogDetail.proxyOverheadBreakdown")}
					</h4>
					<div className="space-y-2">
						{[
							{
								label: t("components.requestLogDetail.requestParsing"),
								value: requestLog.parse_ms,
								tooltip: t("components.requestLogDetail.timeToParseRequest"),
								cacheHit: null as boolean | null,
							},
							{
								label: t("components.requestLogDetail.failoverGroupLookup"),
								value: requestLog.failover_lookup_ms,
								tooltip: t("components.requestLogDetail.timeToResolveFailover"),
								cacheHit: requestLog.cache_hits?.failover ?? null,
							},
							{
								label: t("components.requestLogDetail.modelLookup"),
								value: requestLog.model_lookup_ms,
								tooltip: t("components.requestLogDetail.timeToLookupModel"),
								cacheHit: requestLog.cache_hits?.model ?? null,
							},
							{
								label: t("components.requestLogDetail.providerLookup"),
								value: requestLog.provider_lookup_ms,
								tooltip: t("components.requestLogDetail.timeToLookupProvider"),
								cacheHit: requestLog.cache_hits?.provider ?? null,
							},
							{
								label: t("components.requestLogDetail.keyDecryption"),
								value: requestLog.key_decrypt_ms,
								tooltip: t("components.requestLogDetail.timeToDecryptKey"),
								cacheHit: requestLog.cache_hits?.key ?? null,
							},
							{
								label: t("components.requestLogDetail.dialDnsTcp"),
								value: requestLog.dial_ms,
								tooltip: t("components.requestLogDetail.timeToEstablishTcp"),
								cacheHit: null as boolean | null,
							},
							{
								label: t("components.requestLogDetail.settingsReads"),
								value: requestLog.settings_read_ms,
								tooltip: t("components.requestLogDetail.timeToReadSettings"),
								cacheHit: requestLog.cache_hits?.settings ?? null,
							},
						].map(
							({ label, value, tooltip, cacheHit }) =>
								(value > 0 ||
									(label === t("components.requestLogDetail.dialDnsTcp") &&
										value === 0)) && (
									<div key={label} className="flex justify-between text-sm">
										<span className="flex items-center gap-1 text-(--text-secondary)">
											{label}
											<span
												title={
													cacheHit === null
														? tooltip
														: cacheHit
															? `${tooltip} ${t("components.requestLogDetail.overheadCacheHit")}`
															: `${tooltip} ${t("components.requestLogDetail.overheadCacheMiss")}`
												}
												className="text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)] transition-all cursor-help"
											>
												<Info size={12} />
											</span>
										</span>
										<span
											className={`font-mono ${
												cacheHit === true
													? "text-emerald-400"
													: cacheHit === false
														? "text-amber-400"
														: "text-(--text-primary)"
											}`}
										>
											{label === t("components.requestLogDetail.dialDnsTcp") &&
											value === 0
												? t("components.requestLogDetail.reused")
												: formatMs(value, 3)}
										</span>
									</div>
								),
						)}
						<div className="border-t border-(--border-default) my-2" />
						<div className="flex justify-between text-sm font-semibold">
							<span className="text-(--text-primary)">
								{t("components.requestLogDetail.totalOverhead")}
							</span>
							<span className="font-mono text-(--accent)">
								{formatMs(
									(requestLog.parse_ms || 0) +
										(requestLog.failover_lookup_ms || 0) +
										(requestLog.model_lookup_ms || 0) +
										(requestLog.provider_lookup_ms || 0) +
										(requestLog.key_decrypt_ms || 0) +
										(requestLog.dial_ms || 0) +
										(requestLog.settings_read_ms || 0),
									3,
								)}
							</span>
						</div>
					</div>
				</div>
			)}

			{/* Error Message */}
			{requestLog.error_message && (
				<div className="p-4 rounded-lg bg-red-500/10 border border-red-500/30">
					<div className="flex items-center gap-2 mb-2">
						<AlertTriangle size={14} className="text-red-400" />
						<CopyablePill
							text={requestLog.error_message}
							displayText={t("components.requestLogDetail.error")}
							tooltip={t("components.requestLogDetail.copyErrorMessage")}
							textClassName="text-sm font-semibold text-red-400"
							iconClassName="text-red-400/50 hover:text-red-300"
						/>
					</div>
					<pre className="text-sm text-red-300 font-mono whitespace-pre-wrap break-words max-h-60 overflow-y-auto">
						{requestLog.error_message}
					</pre>
				</div>
			)}
		</Modal>
	);
}
