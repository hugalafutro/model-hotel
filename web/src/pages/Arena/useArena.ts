import { produce } from "immer";
import { GitCompare, Swords } from "lucide-react";
import { useCallback, useMemo } from "react";
import { API_BASE, getAuthHeaders } from "../../api/client";
import type { GenerationParams } from "../../api/types";
import {
	getArenaHistoryEnabled,
	saveCompetitionToHistory,
} from "../../utils/arenaHistory";
import { providerFromModelID } from "../../utils/model";
import { hasAnyParam } from "../../utils/params";
import { readSSEStream, type StreamChunk } from "../../utils/sse";
import { fetchWithRetry, staggerByProvider } from "../../utils/stagger";
import {
	extractThinking,
	sanitizeDelta,
	shouldReExtract,
} from "../../utils/thinking";
import { getRoundLabel } from "./builders";
import type {
	ArenaResponse,
	BracketRound,
	Matchup,
	MatchupSlot,
} from "./types";
import { useArenaState } from "./useArenaState";

/**
 * Initialize matchup response objects with empty ArenaResponse.
 * Returns a function suitable for use with Array.map().
 */
function initMatchupResponses(
	now: number,
): (mu: Matchup) => Matchup {
	return (mu: Matchup) => ({
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
	});
}

/**
 * Collect all slots from a round's matchups for streaming.
 */
function collectSlots(round: BracketRound): Array<{
	modelId: string;
	personaPrompt: string;
	slotKey: "A" | "B";
	matchupIdx: number;
	params?: GenerationParams;
}> {
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
	return slots;
}

/**
 * Stagger slots by provider and dispatch with optional delay.
 */
function staggerAndDispatch(
	slots: Array<{
		modelId: string;
		personaPrompt: string;
		slotKey: "A" | "B";
		matchupIdx: number;
		params?: GenerationParams;
	}>,
	knownProviders: string[],
	dispatch: (slot: (typeof slots)[number]) => void,
) {
	const staggered = staggerByProvider(
		slots,
		(s) => providerFromModelID(s.modelId, knownProviders),
		300,
	);
	for (const { item, delayMs } of staggered) {
		if (delayMs > 0) {
			setTimeout(() => dispatch(item), delayMs);
		} else {
			dispatch(item);
		}
	}
}

