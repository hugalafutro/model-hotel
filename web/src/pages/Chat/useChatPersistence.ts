import { useMemo } from "react";
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
	// Text only: an attached image or audio clip is a base64 payload of the
	// file's size (the picker admits 20 MB; localStorage holds about 5 MB per
	// origin), so persisting it ended persistence for the whole session at the
	// first attachment and lost the transcript around it. The turn survives a
	// reload without its attachment.
	const persisted = useMemo(() => forPersistence(messages), [messages]);
	usePersistedJSON(
		isChat ? "chatMessages" : "conversationMessages",
		persisted,
		isChat ? persistChat : persistConversation,
		"hooks.useChatPersistence.storageFullChat",
	);
}

/** forPersistence is the transcript without its attachment payloads. */
export function forPersistence(messages: ChatMessage[]): ChatMessage[] {
	if (!messages.some((m) => m.imageUrl || m.audioAttachment)) return messages;
	return messages.map((m) => {
		if (!m.imageUrl && !m.audioAttachment) return m;
		const { imageUrl: _image, audioAttachment: _audio, ...rest } = m;
		return rest;
	});
}
