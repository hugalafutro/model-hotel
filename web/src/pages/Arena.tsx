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
    ThumbsUp,
    ThumbsDown,
    Trophy,
    RotateCcw,
    RefreshCw,
    ChevronsUpDown,
    ChevronsDownUp,
    CircleStop,
} from "lucide-react";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { extractThinking } from "../utils/thinking";
import { ThinkingBlock } from "../components/ThinkingBlock";
import { useToast } from "../context/ToastContext";
import { useStorage } from "../context/StorageContext";
import { ModelPicker } from "../components/ModelPicker";
import { PresetBar } from "../components/PresetBar";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { FilterInput } from "../components/FilterInput";
import { ARENA_PROMPTS, CHAT_PERSONAS } from "../data/presets";

function formatDuration(ms: number): string {
    if (ms < 1000) return `${ms.toFixed(0)}ms`;
    return `${(ms / 1000).toFixed(2)}s`;
}

interface ArenaResponse {
    model: string;
    rawContent: string;
    content: string;
    thinkingContent: string;
    startTimeMs: number;
    done: boolean;
    error: string | null;
    metrics: {
        tokensPerSecond: number | null;
        durationMs: number;
        promptTokens: number;
        completionTokens: number;
    } | null;
}

interface MatchupSlot {
    modelId: string;
    personaId: string | null;
    personaPrompt: string;
}

interface Matchup {
    slotA: MatchupSlot | null;
    slotB: MatchupSlot | null;
    responseA: ArenaResponse | null;
    responseB: ArenaResponse | null;
    vote: "A" | "B" | null;
}

interface BracketRound {
    matchups: Matchup[];
}

type BracketPhase =
    | "setup"
    | "running"
    | "voting"
    | "next_round_ready"
    | "finished";

interface WinnerModal {
    winner: string;
    rounds: BracketRound[];
}

