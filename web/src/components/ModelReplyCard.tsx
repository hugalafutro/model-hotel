import { useState, useEffect, type ReactNode } from "react";
import { Bot, Clock, Zap } from "lucide-react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { ThinkingBlock } from "./ThinkingBlock";
import { formatDuration } from "../utils/format";

export const MARKDOWN_PROSE_CLASSES =
    "prose prose-invert prose-xs max-w-none text-(--text-primary) text-xs " +
    "[&_p]:my-1 [&_ul]:my-1 [&_ol]:my-1 [&_li]:my-0.5 " +
    "[&_h1]:text-sm [&_h2]:text-xs [&_h3]:text-xs " +
    "[&_code]:text-(--accent) [&_code]:bg-(--surface-hover) [&_code]:px-1 [&_code]:py-0.5 [&_code]:rounded [&_code]:text-[11px] " +
    "[&_pre]:bg-(--surface-hover) [&_pre]:rounded-lg [&_pre]:p-3 [&_pre]:overflow-x-auto [&_pre]:my-2 [&_pre]:text-[11px] " +
    "[&_blockquote]:border-l-2 [&_blockquote]:border-(--accent)/40 [&_blockquote]:pl-3 [&_blockquote]:text-(--text-secondary) " +
    "[&_strong]:text-white [&_em]:text-(--text-secondary) " +
    "[&_a]:text-(--accent) [&_a]:underline " +
    "[&_hr]:border-(--border-subtle) " +
    "[&_table]:text-[10px] [&_th]:px-1.5 [&_th]:py-0.5 [&_td]:px-1.5 [&_td]:py-0.5 " +
    "[&_th]:border [&_th]:border-(--border-subtle) [&_td]:border [&_td]:border-(--border-subtle)";

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
                        <Bot
                            size={14}
                            className="text-(--accent) shrink-0"
                        />
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
                            <div className={MARKDOWN_PROSE_CLASSES}>
                                <ReactMarkdown remarkPlugins={[remarkGfm]}>
                                    {content}
                                </ReactMarkdown>
                            </div>
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
                                        {metrics.tokensPerSecond.toFixed(1)}{" "}
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
