import type { TFunction } from "i18next";
import { produce } from "immer";
import { API_BASE, getAuthHeaders } from "../../api/client";
import type { GenerationParams } from "../../api/types";
import { hasAnyParam } from "../../utils/params";
import { readSSEStream, type StreamChunk } from "../../utils/sse";
import { fetchWithRetry } from "../../utils/stagger";
import { extractThinking, sanitizeDelta } from "../../utils/thinking";
import type { ArenaResponse } from "./types";
import type { ArenaRunnerDeps } from "./useArenaRunner";

/** What one arena stream needs from the runner hook: mount-gated setters plus the mode and abort registries. */
export interface ArenaStreamContext
	extends Pick<
		ArenaRunnerDeps,
		"setRounds" | "setPhase" | "setRunningModels" | "arenaModeRef" | "toast"
	> {
	t: TFunction;
	abortMapRef: React.RefObject<Map<string, AbortController>>;
	mountedRef: React.RefObject<boolean>;
}

export interface ArenaStreamArgs {
	model: string;
	personaPrompt: string;
	userPrompt: string;
	roundIdx: number;
	slotKey: "A" | "B";
	matchupIdx: number;
	slotParams?: GenerationParams;
	abortCtrl: AbortController;
}

/**
 * Streams one model's answer into its matchup slot: POSTs the arena chat
 * request, folds SSE deltas (content, reasoning, usage) into the round state,
 * stamps the slot done with metrics or an error, and finally clears the model
 * from the running set, flipping the phase when it was the last one. The
 * caller registers `abortCtrl` in `abortMapRef` before calling.
 */
export async function streamArenaResponse(
	ctx: ArenaStreamContext,
	args: ArenaStreamArgs,
): Promise<void> {
	const {
		t,
		toast,
		setRounds,
		setPhase,
		setRunningModels,
		arenaModeRef,
		abortMapRef,
		mountedRef,
	} = ctx;
	const {
		model,
		personaPrompt,
		userPrompt,
		roundIdx,
		slotKey,
		matchupIdx,
		slotParams,
		abortCtrl,
	} = args;
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
						t("hooks.useArenaRunner.retry", {
							model,
							status: status || t("hooks.useArenaRunner.networkError"),
							attempt,
							delay: (delayMs / 1000).toFixed(1),
						}),
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
								const respKey = slotKey === "A" ? "responseA" : "responseB";
								const resp = mu[respKey] as ArenaResponse;
								const newRaw = resp.rawContent + clean;
								const extracted = extractThinking(newRaw);
								const nextContent = extracted.content;
								const nextThinking = extracted.thinking || resp.thinkingContent;
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
								const respKey = slotKey === "A" ? "responseA" : "responseB";
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
					? t("chat.stream.stalledTimeout")
					: t("chat.stream.cutoffIncomplete")
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
		const msg =
			err instanceof Error ? err.message : t("chat.stream.unknownError");
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
		if (mountedRef.current) {
			toast(
				t("hooks.useArenaRunner.generationError", { model, error: msg }),
				"error",
			);
		}
	} finally {
		setRunningModels((prev) => {
			const next = new Set(prev);
			next.delete(model);
			if (next.size === 0 && !abortCtrl.signal.aborted) {
				setPhase(arenaModeRef.current === "compare" ? "finished" : "voting");
			}
			return next;
		});
		abortMapRef.current.delete(model);
	}
}
