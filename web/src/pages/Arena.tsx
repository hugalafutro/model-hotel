import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import { useState, useRef, useCallback, useMemo, useEffect } from "react";
import {
    Swords,
    Play,
    X,
    Bot,
    Clock,
    Zap,
    CheckCircle2,
    AlertCircle,
    Trash2,
    Settings as SettingsIcon,
    ChevronDown,
    ChevronUp,
} from "lucide-react";
import { useToast } from "../context/ToastContext";
import { ModelPicker } from "../components/ModelPicker";

function formatDuration(ms: number): string {
    if (ms < 1000) return `${ms.toFixed(0)}ms`;
    return `${(ms / 1000).toFixed(2)}s`;
}

interface ArenaResponse {
    model: string;
    content: string;
    done: boolean;
    error: string | null;
    metrics: {
        tokensPerSecond: number | null;
        durationMs: number;
        promptTokens: number;
        completionTokens: number;
    } | null;
}

function gridCols(count: number): string {
    if (count <= 1) return "grid-cols-1";
    if (count <= 2) return "grid-cols-1 md:grid-cols-2";
    if (count <= 3) return "grid-cols-1 md:grid-cols-2 lg:grid-cols-3";
    return "grid-cols-1 md:grid-cols-2 lg:grid-cols-2 xl:grid-cols-4";
}

