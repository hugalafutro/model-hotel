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
    Users,
    Play,
    Pause,
    Timer,
    Gauge,
} from "lucide-react";
import type { ChatMessage, GenerationParams } from "../api/types";

import { useToast } from "../context/ToastContext";
import { useStorage } from "../context/StorageContext";
import { useSidebarMode } from "../context/SidebarModeContext";
import { ModelPicker } from "../components/ModelPicker";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { PersonaPicker } from "../components/PersonaPicker";
import { ModelDetailPanel } from "../components/ModelDetailPanel";
import { proxyModelID } from "../utils/model";
import { CHAT_PERSONAS } from "../data/presets";
import { extractThinking } from "../utils/thinking";
import { ModelReplyCard } from "../components/ModelReplyCard";
import { MarkdownContent } from "../components/MarkdownContent";
import { ConversationConfig } from "../components/ConversationConfig";

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

type ConversationState = "idle" | "running" | "paused" | "completed";

function getApiMessagesForModel(
    allMessages: ChatMessage[],
    targetModelId: string,
    modelAId: string,
    persona: string,
): Array<{ role: string; content: string }> {
    const apiMessages: Array<{ role: string; content: string }> = [];
    if (persona.trim()) {
        apiMessages.push({ role: "system", content: persona.trim() });
    }
    const isModelA = targetModelId === modelAId;

    for (const msg of allMessages) {
        if (msg.role === "user") {
            apiMessages.push({
                role: isModelA ? "assistant" : "user",
                content: msg.content,
            });
        } else if (msg.role === "assistant") {
            if (msg.model === targetModelId) {
                apiMessages.push({
                    role: "assistant",
                    content: msg.content,
                });
            } else {
                apiMessages.push({
                    role: "user",
                    content: msg.content,
                });
            }
        }
    }
    return apiMessages;
}

interface StreamResult {
    rawContent: string;
    content: string;
    thinkingContent: string;
    error: string | null;
    durationMs: number;
    tokensPerSecond: number | null;
    promptTokens: number;
    completionTokens: number;
}

