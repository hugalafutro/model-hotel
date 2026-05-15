import type { Dispatch, MutableRefObject, SetStateAction } from "react";
import { useCallback } from "react";
import type { ChatMessage, GenerationParams } from "../../api/types";
import { hasAnyParam } from "../../utils/params";
import {
	type ConversationState,
	getApiMessagesForModel,
	streamModelResponse,
} from "./chatStreaming";

interface UseConversationRunnerParams {
	selectedModel: string;
	selectedModelB: string;
	input: string;
	messages: ChatMessage[];
	currentTurn: number;
	maxTurns: number;
	turnDelayMs: number;
	systemPrompt: string;
	systemPromptB: string;
	messageParams: GenerationParams;
	messageParamsB: GenerationParams;
	conversationState: ConversationState;
	toast: (msg: string, type?: "success" | "error" | "info" | "warning") => void;
	// Refs
	conversationAbortRef: MutableRefObject<AbortController | null>;
	cleanupConvAbortRef: MutableRefObject<AbortController | null>;
	conversationRunningRef: MutableRefObject<boolean>;
	capturedModelARef: MutableRefObject<string>;
	capturedModelBRef: MutableRefObject<string>;
	lastPromptRef: MutableRefObject<string>;
	// State setters
	setMessages: Dispatch<SetStateAction<ChatMessage[]>>;
	setInput: (v: string) => void;
	setIsStreaming: (v: boolean) => void;
	setConversationState: (v: ConversationState) => void;
	setCurrentTurn: (v: number) => void;
	setTurnCountdown: (v: number) => void;
}

