import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import { useState, useRef, useCallback, useEffect } from "react";
import {
    MessageSquare,
    Send,
    Settings as SettingsIcon,
    X,
    Bot,
    Info,
    Clock,
    Zap,
    Trash2,
    ChevronDown,
    ChevronUp,
} from "lucide-react";
import type { Model } from "../api/types";
import type { ChatMessage } from "../api/types";
import { useToast } from "../context/ToastContext";
import { ModelPicker } from "../components/ModelPicker";

function formatDuration(ms: number): string {
    if (ms < 1000) return `${ms}ms`;
    return `${(ms / 1000).toFixed(1)}s`;
}

function formatTime(ts: number): string {
    const d = new Date(ts);
    return d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
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

interface ModelDetailModalProps {
    model: Model;
    onClose: () => void;
}

function ModelDetailModal({ model, onClose }: ModelDetailModalProps) {
    const caps = parseCapabilities(model.capabilities);
    const capList = Object.entries(caps)
        .filter(([, v]) => v)
        .map(([k]) => k.replace(/_/g, " "));

    return (
        <div
            className="fixed inset-0 bg-black/60 backdrop-blur-sm flex items-center justify-center z-50"
            onClick={(e) => {
                if (e.target === e.currentTarget) onClose();
            }}
        >
            <div className="ui-card w-full max-w-lg mx-4 p-6 space-y-4 max-h-[80vh] overflow-y-auto">
                <div className="flex items-center justify-between">
                    <h2 className="text-xl font-semibold text-(--text-primary)">
                        {model.display_name || model.model_id}
                    </h2>
                    <button
                        onClick={onClose}
                        className="text-(--text-tertiary) hover:text-white transition-colors"
                    >
                        <X size={20} />
                    </button>
                </div>
                <p className="text-sm text-(--text-secondary)">{model.description}</p>
                <div className="grid grid-cols-2 gap-3 text-sm">
                    <div className="ui-card p-3">
                        <span className="text-(--text-tertiary)">Provider</span>
                        <div className="text-(--text-primary) font-medium">{model.provider_name}</div>
                    </div>
                    <div className="ui-card p-3">
                        <span className="text-(--text-tertiary)">Model ID</span>
                        <div className="text-(--text-primary) font-medium">{model.model_id}</div>
                    </div>
                    <div className="ui-card p-3">
                        <span className="text-(--text-tertiary)">Context Length</span>
                        <div className="text-(--text-primary) font-medium">
                            {model.context_length?.toLocaleString() ?? "-"}
                        </div>
                    </div>
                    <div className="ui-card p-3">
                        <span className="text-(--text-tertiary)">Max Output</span>
                        <div className="text-(--text-primary) font-medium">
                            {model.max_output_tokens?.toLocaleString() ?? "-"}
                        </div>
                    </div>
                    <div className="ui-card p-3">
                        <span className="text-(--text-tertiary)">Input Price</span>
                        <div className="text-(--text-primary) font-medium">
                            ${formatPrice(model.input_price_per_million)}/1M
                        </div>
                    </div>
                    <div className="ui-card p-3">
                        <span className="text-(--text-tertiary)">Output Price</span>
                        <div className="text-(--text-primary) font-medium">
                            ${formatPrice(model.output_price_per_million)}/1M
                        </div>
                    </div>
                </div>
                {capList.length > 0 && (
                    <div>
                        <span className="text-sm text-(--text-tertiary)">Capabilities</span>
                        <div className="flex flex-wrap gap-2 mt-2">
                            {capList.map((c) => (
                                <span
                                    key={c}
                                    className="px-2 py-1 text-xs rounded-full bg-(--accent)/10 text-(--accent) border border-(--accent)/20"
                                >
                                    {c}
                                </span>
                            ))}
                        </div>
                    </div>
                )}
                <div>
                    <span className="text-sm text-(--text-tertiary)">Proxy Model ID</span>
                    <code className="block mt-1 p-2 rounded bg-(--surface-input) text-xs text-(--text-secondary)">
                        {proxyModelID(model.provider_name, model.model_id)}
                    </code>
                </div>
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

    const [messages, setMessages] = useState<ChatMessage[]>([]);
    const [selectedModel, setSelectedModel] = useState<string>(() =>
        localStorage.getItem("chat_selected_model") || "",
    );
    const [systemPrompt, setSystemPrompt] = useState<string>(() =>
        localStorage.getItem("chat_system_prompt") || "",
    );
    const [showSystemPrompt, setShowSystemPrompt] = useState(false);
    const [input, setInput] = useState("");
    const [isStreaming, setIsStreaming] = useState(false);
    const [detailModel, setDetailModel] = useState<Model | null>(null);
    const abortRef = useRef<AbortController | null>(null);
    const messagesEndRef = useRef<HTMLDivElement>(null);
    const { toast } = useToast();

    const enabledModels =
        models?.filter((m) => m.enabled && m.provider_name) || [];

    const selectedModelObj = enabledModels.find(
        (m) => proxyModelID(m.provider_name, m.model_id) === selectedModel,
    );

    useEffect(() => {
        if (selectedModel) {
            localStorage.setItem("chat_selected_model", selectedModel);
        }
    }, [selectedModel]);

    useEffect(() => {
        localStorage.setItem("chat_system_prompt", systemPrompt);
    }, [systemPrompt]);

    useEffect(() => {
        messagesEndRef.current?.scrollIntoView({ behavior: "smooth" });
    }, [messages]);

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
            model: selectedModel,
            timestamp: Date.now(),
        };
        setMessages((prev) => [...prev, assistantMessage]);

        try {
            const resp = await api.chat.completions({
                model: selectedModel,
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
                            assistantMessage.content += delta;
                            setMessages((prev) => {
                                const next = [...prev];
                                next[next.length - 1] = { ...assistantMessage };
                                return next;
                            });
                        }
                    } catch {
                        // ignore parse errors
                    }
                }
            }

            const durationMs = performance.now() - startTime;
            const tokensPerSecond =
                durationMs > 0 ? (charCount / (durationMs / 1000)) : null;

            assistantMessage.metrics = {
                tokensPerSecond,
                durationMs: Math.round(durationMs),
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
    }, [input, selectedModel, isStreaming, messages, systemPrompt, toast]);

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

    const handleClear = () => {
        setMessages([]);
    };

    return (
        <div className="space-y-6">
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
                        Chat with enabled models through the proxy
                    </p>
                </div>
                <div className="flex items-center gap-3">
                    <button
                        onClick={handleClear}
                        disabled={messages.length === 0}
                        className="ui-btn-secondary flex items-center gap-2 disabled:opacity-40"
                    >
                        <Trash2 size={16} />
                        Clear
                    </button>
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
                />

                <div className="flex items-center gap-3 flex-wrap">
                    {selectedModelObj && (
                        <button
                            onClick={() => setDetailModel(selectedModelObj)}
                            className="ui-btn-secondary flex items-center gap-1.5"
                        >
                            <Info size={14} />
                            {selectedModelObj.display_name || selectedModelObj.model_id}
                        </button>
                    )}

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
                            rows={3}
                            className="ui-input w-full resize-none text-sm"
                        />
                    </div>
                )}
            </div>

            {/* Messages */}
            <div className="space-y-4 min-h-75">
                {messages.length === 0 && (
                    <div className="flex flex-col items-center justify-center py-20 text-(--text-tertiary)">
                        <Bot size={48} strokeWidth={1} className="mb-4 opacity-40" />
                        <p>Select a model and start chatting</p>
                    </div>
                )}

                {messages.map((msg, i) => {
                    if (msg.role === "system") return null;
                    const isUser = msg.role === "user";

                    return (
                        <div
                            key={i}
                            className={`flex ${isUser ? "justify-end" : "justify-start"}`}
                        >
                            <div
                                className={`max-w-[80%] rounded-xl p-4 ${
                                    isUser
                                        ? "bg-(--accent) text-white rounded-br-sm"
                                        : "ui-card rounded-bl-sm"
                                }`}
                            >
                                {!isUser && msg.model && (
                                    <div className="flex items-center gap-2 mb-2">
                                        <Bot
                                            size={14}
                                            className="text-(--accent)"
                                        />
                                        <span className="text-xs text-(--accent) font-medium">
                                            {msg.model}
                                        </span>
                                        {isStreaming &&
                                            i === messages.length - 1 && (
                                                <span className="w-1.5 h-1.5 rounded-full bg-(--accent) animate-pulse ml-1" />
                                            )}
                                    </div>
                                )}
                                <div
                                    className={`whitespace-pre-wrap text-sm ${
                                        isUser ? "" : "text-(--text-primary)"
                                    }`}
                                >
                                    {msg.content || (isStreaming && i === messages.length - 1 ? "Thinking..." : "")}
                                </div>
                                <div
                                    className={`flex items-center gap-3 mt-2 text-[11px] ${
                                        isUser
                                            ? "text-white/60"
                                            : "text-(--text-tertiary)"
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
                                            {msg.metrics.tokensPerSecond !== null && (
                                                <span className="flex items-center gap-1">
                                                    <Zap size={10} />
                                                    {msg.metrics.tokensPerSecond.toFixed(1)}{" "}
                                                    tok/s
                                                </span>
                                            )}
                                        </>
                                    )}
                                </div>
                            </div>
                        </div>
                    );
                })}
                <div ref={messagesEndRef} />
            </div>

            {/* Input */}
            <div className="ui-card p-4">
                <div className="flex items-end gap-3">
                    <textarea
                        value={input}
                        onChange={(e) => setInput(e.target.value)}
                        onKeyDown={handleKeyDown}
                        placeholder={
                            selectedModel
                                ? "Type a message..."
                                : "Select a model first"
                        }
                        disabled={!selectedModel || isStreaming}
                        rows={1}
                        className="flex-1 ui-input resize-none max-h-32 min-h-11"
                        style={{ height: "auto" }}
                    />
                    <button
                        onClick={isStreaming ? handleStop : handleSend}
                        disabled={!selectedModel}
                        className={`ui-btn flex items-center gap-2 shrink-0 ${
                            isStreaming
                                ? "ui-btn-danger"
                                : "ui-btn-primary"
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

            {/* Model Detail Modal */}
            {detailModel && (
                <ModelDetailModal
                    model={detailModel}
                    onClose={() => setDetailModel(null)}
                />
            )}
        </div>
    );
}
