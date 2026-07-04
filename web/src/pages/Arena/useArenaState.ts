import i18next from "i18next";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { GenerationParams } from "../../api/types";
import type { ArenaSubMode } from "../../context/SidebarModeContext";
import { useSidebarMode } from "../../context/SidebarModeContext";
import { useStorage } from "../../context/StorageContext";
import { useToast } from "../../context/ToastContext";
import { CHAT_PERSONAS } from "../../data/presets";
import { useLocalStorage } from "../../hooks/useLocalStorage";
import { useChatModels } from "../../hooks/useModels";
import {
	getArenaHistoryEnabled,
	saveCompareToHistory,
} from "../../utils/arenaHistory";
import { proxyModelID } from "../../utils/model";
import {
	buildCompareRound,
	buildInitialRounds,
	getPreviewPairs,
} from "./builders";
import type { BracketPhase, BracketRound, WinnerModal } from "./types";
import { useArenaPersistence } from "./useArenaPersistence";
import { nextBracketSize } from "./utils";

export interface ArenaStateAndActions {
	// State values
	compareModels: string[];
	setCompareModels: React.Dispatch<React.SetStateAction<string[]>>;
	bracketModels: string[];
	setBracketModels: React.Dispatch<React.SetStateAction<string[]>>;
	competitionActivePromptId: string | null;
	setCompetitionActivePromptId: React.Dispatch<
		React.SetStateAction<string | null>
	>;
	compareActivePromptId: string | null;
	setCompareActivePromptId: React.Dispatch<React.SetStateAction<string | null>>;
	competitionPrompt: string;
	setCompetitionPrompt: React.Dispatch<React.SetStateAction<string>>;
	comparePrompt: string;
	setComparePrompt: React.Dispatch<React.SetStateAction<string>>;
	prompt: string;
	setPrompt: (v: string) => void;
	activePromptId: string | null;
	setActivePromptId: (v: string | null) => void;
	savedPrompt: string;
	setSavedPrompt: React.Dispatch<React.SetStateAction<string>>;
	comparePersonaId: string | null;
	setComparePersonaId: React.Dispatch<React.SetStateAction<string | null>>;
	comparePersonaPrompt: string;
	setComparePersonaPrompt: React.Dispatch<React.SetStateAction<string>>;
	rounds: BracketRound[];
	setRounds: React.Dispatch<React.SetStateAction<BracketRound[]>>;
	currentRound: number;
	setCurrentRound: React.Dispatch<React.SetStateAction<number>>;
	phase: BracketPhase;
	setPhase: React.Dispatch<React.SetStateAction<BracketPhase>>;
	runningModels: Set<string>;
	setRunningModels: React.Dispatch<React.SetStateAction<Set<string>>>;
	winnerModal: WinnerModal | null;
	setWinnerModal: React.Dispatch<React.SetStateAction<WinnerModal | null>>;
	disabledModels: Set<string>;
	setDisabledModels: React.Dispatch<React.SetStateAction<Set<string>>>;
	arenaCollapsed: boolean;
	setArenaCollapsed: React.Dispatch<React.SetStateAction<boolean>>;
	pendingFullReset: boolean;
	setPendingFullReset: React.Dispatch<React.SetStateAction<boolean>>;
	showHistoryModal: boolean;
	setShowHistoryModal: React.Dispatch<React.SetStateAction<boolean>>;
	modelParams: Record<string, GenerationParams>;
	setModelParams: React.Dispatch<
		React.SetStateAction<Record<string, GenerationParams>>
	>;
	paramEditorModel: string | null;
	setParamEditorModel: React.Dispatch<React.SetStateAction<string | null>>;
	arenaMode: ArenaSubMode;
	setArenaMode: (mode: ArenaSubMode) => void;
	// Refs
	abortMapRef: React.RefObject<Map<string, AbortController>>;
	lastExtractLenRef: React.RefObject<Map<string, number>>;
	currentRoundRef: React.RefObject<number>;
	roundsLengthRef: React.RefObject<number>;
	roundsRef: React.RefObject<BracketRound[]>;
	activePromptIdRef: React.RefObject<string | null>;
	comparePersonaIdRef: React.RefObject<string | null>;
	arenaModeRef: React.RefObject<ArenaSubMode>;
	// Computed values
	canRun: boolean;
	disabledReason: string;
	buildCompareRoundWithParams: (
		modelIds: string[],
		personaId?: string | null,
		personaPrompt?: string,
	) => BracketRound[];
	buildInitialRoundsWithParams: (models: string[]) => BracketRound[];
	handleRandomComparePersona: () => void;
	handleRandomBracketModel: () => void;
	handleRandomCompareModel: () => void;
	previewPairs: ReturnType<typeof getPreviewPairs>;
	// Dependencies
	enabledModels: ReturnType<typeof useChatModels>["data"];
	toast: ReturnType<typeof useToast>["toast"];
}

