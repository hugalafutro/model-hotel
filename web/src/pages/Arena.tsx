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
    Eraser,
    RefreshCw,
    ChevronsUpDown,
    ChevronsDownUp,
    CircleStop,
    Copy,
    Columns3,
    GitCompare,
    Settings,
    History,
} from "lucide-react";
import {
    extractThinking,
    sanitizeDelta,
    shouldReExtract,
} from "../utils/thinking";
import { ModelReplyCard } from "../components/ModelReplyCard";
import { ModelDetailModal } from "../components/ModelDetailPanel";
import { proxyModelID } from "../utils/model";
import type { Model, GenerationParams } from "../api/types";
import { useToast } from "../context/ToastContext";
import { useStorage } from "../context/StorageContext";
import {
    useSidebarMode,
    type ArenaSubMode,
} from "../context/SidebarModeContext";
import { ModelPicker } from "../components/ModelPicker";
import { PresetBar } from "../components/PresetBar";
import { PromptPicker } from "../components/PromptPicker";
import { PersonaPicker } from "../components/PersonaPicker";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { FilterInput } from "../components/FilterInput";
import { ArenaHistoryModal } from "../components/ArenaHistoryModal";
import {
    saveCompetitionToHistory,
    saveCompareToHistory,
    getArenaHistoryEnabled,
} from "../utils/arenaHistory";
import { ARENA_PROMPTS, CHAT_PERSONAS } from "../data/presets";

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

interface ArenaResponse {
    model: string;
    rawContent: string;
    content: string;
    thinkingContent: string;
    startTimeMs: number;
    done: boolean;
    error: string | null;
    metrics: {
        charsPerSecond: number | null;
        durationMs: number;
        promptTokens: number;
        completionTokens: number;
    } | null;
}

interface MatchupSlot {
    modelId: string;
    personaId: string | null;
    personaPrompt: string;
    params?: GenerationParams;
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
    const { arenaSubMode, setArenaSubMode } = useSidebarMode();

    const arenaMode = arenaSubMode;
    const setArenaMode = setArenaSubMode;

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

    const [bracketModels, setBracketModels] = useState<string[]>(() => {
        try {
            if (localStorage.getItem("persistArena") === "true") {
                const raw = localStorage.getItem("arenaState");
                if (raw) {
                    const s = JSON.parse(raw);
                    if (s.bracketModels) return s.bracketModels;
                    const g1: string[] = s.group1Models ?? [];
                    const g2: string[] = s.group2Models ?? [];
                    if (g1.length > 0 || g2.length > 0) return [...g1, ...g2];
                }
            }
        } catch {
            /* ignore */
        }
        return [];
    });

    const [competitionActivePromptId, setCompetitionActivePromptId] = useState<
        string | null
    >(() => {
        try {
            if (localStorage.getItem("persistArena") === "true") {
                const v = localStorage.getItem(
                    "arenaCompetitionActivePromptId",
                );
                return v || null;
            }
        } catch {
            /* ignore */
        }
        return null;
    });
    const [compareActivePromptId, setCompareActivePromptId] = useState<
        string | null
    >(() => {
        try {
            if (localStorage.getItem("persistArena") === "true") {
                const v = localStorage.getItem("arenaCompareActivePromptId");
                return v || null;
            }
        } catch {
            /* ignore */
        }
        return null;
    });

    const [competitionPrompt, setCompetitionPrompt] = useState<string>(() => {
        try {
            if (localStorage.getItem("persistArena") === "true") {
                return localStorage.getItem("arenaCompetitionPrompt") ?? "";
            }
        } catch {
            /* ignore */
        }
        return "";
    });
    const [comparePrompt, setComparePrompt] = useState<string>(() => {
        try {
            if (localStorage.getItem("persistArena") === "true") {
                return localStorage.getItem("arenaComparePrompt") ?? "";
            }
        } catch {
            /* ignore */
        }
        return "";
    });

    // Derived: pick the active mode's prompt / preset id
    const prompt =
        arenaMode === "competition" ? competitionPrompt : comparePrompt;
    const activePromptId =
        arenaMode === "competition"
            ? competitionActivePromptId
            : compareActivePromptId;

    // Ref for mode-aware setters (declared early, synced via effect below)
    const arenaModeRef = useRef<ArenaSubMode>(arenaMode);

