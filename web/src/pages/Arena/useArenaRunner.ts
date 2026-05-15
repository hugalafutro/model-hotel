import { produce } from "immer";
import { useCallback, useRef } from "react";
import { API_BASE, getAuthHeaders } from "../../api/client";
import type { GenerationParams } from "../../api/types";
import type { ArenaSubMode } from "../../context/SidebarModeContext";
import type { useToast } from "../../context/ToastContext";
import { hasAnyParam } from "../../utils/params";
import { readSSEStream, type StreamChunk } from "../../utils/sse";
import { fetchWithRetry } from "../../utils/stagger";
import { extractThinking, sanitizeDelta } from "../../utils/thinking";
import type { ArenaResponse, BracketRound } from "./types";
import {
	collectSlots,
	initMatchupResponses,
	staggerAndDispatch,
} from "./utils";

export interface ArenaRunnerDeps {
	arenaModeRef: React.MutableRefObject<ArenaSubMode>;
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
	roundsRef: React.MutableRefObject<BracketRound[]>;
	modelParams: Record<string, GenerationParams>;
	enabledModels: Array<{ provider_name: string; model_id: string }>;
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
	abortMapRef: React.MutableRefObject<Map<string, AbortController>>;
}

export function useArenaRunner(deps: ArenaRunnerDeps): ArenaRunner {
	const {
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
	} = deps;

	const abortMapRef = useRef<Map<string, AbortController>>(new Map());

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
				const startTime = performance.now();
				let promptTokens = 0;
				let completionTokens = 0;

				const chatMessages: Array<{ role: string; content: string }> = [];
				if (personaPrompt.trim()) {
					chatMessages.push({ role: "system", content: personaPrompt.trim() });
				}
				chatMessages.push({ role: "user", content: userPrompt });

				try {
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
							onRetry: (
								attempt: number,
								delayMs: number,
								status?: number | string,
							) => {
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
											const extracted = extractThinking(newRaw);
											const nextContent = extracted.content;
											const nextThinking =
												extracted.thinking || resp.thinkingContent;
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
													(mu[respKey]?.thinkingContent ?? "") + thinkingDelta,
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
					abortMapRef.current.delete(model);
				}
			};

			run();
		},
		[toast, setRunningModels, setPhase, setRounds, arenaModeRef],
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
		[
			savedPrompt,
			prompt,
			streamModel,
			enabledModels,
			setRounds,
			setPhase,
			setRunningModels,
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
	}, [setPhase, setRunningModels, setRounds, arenaModeRef]);

	const handleRetry = useCallback(
		(roundIdx: number, matchupIdx: number, slotKey: "A" | "B") => {
			const round = rounds[roundIdx];
			if (!round) return;
			const mu = round.matchups[matchupIdx];
			if (!mu) return;
			const slot = slotKey === "A" ? mu.slotA : mu.slotB;
			if (!slot) return;

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
		[rounds, savedPrompt, streamModel, setRounds, setRunningModels, setPhase],
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
