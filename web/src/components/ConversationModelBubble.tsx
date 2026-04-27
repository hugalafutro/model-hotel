import {
    Bot,
    Copy,
    Trash2,
    CircleStop,
    Settings,
    Clock,
    Zap,
} from "lucide-react";
import type { ChatMessage } from "../api/types";
import { MarkdownContent } from "./MarkdownContent";

function formatTime(ts: number): string {
    const d = new Date(ts);
    return d.toLocaleTimeString(undefined, {
        hour: "2-digit",
        minute: "2-digit",
    });
}

function formatDuration(ms: number): string {
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(1)}s`;
}

interface ConversationModelBubbleProps {
    msg: ChatMessage;
    isStreaming: boolean;
    onCopy: (text: string) => void;
    onDelete: () => void;
    onStop: () => void;
}

export function ConversationModelBubble({
    msg,
    isStreaming,
    onCopy,
    onDelete,
    onStop,
}: ConversationModelBubbleProps) {
    const modelName = msg.model?.split("/").pop() || "Model B";
    const metrics = msg.metrics;

    const hasParams =
        !!msg.params && Object.values(msg.params).some((v) => v !== undefined);
    const paramsTooltip = hasParams
        ? Object.entries(msg.params!)
              .filter(([, v]) => v !== undefined)
              .map(([k, v]) => {
                  const label = k
                      .replace(/_/g, " ")
                      .replace(/^\w/, (c) => c.toUpperCase());
                  return `${label}: ${v}`;
              })
              .join("\n")
        : undefined;

    return (
        <div className="flex justify-end">
            <div className="max-w-[80%] ui-card p-2.5 bg-(--accent)/10 border-(--accent)/30">
                <div className="flex items-center gap-2 mb-1.5 text-(--accent)">
                    <Bot size={12} />
                    <span className="text-[11px] font-medium">{modelName}</span>
                    {hasParams && (
                        <span className="cursor-help" title={paramsTooltip}>
                            <Settings
                                size={10}
                                className="text-(--accent)/60"
                            />
                        </span>
                    )}
                </div>

                <MarkdownContent className="[&_strong]:text-(--text-primary) [&_em]:text-(--text-secondary)">
                    {msg.content}
                </MarkdownContent>

                {msg.thinkingContent && (
                    <div className="mt-2 text-(--text-secondary) text-xs border-t border-(--border-subtle) pt-2">
                        <div className="font-medium mb-1 opacity-60">
                            Thinking
                        </div>
                        <div className="italic">{msg.thinkingContent}</div>
                    </div>
                )}

                {msg.error && (
                    <div className="mt-2 px-2 py-1 rounded bg-red-500/20 text-red-400 text-xs">
                        ⚠ {msg.error}
                    </div>
                )}

                <div className="flex items-center justify-between text-[11px] mt-1.5 text-(--text-tertiary)">
                    <div className="flex items-center gap-2">
                        <span>{formatTime(msg.timestamp)}</span>
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
                                <span>
                                    {metrics.promptTokens +
                                        metrics.completionTokens}{" "}
                                    tok
                                </span>
                            </>
                        )}
                        {isStreaming && (
                            <span className="flex items-center gap-1">
                                <span className="w-1.5 h-1.5 rounded-full bg-(--accent) animate-pulse" />
                                Typing…
                            </span>
                        )}
                    </div>

                    <div className="flex items-center gap-2">
                        <button
                            className="inline-flex items-center cursor-pointer transition-all text-(--accent) hover:drop-shadow-[0_0_4px_var(--accent)]"
                            onClick={() => onCopy(msg.content)}
                            title="Copy"
                        >
                            <Copy size={10} />
                        </button>
                        <button
                            className="inline-flex items-center cursor-pointer hover:drop-shadow-[0_0_4px_var(--color-red-500,red)] text-red-500 transition-all"
                            onClick={onDelete}
                            title="Delete"
                        >
                            <Trash2 size={10} />
                        </button>
                        {isStreaming && (
                            <button
                                onClick={onStop}
                                className="text-red-400 hover:text-red-300 transition-colors cursor-pointer"
                                title="Cancel"
                            >
                                <CircleStop size={12} />
                            </button>
                        )}
                    </div>
                </div>
            </div>
        </div>
    );
}
