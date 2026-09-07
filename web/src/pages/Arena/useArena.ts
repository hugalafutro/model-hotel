import i18next from "i18next";
import { produce } from "immer";
import { useCallback, useEffect, useMemo, useRef } from "react";
import { GitCompare, Swords } from "@/lib/icons";
import {
	getArenaHistoryEnabled,
	saveCompetitionToHistory,
} from "../../utils/arenaHistory";
import { advanceWinners, getRoundLabel, roundWinner } from "./builders";
import type { Matchup, MatchupSlot } from "./types";
import { useArenaRunner } from "./useArenaRunner";
import { ARENA_STORAGE_KEYS, useArenaState } from "./useArenaState";
import { clearSlot } from "./utils";

export function useArena() {
	const {
		// State values
		compareModels,
		setCompareModels,
		bracketModels,
		setBracketModels,
		setCompetitionActivePromptId,
		setCompareActivePromptId,
		setCompetitionPrompt,
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
		modelsReady,
		toast,
	} = useArenaState();

	const {
		runRound,
		handleStopAll,
		handleRetry: handleRetrySlot,
		handleCancelSlot,
		handleSwapComplete,
		abortAll,
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
		modelsReady,
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
			const winner = roundWinner(round);
			if (winner) {
				setWinnerModal({ winner, rounds });
			}
			setPhase("finished");
		} else {
			// Not last round — build next round matchups and advance
			const nextRounds = produce(rounds, (draft) => {
				advanceWinners(draft, currentRound);
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

	// Resume a run that was deferred because the chat model list wasn't usable
	// yet. A run can be marked "running" (initial dispatch or a vote-advanced
	// round) while the allowlist is still loading or empty, in which case the
	// runner defers every slot and nothing is actually streaming. Once the list
	// settles we un-stick the round so it can't stay in "running" with
	// done:false slots and no active stream forever:
	//   - list settled with chat models -> re-dispatch the current round;
	//   - list settled with NO chat models -> the run can't proceed, so drop it
	//     back to "setup" (the user has to add a provider before it can run).
	//
	// The re-dispatch fires only on the not-usable -> usable transition (not on
	// every render while usable) so it never races the normal staggered
	// dispatch. The "previous" ref starts at false (not the current value) so the
	// first eligible render counts as such a transition: a saved "running" round
	// reloaded with a warm model cache (usable on the very first render) is
	// recovered too, not only a later false->true transition. A normal in-session
	// run start happens after mount, by which point the ref is already true, so it
	// is still not double-dispatched. The setup fallback is condition-gated (an
	// empty list may already be settled on mount, with no transition to observe)
	// and self-limits: once the phase leaves "running" it can't fire again.
	const hasUsableAllowlist = modelsReady && enabledModels.length > 0;
	const prevUsableAllowlistRef = useRef(false);
	useEffect(() => {
		const wasUsable = prevUsableAllowlistRef.current;
		prevUsableAllowlistRef.current = hasUsableAllowlist;
		if (phase !== "running") return;
		// Something is genuinely streaming -> not a deferred run.
		if (abortMapRef.current.size > 0) return;
		const round = rounds[currentRound];
		if (!round) return;
		const hasPendingSlot = round.matchups.some(
			(m: Matchup) =>
				(m.slotA && !m.responseA?.done) || (m.slotB && !m.responseB?.done),
		);
		if (!hasPendingSlot) return;

		if (modelsReady && enabledModels.length === 0) {
			// Settled with no chat-capable models: the run can't proceed.
			setPhase("setup");
			return;
		}
		if (!wasUsable && hasUsableAllowlist) {
			runRound(currentRound);
		}
	}, [
		hasUsableAllowlist,
		modelsReady,
		enabledModels,
		phase,
		rounds,
		currentRound,
		runRound,
		setPhase,
		abortMapRef,
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
		roundsRef.current = initialRounds;
		currentRoundRef.current = 0;
		setCurrentRound(0);
		// Marked running before the round is dispatched so a run started while
		// the chat model list is still loading is recovered by the deferred-run
		// effect above once the list settles.
		setPhase("running");
		// The prompt is passed explicitly: `savedPrompt` has not re-rendered yet.
		runRound(0, currentPrompt);
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
		runRound,
		setSavedPrompt,
		currentRoundRef,
		roundsRef,
		setPhase,
		setRounds,
		setCurrentRound,
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
						advanceWinners(draft, roundIdx);
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
				const winner = finalRound ? roundWinner(finalRound) : undefined;
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
					clearSlot(draft, roundIdx, matchupIdx, slotKey);
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

	/** Drops the board and every result, keeping models, prompt and persona. */
	const clearResults = useCallback(() => {
		abortAll();
		setRounds([]);
		setCurrentRound(0);
		setPhase("setup");
		setRunningModels(new Set());
		setWinnerModal(null);
		setDisabledModels(new Set());
	}, [
		abortAll,
		setRounds,
		setCurrentRound,
		setPhase,
		setRunningModels,
		setWinnerModal,
		setDisabledModels,
	]);

	/** Clears the board and the whole set-up: models, prompts, persona, params. */
	const resetAll = useCallback(() => {
		clearResults();
		setCompareModels([]);
		setBracketModels([]);
		setCompetitionPrompt("");
		setComparePrompt("");
		setSavedPrompt("");
		setCompetitionActivePromptId(null);
		setCompareActivePromptId(null);
		setComparePersonaId(null);
		setComparePersonaPrompt("");
		setModelParams({});
		// The write-through setters above only reach disk while persistence is
		// on, so the keys are removed directly: a reset must not leave a board
		// that comes back when persistence is switched on again.
		try {
			for (const key of ARENA_STORAGE_KEYS) localStorage.removeItem(key);
		} catch {
			/* a storage that refuses removal has nothing to resurrect either */
		}
	}, [
		clearResults,
		setCompareModels,
		setBracketModels,
		setCompetitionPrompt,
		setComparePrompt,
		setSavedPrompt,
		setCompetitionActivePromptId,
		setCompareActivePromptId,
		setComparePersonaId,
		setComparePersonaPrompt,
		setModelParams,
	]);

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
		rounds,
		currentRound,
		phase,
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
		clearResults,
		resetAll,
		setPrompt,
		setActivePromptId,
		// Helpers
		roundLabel,
		// Internal dependencies exposed for JSX
		enabledModels,
		toast,
	};
}

/** Everything the Arena page and its sections read. */
export type ArenaView = ReturnType<typeof useArena>;