export function Arena() {
    const { data: models } = useQuery({
        queryKey: ["models"],
        queryFn: () => api.models.list(),
        staleTime: 60_000,
    });

    const [selectedModels, setSelectedModels] = useState<string[]>(() => {
        try {
            const raw = localStorage.getItem("arena_selected_models");
            return raw ? JSON.parse(raw) : [];
        } catch {
            return [];
        }
    });
    const [systemPrompt, setSystemPrompt] = useState<string>(() =>
        localStorage.getItem("chat_system_prompt") || "",
    );
    const [showSystemPrompt, setShowSystemPrompt] = useState(false);
    const [prompt, setPrompt] = useState("");
    const [responses, setResponses] = useState<ArenaResponse[]>([]);
    const [runningModels, setRunningModels] = useState<Set<string>>(new Set());
    const abortMapRef = useRef<Map<string, AbortController>>(new Map());
    const { toast } = useToast();

    const enabledModels = useMemo(
        () => models?.filter((m) => m.enabled && m.provider_name) || [],
        [models],
    );

    useEffect(() => {
        localStorage.setItem(
            "arena_selected_models",
            JSON.stringify(selectedModels),
        );
    }, [selectedModels]);

    const runArena = useCallback(async () => {
        if (!prompt.trim() || selectedModels.length === 0) return;
        
        const currentPrompt = prompt.trim();
        setPrompt("");
        
        const newResponses: ArenaResponse[] = selectedModels.map((model) => ({
            model,
            content: "",
            done: false,
            error: null,
            metrics: null,
        }));
        setResponses(newResponses);
        setRunningModels(new Set(selectedModels));

        const chatMessages: Array<{ role: string; content: string }> = [];
        if (systemPrompt.trim()) {
            chatMessages.push({ role: "system", content: systemPrompt.trim() });
        }
        chatMessages.push({ role: "user", content: currentPrompt });

        for (const model of selectedModels) {
            const abortCtrl = new AbortController();
            abortMapRef.current.set(model, abortCtrl);

            const runModel = async () => {
                const startTime = performance.now();
                let charCount = 0;
                let promptTokens = 0;
                let completionTokens = 0;
                
                try {
                    const resp = await api.chat.completions({
                        model,
                        stream: true,
                        messages: chatMessages,
                    });

                    const reader = resp.body?.getReader();
                    if (!reader) throw new Error("No readable stream");

                    const decoder = new TextDecoder();
                    let buffer = "";

                    while (true) {
                        const { done, value } = await reader.read();
                        if (done || abortCtrl.signal.aborted) break;

                        buffer += decoder.decode(value, { stream: true });
                        const lines = buffer.split("\n");
                        buffer = lines.pop() || "";

                        for (const line of lines) {
                            if (!line.startsWith("data: ")) continue;
                            const data = line.slice(6);
                            if (data === "[DONE]") break;
                            try {
                                const chunk = JSON.parse(data);
                                const delta = chunk.choices?.[0]?.delta?.content;
                                if (delta) {
                                    charCount += delta.length;
                                    setResponses((prev) => {
                                        const next = [...prev];
                                        const idx = selectedModels.indexOf(model);
                                        if (idx >= 0) {
                                            next[idx] = {
                                                ...next[idx],
                                                content: next[idx].content + delta,
                                            };
                                        }
                                        return next;
                                    });
                                }
                                if (chunk.usage) {
                                    promptTokens = chunk.usage.prompt_tokens ?? 0;
                                    completionTokens = chunk.usage.completion_tokens ?? 0;
                                }
                            } catch {
                                // ignore parse errors
                            }
                        }
                    }

                    const durationMs = performance.now() - startTime;
                    const tokensPerSecond = durationMs > 0 ? (charCount / (durationMs / 1000)) : null;

                    setResponses((prev) => {
                        const next = [...prev];
                        const idx = selectedModels.indexOf(model);
                        if (idx >= 0) {
                            next[idx] = {
                                ...next[idx],
                                done: true,
                                metrics: {
                                    tokensPerSecond,
                                    durationMs: Math.round(durationMs),
                                    promptTokens,
                                    completionTokens,
                                },
                            };
                        }
                        return next;
                    });
                } catch (err) {
                    const msg = err instanceof Error ? err.message : "Unknown error";
                    setResponses((prev) => {
                        const next = [...prev];
                        const idx = selectedModels.indexOf(model);
                        if (idx >= 0) {
                            next[idx] = {
                                ...next[idx],
                                done: true,
                                error: msg,
                                metrics: {
                                    tokensPerSecond: null,
                                    durationMs: Math.round(performance.now() - startTime),
                                    promptTokens: 0,
                                    completionTokens: 0,
                                },
                            };
                        }
                        return next;
                    });
                    toast(`${model}: ${msg}`, "error");
                } finally {
                    setRunningModels((prev) => {
                        const next = new Set(prev);
                        next.delete(model);
                        return next;
                    });
                    abortMapRef.current.delete(model);
                }
            };

            runModel();
        }
    }, [prompt, selectedModels, systemPrompt, toast]);

    const handleStopAll = useCallback(() => {
        for (const [, ctrl] of abortMapRef.current) {
            ctrl.abort();
        }
        abortMapRef.current.clear();
        setRunningModels(new Set());
        setResponses((prev) =>
            prev.map((r) => (r.done ? r : { ...r, done: true })),
        );
    }, []);

    const handleClear = () => {
        setResponses([]);
        setRunningModels(new Set());
    };

    const isRunning = runningModels.size > 0;

    return (
        <div className="space-y-6">
            {/* Header */}
            <div className="flex justify-between items-center">
                <div>
                    <div className="flex items-center gap-3">
                        <Swords
                            size={28}
                            strokeWidth={2}
                            className="text-(--accent)"
                        />
                        <h1 className="text-3xl font-bold text-white">Arena</h1>
                    </div>
                    <p className="text-gray-400">
                        Send one prompt to multiple models and compare
                    </p>
                </div>
                <div className="flex items-center gap-3">
                    <button
                        onClick={handleClear}
                        disabled={responses.length === 0}
                        className="ui-btn-secondary flex items-center gap-2 disabled:opacity-40"
                    >
                        <Trash2 size={16} />
                        Clear
                    </button>
                </div>
            </div>

            {/* Controls */}
            <div className="ui-card p-4 space-y-4">
                {/* Model selection */}
                <div>
                    <label className="text-sm text-(--text-secondary) mb-2 block">
                        Models ({selectedModels.length}/4)
                    </label>
                    <ModelPicker
                        models={enabledModels}
                        selected={selectedModels}
                        onChange={setSelectedModels}
                        multi={true}
                        maxSelections={4}
                    />
                </div>

                {/* Prompt */}
                <div>
                    <label className="text-sm text-(--text-secondary) mb-2 block">
                        Prompt
                    </label>
                    <textarea
                        value={prompt}
                        onChange={(e) => setPrompt(e.target.value)}
                        placeholder="Enter your prompt..."
                        rows={3}
                        className="ui-input w-full resize-none"
                    />
                </div>

                <div className="flex items-center gap-3 flex-wrap">
                    <button
                        onClick={isRunning ? handleStopAll : runArena}
                        disabled={selectedModels.length === 0 || !prompt.trim()}
                        className={`ui-btn flex items-center gap-2 ${
                            isRunning ? "ui-btn-danger" : "ui-btn-primary"
                        } disabled:opacity-40`}
                    >
                        {isRunning ? (
                            <>
                                <X size={16} />
                                Stop
                            </>
                        ) : (
                            <>
                                <Play size={16} />
                                Run Arena
                            </>
                        )}
                    </button>

                    <button
                        onClick={() => setShowSystemPrompt((s) => !s)}
                        className="ui-btn-secondary flex items-center gap-2"
                    >
                        <SettingsIcon size={16} />
                        System Prompt
                        {showSystemPrompt ? (
                            <ChevronUp size={14} />
                        ) : (
                            <ChevronDown size={14} />
                        )}
                    </button>
                </div>

                {showSystemPrompt && (
                    <div className="ui-card p-3 space-y-2 border border-(--accent)/20 bg-(--accent)/5">
                        <div className="flex items-center justify-between">
                            <label className="text-sm font-medium text-(--accent)">
                                System Prompt
                            </label>
                            <button
                                onClick={() => setSystemPrompt("")}
                                className="text-(--text-muted) hover:text-(--text-secondary) transition-colors"
                                title="Clear system prompt"
                            >
                                <X size={12} />
                            </button>
                        </div>
                        <textarea
                            value={systemPrompt}
                            onChange={(e) => setSystemPrompt(e.target.value)}
                            placeholder="You are a helpful assistant..."
                            rows={2}
                            className="ui-input w-full resize-none text-sm"
                        />
                    </div>
                )}
            </div>

            {/* Responses Grid */}
            {responses.length > 0 && (
                <div className={`grid ${gridCols(responses.length)} gap-4`}>
                    {responses.map((resp, i) => (
                        <div key={i} className="ui-card flex flex-col h-full">
                            {/* Panel Header */}
                            <div className="flex items-center justify-between px-4 pt-4 pb-2 border-b border-(--border-subtle)">
                                <div className="flex items-center gap-2">
                                    <Bot
                                        size={14}
                                        className="text-(--accent)"
                                    />
                                    <span className="text-sm font-medium text-(--text-primary) truncate">
                                        {resp.model}
                                    </span>
                                </div>
                                <div className="flex items-center gap-2">
                                    {resp.done && !resp.error && (
                                        <CheckCircle2
                                            size={14}
                                            className="text-green-400"
                                        />
                                    )}
                                    {resp.error && (
                                        <AlertCircle
                                            size={14}
                                            className="text-red-400"
                                        />
                                    )}
                                    {!resp.done && (
                                        <span className="w-1.5 h-1.5 rounded-full bg-(--accent) animate-pulse" />
                                    )}
                                    <button
                                        onClick={() => {
                                            const ctrl = abortMapRef.current.get(resp.model);
                                            if (ctrl) {
                                                ctrl.abort();
                                                abortMapRef.current.delete(resp.model);
                                            }
                                        }}
                                        disabled={resp.done || !runningModels.has(resp.model)}
                                        className="text-(--text-tertiary) hover:text-(--text-primary) disabled:opacity-30 transition-colors"
                                    >
                                        <X size={12} />
                                    </button>
                                </div>
                            </div>

                            {/* Content */}
                            <div className="flex-1 p-4 overflow-y-auto min-h-50 max-h-150">
                                {resp.error ? (
                                    <div className="text-red-400 text-sm">
                                        {resp.error}
                                    </div>
                                ) : resp.content ? (
                                    <div className="whitespace-pre-wrap text-sm text-(--text-primary)">
                                        {resp.content}
                                    </div>
                                ) : (
                                    <div className="text-(--text-tertiary) text-sm flex items-center gap-2">
                                        <span className="w-1.5 h-1.5 rounded-full bg-(--accent) animate-pulse" />
                                        Thinking...
                                    </div>
                                )}
                            </div>

                            {/* Footer Metrics */}
                            {resp.metrics && (
                                <div className="px-4 py-2 border-t border-(--border-subtle) flex items-center gap-3 text-[11px] text-(--text-tertiary)">
                                    <span className="flex items-center gap-1">
                                        <Clock size={10} />
                                        {formatDuration(resp.metrics.durationMs)}
                                    </span>
                                    {resp.metrics.tokensPerSecond !== null && (
                                        <span className="flex items-center gap-1">
                                            <Zap size={10} />
                                            {resp.metrics.tokensPerSecond.toFixed(1)}{" "}
                                            tok/s
                                        </span>
                                    )}
                                    {(resp.metrics.completionTokens > 0) && (
                                        <span>
                                            {resp.metrics.completionTokens} tok
                                        </span>
                                    )}
                                </div>
                            )}
                        </div>
                    ))}
                </div>
            )}
        </div>
    );
}