export function useArenaState(): ArenaStateAndActions {
	const { data: enabledModels } = useChatModels();
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

	const [competitionActivePromptId, setCompetitionActivePromptId] =
		useLocalStorage<string | null>("arenaCompetitionActivePromptId", null, {
			enabled: persistArena,
			serialize: (v) => v ?? "",
			deserialize: (v) => v || null,
		});
	const [compareActivePromptId, setCompareActivePromptId] = useLocalStorage<
		string | null
	>("arenaCompareActivePromptId", null, {
		enabled: persistArena,
		serialize: (v) => v ?? "",
		deserialize: (v) => v || null,
	});

	const [competitionPrompt, setCompetitionPrompt] = useLocalStorage<string>(
		"arenaCompetitionPrompt",
		"",
		{ enabled: persistArena },
	);
	const [comparePrompt, setComparePrompt] = useLocalStorage<string>(
		"arenaComparePrompt",
		"",
		{ enabled: persistArena },
	);

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
	const setPrompt = useCallback(
		(v: string) => {
			if (arenaModeRef.current === "competition") setCompetitionPrompt(v);
			else setComparePrompt(v);
		},
		[setCompetitionPrompt, setComparePrompt],
	);
	const setActivePromptId = useCallback(
		(v: string | null) => {
			if (arenaModeRef.current === "competition")
				setCompetitionActivePromptId(v);
			else setCompareActivePromptId(v);
		},
		[setCompetitionActivePromptId, setCompareActivePromptId],
	);
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

	const [comparePersonaId, setComparePersonaId] = useLocalStorage<
		string | null
	>("arenaComparePersonaId", null, {
		enabled: persistArena,
		serialize: (v) => v ?? "",
		deserialize: (v) => v || null,
	});
	const [comparePersonaPrompt, setComparePersonaPrompt] =
		useLocalStorage<string>("arenaComparePersonaPrompt", "", {
			enabled: persistArena,
		});

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
	const [disabledModels, setDisabledModels] = useState<Set<string>>(new Set());
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

	const [paramEditorModel, setParamEditorModel] = useState<string | null>(null);

	useArenaPersistence({
		arenaMode,
		compareModels,
		bracketModels,
		rounds,
		currentRound,
		phase,
		arenaCollapsed,
		savedPrompt,
		modelParams,
	});

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
		const map = abortMapRef.current;
		return () => {
			for (const [, ctrl] of map) {
				ctrl.abort();
			}
			map.clear();
		};
	}, []);

	useEffect(() => {
		activePromptIdRef.current = activePromptId;
	}, [activePromptId]);

	useEffect(() => {
		comparePersonaIdRef.current = comparePersonaId;
	}, [comparePersonaId]);

	// Drop persisted model ids that are no longer valid chat models (e.g. one
	// that became an embedding/rerank model, or got disabled) so a stale
	// localStorage entry can't make a run start against a model that can't
	// serve chat. Only in setup phase, so a running competition's captured
	// line-up is never mutated; only once the list has loaded so a transient
	// empty fetch never wipes valid selections.
	useEffect(() => {
		if (phase !== "setup" || enabledModels.length === 0) return;
		const valid = new Set(
			enabledModels.map((m) => proxyModelID(m.provider_name, m.model_id)),
		);
		// Reconciling persisted state against freshly-loaded data; the functional
		// updates return the same reference when nothing changes, so this settles
		// in one pass without an update loop.
		// eslint-disable-next-line react-hooks/set-state-in-effect
		setBracketModels((prev) => {
			const next = prev.filter((id) => valid.has(id));
			return next.length === prev.length ? prev : next;
		});
		setCompareModels((prev) => {
			const next = prev.filter((id) => valid.has(id));
			return next.length === prev.length ? prev : next;
		});
	}, [phase, enabledModels]);

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
						tokensPerSecond: number | null;
						durationMs: number;
						promptTokens: number;
						completionTokens: number;
					} | null;
				}[] = [];
				for (const mu of round.matchups) {
					if (mu.slotA) {
						models.push(mu.slotA.modelId);
						if (mu.responseA?.done) {
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

	const canRun = useMemo(() => {
		if (phase !== "setup" && phase !== "next_round_ready") return false;
		if (!prompt.trim()) return false;
		if (arenaMode === "compare") {
			if (compareModels.length < 2) return false;
			if (new Set(compareModels).size !== compareModels.length) return false;
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
					return i18next.t("arena.disabledReason.selectTwoModels");
				if (compareModels.length === 1)
					return i18next.t("arena.disabledReason.pickOneMore");
				if (new Set(compareModels).size !== compareModels.length)
					return i18next.t("arena.disabledReason.noDuplicates");
				if (!prompt.trim())
					return i18next.t("arena.disabledReason.enterPrompt");
				return "";
			}
			if (bracketModels.length === 0)
				return i18next.t("arena.disabledReason.selectBracketModels");
			if (bracketModels.length === 1)
				return i18next.t("arena.disabledReason.pickOneMore");
			if (new Set(bracketModels).size !== bracketModels.length)
				return i18next.t("arena.disabledReason.noDuplicates");
			if (![2, 4, 8].includes(bracketModels.length)) {
				const nextValid = nextBracketSize(bracketModels.length);
				return i18next.t("arena.disabledReason.pickOrRemove", {
					count: nextValid - bracketModels.length,
					nextValid,
				});
			}
			if (!prompt.trim()) return i18next.t("arena.disabledReason.enterPrompt");
		}
		if (phase === "voting")
			return i18next.t("arena.disabledReason.voteToContinue");
		if (phase === "next_round_ready") {
			if (!prompt.trim())
				return i18next.t("arena.disabledReason.enterPromptNextRound");
		}
		return "";
	}, [phase, arenaMode, compareModels, bracketModels, prompt]);

	const buildCompareRoundWithParams = useCallback(
		(
			modelIds: string[],
			personaId: string | null = null,
			personaPrompt: string = "",
		) => buildCompareRound(modelIds, personaId, personaPrompt, modelParams),
		[modelParams],
	);

	const buildInitialRoundsWithParams = useCallback(
		(models: string[]) => buildInitialRounds(models, modelParams),
		[modelParams],
	);

	const handleRandomComparePersona = useCallback(() => {
		const available = CHAT_PERSONAS.filter((p) => p.id !== comparePersonaId);
		if (available.length === 0) return;
		const pick = available[Math.floor(Math.random() * available.length)];
		setComparePersonaId(pick.id);
		setComparePersonaPrompt(i18next.t(pick.systemPrompt));
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
	}, [enabledModels, bracketModels]);

	const handleRandomCompareModel = useCallback(() => {
		const available = enabledModels.filter((m) => {
			const val = proxyModelID(m.provider_name, m.model_id);
			return !compareModels.includes(val);
		});
		if (available.length === 0 || compareModels.length >= 6) return;
		const pick = available[Math.floor(Math.random() * available.length)];
		const val = proxyModelID(pick.provider_name, pick.model_id);
		setCompareModels([...compareModels, val]);
	}, [enabledModels, compareModels]);
	// Compute bracket preview pairs for setup phase
	const previewPairs = useMemo(() => {
		if (
			arenaMode !== "competition" ||
			phase !== "setup" ||
			bracketModels.length === 0
		)
			return null;
		return getPreviewPairs(bracketModels);
	}, [arenaMode, phase, bracketModels]);

	return {
		// State values
		compareModels,
		setCompareModels,
		bracketModels,
		setBracketModels,
		competitionActivePromptId,
		setCompetitionActivePromptId,
		compareActivePromptId,
		setCompareActivePromptId,
		competitionPrompt,
		setCompetitionPrompt,
		comparePrompt,
		setComparePrompt,
		prompt,
		setPrompt,
		activePromptId,
		setActivePromptId,
		savedPrompt,
		setSavedPrompt,
		comparePersonaId,
		setComparePersonaId,
		comparePersonaPrompt,
		setComparePersonaPrompt,
		rounds,
		setRounds,
		currentRound,
		setCurrentRound,
		phase,
		setPhase,
		runningModels,
		setRunningModels,
		winnerModal,
		setWinnerModal,
		disabledModels,
		setDisabledModels,
		arenaCollapsed,
		setArenaCollapsed,
		pendingFullReset,
		setPendingFullReset,
		showHistoryModal,
		setShowHistoryModal,
		modelParams,
		setModelParams,
		paramEditorModel,
		setParamEditorModel,
		arenaMode,
		setArenaMode,
		// Refs
		abortMapRef,
		lastExtractLenRef,
		currentRoundRef,
		roundsLengthRef,
		roundsRef,
		activePromptIdRef,
		comparePersonaIdRef,
		arenaModeRef,
		// Computed values
		canRun,
		disabledReason,
		buildCompareRoundWithParams,
		buildInitialRoundsWithParams,
		handleRandomComparePersona,
		handleRandomBracketModel,
		handleRandomCompareModel,
		previewPairs,
		// Dependencies
		enabledModels,
		toast,
	};
}
