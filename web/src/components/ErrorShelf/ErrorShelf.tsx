import { useCallback, useEffect, useId, useState } from "react";
import { useTranslation } from "react-i18next";
import {
	AlertTriangle,
	ChevronUp,
	Copy,
	Eraser,
	ExternalLink,
	X,
} from "@/lib/icons";
import type { AppLogEntry, LogEntry } from "../../api/types";
import { useToast } from "../../context/ToastContext";
import { formatRelativeTime, formatTimestamp } from "../../utils/format";
import { truncateWithEllipsis } from "../../utils/truncate";
import { LogDetailModal } from "../LogDetailModal";
import { type ShelfError, useErrorShelf } from "./useErrorShelf";

/**
 * Sidebar error shelf — a collapsible header that expands into a newest-first,
 * interleaved feed of recent request (5xx) and app (error-level) failures.
 * Each row can be copied, opened in the log-detail modal, or acknowledged;
 * "Clear all" acks the whole set. The shelf hides itself entirely once nothing
 * is unacknowledged. Replaces the old two-pill LastErrorPills.
 */
export function ErrorShelf() {
	const { t } = useTranslation();
	const { toast } = useToast();
	const { unacked, ack, ackAll } = useErrorShelf();
	const [expanded, setExpanded] = useState(false);
	// Two-step Clear all: first click arms (shows a confirm hint), second
	// commits. Auto-disarms after a few seconds so a stray click doesn't linger.
	const [clearArmed, setClearArmed] = useState(false);
	const [detailEntry, setDetailEntry] = useState<{
		log: LogEntry | AppLogEntry;
		type: "request" | "app";
	} | null>(null);
	const listId = useId();

	useEffect(() => {
		if (!clearArmed) return;
		const id = setTimeout(() => setClearArmed(false), 3000);
		return () => clearTimeout(id);
	}, [clearArmed]);

	const handleAck = useCallback(
		(key: string) => {
			ack(key);
			toast(t("layout.toast.errorAcknowledged"), "info");
		},
		[ack, toast, t],
	);

	const handleClearAll = useCallback(() => {
		if (!clearArmed) {
			setClearArmed(true);
			return;
		}
		setClearArmed(false);
		ackAll();
		toast(t("layout.toast.errorsCleared"), "info");
	}, [clearArmed, ackAll, toast, t]);

	const handleCopy = useCallback(
		(message: string) => {
			// Run the write inside a promise chain so a missing Clipboard API
			// (navigator.clipboard is undefined in non-secure/HTTP contexts)
			// surfaces as a rejection and hits the failure toast, rather than
			// throwing synchronously and bypassing it.
			Promise.resolve()
				.then(() => navigator.clipboard.writeText(message))
				.then(() => toast(t("common.copiedToClipboard"), "info"))
				.catch(() => toast(t("common.failedToCopy"), "error"));
		},
		[toast, t],
	);

	const handleViewDetails = useCallback((err: ShelfError) => {
		// entry is always populated by the hook, so the log-detail modal is the
		// only path; LogDetailModal renders null defensively if it ever isn't.
		setDetailEntry({ log: err.entry, type: err.kind });
	}, []);

	// Nothing new to surface: stay out of the way.
	if (unacked.length === 0) {
		return detailEntry ? (
			<LogDetailModal
				log={detailEntry.log}
				type={detailEntry.type}
				onClose={() => setDetailEntry(null)}
			/>
		) : null;
	}

	return (
		<>
			{detailEntry && (
				<LogDetailModal
					log={detailEntry.log}
					type={detailEntry.type}
					onClose={() => setDetailEntry(null)}
				/>
			)}
			<div
				className="ui-error-shelf mb-2 overflow-hidden rounded-lg border border-[var(--error-border)] bg-[var(--error-bg)]"
				data-testid="error-shelf"
			>
				{/* Header is a single toggle button whose layout never changes
				    between states — the caret stays pinned far-right so the
				    collapse affordance doesn't wander. It points up (the shelf
				    opens upward) and rotates down when open. */}
				<button
					type="button"
					onClick={() => {
						setExpanded((v) => !v);
						setClearArmed(false);
					}}
					aria-expanded={expanded}
					aria-controls={listId}
					className="ui-error-shelf-toggle flex w-full items-center gap-2 bg-[var(--error-bg-strong)] px-2.5 py-1.5 text-left"
				>
					<AlertTriangle
						size={12}
						className="ui-error-shelf-spark shrink-0 text-[var(--error-icon)]"
					/>
					<span className="text-[11px] font-semibold uppercase tracking-wider text-[var(--error-text)]">
						{t("layout.errorShelf.title")}
					</span>
					<span
						className="ui-error-shelf-badge text-[10px] font-bold tabular-nums"
						data-testid="error-shelf-count"
					>
						{unacked.length}
					</span>
					<span className="flex-1" />
					<ChevronUp
						size={13}
						className={`ui-error-shelf-caret shrink-0 text-[var(--error-text-muted)] transition-transform duration-200 ${
							expanded ? "rotate-180" : ""
						}`}
					/>
				</button>

				{/* The feed: a Clear-all toolbar (kept out of the header so the
				    header never shifts), then the newest-first scrollable list. */}
				{expanded && (
					<div id={listId}>
						<div className="ui-error-shelf-toolbar flex items-center justify-end gap-2 border-b border-[var(--error-border)] px-2 py-1">
							{clearArmed && (
								<span
									className="ui-error-shelf-confirm text-[10px] font-medium text-[var(--error-text)]"
									data-testid="error-shelf-clear-confirm"
								>
									{t("layout.errorShelf.clearAllConfirm")}
								</span>
							)}
							<button
								type="button"
								onClick={handleClearAll}
								className={`ui-error-shelf-action flex items-center gap-1 rounded px-1.5 py-0.5 text-[10px] font-medium ${
									clearArmed
										? "ui-error-shelf-action--armed"
										: "text-[var(--error-text-muted)] hover:text-[var(--error-text)]"
								}`}
								title={t("layout.errorShelf.clearAll")}
								aria-label={
									clearArmed
										? t("layout.errorShelf.clearAllConfirm")
										: t("layout.errorShelf.clearAll")
								}
								data-testid="error-shelf-clear-all"
							>
								<Eraser size={11} />
								{t("layout.errorShelf.clearAll")}
							</button>
						</div>
						<ul className="ui-error-shelf-list max-h-[40vh] divide-y divide-[var(--error-border)] overflow-y-auto">
							{unacked.map((err, i) => (
								<li
									key={err.key}
									className={`ui-error-shelf-row ui-error-shelf-row--${err.kind} group/row px-2.5 py-1.5`}
									style={{ animationDelay: `${Math.min(i, 10) * 28}ms` }}
									data-testid="error-shelf-row"
								>
									<div className="flex items-center gap-1.5">
										<span
											className={`ui-error-shelf-chip ui-error-shelf-chip--${err.kind} shrink-0`}
											data-testid={`error-shelf-chip-${err.kind}`}
										>
											{err.kind === "request"
												? t("layout.errorShelf.requestKind")
												: t("layout.errorShelf.appKind")}
										</span>
										<span
											className="text-[10px] text-[var(--error-text-muted)] tabular-nums"
											title={formatTimestamp(err.timestamp)}
										>
											{formatRelativeTime(err.timestamp)}
										</span>
										<span className="flex-1" />
										<div className="flex shrink-0 items-center gap-0.5 opacity-70 transition-opacity group-hover/row:opacity-100">
											<button
												type="button"
												onClick={() => handleCopy(err.message)}
												className="ui-error-shelf-rowbtn"
												title={t("layout.errorShelf.copyError")}
											>
												<Copy size={11} />
											</button>
											<button
												type="button"
												onClick={() => handleViewDetails(err)}
												className="ui-error-shelf-rowbtn"
												title={t("layout.errorShelf.viewDetails")}
											>
												<ExternalLink size={11} />
											</button>
											<button
												type="button"
												onClick={() => handleAck(err.key)}
												className="ui-error-shelf-rowbtn"
												title={t("layout.errorShelf.acknowledge")}
												data-testid="error-shelf-ack"
											>
												<X size={11} />
											</button>
										</div>
									</div>
									<p
										className="ui-error-shelf-msg mt-0.5 break-words font-mono text-[9.5px] leading-relaxed text-[var(--error-text-muted)]"
										title={err.message}
									>
										{err.message.length > 200
											? truncateWithEllipsis(err.message, 200)
											: err.message}
									</p>
								</li>
							))}
						</ul>
					</div>
				)}
			</div>
		</>
	);
}
