import { Bot, Clock, Copy, Info, Maximize2, Settings, Zap } from "lucide-react";
import { memo, type ReactNode, useEffect, useState } from "react";
import type { Components } from "react-markdown";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import type { GenerationParams } from "../api/types";
import { formatDuration } from "../utils/format";
import { MARKDOWN_PROSE_CLASSES, MarkdownContent } from "./MarkdownContent";
import { Modal } from "./Modal";
import { ThinkingBlock } from "./ThinkingBlock";

export { MARKDOWN_PROSE_CLASSES };

export interface ModelReplyMetrics {
	charsPerSecond: number | null;
	durationMs: number;
	promptTokens: number;
	completionTokens: number;
}

interface ModelReplyCardProps {
	/** Model identifier string (e.g. "provider/model-name") */
	model: string;
	/** Rendered markdown content */
	content: string;
	/** Raw thinking/reasoning content */
	thinkingContent?: string;
	/** Error message to display instead of content */
	error?: string | null;
	/** Performance metrics */
	metrics?: ModelReplyMetrics | null;
	/** Whether the response is currently streaming */
	isStreaming: boolean;
	/** Start time in ms since epoch, enables the live elapsed counter */
	startTimeMs?: number;
	/** Show winner ring/glow */
	isWinner?: boolean;
	/** Dim the card (losing side) */
	isLoser?: boolean;
	/** Extra content rendered right after the model name (left side of header) */
	afterModel?: ReactNode;
	/** Actions rendered on the right side of the header (after streaming indicator) */
	headerEnd?: ReactNode;
	/** Content rendered before metrics in the footer (left side) */
	footerStart?: ReactNode;
	/** Content rendered on the right side of the footer */
	footerEnd?: ReactNode;
	/** Tint style for the card — "accent" applies a light accent background tint, "blue" applies a light blue tint */
	tint?: "accent" | "blue" | "default";
	/** Additional class names for the root card element */
	className?: string;
	/** Additional class names for the header row */
	headerClassName?: string;
	/** Additional class names for the body/content area */
	bodyClassName?: string;
	/** Additional class names for the footer row */
	footerClassName?: string;
	/** Tailwind max-width class for the model name (e.g. "max-w-60"), defaults to "max-w-45" */
	modelMaxWidth?: string;
	/** Called when the model name is clicked; enables clickable styling with accent glow */
	onModelNameClick?: () => void;
	/** Whether to shorten the model name to the part after the last "/" (default: true) */
	shortenModelName?: boolean;
	/** Whether to show a small info icon after the model name to indicate clickability */
	showInfoIcon?: boolean;
	/** Generation params used for this response — shown as tooltip on a settings indicator */
	params?: GenerationParams;
	/** Persona name to display in the footer/status bar */
	personaName?: string;
	/** Tooltip text for the persona badge (e.g. full persona prompt) */
	personaTooltip?: string;
	/** Turn number to display in the header (e.g. "Turn 3") */
	turnNumber?: number;
}

/** Larger prose classes used in the maximized modal view */
const MAXIMIZED_PROSE_CLASSES =
	"prose prose-invert prose-base max-w-none text-(--text-primary) text-base font-medium " +
	"[&_p]:my-2.5 [&_ul]:my-2.5 [&_ol]:my-2.5 [&_li]:my-0.5 " +
	"[&_h1]:text-lg [&_h2]:text-base [&_h3]:text-base " +
	"[&_code]:text-(--accent) [&_code]:bg-(--surface-hover) [&_code]:px-1 [&_code]:py-0.5 [&_code]:rounded [&_code]:text-sm " +
	"[&_pre]:bg-(--surface-hover) [&_pre]:rounded-lg [&_pre]:p-4 [&_pre]:overflow-x-auto [&_pre]:my-3 [&_pre]:text-sm " +
	"[&_blockquote]:border-l-2 [&_blockquote]:border-(--accent)/40 [&_blockquote]:pl-4 [&_blockquote]:text-(--text-secondary) " +
	"[&_strong]:font-semibold [&_strong]:text-(--text-primary) [&_em]:text-(--text-secondary) " +
	"[&_a]:text-(--accent) [&_a]:underline " +
	"[&_hr]:border-(--border-subtle) " +
	"[&_table]:text-sm [&_th]:px-2 [&_th]:py-1 [&_td]:px-2 [&_td]:py-1 " +
	"[&_th]:border [&_th]:border-(--border-subtle) [&_td]:border [&_td]:border-(--border-subtle)";

/** Shared markdown renderer components (external links open in new tab) */
const mdComponents: Components = {
	a: ({ children, ...props }) => (
		<a {...props} target="_blank" rel="noopener noreferrer">
			{children}
		</a>
	),
};