async function streamModelResponse(
    modelId: string,
    apiMessages: Array<{ role: string; content: string }>,
    params: GenerationParams,
    abortCtrl: AbortController,
    onDelta: (raw: string, content: string, thinking: string) => void,
): Promise<StreamResult> {
    const startTime = performance.now();
    let charCount = 0;
    let promptTokens = 0;
    let completionTokens = 0;
    let rawContent = "";
    let content = "";
    let thinkingContent = "";

    try {
        const resp = await api.chat.chat({
            model: modelId,
            stream: true,
            messages: apiMessages,
            ...(hasAnyParam(params) ? params : {}),
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
                        rawContent += delta;
                        const extracted = extractThinking(rawContent);
                        content = extracted.content;
                        thinkingContent = extracted.thinking || thinkingContent;
                        onDelta(rawContent, content, thinkingContent);
                    }
                    const thinkingDelta =
                        chunk.choices?.[0]?.delta?.reasoning_content ??
                        chunk.choices?.[0]?.delta?.reasoning;
                    if (thinkingDelta) {
                        thinkingContent += thinkingDelta;
                        onDelta(rawContent, content, thinkingContent);
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
    } catch (err) {
        const errorMsg = err instanceof Error ? err.message : "Unknown error";
        return {
            rawContent,
            content,
            thinkingContent,
            error: errorMsg,
            durationMs: Math.round(performance.now() - startTime),
            tokensPerSecond:
                performance.now() - startTime > 0
                    ? charCount / ((performance.now() - startTime) / 1000)
                    : null,
            promptTokens,
            completionTokens,
        };
    }

    const durationMs = performance.now() - startTime;
    const tokensPerSecond =
        durationMs > 0 ? charCount / (durationMs / 1000) : null;

    return {
        rawContent,
        content,
        thinkingContent,
        error: null,
        durationMs: Math.round(durationMs),
        tokensPerSecond,
        promptTokens,
        completionTokens,
    };
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

    const { chatSubMode, setChatSubMode } = useSidebarMode();

    const [messages, setMessages] = useState<ChatMessage[]>(() => {
        try {
            if (localStorage.getItem("persistChat") === "true") {
                const stored = localStorage.getItem("chatMessages");
                if (stored) return JSON.parse(stored);
            }
        } catch {
            /* ignore */
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

    // ── Conversation mode state ──
    const [selectedModelB, setSelectedModelB] = useState<string>(() => {
        try {
            if (localStorage.getItem("persistConversation") === "true") {
                return localStorage.getItem("conversationModelB") ?? "";
            }
        } catch {
            /* ignore */
        }
        return "";
    });
    const [systemPromptB, setSystemPromptB] = useState<string>(() => {
        try {
            if (localStorage.getItem("persistConversation") === "true") {
                return localStorage.getItem("conversationSystemPromptB") ?? "";
            }
        } catch {
            /* ignore */
        }
        return "";
    });
    const [activePersonaIdB, setActivePersonaIdB] = useState<string | null>(
        () => {
            try {
                if (localStorage.getItem("persistConversation") === "true") {
                    const v = localStorage.getItem(
                        "conversationActivePersonaIdB",
                    );
                    return v || null;
                }
            } catch {
                /* ignore */
            }
            return null;
        },
    );
    const [messageParamsB, setMessageParamsB] = useState<GenerationParams>(
        () => {
            try {
                if (localStorage.getItem("persistConversation") === "true") {
                    const v = localStorage.getItem("conversationParamsB");
                    if (v) return JSON.parse(v);
                }
            } catch {
                /* ignore */
            }
            return {};
        },
    );
    const [conversationState, setConversationState] =
        useState<ConversationState>("idle");
    const [currentTurn, setCurrentTurn] = useState(0);

    // Reset conversation state when chatSubMode changes (e.g. sidebar click),
    // but skip the initial mount so we don't wipe persisted messages.
    const prevChatSubModeRef = useRef(chatSubMode);
    useEffect(() => {
        if (prevChatSubModeRef.current !== chatSubMode) {
            prevChatSubModeRef.current = chatSubMode;
            setMessages([]);
            setConversationState("idle");
            setCurrentTurn(0);
            setInput("");
        }
    }, [chatSubMode]);
    const [maxTurns, setMaxTurns] = useState(() => {
        try {
            const v = localStorage.getItem("conversationMaxTurns");
            return v ? parseInt(v, 10) : 10;
        } catch {
            return 10;
        }
    });
    const [turnDelayMs, setTurnDelayMs] = useState(() => {
        try {
            const v = localStorage.getItem("conversationTurnDelayMs");
            return v ? parseInt(v, 10) : 500;
        } catch {
            return 500;
        }
    });
    const [configCollapsed, setConfigCollapsed] = useState(false);
    const conversationAbortRef = useRef<AbortController | null>(null);

    const enabledModels =
        models?.filter((m) => m.enabled && m.provider_name) || [];

    const selectedModelObj = enabledModels.find(
        (m) => proxyModelID(m.provider_name, m.model_id) === selectedModel,
    );
    const selectedModelObjB = enabledModels.find(
        (m) => proxyModelID(m.provider_name, m.model_id) === selectedModelB,
    );

    const providerData =
        providers?.map((p) => ({
            name: p.name,
            base_url: p.base_url,
        })) ?? [];

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

    // ── Conversation persistence effects ──
    useEffect(() => {
        if (localStorage.getItem("persistConversation") !== "true") return;
        try {
            localStorage.setItem("conversationModelB", selectedModelB);
        } catch {
            /* ignore */
        }
    }, [selectedModelB]);

    useEffect(() => {
        if (localStorage.getItem("persistConversation") !== "true") return;
        try {
            localStorage.setItem("conversationSystemPromptB", systemPromptB);
        } catch {
            /* ignore */
        }
    }, [systemPromptB]);

    useEffect(() => {
        if (localStorage.getItem("persistConversation") !== "true") return;
        try {
            localStorage.setItem(
                "conversationActivePersonaIdB",
                activePersonaIdB ?? "",
            );
        } catch {
            /* ignore */
        }
    }, [activePersonaIdB]);

    useEffect(() => {
        if (localStorage.getItem("persistConversation") !== "true") return;
        try {
            localStorage.setItem(
                "conversationParamsB",
                JSON.stringify(messageParamsB),
            );
        } catch {
            /* ignore */
        }
    }, [messageParamsB]);

    useEffect(() => {
        try {
            localStorage.setItem("conversationMaxTurns", String(maxTurns));
        } catch {
            /* ignore */
        }
    }, [maxTurns]);

    useEffect(() => {
        try {
            localStorage.setItem(
                "conversationTurnDelayMs",
                String(turnDelayMs),
            );
        } catch {
            /* ignore */
        }
    }, [turnDelayMs]);

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

        const chatMessages: Array<{ role: string; content: string }> = [];
        if (systemPrompt.trim()) {
            chatMessages.push({
                role: "system",
                content: systemPrompt.trim(),
            });
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
        const messageIndex = updatedMessages.length;

        const result = await streamModelResponse(
            selectedModel,
            chatMessages,
            messageParams,
            abortCtrl,
            (raw, content, thinking) => {
                setMessages((prev) => {
                    if (prev.length <= messageIndex) return prev;
                    const next = [...prev];
                    next[messageIndex] = {
                        ...next[messageIndex],
                        rawContent: raw,
                        content,
                        thinkingContent: thinking,
                    };
                    return next;
                });
            },
        );

        setMessages((prev) => {
            if (prev.length <= messageIndex) return prev;
            const next = [...prev];
            next[messageIndex] = {
                ...next[messageIndex],
                rawContent: result.rawContent,
                content: result.content,
                thinkingContent: result.thinkingContent,
                error: result.error,
                metrics: {
                    tokensPerSecond: result.tokensPerSecond,
                    durationMs: result.durationMs,
                    promptTokens: result.promptTokens,
                    completionTokens: result.completionTokens,
                },
            };
            return next;
        });

        if (result.error) toast(result.error, "error");

        setIsStreaming(false);
        abortRef.current = null;
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

    const handleRegenerate = useCallback(async () => {
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
            chatMessages.push({
                role: "system",
                content: systemPrompt.trim(),
            });
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
        const messageIndex = updatedMessages.length;

        try {
            const result = await streamModelResponse(
                selectedModel || "",
                chatMessages,
                messageParams,
                abortCtrl,
                (raw, content, thinking) => {
                    setMessages((prev) => {
                        if (prev.length <= messageIndex) return prev;
                        const next = [...prev];
                        next[messageIndex] = {
                            ...next[messageIndex],
                            rawContent: raw,
                            content,
                            thinkingContent: thinking,
                        };
                        return next;
                    });
                },
            );

            setMessages((prev) => {
                if (prev.length <= messageIndex) return prev;
                const next = [...prev];
                next[messageIndex] = {
                    ...next[messageIndex],
                    rawContent: result.rawContent,
                    content: result.content,
                    thinkingContent: result.thinkingContent,
                    error: result.error,
                    metrics: {
                        tokensPerSecond: result.tokensPerSecond,
                        durationMs: result.durationMs,
                        promptTokens: result.promptTokens,
                        completionTokens: result.completionTokens,
                    },
                };
                return next;
            });

            if (result.error) toast(result.error, "error");
        } catch (err) {
            const msg = err instanceof Error ? err.message : "Unknown error";
            toast(msg, "error");
        } finally {
            setIsStreaming(false);
            abortRef.current = null;
        }
    }, [
        isStreaming,
        messages,
        selectedModel,
        systemPrompt,
        messageParams,
        toast,
    ]);

    // ── Unified conversation orchestration ──
    const runConversation = useCallback(
        async (resume = false) => {
            const canStart =
                selectedModel &&
                selectedModelB &&
                (resume || input.trim()) &&
                conversationState !== "running";

            if (!canStart) return;

            const abortCtrl = new AbortController();
            conversationAbortRef.current = abortCtrl;
            setConversationState("running");
            setIsStreaming(true);

            let currentMessages = messages;
            let turn = currentTurn;
            let modelTurn: "A" | "B";

            if (!resume) {
                setCurrentTurn(0);
                turn = 0;
                const userMessage: ChatMessage = {
                    role: "user",
                    content: input.trim(),
                    timestamp: Date.now(),
                };
                currentMessages = [...messages, userMessage];
                setMessages(currentMessages);
                setInput("");
                modelTurn = "B";
            } else {
                // Resume: figure out whose turn it is based on last assistant
                const lastAssistantIdx = currentMessages.findLastIndex(
                    (m) => m.role === "assistant",
                );
                modelTurn =
                    lastAssistantIdx >= 0 &&
                    currentMessages[lastAssistantIdx].model === selectedModel
                        ? "B"
                        : "A";
            }

            while (turn < maxTurns * 2 && !abortCtrl.signal.aborted) {
                const isModelA = modelTurn === "A";
                const modelId = isModelA ? selectedModel : selectedModelB;
                const persona = isModelA ? systemPrompt : systemPromptB;
                const params = isModelA ? messageParams : messageParamsB;

                const apiMessages = getApiMessagesForModel(
                    currentMessages,
                    modelId,
                    selectedModel,
                    persona,
                );

                const assistantMessage: ChatMessage = {
                    role: "assistant",
                    content: "",
                    rawContent: "",
                    thinkingContent: "",
                    model: modelId,
                    timestamp: Date.now(),
                    params: hasAnyParam(params) ? params : undefined,
                };
                currentMessages = [...currentMessages, assistantMessage];
                setMessages(currentMessages);
                const messageIndex = currentMessages.length - 1;

                const result = await streamModelResponse(
                    modelId,
                    apiMessages,
                    params,
                    abortCtrl,
                    (raw, content, thinking) => {
                        setMessages((prev) => {
                            if (prev.length <= messageIndex) return prev;
                            const next = [...prev];
                            next[messageIndex] = {
                                ...next[messageIndex],
                                rawContent: raw,
                                content,
                                thinkingContent: thinking,
                            };
                            return next;
                        });
                    },
                );

                setMessages((prev) => {
                    if (prev.length <= messageIndex) return prev;
                    const next = [...prev];
                    next[messageIndex] = {
                        ...next[messageIndex],
                        rawContent: result.rawContent,
                        content: result.content,
                        thinkingContent: result.thinkingContent,
                        error: result.error,
                        metrics: {
                            tokensPerSecond: result.tokensPerSecond,
                            durationMs: result.durationMs,
                            promptTokens: result.promptTokens,
                            completionTokens: result.completionTokens,
                        },
                    };
                    return next;
                });

                currentMessages = currentMessages.map((m, i) =>
                    i === messageIndex
                        ? {
                              ...m,
                              rawContent: result.rawContent,
                              content: result.content,
                              thinkingContent: result.thinkingContent,
                              error: result.error,
                              metrics: {
                                  tokensPerSecond: result.tokensPerSecond,
                                  durationMs: result.durationMs,
                                  promptTokens: result.promptTokens,
                                  completionTokens: result.completionTokens,
                              },
                          }
                        : m,
                );

                if (result.error) {
                    toast(`${modelId}: ${result.error}`, "error");
                    break;
                }

                turn++;
                modelTurn = modelTurn === "A" ? "B" : "A";
                setCurrentTurn(turn);

                if (turn < maxTurns * 2 && !abortCtrl.signal.aborted) {
                    await new Promise((r) => setTimeout(r, turnDelayMs));
                }
            }

            setIsStreaming(false);
            setConversationState((prev) =>
                prev === "running" ? "completed" : prev,
            );
            conversationAbortRef.current = null;
        },
        [
            selectedModel,
            selectedModelB,
            input,
            messages,
            currentTurn,
            maxTurns,
            turnDelayMs,
            systemPrompt,
            systemPromptB,
            messageParams,
            messageParamsB,
            toast,
            conversationState,
        ],
    );

    const handleStopConversation = useCallback(() => {
        conversationAbortRef.current?.abort();
        conversationAbortRef.current = null;
        setIsStreaming(false);
        setConversationState("paused");
    }, []);

    const handleKeyDown = (e: React.KeyboardEvent) => {
        if (e.key === "Enter" && !e.shiftKey) {
            e.preventDefault();
            if (chatSubMode === "chat") {
                if (isStreaming) handleStop();
                else handleSend();
            }
            // In conversation mode, Enter doesn't auto-submit
        }
    };

    const totalTokens = messages.reduce(
        (acc, m) =>
            acc +
            (m.metrics?.promptTokens ?? 0) +
            (m.metrics?.completionTokens ?? 0),
        0,
    );
    const totalDuration = messages.reduce(
        (acc, m) => acc + (m.metrics?.durationMs ?? 0),
        0,
    );

    const canStartConversation =
        chatSubMode === "conversation" &&
        !!selectedModel &&
        !!selectedModelB &&
        !!input.trim() &&
        conversationState === "idle";

    return (
        <div
            className={`flex flex-col gap-6 min-h-[calc(100vh-64px)] ${chatSubMode === "conversation" ? "" : "lg:h-[calc(100vh-64px)] lg:overflow-hidden"}`}
        >
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
                        {chatSubMode === "chat"
                            ? "Test enabled models in temporary chat"
                            : "Watch two models converse with each other"}
                    </p>
                </div>
            </div>

            {/* Controls */}
            <div className="ui-card p-4 shrink-0">
                <div className="flex items-center justify-between">
                    <div className="flex items-center gap-3">
                        <span className="text-sm font-semibold text-(--text-primary)">
                            Controls
                        </span>
                        <div className="flex items-center gap-1">
                            <button
                                onClick={() => setChatSubMode("chat")}
                                className={`px-3 py-1 rounded-md text-xs font-medium transition-all ${
                                    chatSubMode === "chat"
                                        ? "bg-(--accent)/20 text-(--accent) border border-(--accent)/40 cursor-default"
                                        : "text-(--text-tertiary) hover:text-(--text-secondary) border border-transparent cursor-pointer"
                                }`}
                            >
                                <MessageSquare
                                    size={12}
                                    className="inline mr-1 -mt-0.5"
                                />
                                Chat with AI
                            </button>
                            <button
                                onClick={() => setChatSubMode("conversation")}
                                className={`px-3 py-1 rounded-md text-xs font-medium transition-all ${
                                    chatSubMode === "conversation"
                                        ? "bg-(--accent)/20 text-(--accent) border border-(--accent)/40 cursor-default"
                                        : "text-(--text-tertiary) hover:text-(--text-secondary) border border-transparent cursor-pointer"
                                }`}
                            >
                                <Users
                                    size={12}
                                    className="inline mr-1 -mt-0.5"
                                />
                                AI Conversation
                            </button>
                        </div>
                    </div>
                    <div className="flex items-center gap-1">
                        {(messages.length > 0 ||
                            selectedModel ||
                            (chatSubMode === "conversation" &&
                                selectedModelB)) && (
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
                            {chatSubMode === "chat" ? (
                                <>
                                    <ModelPicker
                                        models={enabledModels}
                                        selected={selectedModel}
                                        onChange={setSelectedModel}
                                        multi={false}
                                        providers={providerData}
                                    />
                                    <PersonaPicker
                                        personas={CHAT_PERSONAS}
                                        activePersonaId={activePersonaId}
                                        systemPrompt={systemPrompt}
                                        onActivePersonaChange={
                                            setActivePersonaId
                                        }
                                        onSystemPromptChange={setSystemPrompt}
                                    />
                                </>
                            ) : (
                                <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
                                    <div>
                                        <label className="text-sm text-(--text-secondary) mb-2 block">
                                            Model A
                                        </label>
                                        <ModelPicker
                                            models={enabledModels}
                                            selected={selectedModel}
                                            onChange={setSelectedModel}
                                            multi={false}
                                            providers={providerData}
                                        />
                                        <div className="mt-3">
                                            <PersonaPicker
                                                personas={CHAT_PERSONAS}
                                                activePersonaId={
                                                    activePersonaId
                                                }
                                                systemPrompt={systemPrompt}
                                                onActivePersonaChange={
                                                    setActivePersonaId
                                                }
                                                onSystemPromptChange={
                                                    setSystemPrompt
                                                }
                                                label="Persona A"
                                            />
                                        </div>
                                    </div>
                                    <div>
                                        <label className="text-sm text-(--text-secondary) mb-2 block">
                                            Model B
                                        </label>
                                        <ModelPicker
                                            models={enabledModels}
                                            selected={selectedModelB}
                                            onChange={setSelectedModelB}
                                            multi={false}
                                            providers={providerData}
                                        />
                                        <div className="mt-3">
                                            <PersonaPicker
                                                personas={CHAT_PERSONAS}
                                                activePersonaId={
                                                    activePersonaIdB
                                                }
                                                systemPrompt={systemPromptB}
                                                onActivePersonaChange={
                                                    setActivePersonaIdB
                                                }
                                                onSystemPromptChange={
                                                    setSystemPromptB
                                                }
                                                label="Persona B"
                                            />
                                        </div>
                                    </div>
                                </div>
                            )}
                        </div>
                    </div>
                </div>
            </div>

            {/* Conversation Config */}
            {chatSubMode === "conversation" && (
                <ConversationConfig
                    maxTurns={maxTurns}
                    onMaxTurnsChange={setMaxTurns}
                    turnDelayMs={turnDelayMs}
                    onTurnDelayMsChange={setTurnDelayMs}
                    conversationState={conversationState}
                    currentTurn={currentTurn}
                    configCollapsed={configCollapsed}
                    onToggleCollapsed={() => setConfigCollapsed((c) => !c)}
                    input={input}
                    onInputChange={setInput}
                    onStart={() => runConversation(false)}
                    canStart={canStartConversation}
                    selectedModel={selectedModel}
                    selectedModelB={selectedModelB}
                />
            )}

            {/* Chat Area: Model Details + Messages */}
            <div
                className={`flex gap-4 flex-1 ${chatSubMode === "conversation" ? "overflow-visible" : "min-h-0 lg:overflow-hidden"}`}
            >
                {/* Sidebar */}
                <div
                    className={`shrink-0 flex flex-col ${
                        chatSubMode === "conversation"
                            ? "w-1/3 gap-3 overflow-visible"
                            : "min-h-0 lg:overflow-y-auto w-1/4"
                    }`}
                >
                    {chatSubMode === "chat" ? (
                        selectedModelObj ? (
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
                        )
                    ) : (
                        <>
                            {selectedModelObj ? (
                                <div className="border-l-2 border-(--accent)">
                                    <ModelDetailPanel
                                        model={selectedModelObj}
                                        params={messageParams}
                                        onParamsChange={setMessageParams}
                                        collapsible
                                    />
                                </div>
                            ) : (
                                <div className="ui-card p-3 flex items-center justify-center text-(--text-tertiary) text-xs">
                                    <Bot
                                        size={20}
                                        className="mr-2 opacity-40"
                                    />
                                    Select Model A
                                </div>
                            )}
                            {selectedModelObjB ? (
                                <div className="border-l-2 border-(--accent) bg-(--accent)/5">
                                    <ModelDetailPanel
                                        model={selectedModelObjB}
                                        params={messageParamsB}
                                        onParamsChange={setMessageParamsB}
                                        collapsible
                                    />
                                </div>
                            ) : (
                                <div className="ui-card p-3 flex items-center justify-center text-(--text-tertiary) text-xs">
                                    <Bot
                                        size={20}
                                        className="mr-2 opacity-40"
                                    />
                                    Select Model B
                                </div>
                            )}
                        </>
                    )}
                </div>

                {/* Messages */}
                <div
                    ref={messagesContainerRef}
                    className={`flex-1 pr-1 space-y-4 ${
                        chatSubMode === "conversation"
                            ? "overflow-visible"
                            : "min-h-0 overflow-y-auto"
                    }`}
                >
                    {messages.length === 0 && (
                        <div className="flex flex-col items-center justify-center py-20 text-(--text-tertiary)">
                            <Bot
                                size={48}
                                strokeWidth={1}
                                className="mb-4 opacity-40"
                            />
                            <p>
                                {chatSubMode === "chat"
                                    ? "Chat will appear here"
                                    : "Conversation will appear here"}
                            </p>
                        </div>
                    )}

                    {messages.map((msg, i) => {
                        if (msg.role === "system") return null;
                        const isUser = msg.role === "user";
                        const isStreamingThis =
                            isStreaming && i === messages.length - 1;
                        const isModelB =
                            msg.role === "assistant" &&
                            msg.model === selectedModelB;

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

                        /* ── Model B message (conversation mode, right side) ── */
                        if (chatSubMode === "conversation" && isModelB) {
                            return (
                                <div key={i} className="flex justify-end">
                                    <div className="max-w-[80%]">
                                        <ModelReplyCard
                                            model={msg.model || ""}
                                            content={msg.content}
                                            thinkingContent={
                                                msg.thinkingContent
                                            }
                                            error={msg.error}
                                            metrics={msg.metrics}
                                            isStreaming={isStreamingThis}
                                            shortenModelName={false}
                                            tint="accent"
                                            headerEnd={
                                                isStreamingThis ? (
                                                    <button
                                                        onClick={
                                                            handleStopConversation
                                                        }
                                                        className="text-red-400/60 hover:text-red-400 transition-colors cursor-pointer ml-1"
                                                        title="Cancel"
                                                    >
                                                        <CircleStop size={14} />
                                                    </button>
                                                ) : null
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
                                                            setMessages(
                                                                (prev) => {
                                                                    const idx =
                                                                        prev.findIndex(
                                                                            (
                                                                                m,
                                                                            ) =>
                                                                                m ===
                                                                                msg,
                                                                        );
                                                                    if (
                                                                        idx ===
                                                                        -1
                                                                    )
                                                                        return prev;
                                                                    return prev.filter(
                                                                        (
                                                                            _,
                                                                            i,
                                                                        ) =>
                                                                            i !==
                                                                            idx,
                                                                    );
                                                                },
                                                            );
                                                            toast(
                                                                "Message deleted",
                                                                "info",
                                                            );
                                                        }}
                                                    >
                                                        <Trash2 size={10} />
                                                    </button>
                                                </div>
                                            }
                                        />
                                    </div>
                                </div>
                            );
                        }

                        /* ── Assistant message (Model A or chat mode) ── */
                        return (
                            <div key={i} className="flex justify-start">
                                <div className="max-w-[80%]">
                                    <ModelReplyCard
                                        model={msg.model || ""}
                                        content={msg.content}
                                        thinkingContent={msg.thinkingContent}
                                        error={msg.error}
                                        metrics={msg.metrics}
                                        isStreaming={isStreamingThis}
                                        shortenModelName={false}
                                        headerEnd={
                                            isStreamingThis ? (
                                                <button
                                                    onClick={
                                                        chatSubMode ===
                                                        "conversation"
                                                            ? handleStopConversation
                                                            : handleStop
                                                    }
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
                                                    ) &&
                                                chatSubMode === "chat" && (
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
                                                                    `${k.replace(
                                                                        /_/g,
                                                                        " ",
                                                                    )}=${v}`,
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

            {/* Input / Stats Area — chat mode input bar + conversation stats when active */}
            {chatSubMode === "chat" && (
                <div className="ui-card p-4 shrink-0">
                    <div className="space-y-2">
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
                        <p className="text-xs text-(--text-muted)">
                            Press Enter to send, Shift+Enter for newline
                        </p>
                    </div>
                </div>
            )}
            {chatSubMode === "conversation" &&
                (conversationState === "running" ||
                    conversationState === "paused" ||
                    conversationState === "completed") && (
                    <div className="ui-card p-4 shrink-0">
                        <div className="space-y-3">
                            <div className="flex items-center justify-between flex-wrap gap-2">
                                <div className="flex items-center gap-4 text-sm text-(--text-secondary)">
                                    <span className="flex items-center gap-1.5">
                                        <Gauge size={14} />
                                        Turn {currentTurn} / {maxTurns * 2}
                                    </span>
                                    <span className="flex items-center gap-1.5">
                                        <Timer size={14} />
                                        {(totalDuration / 1000).toFixed(1)}s
                                    </span>
                                    <span className="flex items-center gap-1.5">
                                        <Bot size={14} />
                                        {totalTokens} tokens
                                    </span>
                                </div>
                                <div className="flex items-center gap-2">
                                    {conversationState === "running" && (
                                        <button
                                            onClick={handleStopConversation}
                                            className="ui-btn ui-btn-danger flex items-center gap-2"
                                        >
                                            <Pause size={16} />
                                            Pause
                                        </button>
                                    )}
                                    {(conversationState === "paused" ||
                                        conversationState === "completed") && (
                                        <>
                                            <button
                                                onClick={() =>
                                                    runConversation(true)
                                                }
                                                disabled={
                                                    currentTurn >= maxTurns * 2
                                                }
                                                className="ui-btn ui-btn-primary flex items-center gap-2"
                                            >
                                                <Play size={16} />
                                                {conversationState ===
                                                "completed"
                                                    ? "Continue"
                                                    : "Resume"}
                                            </button>
                                            <button
                                                onClick={() =>
                                                    setPendingReset(true)
                                                }
                                                className="ui-btn flex items-center gap-2 text-red-500 hover:drop-shadow-[0_0_6px_var(--color-red-500,red)]"
                                            >
                                                <RotateCcw size={16} />
                                                Reset
                                            </button>
                                        </>
                                    )}
                                </div>
                            </div>
                            {conversationState === "running" && (
                                <div className="flex items-center gap-2 text-xs text-(--text-muted)">
                                    <span className="w-1.5 h-1.5 rounded-full bg-(--accent) animate-pulse" />
                                    {isStreaming
                                        ? "Model is generating…"
                                        : "Waiting for next turn…"}
                                </div>
                            )}
                        </div>
                    </div>
                )}

            {pendingReset && (
                <ConfirmDialog
                    title={
                        chatSubMode === "chat"
                            ? "Reset Chat"
                            : "Reset Conversation"
                    }
                    message={
                        chatSubMode === "chat"
                            ? "This will clear all messages and reset the chat. Continue?"
                            : "This will clear the conversation and reset both models. Continue?"
                    }
                    fields={[]}
                    confirmLabel="Reset"
                    onConfirm={() => {
                        setMessages([]);
                        setInput("");
                        setConversationState("idle");
                        setCurrentTurn(0);
                        if (chatSubMode === "chat") {
                            setSelectedModel("");
                            setSystemPrompt("");
                            setActivePersonaId(null);
                            setMessageParams({});
                        }
                        setPendingReset(false);
                        toast(
                            chatSubMode === "chat"
                                ? "Chat reset"
                                : "Conversation reset",
                            "info",
                        );
                    }}
                    onCancel={() => setPendingReset(false)}
                />
            )}
        </div>
    );
}
