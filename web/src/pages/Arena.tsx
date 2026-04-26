import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import { useState, useRef, useCallback, useMemo, useEffect } from "react";
import {
    Swords,
    Play,
    X,
    Bot,
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
    Copy,
    Columns3,
} from "lucide-react";
import { extractThinking } from "../utils/thinking";
import { ModelReplyCard } from "../components/ModelReplyCard";
import { useToast } from "../context/ToastContext";
import { useStorage } from "../context/StorageContext";
import { ModelPicker } from "../components/ModelPicker";
import { PresetBar } from "../components/PresetBar";
import { PersonaPicker } from "../components/PersonaPicker";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { FilterInput } from "../components/FilterInput";
import { ARENA_PROMPTS, CHAT_PERSONAS } from "../data/presets";

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

type ArenaMode = "competition" | "compare";

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

    const [arenaMode, setArenaMode] = useState<ArenaMode>(() => {
        try {
            if (localStorage.getItem("persistArena") === "true") {
                const raw = localStorage.getItem("arenaState");
                if (raw)
                    return (
                        (JSON.parse(raw).arenaMode as ArenaMode) ??
                        "competition"
                    );
            }
        } catch {
            /* ignore */
        }
        return "competition";
    });

    const [compareModels, setCompareModels] = useState<string[]>(() => {
        try {
            if (localStorage.getItem("persistArena") === "true") {
                const raw = localStorage.getItem("arenaState");
                if (raw) {
                    const s = JSON.parse(raw);
                    return s.compareModels ?? [];
                }
            }
        } catch {
            /* ignore */
        }
        return [];
    });

    const [group1Models, setGroup1Models] = useState<string[]>(() => {
        try {
            if (localStorage.getItem("persistArena") === "true") {
                const raw = localStorage.getItem("arenaState");
                if (raw) {
                    const s = JSON.parse(raw);
                    return s.group1Models ?? [];
                }
            }
        } catch {
            /* ignore */
        }
        return [];
    });
    const [group2Models, setGroup2Models] = useState<string[]>(() => {
        try {
            if (localStorage.getItem("persistArena") === "true") {
                const raw = localStorage.getItem("arenaState");
                if (raw) {
                    const s = JSON.parse(raw);
                    return s.group2Models ?? [];
                }
            }
        } catch {
            /* ignore */
        }
        return [];
    });

    const [activePromptId, setActivePromptId] = useState<string | null>(() => {
        try {
            if (localStorage.getItem("persistArena") === "true") {
                const v = localStorage.getItem("arenaActivePromptId");
                return v || null;
            }
        } catch {
            /* ignore */
        }
        return null;
    });
    const [pendingPrompt, setPendingPrompt] = useState<
        import("../data/presets").ArenaPromptPreset | null
    >(null);
    const [prompt, setPrompt] = useState<string>(() => {
        try {
            if (localStorage.getItem("persistArena") === "true") {
                return localStorage.getItem("arenaPrompt") ?? "";
            }
        } catch {
            /* ignore */
        }
        return "";
    });
    const [savedPrompt, setSavedPrompt] = useState<string>("");

    const [comparePersonaId, setComparePersonaId] = useState<string | null>(
        () => {
            try {
                if (localStorage.getItem("persistArena") === "true") {
                    const v = localStorage.getItem("arenaComparePersonaId");
                    return v || null;
                }
            } catch {
                /* ignore */
            }
            return null;
        },
    );
    const [comparePersonaPrompt, setComparePersonaPrompt] = useState<string>(
        () => {
            try {
                if (localStorage.getItem("persistArena") === "true") {
                    return (
                        localStorage.getItem("arenaComparePersonaPrompt") ?? ""
                    );
                }
            } catch {
                /* ignore */
            }
            return "";
        },
    );

    const [rounds, setRounds] = useState<BracketRound[]>(() => {
        try {
            if (localStorage.getItem("persistArena") === "true") {
                const raw = localStorage.getItem("arenaState");
                if (raw) {
                    const s = JSON.parse(raw);
                    return s.rounds ?? [];
                }
            }
        } catch {
            /* ignore */
        }
        return [];
    });
    const [currentRound, setCurrentRound] = useState(() => {
        try {
            if (localStorage.getItem("persistArena") === "true") {
                const raw = localStorage.getItem("arenaState");
                if (raw) {
                    const s = JSON.parse(raw);
                    return s.currentRound ?? 0;
                }
            }
        } catch {
            /* ignore */
        }
        return 0;
    });
    const [phase, setPhase] = useState<BracketPhase>(() => {
        try {
            if (localStorage.getItem("persistArena") === "true") {
                const raw = localStorage.getItem("arenaState");
                if (raw) {
                    const s = JSON.parse(raw);
                    return s.phase ?? "setup";
                }
            }
        } catch {
            /* ignore */
        }
        return "setup";
    });
    const [runningModels, setRunningModels] = useState<Set<string>>(new Set());
    const [winnerModal, setWinnerModal] = useState<WinnerModal | null>(null);
    const [disabledModels, setDisabledModels] = useState<Set<string>>(
        new Set(),
    );
    const [arenaCollapsed, setArenaCollapsed] = useState<boolean>(() => {
        try {
            if (localStorage.getItem("persistArena") === "true") {
                const raw = localStorage.getItem("arenaState");
                if (raw) {
                    const s = JSON.parse(raw);
                    return s.arenaCollapsed ?? false;
                }
            }
        } catch {
            /* ignore */
        }
        return false;
    });
    const [pendingReset, setPendingReset] = useState(false);

    useEffect(() => {
        if (!persistArena) return;
        try {
            localStorage.setItem("arenaPrompt", prompt);
        } catch {
            /* quota exceeded */
        }
    }, [prompt, persistArena]);

    useEffect(() => {
        if (!persistArena) return;
        try {
            localStorage.setItem("arenaActivePromptId", activePromptId ?? "");
        } catch {
            /* quota exceeded */
        }
    }, [activePromptId, persistArena]);

    useEffect(() => {
        if (!persistArena) return;
        try {
            localStorage.setItem(
                "arenaComparePersonaId",
                comparePersonaId ?? "",
            );
        } catch {
            /* quota exceeded */
        }
    }, [comparePersonaId, persistArena]);

    useEffect(() => {
        if (!persistArena) return;
        try {
            localStorage.setItem(
                "arenaComparePersonaPrompt",
                comparePersonaPrompt,
            );
        } catch {
            /* quota exceeded */
        }
    }, [comparePersonaPrompt, persistArena]);

    useEffect(() => {
        if (!persistArena) return;
        try {
            localStorage.setItem(
                "arenaState",
                JSON.stringify({
                    arenaMode,
                    compareModels,
                    group1Models,
                    group2Models,
                    rounds,
                    currentRound,
                    phase,
                    arenaCollapsed,
                }),
            );
        } catch {
            /* quota exceeded */
        }
    }, [
        arenaMode,
        compareModels,
        group1Models,
        group2Models,
        rounds,
        currentRound,
        phase,
        arenaCollapsed,
        persistArena,
    ]);

    const arenaModeRef = useRef<ArenaMode>(arenaMode);
    const abortMapRef = useRef<Map<string, AbortController>>(new Map());
    const currentRoundRef = useRef(0);
    const roundsLengthRef = useRef(0);
    const roundsRef = useRef<BracketRound[]>([]);
    const promptRef = useRef<HTMLTextAreaElement>(null);

    useEffect(() => {
        arenaModeRef.current = arenaMode;
    }, [arenaMode]);

    useEffect(() => {
        roundsRef.current = rounds;
    }, [rounds]);

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
        if (arenaMode !== "competition") return false;
        if (group2Models.length === 0) return false;
        return group1Models.some((m) => group2Models.includes(m));
    }, [arenaMode, group1Models, group2Models]);

    const canRun = useMemo(() => {
        if (phase !== "setup" && phase !== "next_round_ready") return false;
        if (!prompt.trim()) return false;
        if (arenaMode === "compare") {
            if (compareModels.length < 2) return false;
            if (new Set(compareModels).size !== compareModels.length)
                return false;
            return true;
        }
        // Competition mode
        if (group1Models.length !== 2) return false;
        if (group2Models.length !== 0 && group2Models.length !== 2)
            return false;
        if (crossDuplicates) return false;
        if (new Set(group1Models).size !== group1Models.length) return false;
        if (
            group2Models.length > 0 &&
            new Set(group2Models).size !== group2Models.length
        )
            return false;
        return true;
    }, [
        phase,
        arenaMode,
        compareModels,
        group1Models,
        group2Models,
        prompt,
        crossDuplicates,
    ]);

    const disabledReason = useMemo(() => {
        if (phase === "setup") {
            if (arenaMode === "compare") {
                if (compareModels.length === 0)
                    return "Select at least 2 models";
                if (compareModels.length === 1)
                    return "Pick at least 1 more model";
                if (new Set(compareModels).size !== compareModels.length)
                    return "No duplicate models";
                if (!prompt.trim()) return "Enter a prompt";
                return "";
            }
            // Competition mode
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
    }, [
        phase,
        arenaMode,
        compareModels,
        group1Models,
        group2Models,
        prompt,
        crossDuplicates,
    ]);

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

    const buildCompareRound = useCallback(
        (
            modelIds: string[],
            personaId: string | null = null,
            personaPrompt: string = "",
        ): BracketRound[] => {
            return [
                {
                    matchups: modelIds.map((id) => ({
                        slotA: {
                            modelId: id,
                            personaId,
                            personaPrompt,
                        } as MatchupSlot,
                        slotB: null,
                        responseA: null,
                        responseB: null,
                        vote: null,
                    })),
                },
            ];
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
                            setPhase(
                                arenaModeRef.current === "compare"
                                    ? "finished"
                                    : "voting",
                            );
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
            const round = roundsRef.current[roundIdx];
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
        [savedPrompt, prompt, streamModel],
    );

    const handleRunArena = useCallback(() => {
        if (!canRun) return;

        const currentPrompt = prompt.trim();
        setSavedPrompt(currentPrompt);

        const initialRounds =
            arenaMode === "compare"
                ? buildCompareRound(
                      compareModels,
                      comparePersonaId,
                      comparePersonaPrompt,
                  )
                : buildInitialRounds(group1Models, group2Models);
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
        arenaMode,
        compareModels,
        comparePersonaId,
        comparePersonaPrompt,
        group1Models,
        group2Models,
        buildInitialRounds,
        buildCompareRound,
        streamModel,
    ]);

    const handleVote = useCallback(
        (roundIdx: number, matchupIdx: number, vote: "A" | "B") => {
            let shouldAdvance = false;
            let advanceRoundIdx = -1;
            let shouldDeclareWinner = false;

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
                    next[roundIdx].matchups.every((m) => m.vote !== null)
                ) {
                    if (roundIdx < next.length - 1) {
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
                    } else {
                        shouldDeclareWinner = true;
                    }
                }

                roundsRef.current = next;
                return next;
            });

            if (shouldAdvance) {
                const nextRI = advanceRoundIdx + 1;
                setCurrentRound(nextRI);
                currentRoundRef.current = nextRI;
                setPhase("running");
                queueMicrotask(() => runRound(nextRI));
            }

            if (shouldDeclareWinner) {
                const finalRound = roundsRef.current[roundIdx];
                const finalMu = finalRound?.matchups[0];
                const winner =
                    finalMu?.vote === "A"
                        ? finalMu.slotA?.modelId
                        : finalMu.slotB?.modelId;
                if (winner) {
                    setWinnerModal({ winner, rounds: roundsRef.current });
                    setPhase("finished");
                }
            }
        },
        [runRound],
    );

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

    const buttonLabel = useMemo(() => {
        if (isRunning) return "Stop";
        if (phase === "setup") return "Run Arena";
        return null;
    }, [isRunning, phase]);

    const showResponseGrid = phase !== "setup";

    return (
        <div className="flex flex-col gap-6 min-h-[calc(100vh-64px)]">
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
                        <div className="flex items-center gap-1 ml-3">
                            <button
                                onClick={() => {
                                    if (phase === "setup")
                                        setArenaMode("competition");
                                }}
                                className={`px-3 py-1 rounded-md text-xs font-medium transition-all ${
                                    arenaMode === "competition"
                                        ? "bg-(--accent)/20 text-(--accent) border border-(--accent)/40 cursor-default"
                                        : phase === "setup"
                                          ? "text-(--text-tertiary) hover:text-(--text-secondary) border border-transparent cursor-pointer"
                                          : "text-(--text-tertiary) border border-transparent cursor-default"
                                }`}
                            >
                                <Swords
                                    size={12}
                                    className="inline mr-1 -mt-0.5"
                                />
                                Competition
                            </button>
                            <button
                                onClick={() => {
                                    if (phase === "setup")
                                        setArenaMode("compare");
                                }}
                                className={`px-3 py-1 rounded-md text-xs font-medium transition-all ${
                                    arenaMode === "compare"
                                        ? "bg-(--accent)/20 text-(--accent) border border-(--accent)/40 cursor-default"
                                        : phase === "setup"
                                          ? "text-(--text-tertiary) hover:text-(--text-secondary) border border-transparent cursor-pointer"
                                          : "text-(--text-tertiary) border border-transparent cursor-default"
                                }`}
                            >
                                <Columns3
                                    size={12}
                                    className="inline mr-1 -mt-0.5"
                                />
                                Compare
                            </button>
                        </div>
                    </div>
                    <p className="text-gray-400">
                        {arenaMode === "competition"
                            ? "Bracket tournament — models compete head-to-head"
                            : "Side-by-side — compare model outputs on the same prompt"}
                    </p>
                </div>
            </div>

            {/* Controls */}
            <div className="ui-card p-4">
                <div className="flex items-center justify-between">
                    <span className="text-sm font-semibold text-(--text-primary)">
                        Controls
                    </span>
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
                            title={
                                arenaCollapsed
                                    ? "Expand controls"
                                    : "Collapse controls"
                            }
                        >
                            {arenaCollapsed ? (
                                <ChevronsUpDown size={14} />
                            ) : (
                                <ChevronsDownUp size={14} />
                            )}
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
                            {phase === "setup" &&
                                arenaMode === "competition" && (
                                    <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
                                        <div>
                                            <label className="text-sm text-(--text-secondary) mb-2 block">
                                                Match 1 ({group1Models.length}
                                                /2)
                                            </label>
                                            <ModelPicker
                                                models={enabledModels}
                                                selected={group1Models}
                                                onChange={setGroup1Models}
                                                multi={true}
                                                maxSelections={2}
                                                providers={providerData}
                                                align="left"
                                                exclude={group2Models}
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
                                                Match 2 ({group2Models.length}
                                                /2){" "}
                                                <span className="text-(--text-tertiary)">
                                                    — optional, adds a final
                                                    round
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
                                                exclude={group1Models}
                                            />
                                            {group2Models.length > 0 &&
                                                group2Models.length < 2 && (
                                                    <p className="text-xs text-amber-400 mt-2">
                                                        Pick exactly 2 or leave
                                                        empty for a single
                                                        match.
                                                    </p>
                                                )}
                                        </div>
                                    </div>
                                )}
                            {phase === "setup" && arenaMode === "compare" && (
                                <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
                                    <div>
                                        <label className="text-sm text-(--text-secondary) mb-2 block">
                                            Models ({compareModels.length}/6)
                                        </label>
                                        <ModelPicker
                                            models={enabledModels}
                                            selected={compareModels}
                                            onChange={setCompareModels}
                                            multi={true}
                                            maxSelections={6}
                                            providers={providerData}
                                            align="left"
                                        />
                                        {compareModels.length > 0 &&
                                            compareModels.length < 2 && (
                                                <p className="text-xs text-amber-400 mt-2">
                                                    Pick at least 2 models.
                                                </p>
                                            )}
                                        {new Set(compareModels).size !==
                                            compareModels.length &&
                                            compareModels.length > 0 && (
                                                <p className="text-xs text-amber-400 mt-2">
                                                    No duplicate models.
                                                </p>
                                            )}
                                    </div>
                                    <div>
                                        <PersonaPicker
                                            personas={CHAT_PERSONAS}
                                            activePersonaId={comparePersonaId}
                                            systemPrompt={comparePersonaPrompt}
                                            onActivePersonaChange={
                                                setComparePersonaId
                                            }
                                            onSystemPromptChange={
                                                setComparePersonaPrompt
                                            }
                                            textareaPlaceholder="Optional system prompt applied to all models…"
                                            wrap
                                        />
                                    </div>
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
                                        phase === "setup" ||
                                        phase === "finished"
                                            ? prompt
                                            : savedPrompt
                                    }
                                    onChange={(e) => {
                                        handlePromptChange(e.target.value);
                                        if (!e.target.value) {
                                            e.target.style.height = "auto";
                                        } else if (
                                            e.target.scrollHeight >
                                            e.target.clientHeight
                                        ) {
                                            e.target.style.height =
                                                e.target.scrollHeight + "px";
                                        }
                                    }}
                                    placeholder="Enter your prompt…"
                                    autoFocus
                                    rows={1}
                                    maxLength={10000}
                                    className="ui-input w-full resize-y max-h-32 min-h-11 overflow-y-auto mt-1.5"
                                    disabled={
                                        phase !== "setup" &&
                                        phase !== "finished"
                                    }
                                />
                            </div>
                        </div>
                    </div>
                </div>
            </div>

            {/* Bracket + Run Bar */}
            <div className="ui-card p-4 shrink-0">
                <div className="flex items-center gap-4 flex-wrap">
                    {/* Bracket Pills */}
                    {rounds.length > 0 && (
                        <div className="flex flex-col gap-2 flex-1 min-w-0">
                            {rounds.map((round, roundIdx) => {
                                if (
                                    phase !== "setup" &&
                                    roundIdx < currentRound
                                )
                                    return null;
                                if (
                                    phase === "finished" &&
                                    roundIdx < rounds.length - 1
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
                                                ? "Final"
                                                : `R${roundIdx + 1}`}
                                        </div>
                                        <div className="flex items-center gap-2 flex-wrap">
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
                                                        {mu.slotB !== null && (
                                                            <>
                                                                <span className="text-(--accent) font-bold text-xs px-1">
                                                                    VS
                                                                </span>
                                                                <MatchupCard
                                                                    slot={
                                                                        mu.slotB
                                                                    }
                                                                    slotKey="B"
                                                                    roundIdx={
                                                                        roundIdx
                                                                    }
                                                                    matchupIdx={
                                                                        matchupIdx
                                                                    }
                                                                    vote={
                                                                        mu.vote
                                                                    }
                                                                    response={
                                                                        mu.responseB
                                                                    }
                                                                    isRunning={
                                                                        isRunning
                                                                    }
                                                                    phase={
                                                                        phase
                                                                    }
                                                                    onPersonaChange={
                                                                        handlePersonaChange
                                                                    }
                                                                    onVote={
                                                                        handleVote
                                                                    }
                                                                />
                                                            </>
                                                        )}
                                                    </div>
                                                ),
                                            )}
                                        </div>
                                    </div>
                                );
                            })}
                        </div>
                    )}

                    {/* Run Button */}
                    {buttonLabel && (
                        <button
                            onClick={isRunning ? handleStopAll : handleRunArena}
                            disabled={phase === "setup" && !canRun}
                            title={
                                phase === "setup" && !canRun
                                    ? disabledReason
                                    : undefined
                            }
                            className={`ui-btn flex items-center gap-2 shrink-0 ${
                                isRunning ? "ui-btn-danger" : "ui-btn-primary"
                            } disabled:opacity-40`}
                        >
                            {isRunning ? (
                                <>
                                    <X size={16} />
                                    {buttonLabel}
                                </>
                            ) : (
                                <>
                                    <Play size={16} />
                                    {buttonLabel}
                                </>
                            )}
                        </button>
                    )}
                </div>
            </div>

            {/* Response Grid */}
            {showResponseGrid &&
                rounds.map((round, roundIdx) => {
                    const hasActualResponse = round.matchups.some(
                        (mu) => mu.responseA || mu.responseB,
                    );
                    if (!hasActualResponse) return null;
                    // Once a later round has responses, skip earlier rounds
                    const laterRoundHasResponses = rounds.some(
                        (r, ri) =>
                            ri > roundIdx &&
                            r.matchups.some(
                                (mu) => mu.responseA || mu.responseB,
                            ),
                    );
                    if (laterRoundHasResponses) return null;
                    const isLastRound =
                        roundIdx === rounds.length - 1 && rounds.length > 1;
                    const isCompare =
                        arenaMode === "compare" &&
                        round.matchups.every((m) => m.slotB === null);
                    return (
                        <div key={roundIdx}>
                            <div className="text-xs text-(--text-tertiary) font-medium uppercase tracking-wider mb-2">
                                {isCompare
                                    ? "Responses"
                                    : isLastRound
                                      ? "Final Round"
                                      : `Round ${roundIdx + 1}`}
                            </div>
                            <div
                                className={`${
                                    isCompare
                                        ? "grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4"
                                        : "space-y-4"
                                } transition-opacity duration-500 ${
                                    roundIdx <= currentRound
                                        ? "opacity-100"
                                        : "opacity-20"
                                }`}
                            >
                                {round.matchups.map((mu, matchupIdx) => {
                                    // Compare mode: flat grid of individual cards
                                    if (isCompare) {
                                        return (
                                            <div
                                                key={matchupIdx}
                                                className="rounded-xl border border-(--border-subtle) bg-(--surface)/50 p-4"
                                            >
                                                {mu.slotA === null &&
                                                roundIdx === currentRound ? (
                                                    <SwapPicker
                                                        enabledModels={
                                                            enabledModels
                                                        }
                                                        disabledModels={
                                                            disabledModels
                                                        }
                                                        alreadyUsed={round.matchups.flatMap(
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
                                                                        m.slotA
                                                                            .modelId,
                                                                    );
                                                                return ids;
                                                            },
                                                        )}
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
                                                            showVote={false}
                                                        />
                                                    )
                                                )}
                                            </div>
                                        );
                                    }

                                    // Competition mode: A-vs-B pairs
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
                        setCompareModels([]);
                        setGroup1Models([]);
                        setGroup2Models([]);
                        setPrompt("");
                        setSavedPrompt("");
                        setActivePromptId(null);
                        setComparePersonaId(null);
                        setComparePersonaPrompt("");
                        setRounds([]);
                        setCurrentRound(0);
                        setPhase("setup");
                        setRunningModels(new Set());
                        setWinnerModal(null);
                        setDisabledModels(new Set());
                        setPendingReset(false);
                        try {
                            localStorage.removeItem("arenaPrompt");
                            localStorage.removeItem("arenaActivePromptId");
                            localStorage.removeItem("arenaComparePersonaId");
                            localStorage.removeItem(
                                "arenaComparePersonaPrompt",
                            );
                        } catch {
                            /* ignore */
                        }
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
                {response?.error && (
                    <AlertCircle size={12} className="text-red-400 shrink-0" />
                )}
                {phase === "finished" && isWinner && (
                    <Trophy size={14} className="text-amber-400 shrink-0" />
                )}
                {isVotingPhase && phase !== "finished" && (
                    <button
                        onClick={
                            vote === null
                                ? () => onVote(roundIdx, matchupIdx, slotKey)
                                : undefined
                        }
                        disabled={vote !== null}
                        className={`flex items-center text-xs transition-all ${
                            vote === null
                                ? "cursor-pointer text-(--text-tertiary) hover:text-(--text-secondary)"
                                : "cursor-default"
                        } ${isWinner ? "text-green-400" : ""}`}
                    >
                        <VoteThumb
                            size={14}
                            isWinner={isWinner}
                            animating={!isWinner && !isLoser}
                        />
                    </button>
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
    const { toast } = useToast();
    const isWinner = vote === slotKey;
    const isLoser = vote !== null && vote !== slotKey;

    return (
        <ModelReplyCard
            model={response.model}
            content={response.content}
            thinkingContent={response.thinkingContent}
            error={response.error}
            metrics={response.metrics}
            isStreaming={!response.done}
            startTimeMs={response.startTimeMs}
            isWinner={isWinner}
            isLoser={isLoser}
            afterModel={
                response.error && response.done ? (
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
                ) : null
            }
            headerEnd={
                <>
                    {response.done && !response.error && (
                        <>
                            <CheckCircle2
                                size={14}
                                className="text-green-400"
                            />
                            <button
                                onClick={() =>
                                    onRetry(roundIdx, matchupIdx, slotKey)
                                }
                                className="text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[0_0_6px_var(--accent)] transition-all cursor-pointer"
                                title="Re-roll"
                            >
                                <RefreshCw size={14} />
                            </button>
                        </>
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
                    {!response.done && (
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
                    )}
                    {isWinner && (
                        <Trophy size={14} className="text-amber-400" />
                    )}
                </>
            }
            footerEnd={
                <div className="flex items-center gap-2">
                    {response.done && !response.error && response.content && (
                        <button
                            className="inline-flex items-center cursor-pointer transition-all text-(--accent) hover:drop-shadow-[0_0_4px_var(--accent)]"
                            onClick={() => {
                                navigator.clipboard
                                    .writeText(response.content)
                                    .then(() =>
                                        toast("Copied to clipboard", "info"),
                                    )
                                    .catch(() =>
                                        toast("Failed to copy", "error"),
                                    );
                            }}
                            title="Copy"
                        >
                            <Copy size={12} />
                        </button>
                    )}
                    {showVote && (
                        <button
                            onClick={
                                vote === null
                                    ? () =>
                                          onVote(roundIdx, matchupIdx, slotKey)
                                    : undefined
                            }
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
            }
            className="flex flex-col"
            headerClassName="px-4 pt-4 pb-2 border-b border-(--border-subtle)"
            bodyClassName="px-4 pb-4 pt-0 overflow-y-auto h-85"
            footerClassName="px-4 py-2 border-t border-(--border-subtle)"
        />
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
        <div className="flex flex-col items-center flex-1 min-h-0">
            <p className="text-xs text-amber-400 mb-2 shrink-0">
                Pick a replacement model
            </p>
            <FilterInput
                value={search}
                onChange={setSearch}
                placeholder="Search models…"
                className="w-full max-w-xs mb-2 shrink-0"
            />
            <div className="flex flex-wrap gap-1 overflow-y-auto w-full justify-center content-start px-2 flex-1 min-h-0">
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