    // Smart setters that dispatch to the active mode
    const setPrompt = useCallback((v: string) => {
        if (arenaModeRef.current === "competition") setCompetitionPrompt(v);
        else setComparePrompt(v);
    }, []);
    const setActivePromptId = useCallback((v: string | null) => {
        if (arenaModeRef.current === "competition")
            setCompetitionActivePromptId(v);
        else setCompareActivePromptId(v);
    }, []);
    const [savedPrompt, setSavedPrompt] = useState<string>(() => {
        try {
            if (localStorage.getItem("persistArena") === "true") {
                const raw = localStorage.getItem("arenaState");
                if (raw) {
                    const s = JSON.parse(raw);
                    return s.savedPrompt ?? "";
                }
            }
        } catch {
            /* ignore */
        }
        return "";
    });

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
    const [pendingFullReset, setPendingFullReset] = useState(false);
    const [showHistoryModal, setShowHistoryModal] = useState(false);

    const [modelParams, setModelParams] = useState<
        Record<string, GenerationParams>
    >(() => {
        try {
            if (localStorage.getItem("persistArena") === "true") {
                const raw = localStorage.getItem("arenaState");
                if (raw) {
                    const s = JSON.parse(raw);
                    return s.modelParams ?? {};
                }
            }
        } catch {
            /* ignore */
        }
        return {};
    });

    const [paramEditorModel, setParamEditorModel] = useState<string | null>(
        null,
    );

    useEffect(() => {
        if (!persistArena) return;
        try {
            localStorage.setItem("arenaCompetitionPrompt", competitionPrompt);
        } catch {
            /* quota exceeded */
        }
    }, [competitionPrompt, persistArena]);

    useEffect(() => {
        if (!persistArena) return;
        try {
            localStorage.setItem("arenaComparePrompt", comparePrompt);
        } catch {
            /* quota exceeded */
        }
    }, [comparePrompt, persistArena]);

    useEffect(() => {
        if (!persistArena) return;
        try {
            localStorage.setItem(
                "arenaCompetitionActivePromptId",
                competitionActivePromptId ?? "",
            );
        } catch {
            /* quota exceeded */
        }
    }, [competitionActivePromptId, persistArena]);

