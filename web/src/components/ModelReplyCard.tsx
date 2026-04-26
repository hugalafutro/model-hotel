import { useState, useEffect, type ReactNode } from "react";
import { Bot, Clock, Zap } from "lucide-react";
import { ThinkingBlock } from "./ThinkingBlock";
import { formatDuration } from "../utils/format";
import { MarkdownContent, MARKDOWN_PROSE_CLASSES } from "./MarkdownContent";

export { MARKDOWN_PROSE_CLASSES };

export interface ModelReplyMetrics {
    tokensPerSecond: number | null;
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
    /** Additional class names for the root card element */
    className?: string;
    /** Additional class names for the header row */
    headerClassName?: string;
    /** Additional class names for the body/content area */
    bodyClassName?: string;
    /** Additional class names for the footer row */
    footerClassName?: string;
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
    afterModel,
    headerEnd,
    footerStart,
    footerEnd,
    className,
    headerClassName,
    bodyClassName,
    footerClassName,
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
    const hasFooter = !!(footerStart || metrics || footerEnd);

    const stateClass = isWinner
        ? "ring-1 ring-green-500/40 shadow-[0_0_12px_rgba(34,197,94,0.1)]"
        : isLoser
          ? "opacity-60"
          : "";

    return (
        <div
            className={`ui-card transition-all ${stateClass} ${className || ""}`}
        >
            {/* ── Header ── */}
            {model && (
                <div
                    className={`flex items-center justify-between ${headerClassName || ""}`}
                >
                    <div className="flex items-center gap-2 min-w-0">
                        <Bot size={14} className="text-(--accent) shrink-0" />
                        <span className="text-sm font-medium text-(--text-primary) truncate">
                            {model.split("/").pop()}
                        </span>
                        {afterModel}
                    </div>
                    <div className="flex items-center gap-2 shrink-0">
                        {isStreaming && startTimeMs && startTimeMs !== 0 && (
                            <>
                                <span className="text-[11px] text-(--text-tertiary) tabular-nums">
                                    {elapsed}s
                                </span>
                                <span className="w-1.5 h-1.5 rounded-full bg-(--accent) animate-pulse" />
                            </>
                        )}
                        {headerEnd}
                    </div>
                </div>
            )}

            {/* ── Body ── */}
            <div className={bodyClassName || ""}>
                {error ? (
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
                                Waiting...
                            </div>
                        ) : null}
                    </>
                )}
            </div>

            {/* ── Footer ── */}
            {hasFooter && (
                <div
                    className={`flex items-center justify-between text-[11px] text-(--text-tertiary) ${footerClassName || ""}`}
                >
                    <div className="flex items-center gap-3">
                        {footerStart}
                        {metrics && (
                            <>
                                <span className="flex items-center gap-1">
                                    <Clock size={10} />
                                    {formatDuration(metrics.durationMs)}
                                </span>
                                {metrics.tokensPerSecond !== null && (
                                    <span className="flex items-center gap-1">
                                        <Zap size={10} />
                                        {metrics.tokensPerSecond.toFixed(
                                            1,
                                        )}{" "}
                                        tok/s
                                    </span>
                                )}
                                {metrics.promptTokens +
                                    metrics.completionTokens >
                                    0 && (
                                    <span>
                                        {metrics.promptTokens +
                                            metrics.completionTokens}{" "}
                                        tok
                                    </span>
                                )}
                            </>
                        )}
                    </div>
                    {footerEnd}
                </div>
            )}
        </div>
    );
}
