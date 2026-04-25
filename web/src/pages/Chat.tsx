import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import { useState, useRef, useCallback, useEffect } from "react";
import {
    MessageSquare,
    Send,
    X,
    Bot,
    Info,
    Clock,
    Zap,
    Brain,
    ChevronDown,
    ChevronRight,
} from "lucide-react";
import type { Model } from "../api/types";
import type { ChatMessage } from "../api/types";
import { useToast } from "../context/ToastContext";
import { ModelPicker } from "../components/ModelPicker";
import { PresetBar } from "../components/PresetBar";
import { ConfirmDialog } from "../components/ConfirmDialog";
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

interface ModelDetailModalProps {
    model: Model;
    onClose: () => void;
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
                <p className="text-sm text-(--text-secondary)">
                    {model.description}
                </p>
                <div className="grid grid-cols-2 gap-3 text-sm">
                    <div className="ui-card p-3">
                        <span className="text-(--text-tertiary)">Provider</span>
                        <div className="text-(--text-primary) font-medium">
                            {model.provider_name}
                        </div>
                    </div>
                    <div className="ui-card p-3">
                        <span className="text-(--text-tertiary)">Model ID</span>
                        <div className="text-(--text-primary) font-medium">
                            {model.model_id}
                        </div>
                    </div>
                    <div className="ui-card p-3">
                        <span className="text-(--text-tertiary)">
                            Context Length
                        </span>
                        <div className="text-(--text-primary) font-medium">
                            {model.context_length?.toLocaleString() ?? "-"}
                        </div>
                    </div>
                    <div className="ui-card p-3">
                        <span className="text-(--text-tertiary)">
                            Max Output
                        </span>
                        <div className="text-(--text-primary) font-medium">
                            {model.max_output_tokens?.toLocaleString() ?? "-"}
                        </div>
                    </div>
                    <div className="ui-card p-3">
                        <span className="text-(--text-tertiary)">
                            Input Price
                        </span>
                        <div className="text-(--text-primary) font-medium">
                            ${formatPrice(model.input_price_per_million)}/1M
                        </div>
                    </div>
                    <div className="ui-card p-3">
                        <span className="text-(--text-tertiary)">
                            Output Price
                        </span>
                        <div className="text-(--text-primary) font-medium">
                            ${formatPrice(model.output_price_per_million)}/1M
                        </div>
                    </div>
                </div>
                {capList.length > 0 && (
                    <div>
                        <span className="text-sm text-(--text-tertiary)">
                            Capabilities
                        </span>
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
                    <span className="text-sm text-(--text-tertiary)">
                        Proxy Model ID
                    </span>
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

    const { data: providers } = useQuery({
        queryKey: ["providers"],
        queryFn: () => api.providers.list(),
        staleTime: 60_000,
    });

    const [messages, setMessages] = useState<ChatMessage[]>([]);
    const [selectedModel, setSelectedModel] = useState<string>("");
    const [systemPrompt, setSystemPrompt] = useState<string>("");
    const [activePersonaId, setActivePersonaId] = useState<string | null>(null);
    const [pendingPersona, setPendingPersona] = useState<
        import("../data/presets").PersonaPreset | null
    >(null);
    const [input, setInput] = useState("");
    const [isStreaming, setIsStreaming] = useState(false);
    const [detailModel, setDetailModel] = useState<Model | null>(null);
    const abortRef = useRef<AbortController | null>(null);
    const messagesContainerRef = useRef<HTMLDivElement>(null);
    const systemPromptRef = useRef<HTMLTextAreaElement>(null);
    const { toast } = useToast();

    const enabledModels =
        models?.filter((m) => m.enabled && m.provider_name) || [];

    const selectedModelObj = enabledModels.find(
        (m) => proxyModelID(m.provider_name, m.model_id) === selectedModel,
    );

    useEffect(() => {
        const el = messagesContainerRef.current;
        if (el) el.scrollTop = el.scrollHeight;
    }, [messages]);

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
        };
        setMessages((prev) => [...prev, assistantMessage]);

        try {
            const resp = await api.chat.chat({
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

                <div className="flex items-center gap-3 flex-wrap">
                    {selectedModelObj && (
                        <button
                            onClick={() => setDetailModel(selectedModelObj)}
                            className="ui-btn-secondary flex items-center gap-1.5"
                        >
                            <Info size={14} />
                            {selectedModelObj.display_name ||
                                selectedModelObj.model_id}
                        </button>
                    )}
                </div>

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
                        placeholder="You are a helpful assistant..."
                        rows={1}
                        maxLength={5000}
                        className="ui-input w-full resize-y max-h-32 min-h-11 overflow-y-auto mt-1.5"
                        style={{ height: "auto" }}
                    />
                </div>
            </div>

            {/* Messages */}
            <div
                ref={messagesContainerRef}
                className="max-h-[calc(100vh-520px)] min-h-48 overflow-y-auto pr-1 space-y-4"
            >
                {messages.length === 0 && (
                    <div className="flex flex-col items-center justify-center py-20 text-(--text-tertiary)">
                        <Bot
                            size={48}
                            strokeWidth={1}
                            className="mb-4 opacity-40"
                        />
                        <p>Select a model and start chatting</p>
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
                                                msg.metrics.completionTokens >
                                                0 && (
                                                <span>
                                                    {msg.metrics.promptTokens +
                                                        msg.metrics
                                                            .completionTokens}{" "}
                                                    tok
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

            {/* Input */}
            <div className="ui-card p-4">
                <div className="flex items-end gap-3">
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

            {/* Model Detail Modal */}
            {detailModel && (
                <ModelDetailModal
                    model={detailModel}
                    onClose={() => setDetailModel(null)}
                />
            )}

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
