import {
    ChevronsDownUp,
    ChevronsUpDown,
    SlidersHorizontal,
    Timer,
    Gauge,
    Play,
} from "lucide-react";

interface ConversationConfigProps {
    maxTurns: number;
    onMaxTurnsChange: (v: number) => void;
    turnDelayMs: number;
    onTurnDelayMsChange: (v: number) => void;
    conversationState: string;
    currentTurn: number;
    configCollapsed: boolean;
    onToggleCollapsed: () => void;
    input: string;
    onInputChange: (value: string) => void;
    onStart: () => void;
    canStart: boolean;
    selectedModel: string;
    selectedModelB: string;
}

export function ConversationConfig({
    maxTurns,
    onMaxTurnsChange,
    turnDelayMs,
    onTurnDelayMsChange,
    conversationState,
    currentTurn,
    configCollapsed,
    onToggleCollapsed,
    input,
    onInputChange,
    onStart,
    canStart,
    selectedModel,
    selectedModelB,
}: ConversationConfigProps) {
    const isRunning = conversationState === "running";

    return (
        <div className="ui-card p-4 shrink-0">
            {/* Header */}
            <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                    <SlidersHorizontal
                        size={14}
                        className="text-(--text-secondary)"
                    />
                    <span className="text-sm text-(--text-secondary)">
                        Conversation Config
                    </span>
                </div>
                <div className="flex items-center gap-3">
                    {/* Collapsed preview: values */}
                    {configCollapsed && (
                        <span className="text-xs text-(--text-muted) flex items-center gap-3">
                            <span>
                                Turns: <span className="text-(--text-primary)">{maxTurns}</span>
                            </span>
                            <span>
                                Delay: <span className="text-(--text-primary)">{turnDelayMs}</span>ms
                            </span>
                        </span>
                    )}
                    {/* Status always in top-right corner */}
                    <span className="text-xs text-(--text-secondary) flex items-center gap-1.5">
                        <Timer size={12} />
                        Status: <span className="text-(--text-primary) capitalize">{conversationState}</span>
                    </span>
                    <button
                        onClick={onToggleCollapsed}
                        className="p-1.5 rounded-md transition-all cursor-pointer text-(--text-tertiary) hover:text-(--accent)"
                    >
                        {configCollapsed ? <ChevronsUpDown size={14} /> : <ChevronsDownUp size={14} />}
                    </button>
                </div>
            </div>

            {/* Expandable content */}
            <div
                className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${
                    configCollapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"
                }`}
            >
                <div className="overflow-hidden">
                    <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 pt-4">
                        {/* Max Turns */}
                        <div>
                            <label className="flex items-center gap-1.5 text-xs text-(--text-secondary) mb-1.5">
                                <span>Max Turns</span>
                                <span className="text-(--text-muted)">(responses per model)</span>
                            </label>
                            <input
                                type="number"
                                value={maxTurns}
                                onChange={(e) =>
                                    onMaxTurnsChange(
                                        Math.max(
                                            1,
                                            Math.min(
                                                50,
                                                parseInt(e.target.value, 10) || 1,
                                            ),
                                        ),
                                    )
                                }
                                min={1}
                                max={50}
                                className="ui-input w-full text-sm"
                                disabled={isRunning}
                            />
                        </div>

                        {/* Turn Delay */}
                        <div>
                            <label className="flex items-center gap-1.5 text-xs text-(--text-secondary) mb-1.5">
                                <span>Turn Delay</span>
                                <span className="text-(--text-muted)">(ms between turns)</span>
                            </label>
                            <input
                                type="number"
                                value={turnDelayMs}
                                onChange={(e) =>
                                    onTurnDelayMsChange(
                                        Math.max(
                                            0,
                                            Math.min(
                                                5000,
                                                parseInt(e.target.value, 10) || 0,
                                            ),
                                        ),
                                    )
                                }
                                min={0}
                                max={5000}
                                step={100}
                                className="ui-input w-full text-sm"
                                disabled={isRunning}
                            />
                        </div>

                        {/* Turn progress */}
                        <div className="flex flex-col justify-center">
                            <div className="text-xs text-(--text-secondary) space-y-1">
                                {conversationState !== "idle" && (
                                    <div className="flex items-center gap-2">
                                        <Gauge size={12} />
                                        <span>
                                            Turn: <span className="text-(--text-primary)">{currentTurn} / {maxTurns * 2}</span>
                                        </span>
                                    </div>
                                )}
                            </div>
                        </div>
                    </div>

                    {/* Initial prompt row */}
                    <div className="pt-4 flex flex-col gap-1.5">
                        <div className="flex items-center gap-3">
                            <textarea
                                value={input}
                                onChange={(e) => onInputChange(e.target.value)}
                                placeholder={!selectedModel || !selectedModelB ? "Select both models first" : "Enter initial prompt…"}
                                className="flex-1 ui-input resize-none max-h-32 min-h-11 overflow-y-auto"
                                disabled={!selectedModel || !selectedModelB || isRunning}
                                rows={1}
                            />
                            <button
                                onClick={onStart}
                                disabled={!canStart}
                                className="ui-btn ui-btn-primary flex items-center gap-2 shrink-0"
                            >
                                <Play size={16} />
                                Start
                            </button>
                        </div>
                        <span className="text-xs text-(--text-muted)">
                            Enter a topic or question to start the conversation
                        </span>
                    </div>
                </div>
            </div>
        </div>
    );
}
