import { produce } from "immer";
import { useCallback, useEffect, useMemo, useRef } from "react";
import { useTranslation } from "react-i18next";
import type { GenerationParams } from "../../api/types";
import type { ArenaSubMode } from "../../context/SidebarModeContext";
import type { useToast } from "../../context/ToastContext";
import { proxyModelID } from "../../utils/model";
import { streamArenaResponse } from "./streamModel";
import type { BracketRound } from "./types";
import {
	collectSlots,
	initMatchupResponses,
	staggerAndDispatch,
} from "./utils";

export interface ArenaRunnerDeps {
	arenaModeRef: React.RefObject<ArenaSubMode>;
	savedPrompt: string;
	prompt: string;
	setRounds: React.Dispatch<React.SetStateAction<BracketRound[]>>;
	setPhase: React.Dispatch<
		React.SetStateAction<
			"setup" | "running" | "voting" | "next_round_ready" | "finished"
		>
	>;
	setRunningModels: React.Dispatch<React.SetStateAction<Set<string>>>;
	rounds: BracketRound[];
	roundsRef: React.RefObject<BracketRound[]>;
	modelParams: Record<string, GenerationParams>;
	enabledModels: Array<{ provider_name: string; model_id: string }>;
	/** False while the chat model list is still doing its first load. */
	modelsReady: boolean;
	toast: ReturnType<typeof useToast>["toast"];
}

export interface ArenaRunner {
	streamModel: (
		model: string,
		personaPrompt: string,
		userPrompt: string,
		roundIdx: number,
		slotKey: "A" | "B",
		matchupIdx: number,
		slotParams?: GenerationParams,
	) => void;
	runRound: (roundIdx: number) => void;
	handleStopAll: () => void;
	handleRetry: (
		roundIdx: number,
		matchupIdx: number,
		slotKey: "A" | "B",
	) => void;
	handleCancelSlot: (
		roundIdx: number,
		matchupIdx: number,
		slotKey: "A" | "B",
		modelId: string,
	) => void;
	handleSwapComplete: (
		roundIdx: number,
		matchupIdx: number,
		slotKey: "A" | "B",
		newModelId: string,
	) => void;
	abortMapRef: React.RefObject<Map<string, AbortController>>;
}

