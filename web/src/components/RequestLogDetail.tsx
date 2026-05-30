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
import type { LogEntry } from "../api/types";
import { formatMs } from "../pages/Logs/utils";
import { CopyablePill } from "./CopyablePill";
import { DetailItem } from "./LogDetailItem";
import { StatusBadge } from "./LogDetailStatusBadge";
import { formatDateTime, splitDuration } from "./logDetailUtils";
import { Modal } from "./Modal";

export function RequestLogDetail({
	requestLog,
	onClose,
}: {
	requestLog: LogEntry;
	onClose: () => void;
}) {
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
						Request Details
					</h2>
					<StatusBadge
						code={requestLog.status_code}
						state={requestLog.state}
						errorMessage={requestLog.error_message}
					/>
					{requestLog.failover_attempt > 0 && (
						<span className="inline-flex items-center gap-1 px-2 py-1 rounded-full text-xs font-medium bg-purple-500/15 text-purple-400 border border-purple-500/30">
							<Layers size={12} />
							Attempt {requestLog.failover_attempt + 1}
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
						Duration
						<span
							title="Total wall-clock time from request start to response end"
							className="text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)] transition-all"
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
						Headers
						<span
							title="Time to receive the first HTTP response headers from the upstream provider"
							className="text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)] transition-all"
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
						TTFT
						<span
							title="Time to First Token: delay between request start and the first token of the response body (streaming) or full response (non-streaming)"
							className="text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)] transition-all"
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
								? "Inflated by prompt cache hits"
								: undefined
						}
					>
						{(requestLog.tokens_per_second ?? 0) > 0
							? (requestLog.tokens_per_second as number).toFixed(1)
							: "-"}
					</div>
					<div className="flex items-center justify-center gap-1 text-[10px] uppercase tracking-wider text-(--text-tertiary)">
						Tokens/s
						<span
							title="Output tokens per second during the generation phase (excludes time-to-first-token). Shown as '-' when generation time is negligible."
							className="text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)] transition-all"
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
						Total Tokens
						<span
							title="Sum of prompt + completion + reasoning tokens"
							className="text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)] transition-all"
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
					label="Timestamp"
					value={formatDateTime(requestLog.created_at)}
				/>
				<DetailItem
					icon={Hash}
					label="Request Hash"
					value={requestLog.request_hash}
					mono
				/>
				<DetailItem icon={Box} label="Model">
					<CopyablePill
						text={requestLog.model_id}
						tooltip="Copy model ID"
						textClassName="font-mono text-sm"
						lines={2}
					/>
					{requestLog.model_id.startsWith("hotel/") &&
						requestLog.resolved_model_id && (
							<span className="text-xs text-gray-500 ml-1">
								(resolved:{" "}
								<span className="text-(--accent) font-mono">
									{requestLog.resolved_model_id}
								</span>
								)
							</span>
						)}
				</DetailItem>
				<DetailItem
					icon={Server}
					label="Provider"
					value={requestLog.provider_name}
				/>
				<DetailItem icon={Hash} label="DB Row ID">
					<CopyablePill
						text={requestLog.id}
						tooltip="Copy DB row ID"
						textClassName="font-mono text-sm"
					/>
				</DetailItem>
				<DetailItem
					icon={Key}
					label="Virtual Key"
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
						Token Usage
					</h4>
					<div className="grid grid-cols-3 gap-3">
						<div>
							<div className="text-[11px] uppercase text-(--text-tertiary)">
								Prompt
							</div>
							<div className="text-sm font-mono text-(--text-primary)">
								{requestLog.tokens_prompt.toLocaleString()}
							</div>
						</div>
						<div>
							<div className="text-[11px] uppercase text-(--text-tertiary)">
								Completion
							</div>
							<div className="text-sm font-mono text-(--text-primary)">
								{requestLog.tokens_completion.toLocaleString()}
							</div>
						</div>
						{hasReasoning && (
							<div>
								<div className="text-[11px] uppercase text-(--text-tertiary)">
									Reasoning
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
											Cache Hit
										</div>
										<div className="text-sm font-mono text-green-400">
											{requestLog.tokens_prompt_cache_hit.toLocaleString()}
										</div>
									</div>
									<div>
										<div className="text-[11px] uppercase text-(--text-tertiary)">
											Cache Miss
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
						Proxy Overhead Breakdown
					</h4>
					<div className="space-y-2">
						{[
							{
								label: "Request Parsing",
								value: requestLog.parse_ms,
								tooltip: "Time to parse and validate the incoming request body",
							},
							{
								label: "Failover Group Lookup",
								value: requestLog.failover_lookup_ms,
								tooltip:
									"Time to resolve the failover group to a specific model and provider",
							},
							{
								label: "Model Lookup",
								value: requestLog.model_lookup_ms,
								tooltip:
									"Time to look up the model configuration in the database",
							},
							{
								label: "Provider Lookup",
								value: requestLog.provider_lookup_ms,
								tooltip: "Time to look up the provider details in the database",
							},
							{
								label: "Key Decryption",
								value: requestLog.key_decrypt_ms,
								tooltip: "Time to decrypt the provider API key",
							},
							{
								label: "Dial (DNS+TCP)",
								value: requestLog.dial_ms,
								tooltip:
									"Time to establish the TCP connection to the upstream provider",
							},
							{
								label: "Settings Reads",
								value: requestLog.settings_read_ms,
								tooltip: "Time to read proxy settings from the database",
							},
						].map(
							({ label, value, tooltip }) =>
								(value > 0 || (label === "Dial (DNS+TCP)" && value === 0)) && (
									<div key={label} className="flex justify-between text-sm">
										<span className="flex items-center gap-1 text-(--text-secondary)">
											{label}
											<span
												title={tooltip}
												className="text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)] transition-all"
											>
												<Info size={12} />
											</span>
										</span>
										<span className="font-mono text-(--text-primary)">
											{label === "Dial (DNS+TCP)" && value === 0
												? "reused"
												: formatMs(value, 3)}
										</span>
									</div>
								),
						)}
						<div className="border-t border-(--border-default) my-2" />
						<div className="flex justify-between text-sm font-semibold">
							<span className="text-(--text-primary)">Total Overhead</span>
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
							displayText="Error"
							tooltip="Copy error message"
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
