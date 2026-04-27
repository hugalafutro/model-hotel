import { useState, useEffect, type ReactNode } from "react";
import { Bot, Clock, Info, Zap, Settings } from "lucide-react";
import type { GenerationParams } from "../api/types";
import { ThinkingBlock } from "./ThinkingBlock";
import { formatDuration } from "../utils/format";
import { MarkdownContent, MARKDOWN_PROSE_CLASSES } from "./MarkdownContent";

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
}

export function ModelReplyCard({
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
    modelMaxWidth = "max-w-45",
    onModelNameClick,
    shortenModelName = true,
    showInfoIcon = false,
    params,
}: ModelReplyCardProps) {
    const [elapsed, setElapsed] = useState(0);

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
    const displayName = shortenModelName ? model.split("/").pop()! : model;

    const hasCustomParams =
        !!params && Object.values(params).some((v) => v !== undefined);
    const paramsTooltip = hasCustomParams
        ? Object.entries(params!)
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
                        <div
                            className={`group/button flex items-center gap-1 min-w-0 ${onModelNameClick ? "cursor-pointer" : ""}`}
                            onClick={onModelNameClick}
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
                                thinking={thinkingContent!}
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
                            {metrics.promptTokens + metrics.completionTokens >
                                0 && (
                                <span>
                                    {metrics.promptTokens +
                                        metrics.completionTokens}{" "}
                                    tok
                                </span>
                            )}
                        </>
                    ) : null}
                </div>
                {footerEnd}
            </div>
        </div>
    );
}
