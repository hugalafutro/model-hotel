import i18next from "i18next";
import { produce } from "immer";
import { useCallback, useEffect, useMemo, useRef } from "react";
import { GitCompare, Swords } from "@/lib/icons";
import {
	getArenaHistoryEnabled,
	saveCompetitionToHistory,
} from "../../utils/arenaHistory";
import { getRoundLabel } from "./builders";
import type { BracketRound, Matchup, MatchupSlot } from "./types";
import { useArenaRunner } from "./useArenaRunner";
import { useArenaState } from "./useArenaState";
import {
	collectSlots,
	initMatchupResponses,
	staggerAndDispatch,
} from "./utils";

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

	const {
		streamModel,
		runRound,
		handleStopAll,
		handleRetry: handleRetrySlot,
		handleCancelSlot,
		handleSwapComplete,
		abortMapRef,
	} = useArenaRunner({
		arenaModeRef,
		savedPrompt,
		prompt,
		setRounds,
		setPhase,
		setRunningModels,
		rounds,
		roundsRef,
		modelParams,
		enabledModels,
		toast,
	});

	// Correct stale "voting" phase after page reload.
	// When rounds and phase are persisted independently, the page may reload
	// with phase="voting" even though all matchups already have votes.
	const phaseCorrectedRef = useRef(false);
	useEffect(() => {
		if (phaseCorrectedRef.current) return;
		if (phase !== "voting") {
			phaseCorrectedRef.current = true;
			return;
		}
		const round = rounds[currentRound];
		if (!round) {
			phaseCorrectedRef.current = true;
			return;
		}
		if (!round.matchups.every((m: Matchup) => m.vote !== null)) {
			phaseCorrectedRef.current = true;
			return;
		}

		phaseCorrectedRef.current = true;

		if (currentRound >= rounds.length - 1) {
			// Last round — declare winner
			const finalMu = round.matchups[0];
			const winner =
				finalMu?.vote === "A" ? finalMu.slotA?.modelId : finalMu.slotB?.modelId;
			if (winner) {
				setWinnerModal({ winner, rounds });
			}
			setPhase("finished");
		} else {
			// Not last round — build next round matchups and advance
			const nextRounds = produce(rounds, (draft) => {
				const winners = draft[currentRound].matchups.map((m: Matchup) =>
					m.vote === "A" ? m.slotA : m.slotB,
				);
				const nextRI = currentRound + 1;
				if (draft[nextRI]) {
					for (let i = 0; i < winners.length; i += 2) {
						const muIdx = i / 2;
						draft[nextRI].matchups[muIdx] = {
							slotA: winners[i] ? { ...(winners[i] as MatchupSlot) } : null,
							slotB: winners[i + 1]
								? { ...(winners[i + 1] as MatchupSlot) }
								: null,
							responseA: null,
							responseB: null,
							vote: null,
						};
					}
				}
			});
			setRounds(nextRounds);
			roundsRef.current = nextRounds;
			setCurrentRound(currentRound + 1);
			currentRoundRef.current = currentRound + 1;
			setPhase("next_round_ready");
		}
	}, [
		phase,
		rounds,
		currentRound,
		setPhase,
		setRounds,
		setWinnerModal,
		setCurrentRound,
		roundsRef,
		currentRoundRef,
	]);

	// Tracks which model is being swapped out so bracketModels can be updated
	const swapOutMapRef = useRef<Map<string, string>>(new Map());

	// Wrap handleSwapComplete to also update bracketModels with the replacement
	const handleSwapCompleteAndUpdate = useCallback(
		(
			roundIdx: number,
			matchupIdx: number,
			slotKey: "A" | "B",
			newModelId: string,
		) => {
			const key = `${roundIdx}-${matchupIdx}-${slotKey}`;
			const oldModelId = swapOutMapRef.current.get(key);
			swapOutMapRef.current.delete(key);

			if (oldModelId) {
				setBracketModels((prev) =>
					prev.map((id) => (id === oldModelId ? newModelId : id)),
				);
			}

			handleSwapComplete(roundIdx, matchupIdx, slotKey, newModelId);
		},
		[handleSwapComplete, setBracketModels],
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
					draft[0].matchups = draft[0].matchups.map(initMatchupResponses(now));
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

			const nextRounds = produce(roundsRef.current, (draft) => {
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
									slotA: winners[i] ? { ...(winners[i] as MatchupSlot) } : null,
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
			});

			setRounds(nextRounds);
			roundsRef.current = nextRounds;

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
			activePromptIdRef,
		],
	);

	const handleSwapModel = useCallback(
		(
			roundIdx: number,
			matchupIdx: number,
			slotKey: "A" | "B",
			failedModelId: string,
		) => {
			setDisabledModels((prev) => new Set(prev).add(failedModelId));

			// Track which model is being swapped out so bracketModels can be updated
			swapOutMapRef.current.set(
				`${roundIdx}-${matchupIdx}-${slotKey}`,
				failedModelId,
			);

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
		if (isRunning) return i18next.t("arena.button.stop");
		if (phase === "setup") return i18next.t("arena.button.run");
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
		handleSwapCompleteAndUpdate,
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