export function Arena() {
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

    const { toast } = useToast();
    const { persistArena } = useStorage();

    const [group1Models, setGroup1Models] = useState<string[]>(() => {
        try {
            if (localStorage.getItem("persistArena") === "true") {
                const stored = localStorage.getItem("arenaGroup1Models");
                if (stored) return JSON.parse(stored);
            }
        } catch { /* ignore corrupt stored data */ }
        return [];
    });
    const [group2Models, setGroup2Models] = useState<string[]>(() => {
        try {
            if (localStorage.getItem("persistArena") === "true") {
                const stored = localStorage.getItem("arenaGroup2Models");
                if (stored) return JSON.parse(stored);
            }
        } catch { /* ignore corrupt stored data */ }
        return [];
    });

    const [activePromptId, setActivePromptId] = useState<string | null>(
        () => {
            try {
                if (localStorage.getItem("persistArena") === "true") {
                    const v = localStorage.getItem("arenaActivePromptId");
                    return v || null;
                }
            } catch { /* ignore */ }
            return null;
        },
    );
    const [pendingPrompt, setPendingPrompt] = useState<
        import("../data/presets").ArenaPromptPreset | null
    >(null);
    const [prompt, setPrompt] = useState<string>(() => {
        try {
            if (localStorage.getItem("persistArena") === "true") {
                return localStorage.getItem("arenaPrompt") ?? "";
            }
        } catch { /* ignore */ }
        return "";
    });
    const [savedPrompt, setSavedPrompt] = useState<string>("");

    const [rounds, setRounds] = useState<BracketRound[]>([]);
    const [currentRound, setCurrentRound] = useState(0);
    const [phase, setPhase] = useState<BracketPhase>("setup");
    const [runningModels, setRunningModels] = useState<Set<string>>(new Set());
    const [winnerModal, setWinnerModal] = useState<WinnerModal | null>(null);
    const [disabledModels, setDisabledModels] = useState<Set<string>>(
        new Set(),
    );
    const [arenaCollapsed, setArenaCollapsed] = useState(false);
    const [pendingReset, setPendingReset] = useState(false);

    useEffect(() => {
        if (!persistArena) return;
        try {
            localStorage.setItem(
                "arenaGroup1Models",
                JSON.stringify(group1Models),
            );
        } catch { /* quota exceeded */ }
    }, [group1Models, persistArena]);

    useEffect(() => {
        if (!persistArena) return;
        try {
            localStorage.setItem(
                "arenaGroup2Models",
                JSON.stringify(group2Models),
            );
        } catch { /* quota exceeded */ }
    }, [group2Models, persistArena]);

    useEffect(() => {
        if (!persistArena) return;
        try {
            localStorage.setItem("arenaPrompt", prompt);
        } catch { /* quota exceeded */ }
    }, [prompt, persistArena]);

    useEffect(() => {
        if (!persistArena) return;
        try {
            localStorage.setItem(
                "arenaActivePromptId",
                activePromptId ?? "",
            );
        } catch { /* quota exceeded */ }
    }, [activePromptId, persistArena]);

    const abortMapRef = useRef<Map<string, AbortController>>(new Map());
    const currentRoundRef = useRef(0);
    const roundsLengthRef = useRef(0);
    const promptRef = useRef<HTMLTextAreaElement>(null);

    const enabledModels = useMemo(
        () => models?.filter((m) => m.enabled && m.provider_name) || [],
        [models],
    );

    const providerData = useMemo(
        () =>
            providers?.map((p) => ({
                name: p.name,
                base_url: p.base_url,
            })) ?? [],
        [providers],
    );

    const crossDuplicates = useMemo(() => {
        if (group2Models.length === 0) return false;
        return group1Models.some((m) => group2Models.includes(m));
    }, [group1Models, group2Models]);

    const canRun = useMemo(() => {
        if (phase !== "setup" && phase !== "next_round_ready") return false;
        if (group1Models.length !== 2) return false;
        if (group2Models.length !== 0 && group2Models.length !== 2)
            return false;
        if (!prompt.trim()) return false;
        if (crossDuplicates) return false;
        if (new Set(group1Models).size !== group1Models.length) return false;
        if (
            group2Models.length > 0 &&
            new Set(group2Models).size !== group2Models.length
        )
            return false;
        return true;
    }, [phase, group1Models, group2Models, prompt, crossDuplicates]);

    const disabledReason = useMemo(() => {
        if (phase === "setup") {
            if (group1Models.length === 0) return "Select models for Match 1";
            if (group1Models.length === 1)
                return "Pick 1 more model for Match 1";
            if (new Set(group1Models).size !== group1Models.length)
                return "No duplicate models in Match 1";
            if (group2Models.length === 1)
                return "Pick 1 more model for Match 2, or clear it";
            if (crossDuplicates) return "Models can't appear in both matches";
            if (
                group2Models.length > 1 &&
                new Set(group2Models).size !== group2Models.length
            )
                return "No duplicate models in Match 2";
            if (!prompt.trim()) return "Enter a prompt";
        }
        if (phase === "voting") return "Vote on all matchups to continue";
        return "";
    }, [phase, group1Models, group2Models, prompt, crossDuplicates]);

    const buildInitialRounds = useCallback(
        (g1: string[], g2: string[]): BracketRound[] => {
            const makeSlots = (ids: string[]): MatchupSlot[] =>
                ids.map((m) => ({
                    modelId: m,
                    personaId: null,
                    personaPrompt: "",
                }));

            const firstRoundMatchups: Matchup[] = [
                {
                    slotA: makeSlots(g1)[0] || null,
                    slotB: makeSlots(g1)[1] || null,
                    responseA: null,
                    responseB: null,
                    vote: null,
                },
            ];

            if (g2.length === 2) {
                firstRoundMatchups.push({
                    slotA: makeSlots(g2)[0],
                    slotB: makeSlots(g2)[1],
                    responseA: null,
                    responseB: null,
                    vote: null,
                });
            }

            const bracketRounds: BracketRound[] = [
                { matchups: firstRoundMatchups },
            ];

            if (g2.length === 2) {
                bracketRounds.push({
                    matchups: [
                        {
                            slotA: null,
                            slotB: null,
                            responseA: null,
                            responseB: null,
                            vote: null,
                        },
                    ],
                });
            }

            return bracketRounds;
        },
        [],
    );

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

    const handlePromptPresetSelect = useCallback(
        (preset: import("../data/presets").ArenaPromptPreset) => {
            if (prompt.trim() && activePromptId === null) {
                setPendingPrompt(preset);
                return;
            }
            setPrompt(preset.prompt);
            setActivePromptId(preset.id);
            autoExpandTextarea(promptRef);
        },
        [prompt, activePromptId, autoExpandTextarea],
    );

    const handleCustomPrompt = useCallback(() => {
        if (activePromptId !== null) {
            setPendingPrompt({
                id: "__custom__",
                icon: "✏️",
                label: "Custom",
                prompt: "",
            } as import("../data/presets").ArenaPromptPreset);
            return;
        }
    }, [activePromptId]);

    const handlePromptChange = useCallback(
        (value: string) => {
            setPrompt(value);
            const current = ARENA_PROMPTS.find((p) => p.id === activePromptId);
            if (current && value !== current.prompt) {
                setActivePromptId(null);
            }
        },
        [activePromptId],
    );

    const streamModel = useCallback(
        (
            model: string,
            personaPrompt: string,
            userPrompt: string,
            roundIdx: number,
            slotKey: "A" | "B",
            matchupIdx: number,
        ) => {
            const abortCtrl = new AbortController();
            abortMapRef.current.set(model, abortCtrl);

            const run = async () => {
                const startTime = performance.now();
                let charCount = 0;
                let promptTokens = 0;
                let completionTokens = 0;

                const chatMessages: Array<{ role: string; content: string }> =
                    [];
                if (personaPrompt.trim()) {
                    chatMessages.push({
                        role: "system",
                        content: personaPrompt.trim(),
                    });
                }
                chatMessages.push({ role: "user", content: userPrompt });

                try {
                    const resp = await api.chat.arena({
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
                                const delta =
                                    chunk.choices?.[0]?.delta?.content;
                                if (delta) {
                                    charCount += delta.length;
                                    setRounds((prev) => {
                                        const next = prev.map((r) => ({
                                            ...r,
                                            matchups: r.matchups.map((m) => ({
                                                ...m,
                                            })),
                                        }));
                                        if (
                                            next[roundIdx]?.matchups[matchupIdx]
                                        ) {
                                            const mu =
                                                next[roundIdx].matchups[
                                                    matchupIdx
                                                ];
                                            const respKey =
                                                slotKey === "A"
                                                    ? "responseA"
                                                    : "responseB";
                                            const prev = mu[respKey]!;
                                            const newRaw =
                                                prev.rawContent + delta;
                                            const extracted =
                                                extractThinking(newRaw);
                                            mu[respKey] = {
                                                ...prev,
                                                rawContent: newRaw,
                                                content: extracted.content,
                                                thinkingContent:
                                                    extracted.thinking ||
                                                    prev.thinkingContent,
                                            };
                                        }
                                        return next;
                                    });
                                }
                                const thinkingDelta =
                                    chunk.choices?.[0]?.delta
                                        ?.reasoning_content ??
                                    chunk.choices?.[0]?.delta?.reasoning;
                                if (thinkingDelta) {
                                    setRounds((prev) => {
                                        const next = prev.map((r) => ({
                                            ...r,
                                            matchups: r.matchups.map((m) => ({
                                                ...m,
                                            })),
                                        }));
                                        if (
                                            next[roundIdx]?.matchups[matchupIdx]
                                        ) {
                                            const mu =
                                                next[roundIdx].matchups[
                                                    matchupIdx
                                                ];
                                            const respKey =
                                                slotKey === "A"
                                                    ? "responseA"
                                                    : "responseB";
                                            mu[respKey] = {
                                                ...mu[respKey]!,
                                                thinkingContent:
                                                    mu[respKey]!
                                                        .thinkingContent +
                                                    thinkingDelta,
                                            };
                                        }
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
                                // ignore parse errors
                            }
                        }
                    }

                    const durationMs = performance.now() - startTime;
                    const tokensPerSecond =
                        durationMs > 0 ? charCount / (durationMs / 1000) : null;

                    setRounds((prev) => {
                        const next = prev.map((r) => ({
                            ...r,
                            matchups: r.matchups.map((m) => ({ ...m })),
                        }));
                        if (next[roundIdx]?.matchups[matchupIdx]) {
                            const mu = next[roundIdx].matchups[matchupIdx];
                            const respKey =
                                slotKey === "A" ? "responseA" : "responseB";
                            mu[respKey] = {
                                ...mu[respKey]!,
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
                    const msg =
                        err instanceof Error ? err.message : "Unknown error";
                    setRounds((prev) => {
                        const next = prev.map((r) => ({
                            ...r,
                            matchups: r.matchups.map((m) => ({ ...m })),
                        }));
                        if (next[roundIdx]?.matchups[matchupIdx]) {
                            const mu = next[roundIdx].matchups[matchupIdx];
                            const respKey =
                                slotKey === "A" ? "responseA" : "responseB";
                            mu[respKey] = {
                                ...mu[respKey]!,
                                done: true,
                                error: msg,
                                metrics: {
                                    tokensPerSecond: null,
                                    durationMs: Math.round(
                                        performance.now() - startTime,
                                    ),
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
                        if (next.size === 0) {
                            setPhase("voting");
                        }
                        return next;
                    });
                    abortMapRef.current.delete(model);
                }
            };

            run();
        },
        [toast],
    );

    const runRound = useCallback(
        (roundIdx: number) => {
            const round = rounds[roundIdx];
            if (!round) return;

            const currentPrompt = savedPrompt || prompt.trim();

            const modelSet = new Set<string>();
            for (const mu of round.matchups) {
                if (mu.slotA) modelSet.add(mu.slotA.modelId);
                if (mu.slotB) modelSet.add(mu.slotB.modelId);
            }
            setRunningModels(modelSet);
            setPhase("running");

            setRounds((prev) => {
                const next = prev.map((r) => ({
                    ...r,
                    matchups: r.matchups.map((m) => ({ ...m })),
                }));
                if (next[roundIdx]) {
                    next[roundIdx].matchups = next[roundIdx].matchups.map(
                        (mu) => {
                            const now = Date.now();
                            return {
                                ...mu,
                                responseA: mu.slotA
                                    ? {
                                          model: mu.slotA.modelId,
                                          rawContent: "",
                                          content: "",
                                          thinkingContent: "",
                                          startTimeMs: now,
                                          done: false,
                                          error: null,
                                          metrics: null,
                                      }
                                    : null,
                                responseB: mu.slotB
                                    ? {
                                          model: mu.slotB.modelId,
                                          rawContent: "",
                                          content: "",
                                          thinkingContent: "",
                                          startTimeMs: now,
                                          done: false,
                                          error: null,
                                          metrics: null,
                                      }
                                    : null,
                            };
                        },
                    );
                }
                return next;
            });

            for (let mi = 0; mi < round.matchups.length; mi++) {
                const mu = round.matchups[mi];
                if (mu.slotA) {
                    streamModel(
                        mu.slotA.modelId,
                        mu.slotA.personaPrompt,
                        currentPrompt,
                        roundIdx,
                        "A",
                        mi,
                    );
                }
                if (mu.slotB) {
                    streamModel(
                        mu.slotB.modelId,
                        mu.slotB.personaPrompt,
                        currentPrompt,
                        roundIdx,
                        "B",
                        mi,
                    );
                }
            }
        },
        [rounds, savedPrompt, prompt, streamModel],
    );

    const handleRunArena = useCallback(() => {
        if (!canRun) return;

        const currentPrompt = prompt.trim();
        setSavedPrompt(currentPrompt);

        const initialRounds = buildInitialRounds(group1Models, group2Models);
        setRounds(initialRounds);
        currentRoundRef.current = 0;
        roundsLengthRef.current = initialRounds.length;
        setCurrentRound(0);
        setPhase("running");

        const modelSet = new Set<string>();
        for (const mu of initialRounds[0].matchups) {
            if (mu.slotA) modelSet.add(mu.slotA.modelId);
            if (mu.slotB) modelSet.add(mu.slotB.modelId);
        }
        setRunningModels(modelSet);

        setRounds((prev) => {
            const next = prev.map((r) => ({
                ...r,
                matchups: r.matchups.map((m) => ({ ...m })),
            }));
            if (next[0]) {
                const now = Date.now();
                next[0].matchups = next[0].matchups.map((mu) => ({
                    ...mu,
                    responseA: mu.slotA
                        ? {
                              model: mu.slotA.modelId,
                              rawContent: "",
                              content: "",
                              thinkingContent: "",
                              startTimeMs: now,
                              done: false,
                              error: null,
                              metrics: null,
                          }
                        : null,
                    responseB: mu.slotB
                        ? {
                              model: mu.slotB.modelId,
                              rawContent: "",
                              content: "",
                              thinkingContent: "",
                              startTimeMs: now,
                              done: false,
                              error: null,
                              metrics: null,
                          }
                        : null,
                }));
            }
            return next;
        });

        for (let mi = 0; mi < initialRounds[0].matchups.length; mi++) {
            const mu = initialRounds[0].matchups[mi];
            if (mu.slotA) {
                streamModel(
                    mu.slotA.modelId,
                    mu.slotA.personaPrompt,
                    currentPrompt,
                    0,
                    "A",
                    mi,
                );
            }
            if (mu.slotB) {
                streamModel(
                    mu.slotB.modelId,
                    mu.slotB.personaPrompt,
                    currentPrompt,
                    0,
                    "B",
                    mi,
                );
            }
        }
    }, [
        canRun,
        prompt,
        group1Models,
        group2Models,
        buildInitialRounds,
        streamModel,
    ]);

    const handleVote = useCallback(
        (roundIdx: number, matchupIdx: number, vote: "A" | "B") => {
            let shouldAdvance = false;
            let advanceRoundIdx = -1;

            setRounds((prev) => {
                const next = prev.map((r) => ({
                    ...r,
                    matchups: r.matchups.map((m) => ({ ...m })),
                }));
                const mu = next[roundIdx]?.matchups[matchupIdx];
                if (mu) {
                    mu.vote = mu.vote === vote ? null : vote;
                }

                if (
                    roundIdx === currentRoundRef.current &&
                    mu?.vote !== null &&
                    next[roundIdx].matchups.every((m) => m.vote !== null) &&
                    roundIdx < next.length - 1
                ) {
                    shouldAdvance = true;
                    advanceRoundIdx = roundIdx;

                    const winners = next[roundIdx].matchups.map((m) =>
                        m.vote === "A" ? m.slotA : m.slotB,
                    );
                    const nextRoundIdx = roundIdx + 1;
                    if (next[nextRoundIdx] && winners.length >= 2) {
                        next[nextRoundIdx].matchups[0] = {
                            slotA: winners[0] ? { ...winners[0] } : null,
                            slotB: winners[1] ? { ...winners[1] } : null,
                            responseA: null,
                            responseB: null,
                            vote: null,
                        };
                    }
                }

                return next;
            });

            if (shouldAdvance) {
                const nextRI = advanceRoundIdx + 1;
                setCurrentRound(nextRI);
                currentRoundRef.current = nextRI;
                setPhase("running");
                queueMicrotask(() => runRound(nextRI));
            }
        },
        [runRound],
    );

    const handleAdvanceRound = useCallback(() => {
        const round = rounds[currentRound];
        if (!round) return;

        const allVoted = round.matchups.every((mu) => mu.vote !== null);
        if (!allVoted) return;

        const isLastRound = currentRound >= rounds.length - 1;

        if (isLastRound) {
            const finalMu = round.matchups[0];
            const winner =
                finalMu?.vote === "A"
                    ? finalMu.slotA?.modelId
                    : finalMu.slotB?.modelId;
            if (winner) {
                setWinnerModal({ winner, rounds });
                setPhase("finished");
            }
            return;
        }

        const winners = round.matchups.map((mu) =>
            mu.vote === "A" ? mu.slotA : mu.slotB,
        );

        setRounds((prev) => {
            const next = prev.map((r) => ({
                ...r,
                matchups: r.matchups.map((m) => ({ ...m })),
            }));
            const nextRoundIdx = currentRound + 1;
            if (next[nextRoundIdx] && winners.length >= 2) {
                next[nextRoundIdx].matchups[0] = {
                    slotA: winners[0] ? { ...winners[0] } : null,
                    slotB: winners[1] ? { ...winners[1] } : null,
                    responseA: null,
                    responseB: null,
                    vote: null,
                };
            }
            return next;
        });

        setCurrentRound((prev) => {
            const next = prev + 1;
            currentRoundRef.current = next;
            return next;
        });
        setPhase("next_round_ready");
    }, [rounds, currentRound]);

    const handleRunNextRound = useCallback(() => {
        if (phase !== "next_round_ready") return;
        runRound(currentRound);
    }, [phase, currentRound, runRound]);

    const handleStopAll = useCallback(() => {
        for (const [, ctrl] of abortMapRef.current) {
            ctrl.abort();
        }
        abortMapRef.current.clear();

        // Put all active reply pills with unfinished messages into "choose replacement model" state
        setRounds((prev) => {
            const next = prev.map((r) => ({
                ...r,
                matchups: r.matchups.map((m) => {
                    const mu = { ...m };
                    if (mu.responseA && !mu.responseA.done) {
                        mu.slotA = null;
                        mu.responseA = null;
                    }
                    if (mu.responseB && !mu.responseB.done) {
                        mu.slotB = null;
                        mu.responseB = null;
                    }
                    return mu;
                }),
            }));
            return next;
        });

        setRunningModels(new Set());
        setPhase("voting");
    }, []);

    const handleReset = useCallback(() => {
        for (const [, ctrl] of abortMapRef.current) {
            ctrl.abort();
        }
        abortMapRef.current.clear();
        setGroup1Models([]);
        setGroup2Models([]);
        setPrompt("");
        setActivePromptId(null);
        setRounds([]);
        setCurrentRound(0);
        setPhase("setup");
        setRunningModels(new Set());
        setWinnerModal(null);
        setSavedPrompt("");
        setDisabledModels(new Set());
        try {
            localStorage.removeItem("arenaGroup1Models");
            localStorage.removeItem("arenaGroup2Models");
            localStorage.removeItem("arenaPrompt");
            localStorage.removeItem("arenaActivePromptId");
        } catch { /* ignore */ }
    }, []);

    const handleRetrySlot = useCallback(
        (roundIdx: number, matchupIdx: number, slotKey: "A" | "B") => {
            const round = rounds[roundIdx];
            if (!round) return;
            const mu = round.matchups[matchupIdx];
            const slot = slotKey === "A" ? mu.slotA : mu.slotB;
            if (!slot) return;

            setRounds((prev) => {
                const next = prev.map((r) => ({
                    ...r,
                    matchups: r.matchups.map((m) => ({ ...m })),
                }));
                const respKey = slotKey === "A" ? "responseA" : "responseB";
                if (next[roundIdx]?.matchups[matchupIdx]) {
                    next[roundIdx].matchups[matchupIdx][respKey] = {
                        model: slot.modelId,
                        rawContent: "",
                        content: "",
                        thinkingContent: "",
                        startTimeMs: Date.now(),
                        done: false,
                        error: null,
                        metrics: null,
                    };
                }
                return next;
            });
            setRunningModels((prev) => new Set(prev).add(slot.modelId));
            setPhase("running");

            streamModel(
                slot.modelId,
                slot.personaPrompt,
                savedPrompt,
                roundIdx,
                slotKey,
                matchupIdx,
            );
        },
        [rounds, savedPrompt, streamModel],
    );

    const handleSwapModel = useCallback(
        (
            roundIdx: number,
            matchupIdx: number,
            slotKey: "A" | "B",
            failedModelId: string,
        ) => {
            setDisabledModels((prev) => new Set(prev).add(failedModelId));

            setRounds((prev) => {
                const next = prev.map((r) => ({
                    ...r,
                    matchups: r.matchups.map((m) => ({ ...m })),
                }));
                const slotKeyStr = slotKey === "A" ? "slotA" : "slotB";
                const respKey = slotKey === "A" ? "responseA" : "responseB";
                if (next[roundIdx]?.matchups[matchupIdx]) {
                    next[roundIdx].matchups[matchupIdx][slotKeyStr] = null;
                    next[roundIdx].matchups[matchupIdx][respKey] = null;
                }
                return next;
            });
        },
        [],
    );

    const handleCancelSlot = useCallback(
        (
            roundIdx: number,
            matchupIdx: number,
            slotKey: "A" | "B",
            modelId: string,
        ) => {
            // Abort the specific request
            const ctrl = abortMapRef.current.get(modelId);
            if (ctrl) {
                ctrl.abort();
                abortMapRef.current.delete(modelId);
            }
            setRunningModels((prev) => {
                const next = new Set(prev);
                next.delete(modelId);
                return next;
            });

            // Put the pill into "choose replacement model" state
            const slotKeyStr = slotKey === "A" ? "slotA" : "slotB";
            const respKey = slotKey === "A" ? "responseA" : "responseB";
            setRounds((prev) => {
                const next = prev.map((r) => ({
                    ...r,
                    matchups: r.matchups.map((m) => ({ ...m })),
                }));
                if (next[roundIdx]?.matchups[matchupIdx]) {
                    next[roundIdx].matchups[matchupIdx][slotKeyStr] = null;
                    next[roundIdx].matchups[matchupIdx][respKey] = null;
                }
                return next;
            });
        },
        [],
    );

    const handleSwapComplete = useCallback(
        (
            roundIdx: number,
            matchupIdx: number,
            slotKey: "A" | "B",
            newModelId: string,
        ) => {
            setRounds((prev) => {
                const next = prev.map((r) => ({
                    ...r,
                    matchups: r.matchups.map((m) => ({ ...m })),
                }));
                const slotKeyStr = slotKey === "A" ? "slotA" : "slotB";
                const respKey = slotKey === "A" ? "responseA" : "responseB";
                if (next[roundIdx]?.matchups[matchupIdx]) {
                    next[roundIdx].matchups[matchupIdx][slotKeyStr] = {
                        modelId: newModelId,
                        personaId: null,
                        personaPrompt: "",
                    };
                    next[roundIdx].matchups[matchupIdx][respKey] = {
                        model: newModelId,
                        rawContent: "",
                        content: "",
                        thinkingContent: "",
                        startTimeMs: Date.now(),
                        done: false,
                        error: null,
                        metrics: null,
                    };
                }
                return next;
            });
            setRunningModels((prev) => new Set(prev).add(newModelId));
            setPhase("running");

            streamModel(
                newModelId,
                "",
                savedPrompt,
                roundIdx,
                slotKey,
                matchupIdx,
            );
        },
        [savedPrompt, streamModel],
    );

    const handlePersonaChange = useCallback(
        (
            roundIdx: number,
            matchupIdx: number,
            slot: "A" | "B",
            personaId: string | null,
            personaPrompt: string,
        ) => {
            setRounds((prev) => {
                const next = prev.map((r) => ({
                    ...r,
                    matchups: r.matchups.map((m) => ({ ...m })),
                }));
                const mu = next[roundIdx]?.matchups[matchupIdx];
                if (mu) {
                    const slotKey = slot === "A" ? "slotA" : "slotB";
                    if (mu[slotKey]) {
                        mu[slotKey] = {
                            ...mu[slotKey]!,
                            personaId,
                            personaPrompt,
                        };
                    }
                }
                return next;
            });
        },
        [],
    );

    const isRunning = runningModels.size > 0;

    const allCurrentRoundVoted = useMemo(() => {
        const round = rounds[currentRound];
        if (!round) return false;
        return round.matchups.every((mu) => mu.vote !== null);
    }, [rounds, currentRound]);

    const buttonLabel = useMemo(() => {
        if (isRunning) return "Stop";
        if (phase === "setup") return "Run Arena";
        if (phase === "voting" && allCurrentRoundVoted) {
            const isLastRound = currentRound >= rounds.length - 1;
            return isLastRound ? "Confirm Winner" : null;
        }
        return null;
    }, [isRunning, phase, currentRound, allCurrentRoundVoted, rounds.length]);

    const showResponseGrid = phase !== "setup";

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
                        Bracket tournament — models compete head-to-head
                    </p>
                </div>
                {phase !== "setup" && (
                    <button
                        onClick={handleReset}
                        className="ui-btn ui-btn-secondary flex items-center gap-2"
                    >
                        <RotateCcw size={16} />
                        Reset
                    </button>
                )}
            </div>

            {/* Controls */}
            <div className="ui-card p-4">
                <div className="flex items-center justify-between">
                    <span className="text-sm font-semibold text-(--text-primary)">Controls</span>
                    <div className="flex items-center gap-1">
                        {phase !== "setup" && (
                            <button
                                onClick={() => setPendingReset(true)}
                                className="p-1.5 rounded-md transition-all cursor-pointer text-red-500 hover:drop-shadow-[0_0_6px_var(--color-red-500,red)]"
                                title="Reset arena"
                            >
                                <RotateCcw size={14} />
                            </button>
                        )}
                        <button
                            onClick={() => setArenaCollapsed((c) => !c)}
                            className="p-1.5 rounded-md transition-all cursor-pointer text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[0_0_6px_var(--accent)]"
                            title={arenaCollapsed ? "Expand controls" : "Collapse controls"}
                        >
                            {arenaCollapsed ? <ChevronsUpDown size={14} /> : <ChevronsDownUp size={14} />}
                        </button>
                    </div>
                </div>
                <div
                    className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${
                        arenaCollapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"
                    }`}
                >
                    <div className="overflow-hidden">
                        <div className="space-y-4 pt-4">
                {phase === "setup" && (
                    <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
                        <div>
                            <label className="text-sm text-(--text-secondary) mb-2 block">
                                Match 1 ({group1Models.length}/2)
                            </label>
                            <ModelPicker
                                models={enabledModels}
                                selected={group1Models}
                                onChange={setGroup1Models}
                                multi={true}
                                maxSelections={2}
                                providers={providerData}
                                align="left"
                            />
                            {group1Models.length > 0 &&
                                group1Models.length < 2 && (
                                    <p className="text-xs text-amber-400 mt-2">
                                        Pick exactly 2 models.
                                    </p>
                                )}
                            {new Set(group1Models).size !==
                                group1Models.length &&
                                group1Models.length > 0 && (
                                    <p className="text-xs text-amber-400 mt-2">
                                        No duplicate models.
                                    </p>
                                )}
                        </div>
                        <div
                            className={`transition-opacity duration-300 ${
                                group1Models.length < 2
                                    ? "opacity-30 pointer-events-none select-none"
                                    : "opacity-100"
                            }`}
                        >
                            <label className="text-sm text-(--text-secondary) mb-2 block">
                                Match 2 ({group2Models.length}/2){" "}
                                <span className="text-(--text-tertiary)">
                                    — optional, adds a final round
                                </span>
                            </label>
                            <ModelPicker
                                models={enabledModels}
                                selected={group2Models}
                                onChange={setGroup2Models}
                                multi={true}
                                maxSelections={2}
                                providers={providerData}
                                align="right"
                            />
                            {group2Models.length > 0 &&
                                group2Models.length < 2 && (
                                    <p className="text-xs text-amber-400 mt-2">
                                        Pick exactly 2 or leave empty for a
                                        single match.
                                    </p>
                                )}
                            {crossDuplicates && (
                                <p className="text-xs text-amber-400 mt-2">
                                    Models can't appear in both matches.
                                </p>
                            )}
                        </div>
                    </div>
                )}

                {/* Bracket Grid */}
                {rounds.length > 0 && (
                    <div className="space-y-3">
                        <div className="flex flex-col gap-3">
                            {rounds.map((round, roundIdx) => {
                                if (
                                    phase !== "setup" &&
                                    roundIdx < currentRound
                                )
                                    return null;
                                const isLastRound =
                                    roundIdx === rounds.length - 1 &&
                                    rounds.length > 1;
                                return (
                                    <div
                                        key={roundIdx}
                                        className={`flex items-center gap-2 transition-opacity duration-500 ${
                                            roundIdx > currentRound + 1 ||
                                            (roundIdx > currentRound &&
                                                phase === "voting")
                                                ? "opacity-30"
                                                : roundIdx > currentRound
                                                  ? "opacity-50"
                                                  : "opacity-100"
                                        }`}
                                    >
                                        <div className="text-xs text-(--text-tertiary) font-medium uppercase tracking-wider whitespace-nowrap">
                                            {isLastRound
                                                ? "Final Round"
                                                : `Round ${roundIdx + 1}`}
                                        </div>
                                        <div className="flex items-center gap-2">
                                            {round.matchups.map(
                                                (mu, matchupIdx) => (
                                                    <div
                                                        key={matchupIdx}
                                                        className="flex items-center gap-2"
                                                    >
                                                        <MatchupCard
                                                            slot={mu.slotA}
                                                            slotKey="A"
                                                            roundIdx={roundIdx}
                                                            matchupIdx={
                                                                matchupIdx
                                                            }
                                                            vote={mu.vote}
                                                            response={
                                                                mu.responseA
                                                            }
                                                            isRunning={
                                                                isRunning
                                                            }
                                                            phase={phase}
                                                            onPersonaChange={
                                                                handlePersonaChange
                                                            }
                                                            onVote={handleVote}
                                                        />
                                                        <span className="text-(--accent) font-bold text-xs px-1">
                                                            VS
                                                        </span>
                                                        <MatchupCard
                                                            slot={mu.slotB}
                                                            slotKey="B"
                                                            roundIdx={roundIdx}
                                                            matchupIdx={
                                                                matchupIdx
                                                            }
                                                            vote={mu.vote}
                                                            response={
                                                                mu.responseB
                                                            }
                                                            isRunning={
                                                                isRunning
                                                            }
                                                            phase={phase}
                                                            onPersonaChange={
                                                                handlePersonaChange
                                                            }
                                                            onVote={handleVote}
                                                        />
                                                    </div>
                                                ),
                                            )}
                                        </div>
                                    </div>
                                );
                            })}
                            {phase === "finished" && (
                                <div className="flex items-center gap-2">
                                    <div className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-amber-500/10 border border-amber-500/30">
                                        <Trophy
                                            size={14}
                                            className="text-amber-400"
                                        />
                                        <span className="text-xs font-bold text-amber-300">
                                            {winnerModal?.winner}
                                        </span>
                                    </div>
                                </div>
                            )}
                        </div>
                    </div>
                )}

                {/* Action Button */}
                {buttonLabel && (
                    <div className="flex items-center gap-3 flex-wrap">
                        <button
                            onClick={
                                isRunning
                                    ? handleStopAll
                                    : phase === "voting" && allCurrentRoundVoted
                                      ? handleAdvanceRound
                                      : phase === "next_round_ready"
                                        ? handleRunNextRound
                                        : handleRunArena
                            }
                            disabled={
                                isRunning
                                    ? false
                                    : phase === "setup"
                                      ? !canRun
                                      : phase === "voting"
                                        ? !allCurrentRoundVoted
                                        : false
                            }
                            title={
                                phase === "setup" && !canRun
                                    ? disabledReason
                                    : phase === "voting" &&
                                        !allCurrentRoundVoted
                                      ? disabledReason
                                      : undefined
                            }
                            className={`ui-btn flex items-center gap-2 ${
                                isRunning
                                    ? "ui-btn-danger"
                                    : phase === "voting" &&
                                        allCurrentRoundVoted &&
                                        currentRound >= rounds.length - 1
                                      ? "bg-amber-600 hover:bg-amber-500 text-white"
                                      : "ui-btn-primary"
                            } disabled:opacity-40`}
                        >
                            {isRunning ? (
                                <>
                                    <X size={16} />
                                    {buttonLabel}
                                </>
                            ) : phase === "voting" &&
                              allCurrentRoundVoted &&
                              currentRound >= rounds.length - 1 ? (
                                <>
                                    <Trophy size={16} />
                                    {buttonLabel}
                                </>
                            ) : (
                                <>
                                    <Play size={16} />
                                    {buttonLabel}
                                </>
                            )}
                        </button>
                    </div>
                )}

                {/* Prompt */}
                <div>
                    <label className="text-sm text-(--text-secondary) mb-2 block">
                        Prompt
                    </label>
                    {phase === "setup" && (
                        <PresetBar
                            items={ARENA_PROMPTS}
                            activeId={activePromptId}
                            onSelect={handlePromptPresetSelect}
                            onCustom={handleCustomPrompt}
                        />
                    )}
                    <textarea
                        ref={promptRef}
                        value={
                            phase === "setup" || phase === "finished"
                                ? prompt
                                : savedPrompt
                        }
                        onChange={(e) => {
                            handlePromptChange(e.target.value);
                            if (!e.target.value) {
                                e.target.style.height = "auto";
                            } else if (
                                e.target.scrollHeight > e.target.clientHeight
                            ) {
                                e.target.style.height =
                                    e.target.scrollHeight + "px";
                            }
                        }}
                        placeholder="Enter your prompt..."
                        autoFocus
                        rows={1}
                        maxLength={10000}
                        className="ui-input w-full resize-y max-h-32 min-h-11 overflow-y-auto mt-1.5"
                        disabled={phase !== "setup" && phase !== "finished"}
                    />
                </div>
                        </div>
                    </div>
                </div>
            </div>

            {/* Response Grid */}
            {showResponseGrid &&
                rounds.map((round, roundIdx) => {
                    const hasAnyResponse = round.matchups.some(
                        (mu) =>
                            mu.responseA ||
                            mu.responseB ||
                            mu.slotA === null ||
                            mu.slotB === null,
                    );
                    if (!hasAnyResponse) return null;
                    return (
                        <div key={roundIdx}>
                            <div className="text-xs text-(--text-tertiary) font-medium uppercase tracking-wider mb-2">
                                Round {roundIdx + 1}
                            </div>
                            <div
                                className={`space-y-4 transition-opacity duration-500 ${
                                    roundIdx <= currentRound
                                        ? "opacity-100"
                                        : "opacity-20"
                                }`}
                            >
                                {round.matchups.map((mu, matchupIdx) => {
                                    return (
                                        <div
                                            key={matchupIdx}
                                            className="rounded-xl border border-(--border-subtle) bg-(--surface)/50 p-4"
                                        >
                                            {round.matchups.length > 1 && (
                                                <div className="text-xs text-(--text-tertiary) font-medium uppercase tracking-wider mb-3">
                                                    Match {matchupIdx + 1}
                                                </div>
                                            )}
                                            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                                                {mu.slotA === null &&
                                                roundIdx === currentRound ? (
                                                    <SwapPicker
                                                        enabledModels={
                                                            enabledModels
                                                        }
                                                        disabledModels={
                                                            disabledModels
                                                        }
                                                        alreadyUsed={[
                                                            ...round.matchups.flatMap(
                                                                (m, mi) => {
                                                                    if (
                                                                        mi ===
                                                                        matchupIdx
                                                                    )
                                                                        return [];
                                                                    const ids: string[] =
                                                                        [];
                                                                    if (m.slotA)
                                                                        ids.push(
                                                                            m
                                                                                .slotA
                                                                                .modelId,
                                                                        );
                                                                    if (m.slotB)
                                                                        ids.push(
                                                                            m
                                                                                .slotB
                                                                                .modelId,
                                                                        );
                                                                    return ids;
                                                                },
                                                            ),
                                                            ...(mu.slotB
                                                                ? [
                                                                      mu.slotB
                                                                          .modelId,
                                                                  ]
                                                                : []),
                                                        ]}
                                                        onSelect={(modelId) =>
                                                            handleSwapComplete(
                                                                roundIdx,
                                                                matchupIdx,
                                                                "A",
                                                                modelId,
                                                            )
                                                        }
                                                    />
                                                ) : (
                                                    mu.responseA && (
                                                        <ResponseCard
                                                            response={
                                                                mu.responseA
                                                            }
                                                            vote={mu.vote}
                                                            slotKey="A"
                                                            roundIdx={roundIdx}
                                                            matchupIdx={
                                                                matchupIdx
                                                            }
                                                            onVote={handleVote}
                                                            onRetry={
                                                                handleRetrySlot
                                                            }
                                                            onSwapModel={
                                                                handleSwapModel
                                                            }
                                                            onCancelSlot={
                                                                handleCancelSlot
                                                            }
                                                            showVote={
                                                                roundIdx <=
                                                                    currentRound &&
                                                                mu.responseA
                                                                    .done &&
                                                                (!mu.responseB ||
                                                                    mu.responseB
                                                                        .done)
                                                            }
                                                        />
                                                    )
                                                )}
                                                {mu.slotB === null &&
                                                roundIdx === currentRound ? (
                                                    <SwapPicker
                                                        enabledModels={
                                                            enabledModels
                                                        }
                                                        disabledModels={
                                                            disabledModels
                                                        }
                                                        alreadyUsed={[
                                                            ...round.matchups.flatMap(
                                                                (m, mi) => {
                                                                    if (
                                                                        mi ===
                                                                        matchupIdx
                                                                    )
                                                                        return [];
                                                                    const ids: string[] =
                                                                        [];
                                                                    if (m.slotA)
                                                                        ids.push(
                                                                            m
                                                                                .slotA
                                                                                .modelId,
                                                                        );
                                                                    if (m.slotB)
                                                                        ids.push(
                                                                            m
                                                                                .slotB
                                                                                .modelId,
                                                                        );
                                                                    return ids;
                                                                },
                                                            ),
                                                            ...(mu.slotA
                                                                ? [
                                                                      mu.slotA
                                                                          .modelId,
                                                                  ]
                                                                : []),
                                                        ]}
                                                        onSelect={(modelId) =>
                                                            handleSwapComplete(
                                                                roundIdx,
                                                                matchupIdx,
                                                                "B",
                                                                modelId,
                                                            )
                                                        }
                                                    />
                                                ) : (
                                                    mu.responseB && (
                                                        <ResponseCard
                                                            response={
                                                                mu.responseB
                                                            }
                                                            vote={mu.vote}
                                                            slotKey="B"
                                                            roundIdx={roundIdx}
                                                            matchupIdx={
                                                                matchupIdx
                                                            }
                                                            onVote={handleVote}
                                                            onRetry={
                                                                handleRetrySlot
                                                            }
                                                            onSwapModel={
                                                                handleSwapModel
                                                            }
                                                            onCancelSlot={
                                                                handleCancelSlot
                                                            }
                                                            showVote={
                                                                roundIdx <=
                                                                    currentRound &&
                                                                mu.responseB
                                                                    .done &&
                                                                (!mu.responseA ||
                                                                    mu.responseA
                                                                        .done)
                                                            }
                                                        />
                                                    )
                                                )}
                                            </div>
                                        </div>
                                    );
                                })}
                            </div>
                        </div>
                    );
                })}

            {/* Prompt Preset Overwrite Confirmation */}
            {pendingPrompt && (
                <ConfirmDialog
                    title={
                        pendingPrompt.id === "__custom__"
                            ? "Switch to Custom"
                            : "Overwrite Prompt"
                    }
                    fields={["Prompt"]}
                    onConfirm={() => {
                        if (pendingPrompt.id === "__custom__") {
                            setPrompt("");
                            setActivePromptId(null);
                        } else {
                            setPrompt(pendingPrompt.prompt);
                            setActivePromptId(pendingPrompt.id);
                        }
                        setPendingPrompt(null);
                        autoExpandTextarea(promptRef);
                    }}
                    onCancel={() => setPendingPrompt(null)}
                />
            )}

            {pendingReset && (
                <ConfirmDialog
                    title="Reset Arena"
                    message="This will clear the bracket and all results. Continue?"
                    fields={[]}
                    confirmLabel="Reset"
                    onConfirm={() => {
                        setGroup1Models([]);
                        setGroup2Models([]);
                        setPrompt("");
                        setSavedPrompt("");
                        setActivePromptId(null);
                        setRounds([]);
                        setCurrentRound(0);
                        setPhase("setup");
                        setRunningModels(new Set());
                        setWinnerModal(null);
                        setDisabledModels(new Set());
                        setPendingReset(false);
                        try {
                            localStorage.removeItem("arenaGroup1Models");
                            localStorage.removeItem("arenaGroup2Models");
                            localStorage.removeItem("arenaPrompt");
                            localStorage.removeItem("arenaActivePromptId");
                        } catch { /* ignore */ }
                        toast("Arena reset", "info");
                    }}
                    onCancel={() => setPendingReset(false)}
                />
            )}

            {/* Winner Modal */}
            {winnerModal && (
                <WinnerSummaryModal
                    winner={winnerModal.winner}
                    rounds={winnerModal.rounds}
                    onClose={() => setWinnerModal(null)}
                />
            )}
        </div>
    );
}

interface MatchupCardProps {
    slot: MatchupSlot | null;
    slotKey: "A" | "B";
    roundIdx: number;
    matchupIdx: number;
    vote: "A" | "B" | null;
    response: ArenaResponse | null;
    isRunning: boolean;
    phase: BracketPhase;
    onPersonaChange: (
        roundIdx: number,
        matchupIdx: number,
        slot: "A" | "B",
        personaId: string | null,
        personaPrompt: string,
    ) => void;
    onVote: (roundIdx: number, matchupIdx: number, vote: "A" | "B") => void;
}

function VoteThumb({
    size,
    isWinner,
    animating,
}: {
    size: number;
    isWinner: boolean;
    animating: boolean;
}) {
    const [showUp, setShowUp] = useState(false);

    useEffect(() => {
        if (!animating) return;
        const id = setInterval(() => setShowUp((v) => !v), 1200);
        return () => clearInterval(id);
    }, [animating]);

    if (isWinner) return <ThumbsUp size={size} />;
    if (!animating) return <ThumbsDown size={size} />;

    return (
        <span
            className="relative inline-flex"
            style={{ width: size, height: size }}
        >
            <ThumbsDown
                size={size}
                className={`absolute inset-0 transition-opacity duration-500 ${showUp ? "opacity-0" : "opacity-100"}`}
            />
            <ThumbsUp
                size={size}
                className={`absolute inset-0 transition-opacity duration-500 ${showUp ? "opacity-100" : "opacity-0"}`}
            />
        </span>
    );
}

function MatchupCard({
    slot,
    slotKey,
    roundIdx,
    matchupIdx,
    vote,
    response,
    isRunning,
    phase,
    onPersonaChange,
    onVote,
}: MatchupCardProps) {
    const [pendingPersona, setPendingPersona] = useState<
        import("../data/presets").PersonaPreset | null
    >(null);

    if (!slot) {
        return (
            <div className="px-4 py-2 rounded-lg bg-(--surface) border border-dashed border-(--border-subtle) text-xs text-(--text-tertiary) min-w-35 text-center">
                TBD
            </div>
        );
    }

    const isVotingPhase =
        (phase === "voting" ||
            phase === "next_round_ready" ||
            phase === "finished") &&
        response?.done;
    const isWinner = vote === slotKey;
    const isLoser = vote !== null && vote !== slotKey;

    return (
        <div
            className={`px-3 py-2 rounded-lg border min-w-40 transition-all ${
                isWinner
                    ? "bg-green-500/10 border-green-500/40 shadow-[0_0_8px_rgba(34,197,94,0.15)]"
                    : isLoser
                      ? "bg-red-500/5 border-red-500/20 opacity-60"
                      : "bg-(--surface) border-(--border-subtle)"
            }`}
        >
            <div className="flex items-center gap-2 mb-1">
                <Bot size={12} className="text-(--accent)" />
                <span className="text-xs font-medium text-(--text-primary) truncate">
                    {slot.modelId.split("/").pop()}
                </span>
                {isRunning && !response?.done && (
                    <span className="w-1.5 h-1.5 rounded-full bg-(--accent) animate-pulse shrink-0" />
                )}
                {response?.done && !response?.error && (
                    <CheckCircle2
                        size={12}
                        className="text-green-400 shrink-0"
                    />
                )}
                {response?.error && (
                    <AlertCircle size={12} className="text-red-400 shrink-0" />
                )}
            </div>

            {phase === "setup" && roundIdx === 0 && (
                <div className="mt-1">
                    <PresetBar
                        items={CHAT_PERSONAS}
                        activeId={slot.personaId}
                        onSelect={(persona) => {
                            if (
                                slot.personaPrompt.trim() &&
                                slot.personaId === null
                            ) {
                                setPendingPersona(persona);
                                return;
                            }
                            onPersonaChange(
                                roundIdx,
                                matchupIdx,
                                slotKey,
                                persona.id,
                                persona.systemPrompt,
                            );
                        }}
                        onCustom={() => {
                            if (slot.personaId !== null) {
                                setPendingPersona({
                                    id: "__custom__",
                                    icon: "✏️",
                                    label: "Custom",
                                    systemPrompt: "",
                                } as import("../data/presets").PersonaPreset);
                                return;
                            }
                        }}
                        customLabel="✏️"
                    />
                </div>
            )}

            {isVotingPhase && (
                <button
                    onClick={vote === null ? () => onVote(roundIdx, matchupIdx, slotKey) : undefined}
                    disabled={vote !== null}
                    className={`mt-1 flex items-center gap-1 text-xs transition-all ${
                        vote === null
                            ? "cursor-pointer text-(--text-tertiary) hover:text-(--text-secondary)"
                            : "cursor-default"
                    } ${
                        isWinner
                            ? "text-green-400"
                            : ""
                    }`}
                >
                    <VoteThumb
                        size={14}
                        isWinner={isWinner}
                        animating={!isWinner && !isLoser}
                    />
                </button>
            )}

            {pendingPersona && (
                <ConfirmDialog
                    title={
                        pendingPersona.id === "__custom__"
                            ? "Switch to Custom"
                            : "Overwrite Persona"
                    }
                    fields={["Persona"]}
                    onConfirm={() => {
                        if (pendingPersona.id === "__custom__") {
                            onPersonaChange(
                                roundIdx,
                                matchupIdx,
                                slotKey,
                                null,
                                "",
                            );
                        } else {
                            onPersonaChange(
                                roundIdx,
                                matchupIdx,
                                slotKey,
                                pendingPersona.id,
                                pendingPersona.systemPrompt,
                            );
                        }
                        setPendingPersona(null);
                    }}
                    onCancel={() => setPendingPersona(null)}
                />
            )}
        </div>
    );
}

interface ResponseCardProps {
    response: ArenaResponse;
    vote: "A" | "B" | null;
    slotKey: "A" | "B";
    roundIdx: number;
    matchupIdx: number;
    onVote: (roundIdx: number, matchupIdx: number, vote: "A" | "B") => void;
    onRetry: (roundIdx: number, matchupIdx: number, slotKey: "A" | "B") => void;
    onSwapModel: (
        roundIdx: number,
        matchupIdx: number,
        slotKey: "A" | "B",
        failedModelId: string,
    ) => void;
    onCancelSlot: (
        roundIdx: number,
        matchupIdx: number,
        slotKey: "A" | "B",
        modelId: string,
    ) => void;
    showVote: boolean;
}

function ResponseCard({
    response,
    vote,
    slotKey,
    roundIdx,
    matchupIdx,
    onVote,
    onRetry,
    onSwapModel,
    onCancelSlot,
    showVote,
}: ResponseCardProps) {
    const [elapsed, setElapsed] = useState(0);
    const isWinner = vote === slotKey;
    const isLoser = vote !== null && vote !== slotKey;
    const hasThinking = response.thinkingContent.length > 0;
    const isStreaming = !response.done;

    useEffect(() => {
        if (response.done || response.startTimeMs === 0) return;
        const tick = () =>
            setElapsed(Math.round((Date.now() - response.startTimeMs) / 1000));
        tick();
        const id = setInterval(tick, 1000);
        return () => clearInterval(id);
    }, [response.done, response.startTimeMs]);

    return (
        <div
            className={`ui-card flex flex-col transition-all ${
                isWinner
                    ? "ring-1 ring-green-500/40 shadow-[0_0_12px_rgba(34,197,94,0.1)]"
                    : isLoser
                      ? "opacity-60"
                      : ""
            }`}
        >
            <div className="flex items-center justify-between px-4 pt-4 pb-2 border-b border-(--border-subtle)">
                <div className="flex items-center gap-2 min-w-0">
                    <Bot size={14} className="text-(--accent) shrink-0" />
                    <span className="text-sm font-medium text-(--text-primary) truncate">
                        {response.model.split("/").pop()}
                    </span>
                    {response.error && response.done && (
                        <button
                            onClick={() =>
                                onSwapModel(
                                    roundIdx,
                                    matchupIdx,
                                    slotKey,
                                    response.model,
                                )
                            }
                            className="shrink-0 text-red-400 hover:text-red-300 transition-colors cursor-pointer"
                            title="Swap model"
                        >
                            <X size={14} />
                        </button>
                    )}
                </div>
                <div className="flex items-center gap-2 shrink-0">
                    {response.done && !response.error && (
                        <CheckCircle2 size={14} className="text-green-400" />
                    )}
                    {response.error && (
                        <>
                            <AlertCircle size={14} className="text-red-400" />
                            <button
                                onClick={() =>
                                    onRetry(roundIdx, matchupIdx, slotKey)
                                }
                                className="text-(--text-tertiary) hover:text-(--text-primary) transition-colors cursor-pointer"
                                title="Retry"
                            >
                                <RefreshCw size={14} />
                            </button>
                        </>
                    )}
                    {isStreaming && (
                        <>
                            <button
                                onClick={() =>
                                    onCancelSlot(
                                        roundIdx,
                                        matchupIdx,
                                        slotKey,
                                        response.model,
                                    )
                                }
                                className="text-red-400/60 hover:text-red-400 transition-colors cursor-pointer"
                                title="Cancel"
                            >
                                <CircleStop size={14} />
                            </button>
                            <span className="text-[11px] text-(--text-tertiary) tabular-nums">
                                {elapsed}s
                            </span>
                            <span className="w-1.5 h-1.5 rounded-full bg-(--accent) animate-pulse" />
                        </>
                    )}
                    {isWinner && (
                        <Trophy size={14} className="text-amber-400" />
                    )}
                </div>
            </div>

            <div className="px-4 pb-4 pt-0 overflow-y-auto h-85">
                {response.error ? (
                    <div className="text-red-400 text-xs">{response.error}</div>
                ) : (
                    <>
                        {hasThinking && (
                            <ThinkingBlock
                                thinking={response.thinkingContent}
                                isStreaming={isStreaming}
                            />
                        )}
                        {response.content ? (
                            <div className="prose prose-invert prose-xs max-w-none text-(--text-primary) text-xs [&_p]:my-1 [&_ul]:my-1 [&_ol]:my-1 [&_li]:my-0.5 [&_h1]:text-sm [&_h2]:text-xs [&_h3]:text-xs [&_code]:text-(--accent) [&_code]:bg-(--surface-hover) [&_code]:px-1 [&_code]:py-0.5 [&_code]:rounded [&_code]:text-[11px] [&_pre]:bg-(--surface-hover) [&_pre]:rounded-lg [&_pre]:p-3 [&_pre]:overflow-x-auto [&_pre]:my-2 [&_pre]:text-[11px] [&_blockquote]:border-l-2 [&_blockquote]:border-(--accent)/40 [&_blockquote]:pl-3 [&_blockquote]:text-(--text-secondary) [&_strong]:text-white [&_em]:text-(--text-secondary) [&_a]:text-(--accent) [&_a]:underline [&_hr]:border-(--border-subtle) [&_table]:text-[10px] [&_th]:px-1.5 [&_th]:py-0.5 [&_td]:px-1.5 [&_td]:py-0.5 [&_th]:border [&_th]:border-(--border-subtle) [&_td]:border [&_td]:border-(--border-subtle)">
                                <ReactMarkdown remarkPlugins={[remarkGfm]}>
                                    {response.content}
                                </ReactMarkdown>
                            </div>
                        ) : !hasThinking && isStreaming ? (
                            <div className="text-(--text-tertiary) text-xs flex items-center gap-2">
                                <span className="w-1.5 h-1.5 rounded-full bg-(--accent) animate-pulse" />
                                Waiting...
                            </div>
                        ) : null}
                    </>
                )}
            </div>

            <div className="px-4 py-2 border-t border-(--border-subtle) flex items-center justify-between">
                {response.metrics && (
                    <div className="flex items-center gap-3 text-[11px] text-(--text-tertiary)">
                        <span className="flex items-center gap-1">
                            <Clock size={10} />
                            {formatDuration(response.metrics.durationMs)}
                        </span>
                        {response.metrics.tokensPerSecond !== null && (
                            <span className="flex items-center gap-1">
                                <Zap size={10} />
                                {response.metrics.tokensPerSecond.toFixed(
                                    1,
                                )}{" "}
                                tok/s
                            </span>
                        )}
                        {response.metrics.promptTokens +
                            response.metrics.completionTokens >
                            0 && (
                            <span>
                                {response.metrics.promptTokens +
                                    response.metrics.completionTokens}{" "}
                                tok
                            </span>
                        )}
                    </div>
                )}
                {showVote && (
                    <button
                        onClick={vote === null ? () => onVote(roundIdx, matchupIdx, slotKey) : undefined}
                        disabled={vote !== null}
                        className={`flex items-center gap-1 transition-all ${
                            vote === null
                                ? "cursor-pointer"
                                : "cursor-default"
                        } ${
                            isWinner
                                ? "text-green-400 hover:text-green-300"
                                : "text-(--text-tertiary) hover:text-(--text-secondary)"
                        }`}
                    >
                        <VoteThumb
                            size={18}
                            isWinner={isWinner}
                            animating={vote === null}
                        />
                    </button>
                )}
            </div>
        </div>
    );
}

interface SwapPickerProps {
    enabledModels: Array<{
        provider_name: string;
        model_id: string;
        display_name?: string;
        enabled?: boolean;
    }>;
    disabledModels: Set<string>;
    alreadyUsed: string[];
    onSelect: (modelId: string) => void;
}

function SwapPicker({
    enabledModels,
    disabledModels,
    alreadyUsed,
    onSelect,
}: SwapPickerProps) {
    const [search, setSearch] = useState("");

    const proxyModelID = (providerName: string, modelId: string) =>
        providerName.replace(/ /g, "-") + "/" + modelId;

    const available = useMemo(() => {
        const usedSet = new Set(alreadyUsed);
        return enabledModels.filter((m) => {
            const id = proxyModelID(m.provider_name, m.model_id);
            if (disabledModels.has(id)) return false;
            if (usedSet.has(id)) return false;
            if (search.trim()) {
                const q = search.trim().toLowerCase();
                const name = (m.display_name || m.model_id).toLowerCase();
                return name.includes(q) || m.model_id.toLowerCase().includes(q);
            }
            return true;
        });
    }, [enabledModels, disabledModels, alreadyUsed, search]);

    return (
        <div className="ui-card flex flex-col items-center justify-center min-h-50">
            <p className="text-xs text-amber-400 mb-2">
                Pick a replacement model
            </p>
            <FilterInput
                value={search}
                onChange={setSearch}
                placeholder="Search models..."
                className="max-w-xs mb-2"
            />
            <div className="flex flex-wrap gap-1 max-h-32 overflow-y-auto w-full justify-center px-2">
                {available.map((m) => {
                    const id = proxyModelID(m.provider_name, m.model_id);
                    return (
                        <button
                            key={id}
                            onClick={() => onSelect(id)}
                            className="px-2 py-0.5 text-[11px] rounded-md border bg-(--surface-hover) border-(--border-subtle) text-(--text-secondary) hover:text-(--text-primary) hover:border-(--accent)/40 transition-colors cursor-pointer"
                        >
                            {m.display_name || m.model_id}
                        </button>
                    );
                })}
                {available.length === 0 && (
                    <span className="text-xs text-(--text-muted)">
                        No models available
                    </span>
                )}
            </div>
        </div>
    );
}

interface WinnerSummaryModalProps {
    winner: string;
    rounds: BracketRound[];
    onClose: () => void;
}

function WinnerSummaryModal({
    winner,
    rounds,
    onClose,
}: WinnerSummaryModalProps) {
    return (
        <div
            role="dialog"
            aria-modal="true"
            className="fixed inset-0 flex items-center justify-center z-60"
        >
            <button
                type="button"
                className="absolute inset-0 bg-black/60 cursor-default"
                onClick={onClose}
                aria-label="Close dialog"
            />
            <div className="relative ui-card p-6 w-full max-w-lg max-h-[80vh] overflow-y-auto">
                <button
                    type="button"
                    onClick={onClose}
                    className="absolute top-4 right-4 text-(--text-secondary) hover:text-(--text-primary) transition-all cursor-default text-xl leading-none hover:drop-shadow-[0_0_8px_var(--accent)]"
                    aria-label="Close"
                >
                    <X size={20} />
                </button>

                <div className="flex items-center gap-3 mb-4">
                    <Trophy size={28} className="text-amber-400" />
                    <h2 className="text-xl font-bold text-white">
                        Match Complete
                    </h2>
                </div>

                <div className="flex items-center gap-2 px-4 py-3 rounded-lg bg-amber-500/10 border border-amber-500/30 mb-4">
                    <Trophy size={18} className="text-amber-400" />
                    <span className="text-sm font-bold text-amber-300">
                        {winner.split("/").pop()}
                    </span>
                    <span className="text-sm text-amber-400/70">wins!</span>
                </div>

                <div className="space-y-3">
                    {rounds.map((round, roundIdx) => (
                        <div key={roundIdx}>
                            <div className="text-xs text-(--text-tertiary) font-medium uppercase tracking-wider mb-1">
                                Round {roundIdx + 1}
                            </div>
                            {round.matchups.map((mu, mi) => (
                                <div
                                    key={mi}
                                    className="flex items-center gap-2 text-sm"
                                >
                                    <span
                                        className={
                                            mu.vote === "A"
                                                ? "text-green-400 font-medium"
                                                : "text-(--text-secondary)"
                                        }
                                    >
                                        {mu.slotA?.modelId.split("/").pop() ??
                                            "TBD"}
                                    </span>
                                    <span className="text-(--text-tertiary)">
                                        vs
                                    </span>
                                    <span
                                        className={
                                            mu.vote === "B"
                                                ? "text-green-400 font-medium"
                                                : "text-(--text-secondary)"
                                        }
                                    >
                                        {mu.slotB?.modelId.split("/").pop() ??
                                            "TBD"}
                                    </span>
                                    {mu.vote && (
                                        <span className="text-xs text-(--accent)">
                                            ←{" "}
                                            {(mu.vote === "A"
                                                ? mu.slotA
                                                : mu.slotB
                                            )?.modelId
                                                .split("/")
                                                .pop()}{" "}
                                            wins
                                        </span>
                                    )}
                                </div>
                            ))}
                        </div>
                    ))}
                </div>

                <div className="flex justify-end mt-4">
                    <button
                        type="button"
                        onClick={onClose}
                        className="ui-btn ui-btn-primary"
                    >
                        Close
                    </button>
                </div>
            </div>
        </div>
    );
}
