import { ChevronsDownUp, ChevronsUpDown, SlidersHorizontal, Timer, Gauge } from "lucide-react";

interface ConversationConfigProps {
    maxTurns: number;
    onMaxTurnsChange: (v: number) => void;
    turnDelayMs: number;
    onTurnDelayMsChange: (v: number) => void;
    conversationState: string;
    currentTurn: number;
    configCollapsed: boolean;
    onToggleCollapsed: () => void;
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
}: ConversationConfigProps) {
    const isRunning = conversationState === "running";

    return (
        <div className="ui-card p-4 shrink-0">
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
            <div
                className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${
                    configCollapsed
                        ? "grid-rows-[0fr]"
                        : "grid-rows-[1fr]"
                }`}
            >
                <div className="overflow-hidden">
                    <div className="grid grid-cols-1 sm:grid-cols-3 gap-4 pt-4">
                        <div>
                            <label className="block text-xs text-(--text-secondary) mb-1.5">
                                Max Turns
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
                            <p className="text-[11px] text-(--text-muted) mt-1">
                                Max responses per model
                            </p>
                        </div>
                        <div>
                            <label className="block text-xs text-(--text-secondary) mb-1.5">
                                Turn Delay
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
                            <p className="text-[11px] text-(--text-muted) mt-1">
                                Milliseconds between turns
                            </p>
                        </div>
                        <div className="flex flex-col justify-center">
                            <div className="text-xs text-(--text-secondary) space-y-1">
                                <div className="flex items-center gap-2">
                                    <Timer size={12} />
                                    <span>
                                        Status:{" "}
                                        <span className="text-(--text-primary) capitalize">
                                            {conversationState}
                                        </span>
                                    </span>
                                </div>
                                {conversationState !== "idle" && (
                                    <div className="flex items-center gap-2">
                                        <Gauge size={12} />
                                        <span>
                                            Turn:{" "}
                                            <span className="text-(--text-primary)">
                                                {currentTurn} / {maxTurns * 2}
                                            </span>
                                        </span>
                                    </div>
                                )}
                            </div>
                        </div>
                    </div>
                </div>
            </div>
        </div>
    );
}