export function useArena() {
	const {
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
	} = useArenaState();

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
					const tokensPerSecond =
						completionTokens > 0 && durationMs > 0
							? completionTokens / (durationMs / 1000)
							: null;

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
										tokensPerSecond,
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
					const errorDurationMs = Math.round(performance.now() - startTime);
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
										tokensPerSecond:
											completionTokens > 0 && errorDurationMs > 0
												? completionTokens / (errorDurationMs / 1000)
												: null,
										durationMs: errorDurationMs,
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
		[toast, setRunningModels, setPhase, setRounds],
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

			const now = Date.now();
			setRounds(
				produce((draft: BracketRound[]) => {
					if (draft[roundIdx]) {
						draft[roundIdx].matchups = draft[roundIdx].matchups.map(
							initMatchupResponses(now),
						);
					}
				}),
			);

			const slots = collectSlots(round);
			const knownProviders = enabledModels.map((m) => m.provider_name);
			staggerAndDispatch(slots, knownProviders, (item) =>
				streamModel(
					item.modelId,
					item.personaPrompt,
					currentPrompt,
					roundIdx,
					item.slotKey,
					item.matchupIdx,
					item.params,
				),
			);
		},
		[savedPrompt, prompt, streamModel, enabledModels, setRounds, setPhase, setRunningModels],
	);

	const handleRunArena = useCallback(() => {
		if (!canRun) return;

		const currentPrompt = prompt.trim();
		setSavedPrompt(currentPrompt);

		const initialRounds =
			arenaMode === "compare"
				? buildCompareRoundWithParams(
						compareModels,
						comparePersonaId,
						comparePersonaPrompt,
					)
				: buildInitialRoundsWithParams(bracketModels);
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

		const now = Date.now();
		setRounds(
			produce((draft: BracketRound[]) => {
				if (draft[0]) {
					draft[0].matchups = draft[0].matchups.map(
						initMatchupResponses(now),
					);
				}
			}),
		);

		const slots = collectSlots(initialRounds[0]);
		const knownProviders = enabledModels.map((m) => m.provider_name);
		staggerAndDispatch(slots, knownProviders, (item) =>
			streamModel(
				item.modelId,
				item.personaPrompt,
				currentPrompt,
				0,
				item.slotKey,
				item.matchupIdx,
				item.params,
			),
		);
	}, [
		canRun,
		prompt,
		arenaMode,
		compareModels,
		comparePersonaId,
		comparePersonaPrompt,
		bracketModels,
		buildInitialRoundsWithParams,
		buildCompareRoundWithParams,
		streamModel,
		enabledModels,
		setSavedPrompt,
		currentRoundRef,
		setPhase,
		setRounds,
		setRunningModels,
		setCurrentRound,
		roundsLengthRef,
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
						draft[roundIdx].matchups.every((m: Matchup) => m.vote !== null)
					) {
						if (roundIdx < draft.length - 1) {
							shouldAdvance = true;
							advanceRoundIdx = roundIdx;

							const winners = draft[roundIdx].matchups.map((m: Matchup) =>
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
		[
			runRound,
			setPhase,
			currentRoundRef,
			setRounds,
			setWinnerModal,
			setCurrentRound,
			roundsRef,
		],
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
	}, [setPhase, setRunningModels, setRounds]);

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
		[rounds, savedPrompt, streamModel, setRunningModels, setRounds, setPhase],
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
		[setRounds, setDisabledModels],
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
		[setRunningModels, setRounds],
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
		[
			savedPrompt,
			streamModel,
			modelParams,
			setRunningModels,
			setRounds,
			setPhase,
		],
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
		[setRounds],
	);

	const isRunning = runningModels.size > 0;

	const arenaIcon = arenaMode === "competition" ? Swords : GitCompare;

	const buttonLabel = useMemo(() => {
		if (isRunning) return "Stop";
		if (phase === "setup") return "Run Arena";
		return null;
	}, [isRunning, phase]);

	const showResponseGrid = phase !== "setup";

	const roundLabel = (roundIdx: number, totalRounds: number): string =>
		getRoundLabel(roundIdx, totalRounds, arenaMode);

	return {
		// State values
		compareModels,
		bracketModels,
		competitionActivePromptId,
		compareActivePromptId,
		competitionPrompt,
		comparePrompt,
		rounds,
		currentRound,
		phase,
		runningModels,
		winnerModal,
		disabledModels,
		arenaCollapsed,
		pendingFullReset,
		showHistoryModal,
		modelParams,
		paramEditorModel,
		comparePersonaId,
		comparePersonaPrompt,
		activePromptId,
		prompt,
		arenaMode,
		savedPrompt,
		// State setters
		setCompareModels,
		setBracketModels,
		setCompetitionActivePromptId,
		setCompareActivePromptId,
		setCompetitionPrompt,
		setComparePrompt,
		setRounds,
		setCurrentRound,
		setPhase,
		setRunningModels,
		setWinnerModal,
		setDisabledModels,
		setArenaCollapsed,
		setPendingFullReset,
		setShowHistoryModal,
		setModelParams,
		setParamEditorModel,
		setComparePersonaId,
		setComparePersonaPrompt,
		setSavedPrompt,
		setArenaMode,
		// Computed values
		canRun,
		disabledReason,
		previewPairs,
		buttonLabel,
		arenaIcon,
		isRunning,
		showResponseGrid,
		// Callback handlers
		handleRunArena,
		handleVote,
		handleStopAll,
		handleRetrySlot,
		handleSwapModel,
		handleCancelSlot,
		handleSwapComplete,
		handlePersonaChange,
		handleRandomComparePersona,
		handleRandomBracketModel,
		handleRandomCompareModel,
		setPrompt,
		setActivePromptId,
		// Refs
		abortMapRef,
		// Helpers
		roundLabel,
		// Internal dependencies exposed for JSX
		enabledModels,
		toast,
	};
}
