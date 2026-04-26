import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import { useState, useRef, useCallback, useEffect } from "react";
import {
    MessageSquare,
    Send,
    X,
    Bot,
    Clock,
    Zap,
    Brain,
    ChevronDown,
    ChevronRight,
    Settings,
} from "lucide-react";
import type { Model, ChatMessage, GenerationParams } from "../api/types";

import { useToast } from "../context/ToastContext";
import { useStorage } from "../context/StorageContext";
import { ModelPicker } from "../components/ModelPicker";
import { PresetBar } from "../components/PresetBar";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { CAP_META } from "../components/capMeta";
import { CHAT_PERSONAS } from "../data/presets";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";

function formatDuration(ms: number): string {
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(1)}s`;
}

function formatTime(ts: number): string {
    const d = new Date(ts);
    return d.toLocaleTimeString(undefined, {
        hour: "2-digit",
        minute: "2-digit",
    });
}

function proxyModelID(providerName: string, modelId: string): string {
    return providerName.replace(/ /g, "-") + "/" + modelId;
}

function parseCapabilities(capStr: string): Record<string, boolean> {
    try {
        return JSON.parse(capStr);
    } catch {
        return {};
    }
}

function formatPrice(n: number | null | undefined): string {
    if (n == null) return "-";
    const rounded = Math.round(n * 10000) / 10000;
    const str = rounded.toString();
    const [intPart, decPart] = str.split(".");
    if (!decPart) return intPart;
    const trimmed = decPart.replace(/0+$/, "");
    return trimmed.length > 0 ? `${intPart}.${trimmed}` : intPart;
}

const THINKING_OPEN_RE = /<(?:thought|start_thought|think)>/g;
const THINKING_CLOSE_RE = /<\/(?:thought|end_thought|think)>/g;

function extractThinking(raw: string): {
    thinking: string;
    content: string;
} {
    let content = raw;
    let thinking = "";

    const fenceMatch = content.match(/^<<\s*\n([\s\S]*?)\n>>\s*\n?/);
    if (fenceMatch) {
        thinking = fenceMatch[1].trim();
        content = content.slice(fenceMatch[0].length);
    }

    const tagOpen = content.search(/<(?:thought|start_thought|think)>/i);
    if (tagOpen !== -1) {
        const afterOpen = content.slice(tagOpen);
        const closeMatch = afterOpen.match(
            /<\/(?:thought|end_thought|think)>/i,
        );
        if (closeMatch) {
            const tagLen = afterOpen.indexOf(">");
            const closeEnd =
                afterOpen.indexOf(closeMatch[0]) + closeMatch[0].length;
            const inner = afterOpen.slice(
                tagLen + 1,
                afterOpen.indexOf(closeMatch[0]),
            );
            thinking = thinking ? thinking + "\n" + inner.trim() : inner.trim();
            content =
                content.slice(0, tagOpen) + content.slice(tagOpen + closeEnd);
        } else {
            const tagLen = afterOpen.indexOf(">");
            const inner = afterOpen.slice(tagLen + 1);
            thinking = thinking ? thinking + "\n" + inner.trim() : inner.trim();
            content = content.slice(0, tagOpen);
        }
    }

    content = content
        .replace(THINKING_OPEN_RE, "")
        .replace(THINKING_CLOSE_RE, "")
        .trimStart();

    return { thinking, content };
}

function hasAnyParam(p: GenerationParams): boolean {
    return (
        p.temperature !== undefined ||
        p.max_tokens !== undefined ||
        p.top_p !== undefined ||
        p.min_p !== undefined ||
        p.top_k !== undefined ||
        p.frequency_penalty !== undefined ||
        p.presence_penalty !== undefined
    );
}

function ChatThinkingBlock({
    thinking,
    isStreaming,
}: {
    thinking: string;
    isStreaming: boolean;
}) {
    const [open, setOpen] = useState(false);

    return (
        <>
            <button
                onClick={() => setOpen(!open)}
                className={`flex items-center gap-1.5 text-xs transition-colors mb-2 w-full text-left ${
                    isStreaming
                        ? "text-(--accent) animate-pulse cursor-pointer"
                        : "text-(--accent)/70 hover:text-(--accent) cursor-pointer"
                }`}
            >
                <Brain size={12} />
                <span>Thinking</span>
                {open ? <ChevronDown size={12} /> : <ChevronRight size={12} />}
            </button>
            {open && (
                <div className="mb-3 px-3 py-2 rounded-lg bg-(--accent)/5 border border-(--accent)/10 text-xs text-(--text-secondary) whitespace-pre-wrap max-h-60 overflow-y-auto">
                    {thinking}
                </div>
            )}
        </>
    );
}

interface ModelDetailPillProps {
    model: Model;
    params: GenerationParams;
    onParamsChange: (params: GenerationParams) => void;
}

function ParamSlider({
    label,
    value,
    min,
    max,
    step,
    onChange,
}: {
    label: string;
    value: number | undefined;
    min: number;
    max: number;
    step: number;
    onChange: (v: number | undefined) => void;
}) {
    const isSet = value !== undefined;
    return (
        <div className="space-y-1">
            <div className="flex items-center justify-between">
                <span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
                    {label}
                </span>
                <input
                    type="number"
                    value={isSet ? value : ""}
                    min={min}
                    max={max}
                    step={step}
                    onChange={(e) => {
                        const v = e.target.value;
                        if (v === "" || v === "-" || v === ".") {
                            onChange(undefined);
                            return;
                        }
                        const n = parseFloat(v);
                        if (!isNaN(n)) onChange(n);
                    }}
                    placeholder="off"
                    className="w-16 text-right px-1 py-0.5 rounded bg-(--surface-input) text-[10px] text-(--text-primary) border border-transparent focus:border-(--accent) outline-none placeholder:text-(--text-tertiary)"
                />
            </div>
            <input
                type="range"
                min={min}
                max={max}
                step={step}
                value={isSet ? value : min}
                onChange={(e) => onChange(parseFloat(e.target.value))}
                className="w-full h-1 rounded-lg appearance-none cursor-pointer bg-(--surface-hover) accent-(--accent)"
                style={{
                    background: isSet
                        ? `linear-gradient(to right, var(--accent) ${((value! - min) / (max - min)) * 100}%, var(--surface-hover) ${((value! - min) / (max - min)) * 100}%)`
                        : undefined,
                }}
            />
        </div>
    );
}

function ModelDetailPill({ model, params, onParamsChange }: ModelDetailPillProps) {
    const caps = parseCapabilities(model.capabilities);
    const [open, setOpen] = useState(false);

    const hasCustom =
        params.temperature !== undefined ||
        params.max_tokens !== undefined ||
        params.top_p !== undefined ||
        params.min_p !== undefined ||
        params.top_k !== undefined ||
        params.frequency_penalty !== undefined ||
        params.presence_penalty !== undefined;

    return (
        <div className="ui-card p-3 space-y-3 text-xs overflow-y-auto max-h-full relative">
            {/* Header with cog */}
            <div className="flex items-start justify-between">
                <div>
                    <h3 className="text-sm font-semibold text-(--text-primary) leading-tight">
                        {model.display_name || model.model_id}
                    </h3>
                    {model.description && (
                        <p className="text-(--text-secondary) mt-1 line-clamp-4 text-[11px]">
                            {model.description}
                        </p>
                    )}
                </div>
                <button
                    onClick={() => setOpen((s) => !s)}
                    className={`p-1 rounded-md transition-colors cursor-pointer shrink-0 ${
                        open || hasCustom
                            ? "text-(--accent)"
                            : "text-(--text-tertiary) hover:text-(--accent)"
                    }`}
                    title="Generation parameters"
                >
                    <Settings size={14} />
                </button>
            </div>

            {/* Slide-out panel */}
            <div
                className={`overflow-hidden transition-all duration-300 ease-in-out border-t border-(--border-subtle) ${
                    open ? "max-h-125 opacity-100 pt-2 mt-1" : "max-h-0 opacity-0 pt-0 mt-0"
                }`}
            >
                <div className="space-y-3">
                    <ParamSlider
                        label="Temperature"
                        value={params.temperature}
                        min={0}
                        max={2}
                        step={0.01}
                        onChange={(v) =>
                            onParamsChange({ ...params, temperature: v })
                        }
                    />
                    <ParamSlider
                        label="Max Tokens"
                        value={params.max_tokens}
                        min={1}
                        max={32768}
                        step={1}
                        onChange={(v) =>
                            onParamsChange({
                                ...params,
                                max_tokens:
                                    v === undefined ? undefined : Math.round(v),
                            })
                        }
                    />
                    <ParamSlider
                        label="Top P"
                        value={params.top_p}
                        min={0}
                        max={1}
                        step={0.01}
                        onChange={(v) =>
                            onParamsChange({ ...params, top_p: v })
                        }
                    />
                    <ParamSlider
                        label="Min P"
                        value={params.min_p}
                        min={0}
                        max={1}
                        step={0.01}
                        onChange={(v) =>
                            onParamsChange({ ...params, min_p: v })
                        }
                    />
                    <ParamSlider
                        label="Top K"
                        value={params.top_k}
                        min={1}
                        max={100}
                        step={1}
                        onChange={(v) =>
                            onParamsChange({
                                ...params,
                                top_k:
                                    v === undefined ? undefined : Math.round(v),
                            })
                        }
                    />
                    <ParamSlider
                        label="Freq Penalty"
                        value={params.frequency_penalty}
                        min={-2}
                        max={2}
                        step={0.01}
                        onChange={(v) =>
                            onParamsChange({
                                ...params,
                                frequency_penalty: v,
                            })
                        }
                    />
                    <ParamSlider
                        label="Pres Penalty"
                        value={params.presence_penalty}
                        min={-2}
                        max={2}
                        step={0.01}
                        onChange={(v) =>
                            onParamsChange({
                                ...params,
                                presence_penalty: v,
                            })
                        }
                    />
                    <button
                        onClick={() => onParamsChange({})}
                        className="w-full py-1 text-[10px] text-(--accent) hover:text-(--accent)/80 text-center border border-(--accent)/20 rounded hover:bg-(--accent)/5 transition-colors cursor-pointer"
                    >
                        Reset all
                    </button>
                </div>
            </div>

            <div className="space-y-2">
                <div>
                    <span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
                        Provider
                    </span>
                    <div className="text-(--text-primary) font-medium">
                        {model.provider_name}
                    </div>
                </div>
                <div>
                    <span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
                        Model ID
                    </span>
                    <div
                        className="text-(--text-primary) font-medium truncate"
                        title={model.model_id}
                    >
                        {model.model_id}
                    </div>
                </div>
                <div className="grid grid-cols-2 gap-2">
                    <div>
                        <span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
                            Context
                        </span>
                        <div className="text-(--text-primary) font-medium">
                            {model.context_length?.toLocaleString() ?? "-"}
                        </div>
                    </div>
                    <div>
                        <span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
                            Max Out
                        </span>
                        <div className="text-(--text-primary) font-medium">
                            {model.max_output_tokens?.toLocaleString() ?? "-"}
                        </div>
                    </div>
                </div>
                <div className="grid grid-cols-2 gap-2">
                    <div>
                        <span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
                            In $/1M
                        </span>
                        <div className="text-(--text-primary) font-medium">
                            ${formatPrice(model.input_price_per_million)}
                        </div>
                    </div>
                    <div>
                        <span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
                            Out $/1M
                        </span>
                        <div className="text-(--text-primary) font-medium">
                            ${formatPrice(model.output_price_per_million)}
                        </div>
                    </div>
                </div>
            </div>

            {CAP_META.some((m) => caps[m.key]) && (
                <div>
                    <span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
                        Capabilities
                    </span>
                    <div className="flex flex-wrap gap-1 mt-1">
                        {CAP_META.filter((m) => caps[m.key]).map((m) => (
                            <span
                                key={m.key}
                                className={`px-1.5 py-0.5 text-[10px] rounded-full border ${m.style}`}
                            >
                                {m.label}
                            </span>
                        ))}
                    </div>
                </div>
            )}

            <div>
                <span className="text-[10px] text-(--text-tertiary) uppercase tracking-wider">
                    Proxy ID
                </span>
                <code className="block mt-0.5 p-1.5 rounded bg-(--surface-input) text-[10px] text-(--text-secondary) break-all">
                    {proxyModelID(model.provider_name, model.model_id)}
                </code>
            </div>
        </div>
    );
}

export function Chat() {
    const { data: models } = useQuery({
        queryKey: ["models"],
        queryFn: () => api.models.list(),
        staleTime: 60_000,
    });

    const { data: providers } = useQuery({
        queryKey: ["providers"],
        queryFn: () => api.providers.list(),
        staleTime: 60_000,
    });

    const [messages, setMessages] = useState<ChatMessage[]>(() => {
        try {
            if (localStorage.getItem("persistChat") === "true") {
                const stored = localStorage.getItem("chatMessages");
                if (stored) return JSON.parse(stored);
            }
        } catch { /* ignore corrupt stored data */ }
        return [];
    });
    const [selectedModel, setSelectedModel] = useState<string>(() => {
        try {
            if (localStorage.getItem("persistChat") === "true") {
                return localStorage.getItem("chatSelectedModel") ?? "";
            }
        } catch { /* ignore */ }
        return "";
    });
    const [systemPrompt, setSystemPrompt] = useState<string>(() => {
        try {
            if (localStorage.getItem("persistChat") === "true") {
                return localStorage.getItem("chatSystemPrompt") ?? "";
            }
        } catch { /* ignore */ }
        return "";
    });
    const [activePersonaId, setActivePersonaId] = useState<string | null>(
        () => {
            try {
                if (localStorage.getItem("persistChat") === "true") {
                    const v = localStorage.getItem("chatActivePersonaId");
                    return v || null;
                }
            } catch { /* ignore */ }
            return null;
        },
    );
    const [pendingPersona, setPendingPersona] = useState<
        import("../data/presets").PersonaPreset | null
    >(null);
    const [input, setInput] = useState("");
    const [isStreaming, setIsStreaming] = useState(false);
    const [messageParams, setMessageParams] = useState<GenerationParams>({});
    const abortRef = useRef<AbortController | null>(null);
    const messagesContainerRef = useRef<HTMLDivElement>(null);
    const systemPromptRef = useRef<HTMLTextAreaElement>(null);
    const { toast } = useToast();
    const { persistChat } = useStorage();

    const enabledModels =
        models?.filter((m) => m.enabled && m.provider_name) || [];

    const selectedModelObj = enabledModels.find(
        (m) => proxyModelID(m.provider_name, m.model_id) === selectedModel,
    );

    useEffect(() => {
        const el = messagesContainerRef.current;
        if (el) el.scrollTop = el.scrollHeight;
    }, [messages]);

    useEffect(() => {
        if (!persistChat) return;
        try {
            localStorage.setItem("chatMessages", JSON.stringify(messages));
        } catch { /* quota exceeded */ }
    }, [messages, persistChat]);

    useEffect(() => {
        if (!persistChat) return;
        try {
            localStorage.setItem("chatSelectedModel", selectedModel);
        } catch { /* quota exceeded */ }
    }, [selectedModel, persistChat]);

    useEffect(() => {
        if (!persistChat) return;
        try {
            localStorage.setItem("chatSystemPrompt", systemPrompt);
        } catch { /* quota exceeded */ }
    }, [systemPrompt, persistChat]);

    useEffect(() => {
        if (!persistChat) return;
        try {
            localStorage.setItem(
                "chatActivePersonaId",
                activePersonaId ?? "",
            );
        } catch { /* quota exceeded */ }
    }, [activePersonaId, persistChat]);

    const autoExpandTextarea = useCallback(
        (ref: React.RefObject<HTMLTextAreaElement | null>) => {
            requestAnimationFrame(() => {
                const el = ref.current;
                if (el) {
                    el.style.height = "auto";
                    el.style.height = el.scrollHeight + "px";
                }
            });
        },
        [],
    );

    const handlePersonaSelect = useCallback(
        (persona: import("../data/presets").PersonaPreset) => {
            if (systemPrompt.trim() && activePersonaId === null) {
                // User has custom text — confirm before overwriting
                setPendingPersona(persona);
                return;
            }
            setSystemPrompt(persona.systemPrompt);
            setActivePersonaId(persona.id);
            autoExpandTextarea(systemPromptRef);
        },
        [systemPrompt, activePersonaId, autoExpandTextarea],
    );

    const handleCustomPersona = useCallback(() => {
        if (activePersonaId !== null) {
            // A preset is active — warn that switching to custom will clear
            setPendingPersona({
                id: "__custom__",
                icon: "✏️",
                label: "Custom",
                systemPrompt: "",
            } as import("../data/presets").PersonaPreset);
            return;
        }
    }, [activePersonaId]);

    const handleSystemPromptChange = useCallback(
        (value: string) => {
            setSystemPrompt(value);
            // If user edits away from a preset, switch to custom
            const current = CHAT_PERSONAS.find((p) => p.id === activePersonaId);
            if (current && value !== current.systemPrompt) {
                setActivePersonaId(null);
            }
        },
        [activePersonaId],
    );

    const handleSend = useCallback(async () => {
        if (!input.trim() || !selectedModel || isStreaming) return;

        const userMessage: ChatMessage = {
            role: "user",
            content: input.trim(),
            timestamp: Date.now(),
        };
        const updatedMessages = [...messages, userMessage];
        setMessages(updatedMessages);
        setInput("");
        setIsStreaming(true);

        const abortCtrl = new AbortController();
        abortRef.current = abortCtrl;
        const startTime = performance.now();
        let charCount = 0;
        let promptTokens = 0;
        let completionTokens = 0;

        const chatMessages: Array<{ role: string; content: string }> = [];
        if (systemPrompt.trim()) {
            chatMessages.push({ role: "system", content: systemPrompt.trim() });
        }
        for (const m of updatedMessages) {
            chatMessages.push({ role: m.role, content: m.content });
        }

        const assistantMessage: ChatMessage = {
            role: "assistant",
            content: "",
            rawContent: "",
            thinkingContent: "",
            model: selectedModel,
            timestamp: Date.now(),
            params: hasAnyParam(messageParams) ? messageParams : undefined,
        };
        setMessages((prev) => [...prev, assistantMessage]);

        try {
            const resp = await api.chat.chat({
                model: selectedModel,
                stream: true,
                messages: chatMessages,
                ...(hasAnyParam(messageParams) ? messageParams : {}),
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
                            assistantMessage.rawContent =
                                (assistantMessage.rawContent || "") + delta;
                            const extracted = extractThinking(
                                assistantMessage.rawContent,
                            );
                            assistantMessage.content = extracted.content;
                            assistantMessage.thinkingContent =
                                extracted.thinking;
                            setMessages((prev) => {
                                const next = [...prev];
                                next[next.length - 1] = {
                                    ...assistantMessage,
                                };
                                return next;
                            });
                        }
                        const thinkingDelta =
                            chunk.choices?.[0]?.delta?.reasoning_content;
                        if (thinkingDelta) {
                            assistantMessage.thinkingContent =
                                (assistantMessage.thinkingContent || "") +
                                thinkingDelta;
                            setMessages((prev) => {
                                const next = [...prev];
                                next[next.length - 1] = {
                                    ...assistantMessage,
                                };
                                return next;
                            });
                        }
                        if (chunk.usage) {
                            promptTokens = chunk.usage.prompt_tokens ?? 0;
                            completionTokens =
                                chunk.usage.completion_tokens ?? 0;
                        }
                    } catch {
                        // ignore parse errors
                    }
                }
            }

            const durationMs = performance.now() - startTime;
            const tokensPerSecond =
                durationMs > 0 ? charCount / (durationMs / 1000) : null;

            assistantMessage.metrics = {
                tokensPerSecond,
                durationMs: Math.round(durationMs),
                promptTokens,
                completionTokens,
            };
            setMessages((prev) => {
                const next = [...prev];
                next[next.length - 1] = { ...assistantMessage };
                return next;
            });
        } catch (err) {
            const msg = err instanceof Error ? err.message : "Unknown error";
            assistantMessage.content = `**Error:** ${msg}`;
            assistantMessage.metrics = {
                tokensPerSecond: null,
                durationMs: Math.round(performance.now() - startTime),
                promptTokens: 0,
                completionTokens: 0,
            };
            setMessages((prev) => {
                const next = [...prev];
                next[next.length - 1] = { ...assistantMessage };
                return next;
            });
            toast(msg, "error");
        } finally {
            setIsStreaming(false);
            abortRef.current = null;
        }
    }, [input, selectedModel, isStreaming, messages, systemPrompt, messageParams, toast]);

    const handleStop = useCallback(() => {
        abortRef.current?.abort();
        abortRef.current = null;
        setIsStreaming(false);
    }, []);

    const handleKeyDown = (e: React.KeyboardEvent) => {
        if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault();
            if (isStreaming) handleStop();
            else handleSend();
        }
    };

    return (
        <div className="flex flex-col gap-6 min-h-[calc(100vh-64px)]">
            {/* Header */}
            <div className="flex justify-between items-center">
                <div>
                    <div className="flex items-center gap-3">
                        <MessageSquare
                            size={28}
                            strokeWidth={2}
                            className="text-(--accent)"
                        />
                        <h1 className="text-3xl font-bold text-white">Chat</h1>
                    </div>
                    <p className="text-gray-400">
                        Test enabled models in temporary chat
                    </p>
                </div>
            </div>

            {/* Controls */}
            <div className="ui-card p-4 space-y-4">
                <ModelPicker
                    models={enabledModels}
                    selected={selectedModel}
                    onChange={setSelectedModel}
                    multi={false}
                    label="Model"
                    providers={
                        providers?.map((p) => ({
                            name: p.name,
                            base_url: p.base_url,
                        })) ?? []
                    }
                />

                <div>
                    <PresetBar
                        items={CHAT_PERSONAS}
                        activeId={activePersonaId}
                        onSelect={handlePersonaSelect}
                        onCustom={handleCustomPersona}
                    />
                    <textarea
                        ref={systemPromptRef}
                        value={systemPrompt}
                        onChange={(e) => {
                            handleSystemPromptChange(e.target.value);
                            if (!e.target.value) {
                                e.target.style.height = "auto";
                            } else if (
                                e.target.scrollHeight > e.target.clientHeight
                            ) {
                                e.target.style.height =
                                    e.target.scrollHeight + "px";
                            }
                        }}
                        placeholder="Enter your custom prompt here..."
                        rows={1}
                        maxLength={5000}
                        className="ui-input w-full resize-y max-h-32 min-h-11 overflow-y-auto mt-1.5"
                        style={{ height: "auto" }}
                    />
                </div>
            </div>

            {/* Chat Area: Model Details + Messages */}
            <div className="flex gap-4 flex-1 min-h-0">
                {/* Model Details Pill */}
                <div className="w-1/4 shrink-0 flex flex-col min-h-0">
                    {selectedModelObj ? (
                        <ModelDetailPill
                            model={selectedModelObj}
                            params={messageParams}
                            onParamsChange={setMessageParams}
                        />
                    ) : (
                        <div className="ui-card p-4 flex flex-col items-center justify-center text-(--text-tertiary) text-xs">
                            <Bot
                                size={32}
                                strokeWidth={1}
                                className="mb-2 opacity-40"
                            />
                            <p>Select a model</p>
                        </div>
                    )}
                </div>
                {/* Messages */}
                <div
                    ref={messagesContainerRef}
                    className="flex-1 min-h-0 overflow-y-auto pr-1 space-y-4"
                >
                    {messages.length === 0 && (
                        <div className="flex flex-col items-center justify-center py-20 text-(--text-tertiary)">
                            <Bot
                                size={48}
                                strokeWidth={1}
                                className="mb-4 opacity-40"
                            />
                            <p>Chat will appear here</p>
                        </div>
                    )}

                    {messages.map((msg, i) => {
                        if (msg.role === "system") return null;
                        const isUser = msg.role === "user";
                        const hasThinking =
                            !isUser && (msg.thinkingContent || "").length > 0;
                        const isStreamingThis =
                            isStreaming && i === messages.length - 1;

                        return (
                            <div
                                key={i}
                                className={`flex ${isUser ? "justify-end" : "justify-start"}`}
                            >
                                <div
                                    className={`max-w-[80%] rounded-xl ${
                                        isUser
                                            ? "bg-(--accent) text-white rounded-br-sm p-2.5"
                                            : "ui-card rounded-bl-sm p-4"
                                    }`}
                                >
                                    {!isUser && msg.model && (
                                        <div className="flex items-center gap-2 mb-2">
                                            <Bot
                                                size={14}
                                                className="text-(--accent)"
                                            />
                                            <span className="text-xs text-(--accent) font-medium">
                                                {msg.model.split("/").pop()}
                                            </span>
                                            {isStreamingThis && (
                                                <span className="w-1.5 h-1.5 rounded-full bg-(--accent) animate-pulse ml-1" />
                                            )}
                                        </div>
                                    )}
                                    {!isUser && hasThinking && (
                                        <ChatThinkingBlock
                                            thinking={msg.thinkingContent || ""}
                                            isStreaming={isStreamingThis}
                                        />
                                    )}
                                    {!isUser && msg.content ? (
                                        <div className="prose prose-invert prose-xs max-w-none text-(--text-primary) text-xs [&_p]:my-1 [&_ul]:my-1 [&_ol]:my-1 [&_li]:my-0.5 [&_h1]:text-sm [&_h2]:text-xs [&_h3]:text-xs [&_code]:text-(--accent) [&_code]:bg-(--surface-hover) [&_code]:px-1 [&_code]:py-0.5 [&_code]:rounded [&_code]:text-[11px] [&_pre]:bg-(--surface-hover) [&_pre]:rounded-lg [&_pre]:p-3 [&_pre]:overflow-x-auto [&_pre]:my-2 [&_pre]:text-[11px] [&_blockquote]:border-l-2 [&_blockquote]:border-(--accent)/40 [&_blockquote]:pl-3 [&_blockquote]:text-(--text-secondary) [&_strong]:text-white [&_em]:text-(--text-secondary) [&_a]:text-(--accent) [&_a]:underline [&_hr]:border-(--border-subtle) [&_table]:text-[10px] [&_th]:px-1.5 [&_th]:py-0.5 [&_td]:px-1.5 [&_td]:py-0.5 [&_th]:border [&_th]:border-(--border-subtle) [&_td]:border [&_td]:border-(--border-subtle)">
                                            <ReactMarkdown
                                                remarkPlugins={[remarkGfm]}
                                            >
                                                {msg.content}
                                            </ReactMarkdown>
                                        </div>
                                    ) : !isUser &&
                                      !hasThinking &&
                                      isStreamingThis ? (
                                        <div className="text-(--text-tertiary) text-xs flex items-center gap-2">
                                            <span className="w-1.5 h-1.5 rounded-full bg-(--accent) animate-pulse" />
                                            Waiting...
                                        </div>
                                    ) : isUser ? (
                                        <div className="whitespace-pre-wrap text-xs">
                                            {msg.content}
                                        </div>
                                    ) : null}
                                    <div
                                        className={`flex items-center gap-3 text-[11px] ${
                                            isUser
                                                ? "mt-0.5 text-white/60"
                                                : "mt-2 text-(--text-tertiary)"
                                        }`}
                                    >
                                        <span>{formatTime(msg.timestamp)}</span>
                                        {msg.metrics && (
                                            <>
                                                <span className="flex items-center gap-1">
                                                    <Clock size={10} />
                                                    {formatDuration(
                                                        msg.metrics.durationMs,
                                                    )}
                                                </span>
                                                {msg.metrics.tokensPerSecond !==
                                                    null && (
                                                    <span className="flex items-center gap-1">
                                                        <Zap size={10} />
                                                        {msg.metrics.tokensPerSecond.toFixed(
                                                            1,
                                                        )}{" "}
                                                        tok/s
                                                    </span>
                                                )}
                                                {msg.metrics.promptTokens +
                                                    msg.metrics
                                                        .completionTokens >
                                                    0 && (
                                                    <span>
                                                        {msg.metrics
                                                            .promptTokens +
                                                            msg.metrics
                                                                .completionTokens}{" "}
                                                        tok
                                                    </span>
                                                )}
                                                {msg.params && (
                                                    <span
                                                        className="inline-flex items-center text-(--accent) cursor-pointer hover:drop-shadow-[0_0_4px_var(--accent)] transition-all"
                                                        title={`Settings: ${Object.entries(msg.params)
                                                            .filter(([, v]) => v !== undefined)
                                                            .map(([k, v]) => `${k.replace(/_/g, " ")}=${v}`)
                                                            .join(", ")}`}
                                                    >
                                                        <Settings size={10} />
                                                    </span>
                                                )}
                                            </>
                                        )}
                                    </div>
                                </div>
                            </div>
                        );
                    })}
                </div>
            </div>

            {/* Input */}
            <div className="ui-card p-4">
                <div className="flex items-center gap-3">
                    <textarea
                        value={input}
                        onChange={(e) => {
                            setInput(e.target.value);
                            e.target.style.height = "auto";
                            e.target.style.height =
                                e.target.scrollHeight + "px";
                        }}
                        onKeyDown={handleKeyDown}
                        placeholder={
                            selectedModel
                                ? "Type a message..."
                                : "Select a model first"
                        }
                        disabled={!selectedModel || isStreaming}
                        autoFocus
                        rows={1}
                        maxLength={10000}
                        className="flex-1 ui-input resize-none max-h-32 min-h-11 overflow-y-auto"
                        style={{ height: "auto" }}
                    />
                    <button
                        onClick={isStreaming ? handleStop : handleSend}
                        disabled={!selectedModel}
                        className={`ui-btn flex items-center gap-2 shrink-0 ${
                            isStreaming ? "ui-btn-danger" : "ui-btn-primary"
                        }`}
                    >
                        {isStreaming ? (
                            <>
                                <X size={16} />
                                Stop
                            </>
                        ) : (
                            <>
                                <Send size={16} />
                                Send
                            </>
                        )}
                    </button>
                </div>
                <p className="text-xs text-(--text-muted) mt-2">
                    Press Enter to send, Shift+Enter for newline
                </p>
            </div>

            {/* Persona Overwrite Confirmation */}
            {pendingPersona && (
                <ConfirmDialog
                    title={
                        pendingPersona.id === "__custom__"
                            ? "Switch to Custom"
                            : "Overwrite System Prompt"
                    }
                    fields={["System prompt"]}
                    onConfirm={() => {
                        if (pendingPersona.id === "__custom__") {
                            setSystemPrompt("");
                            setActivePersonaId(null);
                        } else {
                            setSystemPrompt(pendingPersona.systemPrompt);
                            setActivePersonaId(pendingPersona.id);
                        }
                        setPendingPersona(null);
                        autoExpandTextarea(systemPromptRef);
                    }}
                    onCancel={() => setPendingPersona(null)}
                />
            )}
        </div>
    );
}