export const ModelReplyCard = memo(function ModelReplyCard({
	model,
	content,
	thinkingContent,
	error,
	metrics,
	isStreaming,
	startTimeMs,
	isWinner = false,
	isLoser = false,
	tint = "default",
	afterModel,
	headerEnd,
	footerStart,
	footerEnd,
	className,
	headerClassName,
	bodyClassName,
	footerClassName,
	modelMaxWidth = "max-w-[26rem]",
	onModelNameClick,
	shortenModelName = true,
	showInfoIcon = false,
	params,
	personaName,
	personaTooltip,
	turnNumber,
}: ModelReplyCardProps) {
	const [elapsed, setElapsed] = useState(0);
	const [maximized, setMaximized] = useState(false);

	// Live elapsed timer while streaming
	useEffect(() => {
		if (!isStreaming || !startTimeMs || startTimeMs === 0) return;
		const tick = () =>
			setElapsed(Math.round((Date.now() - startTimeMs) / 1000));
		tick();
		const id = setInterval(tick, 1000);
		return () => clearInterval(id);
	}, [isStreaming, startTimeMs]);

	const hasThinking = (thinkingContent || "").length > 0;
	const displayName = shortenModelName
		? (model.split("/").pop() as string)
		: model;

	// Show maximize button only when streaming finished without error and there's content
	const canMaximize = !isStreaming && !error && content.trim().length > 0;

	const hasCustomParams =
		!!params && Object.values(params).some((v) => v !== undefined);
	const paramsTooltip = hasCustomParams
		? Object.entries(params as GenerationParams)
				.filter(([, v]) => v !== undefined)
				.map(([k, v]) => {
					const label = k
						.replace(/_/g, " ")
						.replace(/^\w/, (c) => c.toUpperCase());
					return `${label}: ${v}`;
				})
				.join("\n")
		: undefined;

	const stateClass = isWinner
		? "ring-1 ring-green-500/40 shadow-[0_0_12px_rgba(34,197,94,0.1)]"
		: isLoser
			? "opacity-60"
			: "";

	const tintClass =
		tint === "accent"
			? "ui-card-tint-accent"
			: tint === "blue"
				? "ui-card-tint-blue"
				: "";

	return (
		<>
			<div
				className={`ui-card transition-all ${stateClass} ${tintClass} ${className || ""}`}
			>
				{/* ── Header ── */}
				{model && (
					<div
						className={`flex items-center justify-between ${headerClassName || ""}`}
					>
						<div className="flex items-center gap-2 min-w-0">
							<Bot size={14} className="text-(--accent) shrink-0" />
							{/* biome-ignore lint/a11y/noStaticElementInteractions: conditionally interactive — role/tabIndex/keyboard handler are only set when onModelNameClick is provided */}
							<div
								role={onModelNameClick ? "button" : undefined}
								tabIndex={onModelNameClick ? 0 : undefined}
								className={`group/button flex items-center gap-1 min-w-0 ${onModelNameClick ? "cursor-pointer" : ""}`}
								onClick={onModelNameClick}
								onKeyDown={
									onModelNameClick
										? (e) => {
												if (e.key === "Enter" || e.key === " ") {
													e.preventDefault();
													onModelNameClick();
												}
											}
										: undefined
								}
							>
								{onModelNameClick ? (
									<span
										className={`text-sm font-medium truncate group-hover/button:text-(--accent) group-hover/button:drop-shadow-[0_0_6px_var(--accent)] transition-all ${modelMaxWidth} ${tint === "accent" || tint === "blue" ? "text-(--accent)" : "text-(--text-primary)"}`}
										title={model}
									>
										{displayName}
									</span>
								) : (
									<span
										className={`text-sm font-medium truncate ${modelMaxWidth} ${tint === "accent" || tint === "blue" ? "text-(--accent)" : "text-(--text-primary)"}`}
										title={model}
									>
										{displayName}
									</span>
								)}
								{showInfoIcon && onModelNameClick && (
									<span
										className="shrink-0 text-(--text-tertiary) group-hover/button:text-(--accent) group-hover/button:drop-shadow-[0_0_6px_var(--accent)] transition-all"
										title="Model details"
									>
										<Info size={12} />
									</span>
								)}
							</div>
							{hasCustomParams && (
								<span
									className="shrink-0 text-(--accent) cursor-help"
									title={paramsTooltip}
								>
									<Settings size={10} />
								</span>
							)}
							{afterModel}
						</div>
						<div className="flex items-center gap-2 shrink-0">
							{turnNumber != null && (
								<span className="text-[11px] text-(--text-tertiary) tabular-nums">
									Turn {turnNumber}
								</span>
							)}
							{canMaximize && (
								<button
									type="button"
									onClick={() => setMaximized(true)}
									className="p-1 rounded-md transition-all cursor-pointer text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[0_0_6px_var(--accent)]"
									title="Maximize reply"
								>
									<Maximize2 size={14} />
								</button>
							)}
							{headerEnd}
						</div>
					</div>
				)}

				{/* ── Body ── */}
				<div className={bodyClassName || ""}>
					{error && !content ? (
						<div className="text-red-400 text-xs">{error}</div>
					) : (
						<>
							{hasThinking && (
								<ThinkingBlock
									thinking={thinkingContent as string}
									isStreaming={isStreaming && !content}
								/>
							)}
							{content ? (
								<MarkdownContent>{content}</MarkdownContent>
							) : !hasThinking && isStreaming ? (
								<div className="text-(--text-tertiary) text-xs flex items-center gap-2">
									<span className="w-1.5 h-1.5 rounded-full bg-(--accent) animate-pulse" />
									Waiting…
								</div>
							) : null}
							{error && content && (
								<div className="mt-3 px-3 py-2 rounded border border-red-500/30 bg-red-500/10 text-red-400 text-xs">
									⚠ {error}
								</div>
							)}
						</>
					)}
				</div>

				{/* ── Footer ── */}
				<div
					className={`flex items-center justify-between text-[11px] text-(--text-tertiary) shrink-0 ${footerClassName || ""}`}
				>
					<div className="flex items-center gap-3">
						{footerStart}
						{personaName && (
							<span
								className="text-[11px] text-(--accent) cursor-help truncate max-w-30"
								title={personaTooltip || personaName}
							>
								{personaName}
							</span>
						)}
						{isStreaming && startTimeMs && startTimeMs !== 0 ? (
							<span className="flex items-center gap-1 tabular-nums">
								<Clock size={10} />
								{elapsed}s
							</span>
						) : metrics ? (
							<>
								<span className="flex items-center gap-1">
									<Clock size={10} />
									{formatDuration(metrics.durationMs)}
								</span>
								{metrics.charsPerSecond !== null && (
									<span className="flex items-center gap-1">
										<Zap size={10} />
										{metrics.charsPerSecond.toFixed(1)} chars/s
									</span>
								)}
								{metrics.promptTokens + metrics.completionTokens > 0 && (
									<span>
										{metrics.promptTokens + metrics.completionTokens} tok
									</span>
								)}
							</>
						) : null}
					</div>
					{footerEnd}
				</div>
			</div>

			{/* ── Maximized Modal ── */}
			{maximized && (
				<Modal
					onClose={() => setMaximized(false)}
					maxWidth="max-w-5xl"
					zIndex="z-50"
				>
					{/* Modal header */}
					<div className="flex items-center justify-between mb-4 -mt-2">
						<div className="flex items-center gap-2 min-w-0">
							<Bot size={18} className="text-(--accent) shrink-0" />
							<span
								className="text-base font-medium text-(--text-primary) truncate"
								title={model}
							>
								{displayName}
							</span>
							{hasCustomParams && (
								<span
									className="shrink-0 text-(--accent) cursor-help"
									title={paramsTooltip}
								>
									<Settings size={12} />
								</span>
							)}
							{afterModel}
						</div>
						<div className="flex items-center gap-3 shrink-0">
							{personaName && (
								<span
									className="text-xs text-(--accent) cursor-help truncate max-w-40"
									title={personaTooltip || personaName}
								>
									{personaName}
								</span>
							)}
							{metrics && (
								<>
									<span className="text-xs text-(--text-tertiary) flex items-center gap-1">
										<Clock size={12} />
										{formatDuration(metrics.durationMs)}
									</span>
									{metrics.charsPerSecond !== null && (
										<span className="text-xs text-(--text-tertiary) flex items-center gap-1">
											<Zap size={12} />
											{metrics.charsPerSecond.toFixed(1)} chars/s
										</span>
									)}
									{metrics.promptTokens + metrics.completionTokens > 0 && (
										<span className="text-xs text-(--text-tertiary)">
											{metrics.promptTokens + metrics.completionTokens} tok
										</span>
									)}
								</>
							)}
							<button
								type="button"
								onClick={() => {
									navigator.clipboard.writeText(content);
								}}
								className="p-1.5 rounded-md transition-all cursor-pointer text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[0_0_6px_var(--accent)]"
								title="Copy"
							>
								<Copy size={16} />
							</button>
						</div>
					</div>

					{/* Modal body — thinking + content */}
					<div className="max-h-[85vh] overflow-y-auto pr-1">
						{hasThinking && (
							<ThinkingBlock
								thinking={thinkingContent as string}
								isStreaming={false}
							/>
						)}
						<div className={MAXIMIZED_PROSE_CLASSES}>
							<ReactMarkdown
								remarkPlugins={[remarkGfm]}
								components={mdComponents}
							>
								{content}
							</ReactMarkdown>
						</div>
					</div>
				</Modal>
			)}
		</>
	);
});
