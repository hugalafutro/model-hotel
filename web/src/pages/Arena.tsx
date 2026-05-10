import { produce } from "immer";
import {
	Columns3,
	Eraser,
	GitCompare,
	History,
	Play,
	RotateCcw,
	Swords,
	X,
} from "lucide-react";
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { API_BASE, getAuthHeaders } from "../api/client";
import type { GenerationParams } from "../api/types";
import { ActionIconButton } from "../components/ActionIconButton";
import { ArenaHistoryModal } from "../components/ArenaHistoryModal";
import { CollapsibleToggle } from "../components/CollapsibleToggle";
import { ConfirmDialog } from "../components/ConfirmDialog";
import { ModelPicker } from "../components/ModelPicker";
import { PageHeader } from "../components/PageHeader";
import { PersonaPicker } from "../components/PersonaPicker";
import { PromptPicker } from "../components/PromptPicker";
import { SubModeToggle } from "../components/SubModeToggle";
import {
	type ArenaSubMode,
	useSidebarMode,
} from "../context/SidebarModeContext";
import { useStorage } from "../context/StorageContext";
import { useToast } from "../context/ToastContext";
import { ARENA_PROMPTS, CHAT_PERSONAS } from "../data/presets";
import { useLocalStorage } from "../hooks/useLocalStorage";
import { useEnabledModels } from "../hooks/useModels";
import {
	getArenaHistoryEnabled,
	saveCompareToHistory,
	saveCompetitionToHistory,
} from "../utils/arenaHistory";
import { providerFromModelID, proxyModelID } from "../utils/model";
import { hasAnyParam } from "../utils/params";
import { readSSEStream, type StreamChunk } from "../utils/sse";
import { fetchWithRetry, staggerByProvider } from "../utils/stagger";
import {
	extractThinking,
	sanitizeDelta,
	shouldReExtract,
} from "../utils/thinking";
import { MatchupCard } from "./Arena/MatchupCard";
import { ParamEditorModal } from "./Arena/ParamEditorModal";
import { ResponseCard } from "./Arena/ResponseCard";
import { SwapPicker } from "./Arena/SwapPicker";
import { BracketPreviewPill } from "./Arena/shared";
import type {
	ArenaResponse,
	BracketPhase,
	BracketRound,
	Matchup,
	MatchupSlot,
	WinnerModal,
} from "./Arena/types";
import { nextBracketSize } from "./Arena/utils";
import { WinnerSummaryModal } from "./Arena/WinnerSummaryModal";