export function useArenaRunner(deps: ArenaRunnerDeps): ArenaRunner {
	const {
		arenaModeRef,
		savedPrompt,
		prompt,
		setRounds: setRoundsRaw,
		setPhase: setPhaseRaw,
		setRunningModels: setRunningModelsRaw,
		rounds,
		roundsRef,
		modelParams,
		enabledModels,
		modelsReady,
		toast,
	} = deps;

	const { t } = useTranslation();

	const abortMapRef = useRef<Map<string, AbortController>>(new Map());

	// Once the Arena unmounts, any still-in-flight stream must stop touching
	// React state: a late setState throws under jsdom teardown ("window is not
	// defined") and leaks work in production. Flip a mounted flag on unmount,
	// abort every in-flight request, and gate the setters through it so the
	// streaming body in streamModel.ts, which receives them on its context,
	// never dispatches into a dead tree.
	const mountedRef = useRef(true);
	useEffect(() => {
		// abortMapRef.current is stable for the component's lifetime (the Map is
		// created once), but capture it so the cleanup reads the same instance.
		const abortMap = abortMapRef.current;
		return () => {
			mountedRef.current = false;
			for (const ctrl of abortMap.values()) ctrl.abort();
			abortMap.clear();
		};
	}, []);

	const setRounds = useCallback<typeof setRoundsRaw>(
		(update) => {
			if (mountedRef.current) setRoundsRaw(update);
		},
		[setRoundsRaw],
	);
	const setPhase = useCallback<typeof setPhaseRaw>(
		(update) => {
			if (mountedRef.current) setPhaseRaw(update);
		},
		[setPhaseRaw],
	);
	const setRunningModels = useCallback<typeof setRunningModelsRaw>(
		(update) => {
			if (mountedRef.current) setRunningModelsRaw(update);
		},
		[setRunningModelsRaw],
	);

	// The ids the picker/random actions can currently produce. enabledModels is
	// already the chat-filtered list, so anything outside it is a non-chat model
	// (reclassified to embedding/rerank, or disabled) that must not be dispatched.
	const validModelIds = useMemo(
		() =>
			new Set(
				enabledModels.map((m) => proxyModelID(m.provider_name, m.model_id)),
			),
		[enabledModels],
	);

	// A usable allowlist means the list has settled AND has at least one chat
	// model to judge against. An empty / failed list is not authoritative, so we
	// neither dispatch nor mutate round state against it (that would erase a
	// maybe-valid persisted competition); we wait for a real list instead.
	const hasUsableAllowlist = modelsReady && validModelIds.size > 0;

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
			// A persisted competition can reload (outside setup phase, so array
			// reconciliation is skipped) with a round slot pointing at a model that
			// is no longer a valid chat target. Never stream a chat request to a
			// non-chat endpoint.
			if (!validModelIds.has(model)) {
				// No usable allowlist to classify against: bail without touching the
				// response, running set, or phase, so a maybe-valid persisted round is
				// never erased (the run initiators also refuse to start without one).
				if (!hasUsableAllowlist) return;
				// Genuine non-chat model: stamp the slot errored and clear it from the
				// run.
				const respKey = slotKey === "A" ? "responseA" : "responseB";
				setRounds(
					produce((draft) => {
						const mu = draft[roundIdx]?.matchups[matchupIdx];
						if (mu) {
							mu[respKey] = {
								model,
								rawContent: "",
								content: "",
								thinkingContent: "",
								startTimeMs: Date.now(),
								done: true,
								error: t("hooks.useArenaRunner.nonChatModel"),
								metrics: null,
							};
						}
					}),
				);
				setRunningModels((prev) => {
					const next = new Set(prev);
					next.delete(model);
					if (next.size === 0) {
						setPhase(
							arenaModeRef.current === "compare" ? "finished" : "voting",
						);
					}
					return next;
				});
				return;
			}

			const abortCtrl = new AbortController();
			abortMapRef.current.set(model, abortCtrl);

			void streamArenaResponse(
				{
					t,
					toast,
					setRounds,
					setPhase,
					setRunningModels,
					arenaModeRef,
					abortMapRef,
					mountedRef,
				},
				{
					model,
					personaPrompt,
					userPrompt,
					roundIdx,
					slotKey,
					matchupIdx,
					slotParams,
					abortCtrl,
				},
			);
		},
		[
			t,
			toast,
			setRunningModels,
			setPhase,
			setRounds,
			arenaModeRef,
			validModelIds,
			hasUsableAllowlist,
		],
	);

	const runRound = useCallback(
		(roundIdx: number) => {
			const round = roundsRef.current[roundIdx];
			if (!round) return;
			// Don't start a round without a usable allowlist: an empty / not-yet-
			// loaded list would defer every slot and could erase the round. Leave
			// state untouched so it can be started once a real list arrives.
			if (!hasUsableAllowlist) return;

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
		[
			savedPrompt,
			prompt,
			streamModel,
			enabledModels,
			setRounds,
			setPhase,
			setRunningModels,
			roundsRef,
			hasUsableAllowlist,
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
	}, [setPhase, setRunningModels, setRounds, arenaModeRef]);

	const handleRetry = useCallback(
		(roundIdx: number, matchupIdx: number, slotKey: "A" | "B") => {
			const round = rounds[roundIdx];
			if (!round) return;
			const mu = round.matchups[matchupIdx];
			if (!mu) return;
			const slot = slotKey === "A" ? mu.slotA : mu.slotB;
			if (!slot) return;
			// Same as runRound: don't retry a slot without a usable allowlist.
			if (!hasUsableAllowlist) return;

			const respKey = slotKey === "A" ? "responseA" : "responseB";
			setRounds(
				produce((draft) => {
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
		[
			rounds,
			savedPrompt,
			streamModel,
			setRounds,
			setRunningModels,
			setPhase,
			hasUsableAllowlist,
		],
	);

	const handleCancelSlot = useCallback(
		(
			roundIdx: number,
			matchupIdx: number,
			slotKey: "A" | "B",
			modelId: string,
		) => {
			const ctrl = abortMapRef.current.get(modelId);
			if (ctrl) {
				ctrl.abort();
				abortMapRef.current.delete(modelId);
			}
			setRunningModels((prev) => {
				const next = new Set(prev);
				next.delete(modelId);
				if (next.size === 0) {
					setPhase(arenaModeRef.current === "compare" ? "finished" : "voting");
				}
				return next;
			});

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
		[setRunningModels, setRounds, setPhase, arenaModeRef],
	);

	const handleSwapComplete = useCallback(
		(
			roundIdx: number,
			matchupIdx: number,
			slotKey: "A" | "B",
			newModelId: string,
		) => {
			// The swap picker only offers loaded chat models, but guard anyway so a
			// swap during an empty/loading window doesn't wedge the slot.
			if (!hasUsableAllowlist) return;
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
			hasUsableAllowlist,
		],
	);

	return {
		streamModel,
		runRound,
		handleStopAll,
		handleRetry,
		handleCancelSlot,
		handleSwapComplete,
		abortMapRef,
	};
}
