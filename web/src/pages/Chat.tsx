import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import { useState, useRef, useCallback, useEffect } from "react";
import {
    MessageSquare,
    Send,
    X,
    Bot,
    Settings,
    RotateCcw,
    ChevronsDownUp,
    ChevronsUpDown,
    Copy,
    Trash2,
    CircleStop,
    RefreshCw,
} from "lucide-react";
import type { ChatMessage, GenerationParams } from "../api/types";

import { useToast } from "../context/ToastContext";
import { useStorage } from "../context/StorageContext";
import { ModelPicker } from "../components/ModelPicker";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { PersonaPicker } from "../components/PersonaPicker";
import { ModelDetailPanel } from "../components/ModelDetailPanel";
import { proxyModelID } from "../utils/model";
import { CHAT_PERSONAS } from "../data/presets";
import { extractThinking } from "../utils/thinking";
import { ModelReplyCard } from "../components/ModelReplyCard";
import { MarkdownContent } from "../components/MarkdownContent";

function formatTime(ts: number): string {
    const d = new Date(ts);
    return d.toLocaleTimeString(undefined, {
        hour: "2-digit",
        minute: "2-digit",
    });
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
        } catch {
            /* ignore corrupt stored data */
        }
        return [];
    });
    const [selectedModel, setSelectedModel] = useState<string>(() => {
        try {
            if (localStorage.getItem("persistChat") === "true") {
                return localStorage.getItem("chatSelectedModel") ?? "";
            }
        } catch {
            /* ignore */
        }
        return "";
    });
    const [systemPrompt, setSystemPrompt] = useState<string>(() => {
        try {
            if (localStorage.getItem("persistChat") === "true") {
                return localStorage.getItem("chatSystemPrompt") ?? "";
            }
        } catch {
            /* ignore */
        }
        return "";
    });
    const [activePersonaId, setActivePersonaId] = useState<string | null>(
        () => {
            try {
                if (localStorage.getItem("persistChat") === "true") {
                    const v = localStorage.getItem("chatActivePersonaId");
                    return v || null;
                }
            } catch {
                /* ignore */
            }
            return null;
        },
    );
    const [pendingReset, setPendingReset] = useState(false);
    const [input, setInput] = useState("");
    const [isStreaming, setIsStreaming] = useState(false);
    const [messageParams, setMessageParams] = useState<GenerationParams>({});
    const [controlsCollapsed, setControlsCollapsed] = useState(false);
    const abortRef = useRef<AbortController | null>(null);
    const messagesContainerRef = useRef<HTMLDivElement>(null);
    const { toast } = useToast();
    const { persistChat } = useStorage();

    const enabledModels =
        models?.filter((m) => m.enabled && m.provider_name) || [];

    const selectedModelObj = enabledModels.find(
        (m) => proxyModelID(m.provider_name, m.model_id) === selectedModel,
    );

    const scrollToBottom = useCallback(() => {
        requestAnimationFrame(() => {
            const el = messagesContainerRef.current;
            if (el) el.scrollTop = el.scrollHeight;
        });
    }, []);

    useEffect(() => {
        scrollToBottom();
    }, [messages, scrollToBottom]);

    useEffect(() => {
        scrollToBottom();
        const timer = setTimeout(scrollToBottom, 320);
        return () => clearTimeout(timer);
    }, [controlsCollapsed, scrollToBottom]);

    useEffect(() => {
        if (!persistChat) return;
        try {
            localStorage.setItem("chatMessages", JSON.stringify(messages));
        } catch {
            /* quota exceeded */
        }
    }, [messages, persistChat]);

    useEffect(() => {
        if (!persistChat) return;
        try {
            localStorage.setItem("chatSystemPrompt", systemPrompt);
        } catch {
            /* quota exceeded */
        }
    }, [systemPrompt, persistChat]);

    useEffect(() => {
        if (!persistChat) return;
        try {
            localStorage.setItem("chatActivePersonaId", activePersonaId ?? "");
        } catch {
            /* quota exceeded */
        }
    }, [activePersonaId, persistChat]);

    useEffect(() => {
        if (!persistChat) return;
        try {
            localStorage.setItem("chatSelectedModel", selectedModel);
        } catch {
            /* quota exceeded */
        }
    }, [selectedModel, persistChat]);

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
                                extracted.thinking ||
                                assistantMessage.thinkingContent;
                            setMessages((prev) => {
                                const next = [...prev];
                                next[next.length - 1] = {
                                    ...assistantMessage,
                                };
                                return next;
                            });
                        }
                        const thinkingDelta =
                            chunk.choices?.[0]?.delta?.reasoning_content ??
                            chunk.choices?.[0]?.delta?.reasoning;
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
    }, [
        input,
        selectedModel,
        isStreaming,
        messages,
        systemPrompt,
        messageParams,
        toast,
    ]);

    const handleStop = useCallback(() => {
        abortRef.current?.abort();
        abortRef.current = null;
        setIsStreaming(false);
    }, []);

    const handleRegenerate = useCallback(() => {
        if (isStreaming) return;
        let lastUserIdx = -1;
        for (let i = messages.length - 1; i >= 0; i--) {
            if (messages[i].role === "user") {
                lastUserIdx = i;
                break;
            }
        }
        if (lastUserIdx === -1) return;
        const userContent = messages[lastUserIdx].content;
        const baseMessages = messages.slice(0, lastUserIdx);
        setMessages(baseMessages);
        setInput(userContent);

        const chatMessages: Array<{ role: string; content: string }> = [];
        if (systemPrompt.trim()) {
            chatMessages.push({ role: "system", content: systemPrompt.trim() });
        }
        for (const m of baseMessages) {
            chatMessages.push({ role: m.role, content: m.content });
        }
        chatMessages.push({ role: "user", content: userContent });

        const userMessage: ChatMessage = {
            role: "user",
            content: userContent,
            timestamp: Date.now(),
        };
        const updatedMessages = [...baseMessages, userMessage];
        setMessages(updatedMessages);
        setInput("");
        setIsStreaming(true);

        const abortCtrl = new AbortController();
        abortRef.current = abortCtrl;
        const startTime = performance.now();
        let charCount = 0;
        let promptTokens = 0;
        let completionTokens = 0;

        const assistantMessage: ChatMessage = {
            role: "assistant",
            content: "",
            rawContent: "",
            thinkingContent: "",
            model: selectedModel || "",
            timestamp: Date.now(),
            params: hasAnyParam(messageParams) ? messageParams : undefined,
        };
        setMessages((prev) => [...prev, assistantMessage]);

        api.chat
            .chat({
                model: selectedModel || "",
                stream: true,
                messages: chatMessages,
                ...(hasAnyParam(messageParams) ? messageParams : {}),
            })
            .then((resp) => {
                const reader = resp.body?.getReader();
                if (!reader) throw new Error("No readable stream");
                const decoder = new TextDecoder();
                let buffer = "";

                const processStream = async () => {
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
                                const delta =
                                    chunk.choices?.[0]?.delta?.content;
                                if (delta) {
                                    charCount += delta.length;
                                    assistantMessage.rawContent =
                                        (assistantMessage.rawContent || "") +
                                        delta;
                                    const extracted = extractThinking(
                                        assistantMessage.rawContent,
                                    );
                                    assistantMessage.content =
                                        extracted.content;
                                    assistantMessage.thinkingContent =
                                        extracted.thinking ||
                                        assistantMessage.thinkingContent;
                                    setMessages((prev) => {
                                        const next = [...prev];
                                        next[next.length - 1] = {
                                            ...assistantMessage,
                                        };
                                        return next;
                                    });
                                }
                                const thinkingDelta =
                                    chunk.choices?.[0]?.delta
                                        ?.reasoning_content ??
                                    chunk.choices?.[0]?.delta?.reasoning;
                                if (thinkingDelta) {
                                    assistantMessage.thinkingContent =
                                        (assistantMessage.thinkingContent ||
                                            "") + thinkingDelta;
                                    setMessages((prev) => {
                                        const next = [...prev];
                                        next[next.length - 1] = {
                                            ...assistantMessage,
                                        };
                                        return next;
                                    });
                                }
                                if (chunk.usage) {
                                    promptTokens =
                                        chunk.usage.prompt_tokens ?? 0;
                                    completionTokens =
                                        chunk.usage.completion_tokens ?? 0;
                                }
                            } catch {
                                // ignore
                            }
                        }
                    }
                };
                return processStream();
            })
            .then(() => {
                const durationMs = performance.now() - startTime;
                assistantMessage.metrics = {
                    tokensPerSecond:
                        durationMs > 0 ? charCount / (durationMs / 1000) : null,
                    durationMs: Math.round(durationMs),
                    promptTokens,
                    completionTokens,
                };
                setMessages((prev) => {
                    const next = [...prev];
                    next[next.length - 1] = { ...assistantMessage };
                    return next;
                });
            })
            .catch((err) => {
                const msg =
                    err instanceof Error ? err.message : "Unknown error";
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
            })
            .finally(() => {
                setIsStreaming(false);
                abortRef.current = null;
            });
    }, [
        isStreaming,
        messages,
        selectedModel,
        systemPrompt,
        messageParams,
        toast,
    ]);

    const handleKeyDown = (e: React.KeyboardEvent) => {
        if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault();
            if (isStreaming) handleStop();
            else handleSend();
        }
    };

    return (
        <div className="flex flex-col gap-6 min-h-[calc(100vh-64px)] lg:h-[calc(100vh-64px)] lg:overflow-hidden">
            {/* Header */}
            <div className="flex justify-between items-center shrink-0">
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
            <div className="ui-card p-4 shrink-0">
                <div className="flex items-center justify-between">
                    <label className="text-sm text-(--text-secondary)">
                        Model
                    </label>
                    <div className="flex items-center gap-1">
                        {messages.some((m) => m.role === "assistant") && (
                            <button
                                onClick={() => setPendingReset(true)}
                                className="p-1.5 rounded-md transition-all cursor-pointer text-red-500 hover:drop-shadow-[0_0_6px_var(--color-red-500,red)]"
                                title="Reset chat"
                            >
                                <RotateCcw size={14} />
                            </button>
                        )}
                        <button
                            onClick={() => setControlsCollapsed((c) => !c)}
                            className="p-1.5 rounded-md transition-all cursor-pointer text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[0_0_6px_var(--accent)]"
                            title={
                                controlsCollapsed
                                    ? "Expand controls"
                                    : "Collapse controls"
                            }
                        >
                            {controlsCollapsed ? (
                                <ChevronsUpDown size={14} />
                            ) : (
                                <ChevronsDownUp size={14} />
                            )}
                        </button>
                    </div>
                </div>
                <div
                    className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${
                        controlsCollapsed
                            ? "grid-rows-[0fr]"
                            : "grid-rows-[1fr]"
                    }`}
                >
                    <div className="overflow-hidden">
                        <div className="space-y-4 pt-4">
                            <ModelPicker
                                models={enabledModels}
                                selected={selectedModel}
                                onChange={setSelectedModel}
                                multi={false}
                                providers={
                                    providers?.map((p) => ({
                                        name: p.name,
                                        base_url: p.base_url,
                                    })) ?? []
                                }
                            />
                            <PersonaPicker
                                personas={CHAT_PERSONAS}
                                activePersonaId={activePersonaId}
                                systemPrompt={systemPrompt}
                                onActivePersonaChange={setActivePersonaId}
                                onSystemPromptChange={setSystemPrompt}
                            />
                        </div>
                    </div>
                </div>
            </div>

            {/* Chat Area: Model Details + Messages */}
            <div className="flex gap-4 flex-1 min-h-0 lg:overflow-hidden">
                {/* Model Details Pill */}
                <div className="w-1/4 shrink-0 flex flex-col min-h-0 lg:overflow-y-auto">
                    {selectedModelObj ? (
                        <ModelDetailPanel
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
                        const isStreamingThis =
                            isStreaming && i === messages.length - 1;

                        /* ── User message ── */
                        if (isUser) {
                            return (
                                <div key={i} className="flex justify-end">
                                    <div
                                        className="max-w-[80%] bg-(--accent) text-white p-2.5"
                                        style={{
                                            borderRadius: "var(--radius-card)",
                                        }}
                                    >
                                        <MarkdownContent className="[&_strong]:text-white [&_em]:text-white/80">
                                            {msg.content}
                                        </MarkdownContent>
                                        <div className="flex items-center gap-3 text-[11px] mt-0.5 text-white/60">
                                            <span>
                                                {formatTime(msg.timestamp)}
                                            </span>
                                            <button
                                                className="inline-flex items-center cursor-pointer transition-all text-white hover:drop-shadow-[0_0_4px_white]"
                                                onClick={() => {
                                                    navigator.clipboard
                                                        .writeText(msg.content)
                                                        .then(() =>
                                                            toast(
                                                                "Copied to clipboard",
                                                                "info",
                                                            ),
                                                        )
                                                        .catch(() =>
                                                            toast(
                                                                "Failed to copy",
                                                                "error",
                                                            ),
                                                        );
                                                }}
                                            >
                                                <Copy size={10} />
                                            </button>
                                        </div>
                                    </div>
                                </div>
                            );
                        }

                        /* ── Assistant message ── */
                        return (
                            <div key={i} className="flex justify-start">
                                <div className="max-w-[80%]">
                                    <ModelReplyCard
                                        model={msg.model || ""}
                                        content={msg.content}
                                        thinkingContent={msg.thinkingContent}
                                        metrics={msg.metrics}
                                        isStreaming={isStreamingThis}
                                        shortenModelName={false}
                                        headerEnd={
                                            isStreamingThis ? (
                                                <button
                                                    onClick={handleStop}
                                                    className="text-red-400/60 hover:text-red-400 transition-colors cursor-pointer ml-1"
                                                    title="Cancel"
                                                >
                                                    <CircleStop size={14} />
                                                </button>
                                            ) : (
                                                i ===
                                                    messages.findLastIndex(
                                                        (m) =>
                                                            m.role ===
                                                            "assistant",
                                                    ) && (
                                                    <button
                                                        onClick={
                                                            handleRegenerate
                                                        }
                                                        className="text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[0_0_6px_var(--accent)] transition-all cursor-pointer ml-1"
                                                        title="Regenerate"
                                                    >
                                                        <RefreshCw size={14} />
                                                    </button>
                                                )
                                            )
                                        }
                                        footerStart={
                                            <span>
                                                {formatTime(msg.timestamp)}
                                            </span>
                                        }
                                        footerEnd={
                                            <div className="flex items-center gap-2">
                                                <button
                                                    className="inline-flex items-center cursor-pointer transition-all text-(--accent) hover:drop-shadow-[0_0_4px_var(--accent)]"
                                                    onClick={() => {
                                                        navigator.clipboard
                                                            .writeText(
                                                                msg.content,
                                                            )
                                                            .then(() =>
                                                                toast(
                                                                    "Copied to clipboard",
                                                                    "info",
                                                                ),
                                                            )
                                                            .catch(() =>
                                                                toast(
                                                                    "Failed to copy",
                                                                    "error",
                                                                ),
                                                            );
                                                    }}
                                                >
                                                    <Copy size={10} />
                                                </button>
                                                <button
                                                    className="inline-flex items-center cursor-pointer hover:drop-shadow-[0_0_4px_var(--color-red-500,red)] text-red-500 transition-all"
                                                    onClick={() => {
                                                        setMessages((prev) => {
                                                            const idx =
                                                                prev.findIndex(
                                                                    (m) =>
                                                                        m ===
                                                                        msg,
                                                                );
                                                            if (idx === -1)
                                                                return prev;
                                                            const toRemove =
                                                                new Set([idx]);
                                                            if (
                                                                idx > 0 &&
                                                                prev[idx - 1]
                                                                    .role ===
                                                                    "user"
                                                            )
                                                                toRemove.add(
                                                                    idx - 1,
                                                                );
                                                            return prev.filter(
                                                                (_, i) =>
                                                                    !toRemove.has(
                                                                        i,
                                                                    ),
                                                            );
                                                        });
                                                        toast(
                                                            "Message deleted",
                                                            "info",
                                                        );
                                                    }}
                                                >
                                                    <Trash2 size={10} />
                                                </button>
                                                {msg.params && (
                                                    <span
                                                        className="inline-flex items-center text-(--accent) cursor-pointer hover:drop-shadow-[0_0_4px_var(--accent)] transition-all"
                                                        title={`Settings: ${Object.entries(
                                                            msg.params,
                                                        )
                                                            .filter(
                                                                ([, v]) =>
                                                                    v !==
                                                                    undefined,
                                                            )
                                                            .map(
                                                                ([k, v]) =>
                                                                    `${k.replace(/_/g, " ")}=${v}`,
                                                            )
                                                            .join(", ")}`}
                                                    >
                                                        <Settings size={10} />
                                                    </span>
                                                )}
                                            </div>
                                        }
                                        className="rounded-xl rounded-bl-sm p-4"
                                        headerClassName="mb-2"
                                        footerClassName="mt-2"
                                    />
                                </div>
                            </div>
                        );
                    })}
                </div>
            </div>

            {/* Input */}
            <div className="ui-card p-4 shrink-0">
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
                                ? "Type a message…"
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

            {pendingReset && (
                <ConfirmDialog
                    title="Reset Chat"
                    message="This will clear all messages and reset the chat. Continue?"
                    fields={[]}
                    confirmLabel="Reset"
                    onConfirm={() => {
                        setMessages([]);
                        setInput("");
                        setSelectedModel("");
                        setSystemPrompt("");
                        setActivePersonaId(null);
                        setMessageParams({});
                        setPendingReset(false);
                        toast("Chat reset", "info");
                    }}
                    onCancel={() => setPendingReset(false)}
                />
            )}
        </div>
    );
}
