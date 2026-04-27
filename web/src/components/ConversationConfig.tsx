import {
    ChevronsDownUp,
    ChevronsUpDown,
    SlidersHorizontal,
    Timer,
    Gauge,
    Play,
    FastForward,
} from "lucide-react";

interface ConversationConfigProps {
    maxTurns: number;
    onMaxTurnsChange: (v: number) => void;
    turnDelayMs: number;
    onTurnDelayMsChange: (v: number) => void;
    conversationState: string;
    currentTurn: number;
    turnCountdown: number;
    configCollapsed: boolean;
    onToggleCollapsed: () => void;
    input: string;
    onInputChange: (value: string) => void;
    onStart: () => void;
    /** Called when resuming a paused conversation */
    onContinue?: () => void;
    canStart: boolean;
    selectedModel: string;
    selectedModelB: string;
}

export function ConversationConfig({
    maxTurns,
    onMaxTurnsChange,
    onTurnDelayMsChange,
    turnDelayMs,
    conversationState,
    currentTurn,
    turnCountdown,
    configCollapsed,
    onToggleCollapsed,
    input,
    onInputChange,
    onStart,
    onContinue,
    canStart,
    selectedModel,
    selectedModelB,
}: ConversationConfigProps) {
    const isPaused = conversationState === "paused";
    const isIdle = conversationState === "idle";
    const showStartArea = isIdle || isPaused;
    const isContinue = isPaused || (isIdle && currentTurn > 0);

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
                                Rounds:{" "}
                                <span className="text-(--text-primary)">
                                    {maxTurns}
                                </span>
                            </span>
                            <span>
                                Delay:{" "}
                                <span className="text-(--text-primary)">
                                    {turnDelayMs}
                                </span>
                                ms
                            </span>
                        </span>
                    )}
                    {/* Round counter (when active) — each round = both models respond */}
                    {conversationState !== "idle" &&
                        conversationState !== "paused" && (
                            <span className="text-xs text-(--text-secondary) flex items-center gap-1.5">
                                <Gauge size={12} />
                                Round:{" "}
                                <span className="text-(--text-primary)">
                                    {Math.ceil(currentTurn / 2)} / {maxTurns}
                                </span>
                                {turnCountdown > 0 && (
                                    <span className="text-(--accent) ml-1">
                                        Next in {turnCountdown}s…
                                    </span>
                                )}
                            </span>
                        )}
                    {/* Status */}
                    <span className="text-xs text-(--text-secondary) flex items-center gap-1.5">
                        <Timer size={12} />
                        Status:{" "}
                        <span className="text-(--text-primary) capitalize">
                            {conversationState}
                        </span>
                    </span>
                    <button
                        onClick={onToggleCollapsed}
                        className="p-1.5 rounded-md transition-all cursor-pointer text-(--text-tertiary) hover:text-(--accent)"
                    >
                        {configCollapsed ? (
                            <ChevronsUpDown size={14} />
                        ) : (
                            <ChevronsDownUp size={14} />
                        )}
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
                    {/* Compact row: Rounds + Delay + (Prompt area or Continue) */}
                    <div className="flex items-end gap-3 pt-4">
                        {/* Max Turns */}
                        <div className="flex flex-col">
                            <label className="text-xs text-(--text-secondary) mb-1">
                                Rounds
                            </label>
                            <input
                                type="number"
                                value={maxTurns}
                                onChange={(e) => {
                                    const v = parseInt(e.target.value, 10);
                                    if (!isNaN(v)) {
                                        onMaxTurnsChange(
                                            Math.max(1, Math.min(50, v)),
                                        );
                                    }
                                }}
                                onBlur={(e) => {
                                    const v = parseInt(e.target.value, 10);
                                    if (isNaN(v) || v < 1) onMaxTurnsChange(1);
                                    else if (v > 50) onMaxTurnsChange(50);
                                }}
                                onFocus={(e) => e.target.select()}
                                min={1}
                                max={50}
                                className="ui-input w-16 text-sm text-center"
                                disabled={conversationState !== "idle"}
                            />
                        </div>

                        {/* Turn Delay */}
                        <div className="flex flex-col">
                            <label className="text-xs text-(--text-secondary) mb-1">
                                Delay (ms)
                            </label>
                            <input
                                type="number"
                                value={turnDelayMs}
                                onChange={(e) => {
                                    const v = parseInt(e.target.value, 10);
                                    if (!isNaN(v)) {
                                        onTurnDelayMsChange(
                                            Math.max(0, Math.min(5000, v)),
                                        );
                                    }
                                }}
                                onBlur={(e) => {
                                    const v = parseInt(e.target.value, 10);
                                    if (isNaN(v) || v < 0)
                                        onTurnDelayMsChange(0);
                                    else if (v > 5000)
                                        onTurnDelayMsChange(5000);
                                }}
                                onFocus={(e) => e.target.select()}
                                min={0}
                                max={5000}
                                step={100}
                                className="ui-input w-20 text-sm text-center"
                                disabled={conversationState !== "idle"}
                            />
                        </div>

                        {/* Prompt + Start/Continue */}
                        {showStartArea && (
                            <div className="flex items-end gap-2 flex-1 min-w-0">
                                {isIdle && (
                                    <>
                                        <div className="flex flex-col flex-1 min-w-0">
                                            <label className="text-xs text-(--text-secondary) mb-1">
                                                Prompt
                                            </label>
                                            <textarea
                                                value={input}
                                                onChange={(e) =>
                                                    onInputChange(
                                                        e.target.value,
                                                    )
                                                }
                                                placeholder={
                                                    !selectedModel ||
                                                    !selectedModelB
                                                        ? "Select both models first"
                                                        : "Enter a topic or question…"
                                                }
                                                className="flex-1 ui-input resize-none overflow-y-auto text-sm min-h-9"
                                                style={{ height: "auto" }}
                                                disabled={
                                                    !selectedModel ||
                                                    !selectedModelB
                                                }
                                                rows={1}
                                            />
                                        </div>
                                        <button
                                            onClick={onStart}
                                            disabled={!canStart}
                                            className="ui-btn ui-btn-primary flex items-center gap-2 shrink-0"
                                        >
                                            <Play size={16} />
                                            {isContinue ? "Continue" : "Start"}
                                        </button>
                                    </>
                                )}
                                {isPaused && (
                                    <button
                                        onClick={onContinue}
                                        className="ui-btn ui-btn-primary flex items-center gap-2 shrink-0"
                                    >
                                        <FastForward size={16} />
                                        Continue
                                    </button>
                                )}
                            </div>
                        )}
                    </div>
                </div>
            </div>
        </div>
    );
}