export function Arena() {
	const { data: enabledModels } = useEnabledModels();
	const { toast } = useToast();
	const { persistArena } = useStorage();
	const quotaWarnedRef = useRef(false);
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
			if (!quotaWarnedRef.current) {
				quotaWarnedRef.current = true;
				toast("Storage full - arena state not saved", "warning");
			}
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
		toast,
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
				if (compareModels.length === 0) return "Select at least 2 models";
				if (compareModels.length === 1) return "Pick at least 1 more model";
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
				const nextValid = nextBracketSize(bracketModels.length);
				return `Pick ${nextValid - bracketModels.length} more or remove to get ${nextValid}`;
			}
			if (!prompt.trim()) return "Enter a prompt";
		}
		if (phase === "voting")
			return "Vote on all matchups to continue to the next round";
		if (phase === "next_round_ready") {
			if (!prompt.trim()) return "Enter a prompt for the next round";
		}
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

			const bracketRounds: BracketRound[] = [{ matchups: firstRoundMatchups }];

			for (let r = 1; r < numRounds; r++) {
				const matchupCount = models.length / 2 ** (r + 1);
				bracketRounds.push({
					matchups: Array.from({ length: matchupCount }, () => emptyMatchup()),
				});
			}

			return bracketRounds;
		},
		[modelParams],
	);

	const handleRandomComparePersona = useCallback(() => {
		const available = CHAT_PERSONAS.filter((p) => p.id !== comparePersonaId);
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
		const target = nextBracketSize(bracketModels.length);
		const items = [...bracketModels];
		while (items.length < target) items.push("");
		const pairs: { a: string; b: string }[] = [];
		for (let i = 0; i < items.length; i += 2) {
			pairs.push({ a: items[i], b: items[i + 1] ?? "" });
		}
		return pairs;
	}, [arenaMode, phase, bracketModels]);

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

				const chatMessages: Array<{ role: string; content: string }> = [];
				if (personaPrompt.trim()) {
					chatMessages.push({
						role: "system",
						content: personaPrompt.trim(),
					});
				}
				chatMessages.push({ role: "user", content: userPrompt });

				try {
					// Use fetchWithRetry for automatic retry on 429/502/503/504
					const resp = await fetchWithRetry(
						`${API_BASE}/api/chat/arena`,
						{
							method: "POST",
							headers: getAuthHeaders(),
							body: JSON.stringify({
								model,
								stream: true,
								messages: chatMessages,
								...(slotParams && hasAnyParam(slotParams) ? slotParams : {}),
							}),
							signal: abortCtrl.signal,
						},
						{
							maxRetries: 2,
							onRetry: (attempt, delayMs, status) => {
								toast(
									`${model}: ${status || "network error"} - retry ${attempt} in ${(delayMs / 1000).toFixed(1)}s…`,
									"info",
								);
							},
						},
					);

					if (!resp.ok) {
						const text = await resp.text();
						throw new Error(`Arena failed: ${resp.status} ${text}`);
					}

					const reader = resp.body?.getReader();
					if (!reader) throw new Error("No readable stream");

					const completion = await readSSEStream<StreamChunk>({
						reader,
						signal: abortCtrl.signal,
						onChunk(chunk) {
							const delta = chunk.choices?.[0]?.delta?.content;
							if (delta) {
								const clean = sanitizeDelta(delta);
								charCount += clean.length;
								setRounds(
									produce((draft) => {
										const mu = draft[roundIdx]?.matchups[matchupIdx];
										if (mu) {
											const respKey =
												slotKey === "A" ? "responseA" : "responseB";
											const resp = mu[respKey] as ArenaResponse;
											const newRaw = resp.rawContent + clean;
											const lastLen =
												lastExtractLenRef.current.get(extractKey) ?? 0;
											const needsExtract =
												shouldReExtract(clean) || newRaw.length - lastLen >= 50;
											let nextContent: string;
											let nextThinking: string;
											if (needsExtract) {
												const extracted = extractThinking(newRaw);
												lastExtractLenRef.current.set(
													extractKey,
													newRaw.length,
												);
												nextContent = extracted.content;
												nextThinking =
													extracted.thinking || resp.thinkingContent;
											} else {
												nextContent = resp.content + clean;
												nextThinking = resp.thinkingContent;
											}
											mu[respKey] = {
												...resp,
												rawContent: newRaw,
												content: nextContent,
												thinkingContent: nextThinking,
											};
										}
									}),
								);
							}
							const thinkingDelta =
								chunk.choices?.[0]?.delta?.reasoning_content ??
								chunk.choices?.[0]?.delta?.reasoning;
							if (thinkingDelta) {
								setRounds(
									produce((draft) => {
										if (draft[roundIdx]?.matchups[matchupIdx]) {
											const mu = draft[roundIdx].matchups[matchupIdx];
											const respKey =
												slotKey === "A" ? "responseA" : "responseB";
											mu[respKey] = {
												...(mu[respKey] as ArenaResponse),
												thinkingContent:
													mu[respKey]?.thinkingContent + thinkingDelta,
											};
										}
									}),
								);
							}
							if (chunk.usage) {
								promptTokens = chunk.usage.prompt_tokens ?? 0;
								completionTokens = chunk.usage.completion_tokens ?? 0;
							}
						},
					});

					const durationMs = performance.now() - startTime;
					const charsPerSecond =
						durationMs > 0 ? charCount / (durationMs / 1000) : null;

					const truncationError: string | null =
						!completion.sawDone && !completion.aborted
							? completion.idleTimeout
								? "Stream stalled - no data received within the timeout period."
								: "Stream was cut off - the response may be incomplete."
							: null;

					setRounds(
						produce((draft) => {
							if (draft[roundIdx]?.matchups[matchupIdx]) {
								const mu = draft[roundIdx].matchups[matchupIdx];
								const respKey = slotKey === "A" ? "responseA" : "responseB";
								mu[respKey] = {
									...(mu[respKey] as ArenaResponse),
									done: true,
									error: truncationError,
									metrics: {
										charsPerSecond,
										durationMs: Math.round(durationMs),
										promptTokens,
										completionTokens,
									},
								};
							}
						}),
					);
				} catch (err) {
					const msg = err instanceof Error ? err.message : "Unknown error";
					setRounds(
						produce((draft) => {
							if (draft[roundIdx]?.matchups[matchupIdx]) {
								const mu = draft[roundIdx].matchups[matchupIdx];
								const respKey = slotKey === "A" ? "responseA" : "responseB";
								mu[respKey] = {
									...(mu[respKey] as ArenaResponse),
									done: true,
									error: msg,
									metrics: {
										charsPerSecond:
											charCount > 0
												? charCount / ((performance.now() - startTime) / 1000)
												: null,
										durationMs: Math.round(performance.now() - startTime),
										promptTokens,
										completionTokens,
									},
								};
							}
						}),
					);
					toast(`${model}: ${msg}`, "error");
				} finally {
					setRunningModels((prev) => {
						const next = new Set(prev);
						next.delete(model);
						if (next.size === 0 && !abortCtrl.signal.aborted) {
							setPhase(
								arenaModeRef.current === "compare" ? "finished" : "voting",
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

			setRounds(
				produce((draft) => {
					if (draft[roundIdx]) {
						draft[roundIdx].matchups = draft[roundIdx].matchups.map((mu) => {
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
						});
					}
				}),
			);

			// Collect all slots to stream, then stagger by provider
			// so same-provider requests are spaced 300ms apart
			const slots: Array<{
				modelId: string;
				personaPrompt: string;
				slotKey: "A" | "B";
				matchupIdx: number;
				params?: GenerationParams;
			}> = [];
			for (let mi = 0; mi < round.matchups.length; mi++) {
				const mu = round.matchups[mi];
				if (mu.slotA) {
					slots.push({
						modelId: mu.slotA.modelId,
						personaPrompt: mu.slotA.personaPrompt,
						slotKey: "A",
						matchupIdx: mi,
						params: mu.slotA.params,
					});
				}
				if (mu.slotB) {
					slots.push({
						modelId: mu.slotB.modelId,
						personaPrompt: mu.slotB.personaPrompt,
						slotKey: "B",
						matchupIdx: mi,
						params: mu.slotB.params,
					});
				}
			}

			const knownProviders = enabledModels.map((m) => m.provider_name);
			const staggered = staggerByProvider(
				slots,
				(s) => providerFromModelID(s.modelId, knownProviders),
				300,
			);

			for (const { item, delayMs } of staggered) {
				if (delayMs > 0) {
					setTimeout(
						() =>
							streamModel(
								item.modelId,
								item.personaPrompt,
								currentPrompt,
								roundIdx,
								item.slotKey,
								item.matchupIdx,
								item.params,
							),
						delayMs,
					);
				} else {
					streamModel(
						item.modelId,
						item.personaPrompt,
						currentPrompt,
						roundIdx,
						item.slotKey,
						item.matchupIdx,
						item.params,
					);
				}
			}
		},
		[savedPrompt, prompt, streamModel, enabledModels],
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

		setRounds(
			produce((draft) => {
				if (draft[0]) {
					const now = Date.now();
					draft[0].matchups = draft[0].matchups.map((mu) => ({
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
			}),
		);

		// Collect all slots to stream, then stagger by provider
		// so same-provider requests are spaced 300ms apart
		const slots: Array<{
			modelId: string;
			personaPrompt: string;
			slotKey: "A" | "B";
			matchupIdx: number;
			params?: GenerationParams;
		}> = [];
		for (let mi = 0; mi < initialRounds[0].matchups.length; mi++) {
			const mu = initialRounds[0].matchups[mi];
			if (mu.slotA) {
				slots.push({
					modelId: mu.slotA.modelId,
					personaPrompt: mu.slotA.personaPrompt,
					slotKey: "A",
					matchupIdx: mi,
					params: mu.slotA.params,
				});
			}
			if (mu.slotB) {
				slots.push({
					modelId: mu.slotB.modelId,
					personaPrompt: mu.slotB.personaPrompt,
					slotKey: "B",
					matchupIdx: mi,
					params: mu.slotB.params,
				});
			}
		}

		const knownProviders = enabledModels.map((m) => m.provider_name);
		const staggered = staggerByProvider(
			slots,
			(s) => providerFromModelID(s.modelId, knownProviders),
			300,
		);

		for (const { item, delayMs } of staggered) {
			if (delayMs > 0) {
				setTimeout(
					() =>
						streamModel(
							item.modelId,
							item.personaPrompt,
							currentPrompt,
							0,
							item.slotKey,
							item.matchupIdx,
							item.params,
						),
					delayMs,
				);
			} else {
				streamModel(
					item.modelId,
					item.personaPrompt,
					currentPrompt,
					0,
					item.slotKey,
					item.matchupIdx,
					item.params,
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
		enabledModels,
	]);

	const handleVote = useCallback(
		(roundIdx: number, matchupIdx: number, vote: "A" | "B") => {
			let shouldAdvance = false;
			let advanceRoundIdx = -1;
			let shouldDeclareWinner = false;

			setRounds(
				produce((draft) => {
					const mu = draft[roundIdx]?.matchups[matchupIdx];
					if (mu) {
						mu.vote = mu.vote === vote ? null : vote;
					}

					if (
						roundIdx === currentRoundRef.current &&
						mu?.vote !== null &&
						draft[roundIdx].matchups.every((m) => m.vote !== null)
					) {
						if (roundIdx < draft.length - 1) {
							shouldAdvance = true;
							advanceRoundIdx = roundIdx;

							const winners = draft[roundIdx].matchups.map((m) =>
								m.vote === "A" ? m.slotA : m.slotB,
							);
							const nextRoundIdx = roundIdx + 1;
							if (draft[nextRoundIdx]) {
								for (let i = 0; i < winners.length; i += 2) {
									const matchupIdx = i / 2;
									draft[nextRoundIdx].matchups[matchupIdx] = {
										slotA: winners[i]
											? { ...(winners[i] as MatchupSlot) }
											: null,
										slotB: winners[i + 1]
											? { ...(winners[i + 1] as MatchupSlot) }
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

					roundsRef.current = draft as BracketRound[];
				}),
			);

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
		setRounds(
			produce((draft) => {
				for (const round of draft) {
					for (const mu of round.matchups) {
						if (mu.responseA && !mu.responseA.done) {
							mu.responseA.done = true;
						}
						if (mu.responseB && !mu.responseB.done) {
							mu.responseB.done = true;
						}
					}
				}
			}),
		);

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

			setRounds(
				produce((draft) => {
					const respKey = slotKey === "A" ? "responseA" : "responseB";
					if (draft[roundIdx]?.matchups[matchupIdx]) {
						draft[roundIdx].matchups[matchupIdx][respKey] = {
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
				}),
			);
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

			setRounds(
				produce((draft) => {
					const slotKeyStr = slotKey === "A" ? "slotA" : "slotB";
					const respKey = slotKey === "A" ? "responseA" : "responseB";
					if (draft[roundIdx]?.matchups[matchupIdx]) {
						draft[roundIdx].matchups[matchupIdx][slotKeyStr] = null;
						draft[roundIdx].matchups[matchupIdx][respKey] = null;
					}
				}),
			);
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
			setRounds(
				produce((draft) => {
					if (draft[roundIdx]?.matchups[matchupIdx]) {
						draft[roundIdx].matchups[matchupIdx][slotKeyStr] = null;
						draft[roundIdx].matchups[matchupIdx][respKey] = null;
					}
				}),
			);
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
			setRounds(
				produce((draft) => {
					const slotKeyStr = slotKey === "A" ? "slotA" : "slotB";
					const respKey = slotKey === "A" ? "responseA" : "responseB";
					if (draft[roundIdx]?.matchups[matchupIdx]) {
						draft[roundIdx].matchups[matchupIdx][slotKeyStr] = {
							modelId: newModelId,
							personaId: null,
							personaPrompt: "",
							params: modelParams[newModelId],
						};
						draft[roundIdx].matchups[matchupIdx][respKey] = {
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
				}),
			);
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
			setRounds(
				produce((draft) => {
					const mu = draft[roundIdx]?.matchups[matchupIdx];
					if (mu) {
						const slotKey = slot === "A" ? "slotA" : "slotB";
						if (mu[slotKey]) {
							mu[slotKey] = {
								...(mu[slotKey] as MatchupSlot),
								personaId,
								personaPrompt,
							};
						}
					}
				}),
			);
		},
		[],
	);

	const isRunning = runningModels.size > 0;

	const arenaIcon = arenaMode === "competition" ? Swords : GitCompare;

	const buttonLabel = useMemo(() => {
		if (isRunning) return "Stop";
		if (phase === "setup") return "Run Arena";
		return null;
	}, [isRunning, phase]);

	const showResponseGrid = phase !== "setup";

	const roundLabel = (roundIdx: number, totalRounds: number): string => {
		if (arenaMode === "compare") return "Generation";
		if (totalRounds === 1) return "Match";
		if (roundIdx === totalRounds - 1) return "Final";
		if (roundIdx === totalRounds - 2) return "Semifinals";
		if (roundIdx === totalRounds - 3) return "Quarterfinals";
		return `Round ${roundIdx + 1}`;
	};

	return (
		<div className="flex flex-col gap-6 min-h-[calc(100vh-64px)]">
			{/* Header */}
			<PageHeader
				icon={arenaIcon}
				title={arenaMode === "competition" ? "Arena" : "Compare"}
				description={
					arenaMode === "competition"
						? "Bracket tournament - models compete head-to-head"
						: "Side-by-side - compare model outputs on the same prompt"
				}
			/>

			{/* Controls */}
			<div className="ui-card p-4">
				<div className="flex items-center justify-between">
					<div className="flex items-center gap-3">
						<span className="text-sm font-semibold text-(--text-primary)">
							Controls
						</span>
						<SubModeToggle
							options={[
								{ value: "competition" as const, label: "Arena", icon: Swords },
								{ value: "compare" as const, label: "Compare", icon: Columns3 },
							]}
							value={arenaMode}
							onChange={(v) => {
								if (phase === "setup") setArenaMode(v);
							}}
							disabled={phase !== "setup"}
						/>
					</div>
					<div className="flex items-center gap-1">
						<button
							type="button"
							onClick={() => setShowHistoryModal(true)}
							className="p-1.5 rounded-md transition-all cursor-pointer text-(--text-tertiary) hover:text-(--accent) hover:drop-shadow-[var(--glow-accent)]"
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
									<ActionIconButton
										icon={Eraser}
										onClick={() => {
											for (const [, ctrl] of abortMapRef.current) {
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
										title="Clear results (keep models & prompt)"
										color="amber"
										pulse={phase === "finished" || phase === "voting"}
									/>
								)}
								{/* Full reset: clear everything */}
								<ActionIconButton
									icon={RotateCcw}
									onClick={() => setPendingFullReset(true)}
									title="Reset all (clear models & prompt)"
									color="red"
								/>
							</>
						)}
						<CollapsibleToggle
							collapsed={arenaCollapsed}
							onToggle={() => setArenaCollapsed((c) => !c)}
						/>
					</div>
				</div>
				<div
					className={`grid transition-[grid-template-rows] duration-300 ease-in-out ${
						arenaCollapsed ? "grid-rows-[0fr]" : "grid-rows-[1fr]"
					}`}
				>
					<div className="overflow-hidden">
						<div className="space-y-4 pt-4">
							{phase === "setup" && arenaMode === "competition" && (
								<div>
									<label
										htmlFor="bracket-models-picker"
										className="text-sm font-semibold text-(--accent) mb-2 block"
									>
										Models ({bracketModels.length}/8)
										<span className="text-(--text-tertiary)">
											{" "}
											Pick 2, 4, or 8 for a bracket
										</span>
									</label>
									<ModelPicker
										id="bracket-models-picker"
										models={enabledModels}
										selected={bracketModels}
										onChange={setBracketModels}
										multi={true}
										maxSelections={8}
										align="left"
										slotParams={modelParams}
										onConfigureParams={setParamEditorModel}
										onRandom={handleRandomBracketModel}
										paramsReadonly={phase !== "setup"}
									/>
								</div>
							)}
							{phase === "setup" && arenaMode === "compare" && (
								<div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
									<div>
										<label
											htmlFor="compare-models-picker"
											className="text-sm font-semibold text-(--accent) mb-2 block"
										>
											Models ({compareModels.length}/6)
										</label>
										<ModelPicker
											id="compare-models-picker"
											models={enabledModels}
											selected={compareModels}
											onChange={setCompareModels}
											multi={true}
											maxSelections={6}
											align="left"
											slotParams={modelParams}
											onConfigureParams={setParamEditorModel}
											onRandom={handleRandomCompareModel}
											paramsReadonly={phase !== "setup"}
										/>
									</div>
									<div>
										<PersonaPicker
											personas={CHAT_PERSONAS}
											activePersonaId={comparePersonaId}
											systemPrompt={comparePersonaPrompt}
											onActivePersonaChange={setComparePersonaId}
											onSystemPromptChange={setComparePersonaPrompt}
											onRandom={handleRandomComparePersona}
											textareaPlaceholder="Optional system prompt applied to all models…"
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
								disabled={phase !== "setup" && phase !== "finished"}
							/>
						</div>
					</div>
				</div>
			</div>

			{/* Bracket + Run Bar */}
			<div className="ui-card p-4 shrink-0">
				<div className="flex items-center gap-4 flex-wrap">
					{/* Bracket Pills */}
					{/* Setup preview: show selected models and matchups before running */}
					{previewPairs && (
						<div className="flex flex-col gap-2 flex-1 min-w-0">
							<div className="flex items-center gap-2">
								<div className="text-xs text-(--text-tertiary) font-medium uppercase tracking-wider whitespace-nowrap">
									First Round
								</div>
								<div className="flex items-center gap-2 flex-wrap">
									{previewPairs.map((p, i) => (
										<div
											// biome-ignore lint/suspicious/noArrayIndexKey: preview position is stable for the static preview
											key={`preview-mu-${i}`}
											className="flex items-center gap-2"
										>
											<BracketPreviewPill modelId={p.a} isTbd={p.a === ""} />
											<span className="text-(--accent) font-bold text-xs px-1">
												VS
											</span>
											<BracketPreviewPill modelId={p.b} isTbd={p.b === ""} />
										</div>
									))}
								</div>
							</div>
						</div>
					)}
					{phase === "setup" &&
						arenaMode === "compare" &&
						compareModels.length > 0 && (
							<div className="flex flex-col gap-2 flex-1 min-w-0">
								<div className="flex items-center gap-2 flex-wrap">
									{compareModels.map((m, i) => (
										// biome-ignore lint/suspicious/noArrayIndexKey: preview list order matches model order
										<BracketPreviewPill key={`preview-cmp-${i}`} modelId={m} />
									))}
								</div>
							</div>
						)}
					{rounds.length > 0 && (
						<div className="flex flex-col gap-2 flex-1 min-w-0">
							{rounds.map((round, roundIdx) => {
								if (phase !== "setup" && roundIdx < currentRound) return null;
								if (phase === "finished" && roundIdx < rounds.length - 1)
									return null;
								return (
									<div
										// biome-ignore lint/suspicious/noArrayIndexKey: round index is the stable identifier for bracket rounds
										key={`round-${roundIdx}`}
										className={`flex items-center gap-2 transition-opacity duration-500 ${
											roundIdx > currentRound + 1 ||
											(roundIdx > currentRound && phase === "voting")
												? "opacity-30"
												: roundIdx > currentRound
													? "opacity-50"
													: "opacity-100"
										}`}
									>
										<div className="text-xs text-(--text-tertiary) font-medium uppercase tracking-wider whitespace-nowrap">
											{roundLabel(roundIdx, rounds.length)}
										</div>
										<div className="flex items-center gap-2 flex-wrap">
											{round.matchups.map((mu, matchupIdx) => (
												<div
													// biome-ignore lint/suspicious/noArrayIndexKey: matchup position within a round is the stable identifier
													key={`matchup-${roundIdx}-${matchupIdx}`}
													className="flex items-center gap-2"
												>
													<MatchupCard
														slot={mu.slotA}
														slotKey="A"
														roundIdx={roundIdx}
														matchupIdx={matchupIdx}
														vote={mu.vote}
														response={mu.responseA}
														isRunning={isRunning}
														phase={phase}
														onPersonaChange={handlePersonaChange}
														onVote={handleVote}
													/>
													{mu.slotB !== null && (
														<>
															<span className="text-(--accent) font-bold text-xs px-1">
																VS
															</span>
															<MatchupCard
																slot={mu.slotB}
																slotKey="B"
																roundIdx={roundIdx}
																matchupIdx={matchupIdx}
																vote={mu.vote}
																response={mu.responseB}
																isRunning={isRunning}
																phase={phase}
																onPersonaChange={handlePersonaChange}
																onVote={handleVote}
															/>
														</>
													)}
												</div>
											))}
										</div>
									</div>
								);
							})}
						</div>
					)}

					{/* Run Button */}
					<div className="flex flex-col">
						{buttonLabel && (
							<button
								type="button"
								onClick={isRunning ? handleStopAll : handleRunArena}
								disabled={phase === "setup" && !canRun}
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
						{phase === "setup" && !canRun && disabledReason && (
							<p className="text-xs text-amber-400 mt-1.5">{disabledReason}</p>
						)}
						{phase === "running" && (
							<p className="text-xs text-(--text-muted) mt-1.5">
								<span className="w-1.5 h-1.5 rounded-full bg-(--accent) animate-pulse inline-block mr-1.5 align-middle" />
								Models are generating - click Stop to cancel
							</p>
						)}
						{phase === "voting" && (
							<p className="text-xs text-amber-400 mt-1.5">
								Vote on all matchups to continue to the next round
							</p>
						)}
						{phase === "next_round_ready" && !canRun && (
							<p className="text-xs text-amber-400 mt-1.5">
								{disabledReason || "Start the next round when ready"}
							</p>
						)}
					</div>
				</div>

				{/* Mode Description */}
				<p className="text-xs text-(--text-tertiary) leading-snug line-clamp-3 mt-3">
					{arenaMode === "competition"
						? "Models compete in a single-elimination bracket. Pick 2, 4, or 8 models - each round, pairs face the same prompt and you vote for the better response. Winners advance until one model remains."
						: "Pick models and run the same prompt through them simultaneously. No voting, no bracket - just pure side-by-side output comparison to evaluate which model best fits your needs."}
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
							r.matchups.some((mu) => mu.responseA || mu.responseB),
					);
					if (laterRoundHasResponses) return null;
					const isCompare =
						arenaMode === "compare" &&
						round.matchups.every((m) => m.slotB === null);
					return (
						// biome-ignore lint/suspicious/noArrayIndexKey: round index is the stable identifier
						<div key={`resp-round-${roundIdx}`}>
							<div className="text-xs text-(--text-tertiary) font-medium uppercase tracking-wider mb-2">
								{isCompare ? "Responses" : roundLabel(roundIdx, rounds.length)}
							</div>
							<div
								className={`${
									isCompare
										? "flex flex-wrap justify-center gap-4 [&>*]:w-full [&>*]:md:w-[calc(50%-0.5rem)] [&>*]:xl:w-[calc(33.333%-0.67rem)]"
										: "space-y-4"
								} transition-opacity duration-500 ${
									roundIdx <= currentRound ? "opacity-100" : "opacity-20"
								}`}
							>
								{round.matchups.map((mu, matchupIdx) => {
									// Compare mode: flat grid of individual cards
									if (isCompare) {
										return (
											<div
												// biome-ignore lint/suspicious/noArrayIndexKey: matchup position is the stable identifier in compare mode
												key={`compare-${roundIdx}-${matchupIdx}`}
												className="rounded-xl border border-(--border-subtle) bg-(--surface)/50 p-4 min-h-[22rem]"
											>
												{mu.slotA === null && roundIdx === currentRound ? (
													<SwapPicker
														enabledModels={enabledModels}
														disabledModels={disabledModels}
														alreadyUsed={round.matchups.flatMap((m, mi) => {
															if (mi === matchupIdx) return [];
															const ids: string[] = [];
															if (m.slotA) ids.push(m.slotA.modelId);
															return ids;
														})}
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
															response={mu.responseA}
															vote={mu.vote}
															slotKey="A"
															roundIdx={roundIdx}
															matchupIdx={matchupIdx}
															onVote={handleVote}
															onRetry={handleRetrySlot}
															onSwapModel={handleSwapModel}
															onCancelSlot={handleCancelSlot}
															enabledModels={enabledModels}
															showVote={false}
															params={mu.slotA?.params}
														/>
													)
												)}
											</div>
										);
									}

									// Competition mode: A-vs-B pairs
									return (
										<div
											// biome-ignore lint/suspicious/noArrayIndexKey: matchup position is the stable identifier in competition mode
											key={`comp-${roundIdx}-${matchupIdx}`}
											className="rounded-xl border border-(--border-subtle) bg-(--surface)/50 p-4"
										>
											{round.matchups.length > 1 && (
												<div className="text-xs text-(--text-tertiary) font-medium uppercase tracking-wider mb-3">
													Match {matchupIdx + 1}
												</div>
											)}
											<div className="grid grid-cols-1 md:grid-cols-2 gap-4">
												{mu.slotA === null && roundIdx === currentRound ? (
													<SwapPicker
														enabledModels={enabledModels}
														disabledModels={disabledModels}
														alreadyUsed={[
															...round.matchups.flatMap((m, mi) => {
																if (mi === matchupIdx) return [];
																const ids: string[] = [];
																if (m.slotA) ids.push(m.slotA.modelId);
																if (m.slotB) ids.push(m.slotB.modelId);
																return ids;
															}),
															...(mu.slotB ? [mu.slotB.modelId] : []),
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
															response={mu.responseA}
															vote={mu.vote}
															slotKey="A"
															roundIdx={roundIdx}
															matchupIdx={matchupIdx}
															onVote={handleVote}
															onRetry={handleRetrySlot}
															onSwapModel={handleSwapModel}
															onCancelSlot={handleCancelSlot}
															enabledModels={enabledModels}
															showVote={
																roundIdx <= currentRound &&
																mu.responseA.done &&
																(!mu.responseB || mu.responseB.done)
															}
															params={mu.slotA?.params}
														/>
													)
												)}
												{mu.slotB === null && roundIdx === currentRound ? (
													<SwapPicker
														enabledModels={enabledModels}
														disabledModels={disabledModels}
														alreadyUsed={[
															...round.matchups.flatMap((m, mi) => {
																if (mi === matchupIdx) return [];
																const ids: string[] = [];
																if (m.slotA) ids.push(m.slotA.modelId);
																if (m.slotB) ids.push(m.slotB.modelId);
																return ids;
															}),
															...(mu.slotA ? [mu.slotA.modelId] : []),
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
															response={mu.responseB}
															vote={mu.vote}
															slotKey="B"
															roundIdx={roundIdx}
															matchupIdx={matchupIdx}
															onVote={handleVote}
															onRetry={handleRetrySlot}
															onSwapModel={handleSwapModel}
															onCancelSlot={handleCancelSlot}
															enabledModels={enabledModels}
															showVote={
																roundIdx <= currentRound &&
																mu.responseB.done &&
																(!mu.responseA || mu.responseA.done)
															}
															params={mu.slotB?.params}
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
							localStorage.removeItem("arenaCompetitionActivePromptId");
							localStorage.removeItem("arenaCompareActivePromptId");
							localStorage.removeItem("arenaComparePersonaId");
							localStorage.removeItem("arenaComparePersonaPrompt");
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
					knownProviders={enabledModels.map((m) => m.provider_name)}
				/>
			)}

			{/* Match History Modal */}
			{showHistoryModal && (
				<ArenaHistoryModal onClose={() => setShowHistoryModal(false)} />
			)}
		</div>
	);
}
