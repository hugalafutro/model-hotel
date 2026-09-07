import type { ChatMessage } from "../../api/types";
import type { ChatSubMode } from "../../context/SidebarModeContext";
import { usePersistedJSON } from "../../hooks/usePersistedJSON";

interface ChatPersistenceParams {
	messages: ChatMessage[];
	chatSubMode: ChatSubMode;
	persistChat: boolean;
	persistConversation: boolean;
}

/**
 * Mirrors the transcript into the key its mode owns. Each mode writes only its
 * own key, so a conversation is never saved as the chat history (and read back
 * as one on the next reload).
 */
export function useChatPersistence({
	messages,
	chatSubMode,
	persistChat,
	persistConversation,
}: ChatPersistenceParams) {
	const isChat = chatSubMode === "chat";
	usePersistedJSON(
		isChat ? "chatMessages" : "conversationMessages",
		messages,
		isChat ? persistChat : persistConversation,
		"hooks.useChatPersistence.storageFullChat",
	);
}
