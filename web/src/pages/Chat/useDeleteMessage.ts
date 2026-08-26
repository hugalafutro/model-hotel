import type { TFunction } from "i18next";
import {
	type Dispatch,
	type RefObject,
	type SetStateAction,
	useCallback,
} from "react";
import type { ChatMessage } from "../../api/types";
import type { useSidebarMode } from "../../context/SidebarModeContext";
import type { useToast } from "../../context/ToastContext";
import type { useChatConversationState } from "./useChatConversationState";

type ConversationState = ReturnType<typeof useChatConversationState>;

/**
 * Deleting a message. Chat mode drops the assistant reply and the user
 * message before it. Conversation mode only ever lets the most recent pair
 * go (or the message still being generated), and re-derives the
 * conversation state from what remains so the run can be continued.
 */
export function useDeleteMessage({
	messages,
	setMessages,
	chatSubMode,
	isStreaming,
	conversationState,
	setConversationState,
	setCurrentTurn,
	setInput,
	lastPromptRef,
	toast,
	t,
}: {
	messages: ChatMessage[];
	setMessages: Dispatch<SetStateAction<ChatMessage[]>>;
	chatSubMode: ReturnType<typeof useSidebarMode>["chatSubMode"];
	isStreaming: boolean;
	conversationState: ConversationState["conversationState"];
	setConversationState: ConversationState["setConversationState"];
	setCurrentTurn: ConversationState["setCurrentTurn"];
	setInput: (v: string) => void;
	lastPromptRef: RefObject<string>;
	toast: ReturnType<typeof useToast>["toast"];
	t: TFunction;
}) {
	return useCallback(
		(msgIndex: number) => {
			const msg = messages[msgIndex];
			if (!msg) return;

			const toRemove = new Set<number>();

			if (chatSubMode === "chat") {
				// In chat mode, delete the assistant and preceding user message
				toRemove.add(msgIndex);
				if (msgIndex > 0 && messages[msgIndex - 1].role === "user") {
					toRemove.add(msgIndex - 1);
				}
				setMessages(messages.filter((_, i) => !toRemove.has(i)));
				toast(t("hooks.useChat.messageDeleted"), "info");
				return;
			}

			// In conversation mode:
			// - If streaming, can only delete the last (currently generating) message
			// - If not streaming, can only delete the last pair
			const lastAssistantIdx = messages.findLastIndex(
				(m) => m.role === "assistant",
			);
			const isLastAssistant = msgIndex === lastAssistantIdx;
			const isStreamingLast = isStreaming && msgIndex === messages.length - 1;

			if (!isLastAssistant && !isStreamingLast) {
				// Can't delete - not the last message
				toast(t("hooks.useChat.canOnlyDeleteRecent"), "error");
				return;
			}

			// Delete this assistant message and the preceding message (either user or other assistant)
			toRemove.add(msgIndex);
			if (msgIndex > 0) {
				toRemove.add(msgIndex - 1);
			}

			// After deletion, determine the correct conversation state
			const remaining = messages.filter((_, i) => !toRemove.has(i));

			if (remaining.length === 0) {
				// Deleted everything - back to idle, restore the prompt
				setConversationState("idle");
				setCurrentTurn(0);
				if (lastPromptRef.current) {
					setInput(lastPromptRef.current);
				}
				setMessages([]);
				toast(t("hooks.useChat.messageDeleted"), "info");
				return;
			}

			if (remaining.length === 1 && remaining[0]?.role === "user") {
				// Only the initial user prompt remains - back to idle
				setConversationState("idle");
				setCurrentTurn(0);
				setInput(remaining[0].content);
				setMessages([]);
				toast(t("hooks.useChat.messageDeleted"), "info");
				return;
			}

			// There are earlier successful turns remaining
			if (conversationState === "error" || conversationState === "completed") {
				// Transition to "paused" so the user can continue
				setConversationState("paused");
				// Adjust turn counter: count remaining assistant messages
				const remainingAssistantCount = remaining.filter(
					(m) => m.role === "assistant",
				).length;
				setCurrentTurn(remainingAssistantCount);
			}

			setMessages(remaining);
			toast(t("hooks.useChat.messageDeleted"), "info");
		},
		[
			chatSubMode,
			t,
			toast,
			isStreaming,
			conversationState,
			messages,
			setCurrentTurn,
			setConversationState,
			setMessages,
			setInput,
			lastPromptRef,
		],
	);
}