export function useConversationRunner(params: UseConversationRunnerParams) {
	const {
		selectedModel,
		selectedModelB,
		input,
		messages,
		currentTurn,
		maxTurns,
		turnDelayMs,
		systemPrompt,
		systemPromptB,
		messageParams,
		messageParamsB,
		conversationState,
		toast,
		conversationAbortRef,
		cleanupConvAbortRef,
		conversationRunningRef,
		capturedModelARef,
		capturedModelBRef,
		lastPromptRef,
		setMessages,
		setInput,
		setIsStreaming,
		setConversationState,
		setCurrentTurn,
		setTurnCountdown,
	} = params;

	const runConversation = useCallback(
		async (resume = false) => {
			if (conversationRunningRef.current) return;

			const canStart =
				selectedModel &&
				selectedModelB &&
				(resume || input.trim()) &&
				conversationState !== "running";

			if (!canStart) return;

			conversationRunningRef.current = true;

			const abortCtrl = new AbortController();
			conversationAbortRef.current = abortCtrl;
			cleanupConvAbortRef.current = abortCtrl;
			setConversationState("running");
			setIsStreaming(true);

			let currentMessages = messages;
			let turn = currentTurn;
			let modelTurn: "A" | "B";

			if (!resume) {
				capturedModelARef.current = selectedModel;
				capturedModelBRef.current = selectedModelB;
				setCurrentTurn(0);
				turn = 0;
				lastPromptRef.current = input.trim();
				const userMessage: ChatMessage = {
					role: "user",
					content: input.trim(),
					timestamp: Date.now(),
				};
				currentMessages = [...messages, userMessage];
				setMessages(currentMessages);
				setInput("");
				modelTurn = "A";
			} else {
				// Resume: figure out whose turn it is based on last assistant
				const lastAssistantIdx = currentMessages.findLastIndex(
					(m) => m.role === "assistant",
				);
				modelTurn =
					lastAssistantIdx >= 0 &&
					currentMessages[lastAssistantIdx].model === capturedModelARef.current
						? "B"
						: "A";
			}

			// maxTurns = number of conversation rounds; each round involves
			// 2 model responses (Model A then Model B), so the loop runs
			// maxTurns * 2 iterations total.
			while (turn < maxTurns * 2 && !abortCtrl.signal.aborted) {
				const isModelA = modelTurn === "A";
				const modelId = isModelA
					? capturedModelARef.current
					: capturedModelBRef.current;
				const persona = isModelA ? systemPrompt : systemPromptB;
				const params = isModelA ? messageParams : messageParamsB;

				const apiMessages = getApiMessagesForModel(
					currentMessages,
					modelId,
					persona,
				);

				const assistantMessage: ChatMessage = {
					role: "assistant",
					content: "",
					rawContent: "",
					thinkingContent: "",
					model: modelId,
					timestamp: Date.now(),
					params: hasAnyParam(params) ? params : undefined,
				};
				currentMessages = [...currentMessages, assistantMessage];
				setMessages(currentMessages);
				const msgTimestamp = assistantMessage.timestamp;

				const result = await streamModelResponse(
					modelId,
					apiMessages,
					params,
					abortCtrl,
					(raw, content, thinking) => {
						setMessages((prev) => {
							const idx = prev.findIndex(
								(m) => m.timestamp === msgTimestamp && m.role === "assistant",
							);
							if (idx === -1) return prev;
							const next = [...prev];
							next[idx] = {
								...next[idx],
								rawContent: raw,
								content,
								thinkingContent: thinking,
							};
							return next;
						});
					},
				);

				setMessages((prev) => {
					const idx = prev.findIndex(
						(m) => m.timestamp === msgTimestamp && m.role === "assistant",
					);
					if (idx === -1) return prev;
					const next = [...prev];
					next[idx] = {
						...next[idx],
						rawContent: result.rawContent,
						content: result.content,
						thinkingContent: result.thinkingContent,
						error: result.error,
						aborted: result.aborted || undefined,
						metrics: {
							tokensPerSecond: result.tokensPerSecond,
							durationMs: result.durationMs,
							promptTokens: result.promptTokens,
							completionTokens: result.completionTokens,
						},
					};
					return next;
				});

				currentMessages = currentMessages.map((m) =>
					m.timestamp === msgTimestamp && m.role === "assistant"
						? {
								...m,
								rawContent: result.rawContent,
								content: result.content,
								thinkingContent: result.thinkingContent,
								error: result.error,
								aborted: result.aborted || undefined,
								metrics: {
									tokensPerSecond: result.tokensPerSecond,
									durationMs: result.durationMs,
									promptTokens: result.promptTokens,
									completionTokens: result.completionTokens,
								},
							}
						: m,
				);

				if (result.error || result.aborted) {
					if (!result.aborted) {
						toast(`${modelId}: ${result.error}`, "error");
					}
					// Skip state cleanup if handleStopConversation already ran
					// (it sets conversationRunningRef to false synchronously)
					if (conversationRunningRef.current) {
						// User aborts pause the conversation; real errors transition to error state
						setConversationState(result.aborted ? "paused" : "error");
						setIsStreaming(false);
						setTurnCountdown(0);
						conversationAbortRef.current = null;
						cleanupConvAbortRef.current = null;
						conversationRunningRef.current = false;
					}
					if (turn === 0 && lastPromptRef.current) {
						setInput(lastPromptRef.current);
					}
					return;
				}

				turn++;
				modelTurn = modelTurn === "A" ? "B" : "A";
				setCurrentTurn(turn);

				// Same maxTurns * 2 semantics as the loop condition above.
				if (turn < maxTurns * 2 && !abortCtrl.signal.aborted) {
					const countdownSeconds = Math.ceil(turnDelayMs / 1000);
					setTurnCountdown(countdownSeconds);
					await new Promise<void>((resolve) => {
						let remaining = countdownSeconds;
						const interval = setInterval(() => {
							remaining--;
							if (remaining <= 0) {
								clearInterval(interval);
								setTurnCountdown(0);
								resolve();
							} else {
								setTurnCountdown(remaining);
							}
						}, 1000);
						// Resolve immediately on abort so the loop can exit cleanly
						abortCtrl.signal.addEventListener(
							"abort",
							() => {
								clearInterval(interval);
								setTurnCountdown(0);
								resolve();
							},
							{ once: true },
						);
					});
				}
			}

			setTurnCountdown(0);
			setIsStreaming(false);
			setConversationState("completed");
			conversationAbortRef.current = null;
			cleanupConvAbortRef.current = null;
			conversationRunningRef.current = false;
		},
		[
			selectedModel,
			selectedModelB,
			input,
			messages,
			currentTurn,
			maxTurns,
			turnDelayMs,
			systemPrompt,
			systemPromptB,
			messageParams,
			messageParamsB,
			toast,
			conversationState,
			capturedModelBRef,
			capturedModelARef,
			conversationAbortRef,
			cleanupConvAbortRef,
			lastPromptRef,
			setConversationState,
			conversationRunningRef,
			setTurnCountdown,
			setCurrentTurn,
			setMessages,
			setInput,
			setIsStreaming,
		],
	);

	const handleStopConversation = useCallback(() => {
		conversationAbortRef.current?.abort();
		conversationAbortRef.current = null;
		cleanupConvAbortRef.current = null;
		setTurnCountdown(0);
		setIsStreaming(false);
		setConversationState("paused");
		conversationRunningRef.current = false;
	}, [
		setConversationState,
		conversationRunningRef,
		setTurnCountdown,
		setIsStreaming,
		conversationAbortRef,
		cleanupConvAbortRef,
	]);

	/** Retry from error state: remove the failed assistant message and
	 *  re-run the conversation from the last successful turn.
	 *  If the first turn failed (currentTurn === 0), the user's prompt
	 *  has already been restored to `input` by the error handler. */
	const handleRetryConversation = useCallback(() => {
		if (conversationState !== "error") return;

		// Remove the last assistant message (the one that errored)
		const lastAssistantIdx = messages.findLastIndex(
			(m) => m.role === "assistant",
		);

		if (lastAssistantIdx >= 0) {
			setMessages((prev) => {
				const next = [...prev];
				next.splice(lastAssistantIdx, 1);
				return next;
			});
		}

		if (currentTurn === 0) {
			// First turn failed - the prompt is already restored in `input`.
			// Reset to idle so runConversation(false) runs as a fresh start.
			setConversationState("idle");
			setCurrentTurn(0);
			// Small delay to let state settle before re-triggering
			requestAnimationFrame(() => {
				runConversation(false);
			});
		} else {
			// Later turn failed - decrement turn counter to re-do the failed turn.
			// The prompt was not lost (it was never in `input` for later turns).
			const newTurn = currentTurn > 0 ? currentTurn - 1 : 0;
			setCurrentTurn(newTurn);
			setConversationState("paused");
			// Resume from the last successful turn
			requestAnimationFrame(() => {
				runConversation(true);
			});
		}
	}, [
		conversationState,
		messages,
		currentTurn,
		runConversation,
		setCurrentTurn,
		setConversationState,
		setMessages,
	]);

	const clearConversationAbort = useCallback(() => {
		conversationAbortRef.current?.abort();
		conversationAbortRef.current = null;
		cleanupConvAbortRef.current = null;
		conversationRunningRef.current = false;
	}, [conversationAbortRef, cleanupConvAbortRef, conversationRunningRef]);

	return {
		runConversation,
		handleStopConversation,
		handleRetryConversation,
		clearConversationAbort,
	};
}
