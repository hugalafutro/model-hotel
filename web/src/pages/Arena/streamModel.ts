import type { TFunction } from "i18next";
import { produce } from "immer";
import { API_BASE, getAuthHeaders } from "../../api/client";
import type { GenerationParams } from "../../api/types";
import { errorMessage } from "../../utils/errors";
import { tokensPerSecond } from "../../utils/format";
import { hasAnyParam } from "../../utils/params";
import { readSSEStream, type StreamChunk } from "../../utils/sse";
import { fetchWithRetry } from "../../utils/stagger";
import { extractThinking, sanitizeDelta } from "../../utils/thinking";
import type { ArenaRunnerDeps } from "./useArenaRunner";
import { patchSlotResponse, RESP_KEY } from "./utils";

/** What one arena stream needs from the runner hook: mount-gated setters plus the abort registry. */
export interface ArenaStreamContext
	extends Pick<ArenaRunnerDeps, "setRounds" | "toast"> {
	t: TFunction;
	/**
	 * Drops the model from the running set. `settle` flips the phase once the
	 * last model is done; an aborted stream leaves the phase to whoever
	 * cancelled it.
	 */
	finishModel: (model: string, settle?: boolean) => void;
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
	const { t, toast, setRounds, finishModel, abortMapRef, mountedRef } = ctx;
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
							const resp =
								draft[roundIdx]?.matchups[matchupIdx]?.[RESP_KEY[slotKey]];
							if (!resp) return;
							const rawContent = resp.rawContent + clean;
							const extracted = extractThinking(rawContent);
							patchSlotResponse(draft, roundIdx, matchupIdx, slotKey, {
								rawContent,
								content: extracted.content,
								thinkingContent: extracted.thinking || resp.thinkingContent,
							});
						}),
					);
				}
				const thinkingDelta =
					chunk.choices?.[0]?.delta?.reasoning_content ??
					chunk.choices?.[0]?.delta?.reasoning;
				if (thinkingDelta) {
					setRounds(
						produce((draft) => {
							const resp =
								draft[roundIdx]?.matchups[matchupIdx]?.[RESP_KEY[slotKey]];
							if (!resp) return;
							patchSlotResponse(draft, roundIdx, matchupIdx, slotKey, {
								thinkingContent: resp.thinkingContent + thinkingDelta,
							});
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

		const truncationError: string | null =
			!completion.sawDone && !completion.aborted
				? completion.idleTimeout
					? t("chat.stream.stalledTimeout")
					: t("chat.stream.cutoffIncomplete")
				: null;

		setRounds(
			produce((draft) => {
				patchSlotResponse(draft, roundIdx, matchupIdx, slotKey, {
					done: true,
					error: truncationError,
					metrics: {
						tokensPerSecond: tokensPerSecond(completionTokens, durationMs),
						durationMs: Math.round(durationMs),
						promptTokens,
						completionTokens,
					},
				});
			}),
		);
	} catch (err) {
		const msg = errorMessage(err, t("chat.stream.unknownError"));
		const errorDurationMs = Math.round(performance.now() - startTime);
		setRounds(
			produce((draft) => {
				patchSlotResponse(draft, roundIdx, matchupIdx, slotKey, {
					done: true,
					error: msg,
					metrics: {
						tokensPerSecond: tokensPerSecond(completionTokens, errorDurationMs),
						durationMs: errorDurationMs,
						promptTokens,
						completionTokens,
					},
				});
			}),
		);
		if (mountedRef.current) {
			toast(
				t("hooks.useArenaRunner.generationError", { model, error: msg }),
				"error",
			);
		}
	} finally {
		finishModel(model, !abortCtrl.signal.aborted);
		abortMapRef.current.delete(model);
	}
}