    useEffect(() => {
        if (!persistArena) return;
        try {
            localStorage.setItem(
                "arenaCompareActivePromptId",
                compareActivePromptId ?? "",
            );
        } catch {
            /* quota exceeded */
        }
    }, [compareActivePromptId, persistArena]);

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
                    bracketModels,
                    rounds,
                    currentRound,
                    phase,
                    arenaCollapsed,
                    savedPrompt,
                    modelParams,
                }),
            );
        } catch {
            /* quota exceeded */
        }
    }, [
        arenaMode,
        compareModels,
        bracketModels,
        rounds,
        currentRound,
        phase,
        arenaCollapsed,
        savedPrompt,
        modelParams,
        persistArena,
    ]);

    const abortMapRef = useRef<Map<string, AbortController>>(new Map());
    const lastExtractLenRef = useRef<Map<string, number>>(new Map());
    const currentRoundRef = useRef(0);
    const roundsLengthRef = useRef(0);
    const roundsRef = useRef<BracketRound[]>([]);
    const activePromptIdRef = useRef<string | null>(null);
    const comparePersonaIdRef = useRef<string | null>(null);

    useEffect(() => {
        arenaModeRef.current = arenaMode;
    }, [arenaMode]);

    useEffect(() => {
        activePromptIdRef.current = activePromptId;
    }, [activePromptId]);

    useEffect(() => {
        comparePersonaIdRef.current = comparePersonaId;
    }, [comparePersonaId]);

    useEffect(() => {
        roundsRef.current = rounds;
    }, [rounds]);

    // Save compare history when phase transitions to "finished" in compare mode
    // (covers natural stream completion, not just manual stop)
    const compareHistorySavedRef = useRef(false);
    useEffect(() => {
        if (
            phase === "finished" &&
            arenaMode === "compare" &&
            getArenaHistoryEnabled() &&
            !compareHistorySavedRef.current
        ) {
            compareHistorySavedRef.current = true;
            const currentRounds = roundsRef.current;
            if (currentRounds.length > 0) {
                const round = currentRounds[0];
                const models: string[] = [];
                const responses: {
                    model: string;
                    content: string;
                    thinkingContent: string;
                    error: string | null;
                    metrics: {
                        charsPerSecond: number | null;
                        durationMs: number;
                        promptTokens: number;
                        completionTokens: number;
                    } | null;
                }[] = [];
                for (const mu of round.matchups) {
                    if (mu.slotA) {
                        models.push(mu.slotA.modelId);
                        if (mu.responseA && mu.responseA.done) {
                            responses.push({
                                model: mu.responseA.model,
                                content: mu.responseA.content,
                                thinkingContent: mu.responseA.thinkingContent,
                                error: mu.responseA.error,
                                metrics: mu.responseA.metrics,
                            });
                        }
                    }
                }
                if (responses.length > 0) {
                    saveCompareToHistory({
                        models,
                        responses,
                        promptPresetId: activePromptIdRef.current,
                        comparePersonaId: comparePersonaIdRef.current,
                    });
                }
            }
        }
        // Reset the saved flag when leaving finished phase
        if (phase !== "finished") {
            compareHistorySavedRef.current = false;
        }
    }, [phase, arenaMode]);

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

    const canRun = useMemo(() => {
        if (phase !== "setup" && phase !== "next_round_ready") return false;
        if (!prompt.trim()) return false;
        if (arenaMode === "compare") {
            if (compareModels.length < 2) return false;
            if (new Set(compareModels).size !== compareModels.length)
                return false;
            return true;
        }
        const validSizes = new Set([2, 4, 8]);
        if (!validSizes.has(bracketModels.length)) return false;
        if (new Set(bracketModels).size !== bracketModels.length) return false;
        return true;
    }, [phase, arenaMode, compareModels, bracketModels, prompt]);

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
            if (bracketModels.length === 0) return "Select 2, 4, or 8 models";
            if (bracketModels.length === 1) return "Pick at least 1 more model";
            if (new Set(bracketModels).size !== bracketModels.length)
                return "No duplicate models";
            if (![2, 4, 8].includes(bracketModels.length)) {
                const nextValid =
                    bracketModels.length < 2
                        ? 2
                        : bracketModels.length < 4
                          ? 4
                          : 8;
                return `Pick ${nextValid - bracketModels.length} more or remove to get ${nextValid}`;
            }
            if (!prompt.trim()) return "Enter a prompt";
        }
        if (phase === "voting") return "Vote on all matchups to continue";
        return "";
    }, [phase, arenaMode, compareModels, bracketModels, prompt]);

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
                            params: modelParams[id],
                        } as MatchupSlot,
                        slotB: null,
                        responseA: null,
                        responseB: null,
                        vote: null,
                    })),
                },
            ];
        },
        [modelParams],
    );

    const buildInitialRounds = useCallback(
        (models: string[]): BracketRound[] => {
            const makeSlot = (id: string): MatchupSlot => ({
                modelId: id,
                personaId: null,
                personaPrompt: "",
                params: modelParams[id],
            });

            const emptyMatchup = (): Matchup => ({
                slotA: null,
                slotB: null,
                responseA: null,
                responseB: null,
                vote: null,
            });

            const numRounds = Math.log2(models.length);
            const firstRoundMatchups: Matchup[] = [];
            for (let i = 0; i < models.length; i += 2) {
                firstRoundMatchups.push({
                    slotA: makeSlot(models[i]),
                    slotB: makeSlot(models[i + 1]),
                    responseA: null,
                    responseB: null,
                    vote: null,
                });
            }

            const bracketRounds: BracketRound[] = [
                { matchups: firstRoundMatchups },
            ];

            for (let r = 1; r < numRounds; r++) {
                const matchupCount = models.length / Math.pow(2, r + 1);
                bracketRounds.push({
                    matchups: Array.from({ length: matchupCount }, () =>
                        emptyMatchup(),
                    ),
                });
            }

            return bracketRounds;
        },
        [modelParams],
    );

    const handleRandomComparePersona = useCallback(() => {
        const available = CHAT_PERSONAS.filter(
            (p) => p.id !== comparePersonaId,
        );
        if (available.length === 0) return;
        const pick = available[Math.floor(Math.random() * available.length)];
        setComparePersonaId(pick.id);
        setComparePersonaPrompt(pick.systemPrompt);
    }, [comparePersonaId, setComparePersonaId, setComparePersonaPrompt]);

    const handleRandomBracketModel = useCallback(() => {
        const available = enabledModels.filter((m) => {
            const val = proxyModelID(m.provider_name, m.model_id);
            return !bracketModels.includes(val);
        });
        if (available.length === 0 || bracketModels.length >= 8) return;
        const pick = available[Math.floor(Math.random() * available.length)];
        const val = proxyModelID(pick.provider_name, pick.model_id);
        setBracketModels([...bracketModels, val]);
    }, [enabledModels, bracketModels, setBracketModels]);

    const handleRandomCompareModel = useCallback(() => {
        const available = enabledModels.filter((m) => {
            const val = proxyModelID(m.provider_name, m.model_id);
            return !compareModels.includes(val);
        });
        if (available.length === 0 || compareModels.length >= 6) return;
        const pick = available[Math.floor(Math.random() * available.length)];
        const val = proxyModelID(pick.provider_name, pick.model_id);
        setCompareModels([...compareModels, val]);
    }, [enabledModels, compareModels, setCompareModels]);

    const streamModel = useCallback(
        (
            model: string,
            personaPrompt: string,
            userPrompt: string,
            roundIdx: number,
            slotKey: "A" | "B",
            matchupIdx: number,
            slotParams?: GenerationParams,
        ) => {
            const abortCtrl = new AbortController();
            abortMapRef.current.set(model, abortCtrl);

            const run = async () => {
                const extractKey = `${roundIdx}-${matchupIdx}-${slotKey}`;
                lastExtractLenRef.current.delete(extractKey);
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
                        signal: abortCtrl.signal,
                        ...(slotParams && hasAnyParam(slotParams)
                            ? slotParams
                            : {}),
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

                        let streamDone = false;
                        for (const line of lines) {
                            if (!line.startsWith("data: ")) continue;
                            const data = line.slice(6);
                            if (data === "[DONE]") {
                                streamDone = true;
                                break;
                            }
                            try {
                                const chunk = JSON.parse(data);
                                const delta =
                                    chunk.choices?.[0]?.delta?.content;
                                if (delta) {
                                    const clean = sanitizeDelta(delta);
                                    charCount += clean.length;
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
                                                prev.rawContent + clean;
                                            const lastLen =
                                                lastExtractLenRef.current.get(
                                                    extractKey,
                                                ) ?? 0;
                                            const needsExtract =
                                                shouldReExtract(clean) ||
                                                newRaw.length - lastLen >= 50;
                                            let nextContent: string;
                                            let nextThinking: string;
                                            if (needsExtract) {
                                                const extracted =
                                                    extractThinking(newRaw);
                                                lastExtractLenRef.current.set(
                                                    extractKey,
                                                    newRaw.length,
                                                );
                                                nextContent = extracted.content;
                                                nextThinking =
                                                    extracted.thinking ||
                                                    prev.thinkingContent;
                                            } else {
                                                nextContent =
                                                    prev.content + clean;
                                                nextThinking =
                                                    prev.thinkingContent;
                                            }
                                            mu[respKey] = {
                                                ...prev,
                                                rawContent: newRaw,
                                                content: nextContent,
                                                thinkingContent: nextThinking,
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
                        if (streamDone) break;
                    }

                    const durationMs = performance.now() - startTime;
                    const charsPerSecond =
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
                                    charsPerSecond,
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
                                    charsPerSecond:
                                        charCount > 0
                                            ? charCount /
                                              ((performance.now() - startTime) /
                                                  1000)
                                            : null,
                                    durationMs: Math.round(
                                        performance.now() - startTime,
                                    ),
                                    promptTokens,
                                    completionTokens,
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
                        if (next.size === 0 && !abortCtrl.signal.aborted) {
                            setPhase(
                                arenaModeRef.current === "compare"
                                    ? "finished"
                                    : "voting",
                            );
                        }
                        return next;
                    });
                    lastExtractLenRef.current.delete(extractKey);
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
                        mu.slotA.params,
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
                        mu.slotB.params,
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
                : buildInitialRounds(bracketModels);
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
                    mu.slotA.params,
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
                    mu.slotB.params,
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
        bracketModels,
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
                        if (next[nextRoundIdx]) {
                            for (let i = 0; i < winners.length; i += 2) {
                                const matchupIdx = i / 2;
                                next[nextRoundIdx].matchups[matchupIdx] = {
                                    slotA: winners[i]
                                        ? { ...winners[i]! }
                                        : null,
                                    slotB: winners[i + 1]
                                        ? { ...winners[i + 1]! }
                                        : null,
                                    responseA: null,
                                    responseB: null,
                                    vote: null,
                                };
                            }
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
                    // Save competition to history (only preset prompts, never user text)
                    if (getArenaHistoryEnabled()) {
                        saveCompetitionToHistory({
                            rounds: roundsRef.current,
                            winner,
                            promptPresetId: activePromptIdRef.current,
                            comparePersonaId: null,
                        });
                    }
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

        // Mark partially streamed responses as done (preserve their content)
        setRounds((prev) => {
            const next = prev.map((r) => ({
                ...r,
                matchups: r.matchups.map((m) => {
                    const mu = { ...m };
                    if (mu.responseA && !mu.responseA.done) {
                        mu.responseA = { ...mu.responseA, done: true };
                    }
                    if (mu.responseB && !mu.responseB.done) {
                        mu.responseB = { ...mu.responseB, done: true };
                    }
                    return mu;
                }),
            }));
            return next;
        });

        setRunningModels(new Set());
        setPhase(arenaModeRef.current === "compare" ? "finished" : "voting");
        // Compare history saving is handled by the useEffect on phase/arenaMode changes
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
                slot.params,
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
                        params: modelParams[newModelId],
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
                modelParams[newModelId],
            );
        },
        [savedPrompt, streamModel, modelParams],
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

    const roundLabel = (roundIdx: number, totalRounds: number): string => {
        if (totalRounds === 1) return "Match";
        if (roundIdx === totalRounds - 1) return "Final";
        if (roundIdx === totalRounds - 2) return "Semifinals";
        if (roundIdx === totalRounds - 3) return "Quarterfinals";
        return `Round ${roundIdx + 1}`;
    };

    return (
        <div className="flex flex-col gap-6 min-h-[calc(100vh-64px)]">
            {/* Header */}
            <div>
                <div className="flex items-center gap-3">
                    {arenaMode === "competition" ? (
                        <Swords
                            size={28}
                            strokeWidth={2}
                            className="text-(--accent)"
                        />
                    ) : (
                        <GitCompare
                            size={28}
                            strokeWidth={2}
                            className="text-(--accent)"
                        />
                    )}
                    <h1 className="text-3xl font-bold text-white">
                        {arenaMode === "competition" ? "Arena" : "Compare"}
                    </h1>
                </div>
                <p className="text-gray-400">
                    {arenaMode === "competition"
                        ? "Bracket tournament — models compete head-to-head"
                        : "Side-by-side — compare model outputs on the same prompt"}
                </p>
            </div>

            {/* Controls */}
            <div className="ui-card p-4">
                <div className="flex items-center justify-between">
                    <div className="flex items-center gap-3">
                        <span className="text-sm font-semibold text-(--text-primary)">
                            Controls
                        </span>
                        <div className="flex items-center gap-1">
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
                                Arena
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
                    <div className="flex items-center gap-1">
                        <button
                            onClick={() => setShowHistoryModal(true)}
                            className="p-1.5 rounded-md transition-all cursor-pointer text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[0_0_6px_var(--accent)]"
                            title="Match history"
                        >
                            <History size={14} />
                        </button>
                        {(phase !== "setup" ||
                            (arenaMode === "competition"
                                ? bracketModels.length > 0
                                : compareModels.length > 0) ||
                            !!activePromptId ||
                            !!prompt.trim() ||
                            !!comparePersonaId ||
                            !!comparePersonaPrompt.trim()) && (
                            <>
                                {/* Light reset: clear results only, keep models/prompt/persona */}
                                {phase !== "setup" && (
                                    <button
                                        onClick={() => {
                                            for (const [
                                                ,
                                                ctrl,
                                            ] of abortMapRef.current) {
                                                ctrl.abort();
                                            }
                                            abortMapRef.current.clear();
                                            setRounds([]);
                                            setCurrentRound(0);
                                            setPhase("setup");
                                            setRunningModels(new Set());
                                            setWinnerModal(null);
                                            setDisabledModels(new Set());
                                            toast("Arena cleared", "info");
                                        }}
                                        className={`p-1.5 rounded-md transition-all cursor-pointer text-amber-400 ${
                                            phase === "finished" ||
                                            phase === "voting"
                                                ? "animate-[pulse-ring_1.5s_ease-in-out_infinite]"
                                                : "hover:drop-shadow-[0_0_6px_var(--color-amber-400,amber)]"
                                        }`}
                                        title="Clear results (keep models & prompt)"
                                    >
                                        <Eraser size={14} />
                                    </button>
                                )}
                                {/* Full reset: clear everything */}
                                <button
                                    onClick={() => setPendingFullReset(true)}
                                    className="p-1.5 rounded-md transition-all cursor-pointer text-red-500 hover:drop-shadow-[0_0_6px_var(--color-red-500,red)]"
                                    title="Reset all (clear models & prompt)"
                                >
                                    <RotateCcw size={14} />
                                </button>
                            </>
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
                                    <div>
                                        <label className="text-sm text-(--text-secondary) mb-2 block">
                                            Models ({bracketModels.length}/8)
                                            <span className="text-(--text-tertiary)">
                                                {" "}
                                                — pick 2, 4, or 8 for a bracket
                                            </span>
                                        </label>
                                        <ModelPicker
                                            models={enabledModels}
                                            selected={bracketModels}
                                            onChange={setBracketModels}
                                            multi={true}
                                            maxSelections={8}
                                            providers={providerData}
                                            align="left"
                                            slotParams={modelParams}
                                            onConfigureParams={
                                                setParamEditorModel
                                            }
                                            onRandom={handleRandomBracketModel}
                                            paramsReadonly={phase !== "setup"}
                                        />
                                        {bracketModels.length > 0 &&
                                            ![2, 4, 8].includes(
                                                bracketModels.length,
                                            ) && (
                                                <p className="text-xs text-amber-400 mt-2">
                                                    Select 2, 4, or 8 models for
                                                    a bracket tournament.
                                                </p>
                                            )}
                                        {new Set(bracketModels).size !==
                                            bracketModels.length &&
                                            bracketModels.length > 0 && (
                                                <p className="text-xs text-amber-400 mt-2">
                                                    No duplicate models.
                                                </p>
                                            )}
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
                                            slotParams={modelParams}
                                            onConfigureParams={
                                                setParamEditorModel
                                            }
                                            onRandom={handleRandomCompareModel}
                                            paramsReadonly={phase !== "setup"}
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
                                            onRandom={
                                                handleRandomComparePersona
                                            }
                                            textareaPlaceholder="Optional system prompt applied to all models…"
                                            wrap
                                        />
                                    </div>
                                </div>
                            )}

                            {/* Prompt */}
                            <PromptPicker
                                prompts={ARENA_PROMPTS}
                                activePromptId={activePromptId}
                                prompt={
                                    phase === "setup" || phase === "finished"
                                        ? prompt
                                        : savedPrompt
                                }
                                onActivePromptIdChange={setActivePromptId}
                                onPromptChange={setPrompt}
                                showPresetBar={phase === "setup"}
                                autoFocus
                                disabled={
                                    phase !== "setup" && phase !== "finished"
                                }
                            />
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
                                            {roundLabel(
                                                roundIdx,
                                                rounds.length,
                                            )}
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

                {/* Mode Description */}
                <p className="text-xs text-(--text-tertiary) leading-snug line-clamp-3 mt-3">
                    {arenaMode === "competition"
                        ? "Models compete in a single-elimination bracket. Pick 2, 4, or 8 models — each round, pairs face the same prompt and you vote for the better response. Winners advance until one model remains."
                        : "Pick models and run the same prompt through them simultaneously. No voting, no bracket — just pure side-by-side output comparison to evaluate which model best fits your needs."}
                </p>
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
                    const isCompare =
                        arenaMode === "compare" &&
                        round.matchups.every((m) => m.slotB === null);
                    return (
                        <div key={roundIdx}>
                            <div className="text-xs text-(--text-tertiary) font-medium uppercase tracking-wider mb-2">
                                {isCompare
                                    ? "Responses"
                                    : roundLabel(roundIdx, rounds.length)}
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
                                                            enabledModels={
                                                                enabledModels
                                                            }
                                                            showVote={false}
                                                            params={
                                                                mu.slotA?.params
                                                            }
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
                                                            enabledModels={
                                                                enabledModels
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
                                                            params={
                                                                mu.slotA?.params
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
                                                            enabledModels={
                                                                enabledModels
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
                                                            params={
                                                                mu.slotB?.params
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

            {pendingFullReset && (
                <ConfirmDialog
                    title="Reset All"
                    message="This will clear all models, prompts, personas, and any in-progress results. Continue?"
                    fields={[]}
                    confirmLabel="Reset All"
                    onConfirm={() => {
                        for (const [, ctrl] of abortMapRef.current) {
                            ctrl.abort();
                        }
                        abortMapRef.current.clear();
                        setCompareModels([]);
                        setBracketModels([]);
                        setCompetitionPrompt("");
                        setComparePrompt("");
                        setSavedPrompt("");
                        setCompetitionActivePromptId(null);
                        setCompareActivePromptId(null);
                        setComparePersonaId(null);
                        setComparePersonaPrompt("");
                        setRounds([]);
                        setCurrentRound(0);
                        setPhase("setup");
                        setRunningModels(new Set());
                        setWinnerModal(null);
                        setDisabledModels(new Set());
                        setModelParams({});
                        setPendingFullReset(false);
                        try {
                            localStorage.removeItem("arenaCompetitionPrompt");
                            localStorage.removeItem("arenaComparePrompt");
                            localStorage.removeItem(
                                "arenaCompetitionActivePromptId",
                            );
                            localStorage.removeItem(
                                "arenaCompareActivePromptId",
                            );
                            localStorage.removeItem("arenaComparePersonaId");
                            localStorage.removeItem(
                                "arenaComparePersonaPrompt",
                            );
                        } catch {
                            /* ignore */
                        }
                        toast("Reset", "info");
                    }}
                    onCancel={() => setPendingFullReset(false)}
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

            {/* Inline Param Editor */}
            {paramEditorModel && (
                <ParamEditorModal
                    modelId={paramEditorModel}
                    params={modelParams[paramEditorModel] ?? {}}
                    onChange={(params) =>
                        setModelParams((prev) => ({
                            ...prev,
                            [paramEditorModel]: params,
                        }))
                    }
                    onClose={() => setParamEditorModel(null)}
                />
            )}

            {/* Match History Modal */}
            {showHistoryModal && (
                <ArenaHistoryModal onClose={() => setShowHistoryModal(false)} />
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

function SlotParamsTooltip({ params }: { params?: GenerationParams }) {
    if (!params) return null;
    const entries = Object.entries(params).filter(([, v]) => v !== undefined);
    if (entries.length === 0) return null;
    const lines = entries
        .map(([k, v]) => {
            const label = k
                .replace(/_/g, " ")
                .replace(/^\w/, (c) => c.toUpperCase());
            return `${label}: ${v}`;
        })
        .join("\n");
    return (
        <span className="shrink-0 text-(--accent) cursor-help" title={lines}>
            <Settings size={10} />
        </span>
    );
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
                <SlotParamsTooltip params={slot.params} />
                {isRunning && !response?.done && (
                    <span className="w-1.5 h-1.5 rounded-full bg-(--accent) animate-pulse shrink-0" />
                )}
                {response?.error && (
                    <AlertCircle size={12} className="text-red-400 shrink-0" />
                )}
                {phase === "finished" && isWinner && (
                    <span title="Winner">
                        <Trophy size={14} className="text-amber-400 shrink-0" />
                    </span>
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
                        title={
                            vote === null ? "Vote for this response" : undefined
                        }
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
                        onRandom={() => {
                            const available = CHAT_PERSONAS.filter(
                                (p) => p.id !== slot.personaId,
                            );
                            if (available.length === 0) return;
                            const pick =
                                available[
                                    Math.floor(Math.random() * available.length)
                                ];
                            if (
                                slot.personaPrompt.trim() &&
                                slot.personaId === null
                            ) {
                                setPendingPersona(pick);
                                return;
                            }
                            onPersonaChange(
                                roundIdx,
                                matchupIdx,
                                slotKey,
                                pick.id,
                                pick.systemPrompt,
                            );
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
    enabledModels: Model[];
    params?: GenerationParams;
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
    enabledModels,
    params,
}: ResponseCardProps) {
    const { toast } = useToast();
    const [detailModel, setDetailModel] = useState<Model | null>(null);
    const isWinner = vote === slotKey;
    const isLoser = vote !== null && vote !== slotKey;

    const modelObj = enabledModels.find(
        (m) => proxyModelID(m.provider_name, m.model_id) === response.model,
    );

    return (
        <>
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
                shortenModelName={true}
                showInfoIcon={true}
                params={params}
                onModelNameClick={
                    modelObj ? () => setDetailModel(modelObj) : undefined
                }
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
                                <span title="Completed">
                                    <CheckCircle2
                                        size={14}
                                        className="text-green-400"
                                    />
                                </span>
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
                                <span title="Error">
                                    <AlertCircle
                                        size={14}
                                        className="text-red-400"
                                    />
                                </span>
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
                            <span title="Winner">
                                <Trophy size={14} className="text-amber-400" />
                            </span>
                        )}
                    </>
                }
                footerEnd={
                    <div className="flex items-center gap-2">
                        {response.done && response.content && (
                            <button
                                className="inline-flex items-center cursor-pointer transition-all text-(--accent) hover:drop-shadow-[0_0_4px_var(--accent)]"
                                onClick={() => {
                                    navigator.clipboard
                                        .writeText(response.content)
                                        .then(() =>
                                            toast(
                                                "Copied to clipboard",
                                                "info",
                                            ),
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
                                              onVote(
                                                  roundIdx,
                                                  matchupIdx,
                                                  slotKey,
                                              )
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
                                title={
                                    vote === null
                                        ? "Vote for this response"
                                        : undefined
                                }
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
            {detailModel && (
                <ModelDetailModal
                    model={detailModel}
                    onClose={() => setDetailModel(null)}
                />
            )}
        </>
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

function ParamEditorModal({
    modelId,
    params,
    onChange,
    onClose,
}: {
    modelId: string;
    params: GenerationParams;
    onChange: (params: GenerationParams) => void;
    onClose: () => void;
}) {
    return (
        <div
            className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 backdrop-blur-sm"
            onClick={onClose}
        >
            <div
                className="ui-card p-4 w-full max-w-sm space-y-4"
                onClick={(e) => e.stopPropagation()}
            >
                <div className="flex items-center justify-between">
                    <h3 className="text-sm font-semibold text-(--text-primary)">
                        {modelId}
                    </h3>
                    <button
                        onClick={onClose}
                        className="p-1.5 rounded-md cursor-pointer text-(--text-tertiary) hover:text-(--text-primary) transition-colors"
                        title="Close"
                    >
                        <X size={14} />
                    </button>
                </div>

                <div className="space-y-3">
                    <ParamSlider
                        label="Temperature"
                        value={params.temperature}
                        min={0}
                        max={2}
                        step={0.01}
                        onChange={(v) =>
                            onChange({ ...params, temperature: v })
                        }
                    />
                    <ParamSlider
                        label="Max Tokens"
                        value={params.max_tokens}
                        min={1}
                        max={32768}
                        step={1}
                        onChange={(v) =>
                            onChange({
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
                        onChange={(v) => onChange({ ...params, top_p: v })}
                    />
                    <ParamSlider
                        label="Min P"
                        value={params.min_p}
                        min={0}
                        max={1}
                        step={0.01}
                        onChange={(v) => onChange({ ...params, min_p: v })}
                    />
                    <ParamSlider
                        label="Top K"
                        value={params.top_k}
                        min={1}
                        max={100}
                        step={1}
                        onChange={(v) =>
                            onChange({
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
                            onChange({ ...params, frequency_penalty: v })
                        }
                    />
                    <ParamSlider
                        label="Pres Penalty"
                        value={params.presence_penalty}
                        min={-2}
                        max={2}
                        step={0.01}
                        onChange={(v) =>
                            onChange({ ...params, presence_penalty: v })
                        }
                    />
                </div>

                <div className="flex items-center justify-between pt-2 border-t border-(--border-subtle)">
                    {hasAnyParam(params) && (
                        <button
                            onClick={() => onChange({})}
                            className="text-[11px] text-red-400 hover:text-red-300 transition-colors cursor-pointer"
                        >
                            Reset all
                        </button>
                    )}
                    <div />
                    <button
                        onClick={onClose}
                        className="ui-btn ui-btn-primary text-xs px-3 py-1"
                    >
                        Done
                    </button>
                </div>
            </div>
        </div>
    );
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
    const pct = isSet ? ((value - min) / (max - min)) * 100 : 0;
    return (
        <div>
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
                    className="w-14 text-right px-1.5 py-0.5 rounded bg-(--surface-input) text-[10px] text-(--text-primary) border border-transparent focus:border-(--accent) outline-none placeholder:text-(--text-tertiary) no-spinner"
                />
            </div>
            <input
                type="range"
                min={min}
                max={max}
                step={step}
                value={isSet ? value : min}
                data-set={isSet ? "true" : undefined}
                onChange={(e) => onChange(parseFloat(e.target.value))}
                className="gen-slider w-full h-1 rounded-lg appearance-none cursor-pointer bg-(--surface-hover) accent-(--accent) mt-0.5"
                style={{
                    background: isSet
                        ? `linear-gradient(to right, var(--accent) ${pct}%, var(--surface-hover) ${pct}%)`
                        : undefined,
                }}
            />
        </div>
    );
}

function SwapPicker({
    enabledModels,
    disabledModels,
    alreadyUsed,
    onSelect,
}: SwapPickerProps) {
    const [search, setSearch] = useState("");

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
        <div className="flex flex-col items-center max-h-60 min-h-0">
            <p className="text-xs text-amber-400 mb-2 shrink-0">
                Pick a replacement model
            </p>
            <FilterInput
                value={search}
                onChange={setSearch}
                placeholder="Search models…"
                className="w-full max-w-xs mb-2 shrink-0"
            />
            <div className="flex flex-wrap gap-1 overflow-y-auto w-full justify-center content-start px-2 min-h-0">
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
                                {rounds.length === 1
                                    ? "Match"
                                    : roundIdx === rounds.length - 1
                                      ? "Final"
                                      : roundIdx === rounds.length - 2
                                        ? "Semifinals"
                                        : roundIdx === rounds.length - 3
                                          ? "Quarterfinals"
                                          : `Round ${roundIdx + 1}`}
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
