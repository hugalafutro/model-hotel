import { useEffect, useRef } from "react";
import type { ChatMessage } from "../../api/types";
import type { ChatSubMode } from "../../context/SidebarModeContext";
import { useToast } from "../../context/ToastContext";

interface ChatPersistenceParams {
	messages: ChatMessage[];
	chatSubMode: ChatSubMode;
	persistChat: boolean;
	persistConversation: boolean;
}

export function useChatPersistence({
	messages,
	chatSubMode,
	persistChat,
	persistConversation,
}: ChatPersistenceParams) {
	const { toast } = useToast();
	const quotaWarnedRef = useRef(false);

	// ── Chat mode persistence effect ──
	useEffect(() => {
		if (!persistChat) return;
		try {
			localStorage.setItem("chatMessages", JSON.stringify(messages));
		} catch {
			/* quota exceeded */
			if (!quotaWarnedRef.current) {
				quotaWarnedRef.current = true;
				toast("Storage full - chat history not saved", "warning");
			}
		}
	}, [messages, persistChat, toast]);

	// ── Conversation messages persistence effect ──
	useEffect(() => {
		if (!persistConversation) return;
		if (chatSubMode !== "conversation") return;
		try {
			localStorage.setItem("conversationMessages", JSON.stringify(messages));
		} catch {
			/* quota exceeded */
			if (!quotaWarnedRef.current) {
				quotaWarnedRef.current = true;
				toast("Storage full - chat history not saved", "warning");
			}
		}
	}, [messages, persistConversation, chatSubMode, toast]);
}
